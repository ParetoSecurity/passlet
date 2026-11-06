package main

import (
	"crypto/tls"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"passlet.dev/passlet/internal/passlet"
	"passlet.dev/passlet/testutil/testkeys"
)

func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "passlet-tests-")
	if err != nil {
		panic(err)
	}
	// Go tooling invoked by fixture subprocesses must not write into per-scenario homes.
	telemetry := filepath.Join(home, "go-telemetry")
	if err := os.Mkdir(telemetry, 0700); err != nil {
		panic(err)
	}
	if err := os.WriteFile(filepath.Join(telemetry, "mode"), []byte("off"), 0600); err != nil {
		panic(err)
	}
	os.Setenv("TEST_TELEMETRY_DIR", telemetry)
	os.Setenv("HOME", home)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

func pairedFixture(t *testing.T, server *http.Server, listener net.Listener) net.Listener {
	t.Helper()
	testkeys.Write(t, os.Getenv("HOME"))
	p, err := passlet.NewPairing(testkeys.Private("approver"), map[string]string{"test-agent": testkeys.Public("agent")})
	if err != nil {
		t.Fatal(err)
	}
	server.Handler = p.Handler(server.Handler)
	return tls.NewListener(listener, p.ServerConfig())
}
