package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"passlet.dev/passlet/internal/passlet"
)

func serveTestPaths(t *testing.T) (string, string) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "passlet-serve-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	helper := filepath.Join(dir, "helper")
	// Health checks must never invoke the approval helper.
	if err := os.WriteFile(helper, []byte("#!/bin/sh\ntouch '"+filepath.Join(dir, "invoked")+"'\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	return dir, helper
}

func TestServeRejectsUnsafeConfiguration(t *testing.T) {
	dir, helper := serveTestPaths(t)
	plain := filepath.Join(dir, "not-executable")
	if err := os.WriteFile(plain, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"unknown flag", []string{"--unknown"}, "flag provided but not defined"},
		{"relative helper", []string{"--helper", "relative"}, "serve_requires_absolute_helper"},
		{"positional argument", []string{"--helper", helper, "unexpected"}, "serve_requires_absolute_helper"},
		{"missing helper", []string{"--helper", filepath.Join(dir, "missing")}, "helper_unavailable"},
		{"directory helper", []string{"--helper", dir}, "helper_unavailable"},
		{"nonexecutable helper", []string{"--helper", plain}, "helper_unavailable"},
		{"public TCP", []string{"--helper", helper, "--tailscale-listen", "0.0.0.0:8484"}, "tailscale_listener_must_be_loopback"},
		{"hostname TCP", []string{"--helper", helper, "--tailscale-listen", "localhost:8484"}, "tailscale_listener_must_be_loopback"},
		{"malformed TCP", []string{"--helper", helper, "--tailscale-listen", "invalid"}, "tailscale_listener_must_be_loopback"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			socket := filepath.Join(dir, "socket")
			args := append([]string{"--socket", socket}, tc.args...)
			err := serve(context.Background(), args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v; want %s", err, tc.want)
			}
			if _, err := os.Stat(socket); !os.IsNotExist(err) {
				t.Fatal("invalid configuration created a socket")
			}
		})
	}
}

func TestServeReadsConfigurationBeforeBinding(t *testing.T) {
	dir, helper := serveTestPaths(t)
	t.Setenv("HOME", dir)
	configDir := filepath.Join(dir, ".config", "passlet")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(configDir, "serve.json")
	if err := os.WriteFile(config, []byte(`{"unexpected":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--helper", helper, "--socket", "relative"}
	if err := serve(context.Background(), args); err == nil || !strings.Contains(err.Error(), "invalid_serve_config") {
		t.Fatalf("invalid config: %v", err)
	}
	if err := os.Remove(config); err != nil {
		t.Fatal(err)
	}
	if err := serve(context.Background(), args); err == nil || !strings.Contains(err.Error(), "socket_requires_absolute") {
		t.Fatalf("invalid socket: %v", err)
	}
}

func TestServeStartupFailuresReleaseSocket(t *testing.T) {
	for _, mode := range []string{"audit unavailable", "TCP occupied"} {
		t.Run(mode, func(t *testing.T) {
			dir, helper := serveTestPaths(t)
			socket := filepath.Join(dir, "socket")
			audit := filepath.Join(dir, "audit.sqlite")
			tcp := ""
			if mode == "audit unavailable" {
				audit = dir
			} else {
				l, err := net.Listen("tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer l.Close()
				tcp = l.Addr().String()
			}
			err := serve(context.Background(), []string{"--helper", helper, "--socket", socket, "--audit", audit, "--tailscale-listen", tcp})
			if err == nil {
				t.Fatal("startup unexpectedly succeeded")
			}
			if mode == "audit unavailable" && err.Error() != "audit_unavailable" {
				t.Fatal(err)
			}
			l, err := passlet.ListenUnix(socket)
			if err != nil {
				t.Fatalf("failed startup retained socket or lock: %v", err)
			}
			l.Close()
		})
	}
}

func TestServeHealthAndCancellation(t *testing.T) {
	for _, tcp := range []string{"", "127.0.0.1:0"} {
		t.Run("TCP="+tcp, func(t *testing.T) {
			dir, helper := serveTestPaths(t)
			socket := filepath.Join(dir, "socket")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				defer close(done)
				done <- serve(ctx, []string{"--helper", helper, "--socket", socket, "--audit", filepath.Join(dir, "audit.sqlite"), "--tailscale-listen", tcp})
			}()
			t.Cleanup(func() {
				cancel()
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("server did not stop")
				}
			})
			deadline := time.Now().Add(5 * time.Second)
			for {
				healthCtx, stop := context.WithTimeout(context.Background(), 100*time.Millisecond)
				err := passlet.Health(healthCtx, "unix://"+socket)
				stop()
				if err == nil {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("server never became healthy: %v", err)
				}
				time.Sleep(10 * time.Millisecond)
			}
			if _, err := os.Stat(filepath.Join(dir, "invoked")); !os.IsNotExist(err) {
				t.Fatal("health invoked approval helper")
			}
			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("shutdown timed out")
			}
			l, err := passlet.ListenUnix(socket)
			if err != nil {
				t.Fatalf("shutdown retained socket or lock: %v", err)
			}
			l.Close()
		})
	}
}
