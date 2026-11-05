package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"passlet.dev/passlet/internal/passlet"
)

func TestOverviewChecksTailscaleSocket(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "pl-tail-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("HOME", dir)
	path := tailscaleSocket(passlet.DefaultSocket())
	endpoint := "unix://" + path
	if err := overviewHealth(context.Background(), endpoint); err == nil {
		t.Fatal("missing socket reported listening")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := overviewHealth(context.Background(), endpoint); err != nil {
		t.Fatal(err)
	}
	if err := overviewHealth(context.Background(), "unix://"+passlet.DefaultSocket()); err == nil {
		t.Fatal("missing local socket reported healthy")
	}
}

func TestOverviewUnverifiedSockets(t *testing.T) {
	for _, tc := range []struct {
		config            string
		sshDown, tailDown bool
		want              string
	}{
		{`ssh = ["cruncher"]`, false, false, "✓ SSH socket (cruncher): listening"},
		{`tailscale = true`, false, false, "passlet connect https://mac.example.ts.net:17400"},
		{`ssh = ["cruncher"]`, true, false, "SSH socket (cruncher): unavailable"},
		{`tailscale = true`, false, true, "Tailscale: unavailable"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			dir := filepath.Join(os.Getenv("HOME"), ".config", "passlet")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "approver.toml"), []byte(tc.config), 0600); err != nil {
				t.Fatal(err)
			}
			f := overviewFixture{socket: passlet.DefaultSocket(), dns: "mac.example.ts.net", sshUnavailable: tc.sshDown, tailUnavailable: tc.tailDown}
			output := runOverviewFixture(t, &f)
			if !strings.Contains(output, tc.want) {
				t.Fatal(output)
			}
		})
	}
}

func TestListenerStatusUnavailable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	f := overviewFixture{unavailable: true}
	if output := runOverviewFixture(t, &f); !strings.Contains(output, "local Approver is not reachable") {
		t.Fatal(output)
	}
}

func TestListenerProblem(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{os.ErrNotExist, "Socket file is missing: /tmp/socket"},
		{syscall.ECONNREFUSED, "No service is listening on socket: /tmp/socket"},
		{os.ErrPermission, "Cannot access socket: /tmp/socket\nIts directory must be owned by you with mode 0700, and the socket with mode 0600."},
		{context.DeadlineExceeded, "Socket check timed out: /tmp/socket"},
		{errors.New("invalid health"), "Socket did not return a healthy Approver response: /tmp/socket"},
	} {
		t.Run(tc.err.Error(), func(t *testing.T) {
			if got := listenerProblem("/tmp/socket", fmt.Errorf("connect: %w", tc.err)); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
