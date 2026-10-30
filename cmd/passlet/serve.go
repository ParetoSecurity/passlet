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
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(self), "PassletHelper")
}

func serve(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	socket := fs.String("socket", passlet.DefaultSocket(), "private Unix socket path")
	tcp := fs.String("tailscale-listen", "", "opt-in loopback TCP address; requires Tailscale Serve PROXY v2")
	helper := fs.String("helper", defaultHelper(), "absolute signed helper path")
	audit := fs.String("audit", filepath.Join(passlet.StateDir(), "audit.sqlite"), "SQLite audit path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || !filepath.IsAbs(*helper) {
		return errors.New("serve_requires_absolute_helper_and_no_positional_arguments")
	}
	if info, err := os.Stat(*helper); err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return errors.New("helper_unavailable_install_signed_Passlet_app_or_set_helper")
	}
	if *tcp != "" {
		host, _, err := net.SplitHostPort(*tcp)
		if err != nil || !net.ParseIP(host).IsLoopback() {
			return errors.New("tailscale_listener_must_be_loopback")
		}
	}
	l, err := passlet.ListenUnix(*socket)
	if err != nil {
		return err
	}
	defer l.Close()
	a, err := passlet.OpenAudit(*audit)
	if err != nil {
		return errors.New("audit_unavailable")
	}
	defer a.Close()
	listeners := []net.Listener{l}
	if *tcp != "" {
		t, err := net.Listen("tcp", *tcp)
		if err != nil {
			return err
		}
		defer t.Close()
		listeners = append(listeners, &passlet.ProxyListener{Listener: t})
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
	if *tcp != "" {
		fmt.Fprintln(os.Stderr, "Tailscale PROXY v2 listener:", *tcp)
	}
	select {
	case err = <-results:
	case <-ctx.Done():
	}
	cancel()
	server.Close()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
