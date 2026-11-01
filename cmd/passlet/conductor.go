package main

import (
	"context"
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

func conductor(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("conductor", flag.ContinueOnError)
	socket := fs.String("socket", passlet.DefaultSocket(), "private Unix socket path")
	tailscale := fs.Bool("tailscale", false, "expose the Conductor through Tailscale Serve on port 17400")
	helper := fs.String("helper", defaultHelper(), "absolute signed helper path")
	audit := fs.String("audit", filepath.Join(passlet.StateDir(), "audit.sqlite"), "SQLite audit path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || !filepath.IsAbs(*helper) {
		return errors.New("conductor_requires_absolute_helper_and_no_positional_arguments")
	}
	if info, err := os.Stat(*helper); err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return errors.New("helper_unavailable_reinstall_Passlet_through_Homebrew_or_Home_Manager")
	}
	explicitTailscale := false
	fs.Visit(func(f *flag.Flag) { explicitTailscale = explicitTailscale || f.Name == "tailscale" })
	if !explicitTailscale {
		configured, err := configuredTailscale()
		if err != nil {
			return err
		}
		*tailscale = configured
	}
	l, err := passlet.ListenUnix(*socket)
	if err != nil {
		return err
	}
	defer func() { _ = l.Close() }()
	a, err := passlet.OpenAudit(*audit)
	if err != nil {
		return errors.New("audit_unavailable")
	}
	defer func() { _ = a.Close() }()
	listeners := []net.Listener{l}
	if *tailscale {
		t, err := passlet.ListenUnix(tailscaleSocket(*socket))
		if err != nil {
			return err
		}
		defer func() { _ = t.Close() }()
		if err := ensureTailscale(ctx, tailscaleSocket(*socket), overviewOutput); err != nil {
			return err
		}
		listeners = append(listeners, &passlet.TailscaleListener{Listener: t})
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	handler := passlet.NewServer(passlet.NativeHelper{Path: *helper}, passlet.NewGitHub(), a)
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 125 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 8192, BaseContext: func(net.Listener) context.Context { return ctx }, ConnContext: passlet.ConnectionContext}
	results := make(chan error, len(listeners))
	for _, listener := range listeners {
		go func() { results <- server.Serve(listener) }()
	}
	fmt.Fprintln(os.Stderr, "Passlet listening on", *socket)
	if *tailscale {
		fmt.Fprintln(os.Stderr, "Tailscale Serve socket:", tailscaleSocket(*socket))
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
