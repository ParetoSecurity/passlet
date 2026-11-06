package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"passlet.dev/passlet/internal/passlet"
)

func TestOverviewChecksTailscaleLoopback(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "pl-tail-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("HOME", dir)
	endpoint := tailscaleTarget
	if err := overviewHealth(context.Background(), endpoint); err == nil {
		t.Fatal("missing socket reported listening")
	}
	listener, err := net.Listen("tcp4", tailscaleBackend)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, `{"status":"ready"}`) })}
	listener = pairedFixture(t, server, listener)
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	if err := overviewHealth(context.Background(), endpoint); err != nil {
		t.Fatal(err)
	}
	if err := overviewHealth(context.Background(), "unix://"+passlet.DefaultSocket()); err == nil {
		t.Fatal("missing local socket reported healthy")
	}
}

func TestOverviewUnverifiedSockets(t *testing.T) {
	for _, tc := range []struct {
		config   string
		tailDown bool
		want     string
	}{
		{`tailscale = true`, false, "passlet connect https://mac.example.ts.net:17400"},
		{`tailscale = true`, true, "Tailscale: unavailable"},
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
			f := overviewFixture{socket: passlet.DefaultSocket(), dns: "mac.example.ts.net", tailUnavailable: tc.tailDown}
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
