package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"passlet.dev/passlet/internal/passlet"
)

func conductorTestPaths(t *testing.T) (string, string) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "passlet-conductor-")
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

func TestConductorRejectsUnsafeConfiguration(t *testing.T) {
	dir, helper := conductorTestPaths(t)
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
		{"relative helper", []string{"--helper", "relative"}, "conductor_requires_absolute_helper"},
		{"positional argument", []string{"--helper", helper, "unexpected"}, "conductor_requires_absolute_helper"},
		{"missing helper", []string{"--helper", filepath.Join(dir, "missing")}, "helper_unavailable"},
		{"directory helper", []string{"--helper", dir}, "helper_unavailable"},
		{"nonexecutable helper", []string{"--helper", plain}, "helper_unavailable"},
		{"removed listener flag", []string{"--tailscale-listen", "127.0.0.1:17401"}, "flag provided but not defined"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			socket := filepath.Join(dir, "socket")
			args := append([]string{"--socket", socket}, tc.args...)
			err := conductor(context.Background(), args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v; want %s", err, tc.want)
			}
			if _, err := os.Stat(socket); !os.IsNotExist(err) {
				t.Fatal("invalid configuration created a socket")
			}
		})
	}
}

func TestConductorReadsConfigurationBeforeBinding(t *testing.T) {
	dir, helper := conductorTestPaths(t)
	t.Setenv("HOME", dir)
	configDir := filepath.Join(dir, ".config", "passlet")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(configDir, "conductor.json")
	if err := os.WriteFile(config, []byte(`{"unexpected":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--helper", helper, "--socket", "relative"}
	if err := conductor(context.Background(), args); err == nil || !strings.Contains(err.Error(), "invalid_conductor_config") {
		t.Fatalf("invalid config: %v", err)
	}
	if err := os.Remove(config); err != nil {
		t.Fatal(err)
	}
	if err := conductor(context.Background(), args); err == nil || !strings.Contains(err.Error(), "socket_requires_absolute") {
		t.Fatalf("invalid socket: %v", err)
	}
}

func TestConductorStartupFailuresReleaseSocket(t *testing.T) {
	for _, mode := range []string{"audit unavailable", "Tailscale socket occupied", "Tailscale unavailable"} {
		t.Run(mode, func(t *testing.T) {
			dir, helper := conductorTestPaths(t)
			socket := filepath.Join(dir, "socket")
			audit := filepath.Join(dir, "audit.sqlite")
			enabled := false
			if mode == "audit unavailable" {
				audit = dir
			} else {
				enabled = true
				if mode == "Tailscale socket occupied" {
					l, err := passlet.ListenUnix(tailscaleSocket(socket))
					if err != nil {
						t.Fatal(err)
					}
					defer l.Close()
				} else {
					t.Setenv("PATH", t.TempDir())
				}
			}
			err := conductor(context.Background(), []string{"--helper", helper, "--socket", socket, "--audit", audit, "--tailscale=" + strconv.FormatBool(enabled)})
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

func TestConductorHealthAndCancellation(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run("Tailscale="+strconv.FormatBool(enabled), func(t *testing.T) {
			dir, helper := conductorTestPaths(t)
			socket := filepath.Join(dir, "socket")
			installTailscaleFixture(t, tailscaleSocket(socket))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				defer close(done)
				done <- conductor(ctx, []string{"--helper", helper, "--socket", socket, "--audit", filepath.Join(dir, "audit.sqlite"), "--tailscale=" + strconv.FormatBool(enabled)})
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
