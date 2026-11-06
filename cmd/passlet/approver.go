package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"passlet.dev/passlet/internal/passlet"
)

func defaultHelper() string {
	self, err := os.Executable()
	if err != nil { // coverage-ignore: Requires OS failure to locate the running executable.
		return ""
	}
	self, err = filepath.EvalSymlinks(self)
	if err != nil { // coverage-ignore: Requires external removal of the running executable during path resolution.
		return ""
	}
	return filepath.Join(filepath.Dir(self), "PassletHelper")
}

const approverHelp = `An approval service you run on your Mac.

Agents request credentials from it, you approve them with Touch ID.

Usage: passlet approver <command>

Commands:
  audit         Show audit history
  doctor        Check service health and credentials
  github        Manage GitHub credentials
  start         Run the Approver service

Run ` + "`passlet approver <command> --help`" + ` for command options.
`

func approverCommand(ctx context.Context, args []string, showOverview func(context.Context) error) error {
	if len(args) == 0 {
		fmt.Print(approverHelp)
		return nil
	}
	switch args[0] {
	case "--help", "-h", "help":
		fmt.Print(approverHelp)
		return nil
	case "start":
		return approver(ctx, args[1:])
	case "doctor":
		if err := localDiagnostics(ctx, args[1:]); err != nil {
			return err
		}
		return showOverview(ctx)
	case "github":
		return githubOverview(ctx, args[1:])
	case "audit":
		return auditCommand(args[1:])
	default:
		return errors.New("unknown approver command")
	}
}

func approver(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("approver start", flag.ContinueOnError)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(fs.Output(), "Run Passlet Approver service on your Mac to receive access requests and ask for your approval. Homebrew and Home Manager normally start it automatically.")
	}
	socket := passlet.DefaultSocket()
	helper := fs.String("helper", defaultHelper(), "absolute signed helper path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || !filepath.IsAbs(*helper) {
		return errors.New("approver_requires_absolute_helper_and_no_positional_arguments")
	}
	if info, err := os.Stat(*helper); err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return errors.New("helper_unavailable_reinstall_Passlet_through_Homebrew_or_Home_Manager")
	}
	config, err := configuredApprover()
	if err != nil {
		return err
	}
	pairing, err := config.pairing()
	if err != nil {
		return err
	}
	l, err := passlet.ListenUnix(socket)
	if err != nil {
		return err
	}
	defer func() { _ = l.Close() }()
	a, err := passlet.OpenAudit(filepath.Join(passlet.StateDir(), "audit.sqlite"))
	if err != nil {
		return errors.New("audit_unavailable")
	}
	defer func() { _ = a.Close() }()
	listeners := []net.Listener{l}
	if config.Tailscale {
		t, err := net.Listen("tcp4", tailscaleBackend)
		if err != nil {
			return err
		}
		defer func() { _ = t.Close() }()
		if err := ensureTailscale(ctx, tailscaleTarget, overviewOutput); err != nil {
			return err
		}
		listeners = append(listeners, t)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	handler := passlet.NewServer(passlet.NativeHelper{Path: *helper}, passlet.NewGitHub(), a)
	server := &http.Server{Handler: pairing.Handler(handler), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 125 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 8192, BaseContext: func(net.Listener) context.Context { return ctx }}
	results := make(chan error, len(listeners))
	for _, listener := range listeners {
		go func() { results <- server.Serve(tls.NewListener(listener, pairing.ServerConfig())) }()
	}
	fmt.Fprintln(os.Stderr, "Passlet listening on", socket)
	if config.Tailscale {
		fmt.Fprintln(os.Stderr, "Tailscale Serve backend:", tailscaleTarget)
	}
	select {
	case err = <-results: // coverage-ignore: Listeners are owned here; unexpected accept failure requires an OS fault.
	case <-ctx.Done():
	}
	cancel()
	_ = server.Close()
	if errors.Is(err, http.ErrServerClosed) { // coverage-ignore: Defensive shutdown normalization; normal cancellation selects ctx.Done.
		return nil
	}
	return err
}

const githubHelp = `Manage GitHub credentials on your Approver Mac.

Usage: passlet approver github <command>

Commands:
  reset         Remove saved GitHub credentials
  setup         Configure GitHub access

Run ` + "`passlet approver github <command> --help`" + ` for command options.
`

func githubOverview(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		fmt.Print(githubHelp)
		return nil
	}
	return githubCommand(ctx, args)
}
