package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"passlet.dev/passlet/testutil/testkeys"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"passlet.dev/passlet/internal/passlet"
)

func approverTestPaths(t *testing.T) (string, string) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "passlet-approver-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("HOME", dir)
	helper := filepath.Join(dir, "helper")
	// Health checks must never invoke the approval helper.
	if err := os.WriteFile(helper, []byte("#!/bin/sh\ntouch '"+filepath.Join(dir, "invoked")+"'\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	writePairingConfig(t, dir, false)
	return dir, helper
}

func TestApproverRejectsUnsafeConfiguration(t *testing.T) {
	dir, helper := approverTestPaths(t)
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
		{"relative helper", []string{"--helper", "relative"}, "approver_requires_absolute_helper"},
		{"positional argument", []string{"--helper", helper, "unexpected"}, "approver_requires_absolute_helper"},
		{"missing helper", []string{"--helper", filepath.Join(dir, "missing")}, "helper_unavailable"},
		{"directory helper", []string{"--helper", dir}, "helper_unavailable"},
		{"nonexecutable helper", []string{"--helper", plain}, "helper_unavailable"},
		{"removed listener flag", []string{"--tailscale-listen", "127.0.0.1:17401"}, "flag provided but not defined"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			socket := passlet.DefaultSocket()
			args := tc.args
			err := approver(context.Background(), args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v; want %s", err, tc.want)
			}
			if _, err := os.Stat(socket); !os.IsNotExist(err) {
				t.Fatal("invalid configuration created a socket")
			}
		})
	}
}

func TestApproverReadsConfigurationBeforeBinding(t *testing.T) {
	dir, helper := approverTestPaths(t)
	t.Setenv("HOME", dir)
	configDir := filepath.Join(dir, ".config", "passlet")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(configDir, "approver.toml")
	if err := os.WriteFile(config, []byte(`unexpected = true`), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"--helper", helper}
	if err := approver(context.Background(), args); err == nil || !strings.Contains(err.Error(), "invalid_approver_config") {
		t.Fatalf("invalid config: %v", err)
	}
	if _, err := os.Stat(passlet.DefaultSocket()); !os.IsNotExist(err) {
		t.Fatal("invalid config created a socket")
	}
}

func TestApproverStartupFailuresReleaseSocket(t *testing.T) {
	for _, mode := range []string{"socket occupied", "audit unavailable", "Tailscale port occupied", "Tailscale unavailable"} {
		t.Run(mode, func(t *testing.T) {
			dir, helper := approverTestPaths(t)
			socket := passlet.DefaultSocket()
			enabled := false
			switch mode {
			case "socket occupied":
				if err := os.MkdirAll(passlet.DefaultSocket(), 0700); err != nil {
					t.Fatal(err)
				}
			case "audit unavailable":
				if err := os.MkdirAll(filepath.Join(passlet.StateDir(), "audit.sqlite"), 0700); err != nil {
					t.Fatal(err)
				}
			default:
				enabled = true
				if mode == "Tailscale port occupied" {
					l, err := net.Listen("tcp4", tailscaleBackend)
					if err != nil {
						t.Fatal(err)
					}
					defer l.Close()
				} else {
					t.Setenv("PATH", t.TempDir())
				}
			}
			writePairingConfig(t, dir, enabled)
			err := approver(context.Background(), []string{"--helper", helper})
			if err == nil {
				t.Fatal("startup unexpectedly succeeded")
			}
			if mode == "audit unavailable" && err.Error() != "audit_unavailable" {
				t.Fatal(err)
			}
			if mode == "socket occupied" {
				if err := os.Remove(socket); err != nil {
					t.Fatal(err)
				}
			}
			l, err := passlet.ListenUnix(socket)
			if err != nil {
				t.Fatalf("failed startup retained socket or lock: %v", err)
			}
			l.Close()

		})
	}
}

func TestApproverHealthAndCancellation(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run("Tailscale="+strconv.FormatBool(enabled), func(t *testing.T) {
			dir, helper := approverTestPaths(t)
			socket := passlet.DefaultSocket()
			writePairingConfig(t, dir, enabled)
			installTailscaleFixture(t, tailscaleTarget)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				defer close(done)
				done <- approver(ctx, []string{"--helper", helper})
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
			for _, path := range []string{socket} {
				l, err := passlet.ListenUnix(path)
				if err != nil {
					t.Fatalf("shutdown retained socket or lock: %v", err)
				}
				l.Close()
			}
		})
	}
}

func writePairingConfig(t *testing.T, home string, tailscale bool) {
	t.Helper()
	testkeys.Write(t, home)
	os.MkdirAll(filepath.Join(home, ".config", "passlet"), 0700)
	data := fmt.Sprintf("tailscale = %t\n", tailscale)
	if err := os.WriteFile(filepath.Join(home, ".config", "passlet", "approver.toml"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestApproverRejectsInvalidPairingKeyBeforeBinding(t *testing.T) {
	dir, helper := approverTestPaths(t)
	if err := os.WriteFile(filepath.Join(dir, ".local", "share", "passlet", "approver.pem"), []byte("invalid existing key"), 0600); err != nil {
		t.Fatal(err)
	}
	err := approver(context.Background(), []string{"--helper", helper})
	if err == nil || err.Error() != "invalid_agent_private_key" {
		t.Fatalf("unexpected startup result: %v", err)
	}
	if _, err := os.Stat(passlet.DefaultSocket()); !os.IsNotExist(err) {
		t.Fatal("unpaired service created a listener")
	}
	if got := doctorFailureMessage(err); !strings.Contains(got, "PKCS#8 PEM") {
		t.Fatal(got)
	}
}

func TestApproverGeneratesDefaultPairingKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := passlet.LoadOrCreatePairingKey(filepath.Join(passlet.StateDir(), "approver.pem"), filepath.Join(passlet.StateDir(), "approver.pub")); err != nil {
		t.Fatal(err)
	}
	if _, err := passlet.ReadPrivateKey(filepath.Join(passlet.StateDir(), "approver.pem")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(passlet.StateDir(), "approver.pub"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := passlet.ParsePublicKey(string(data)); err != nil {
		t.Fatal(err)
	}
}
