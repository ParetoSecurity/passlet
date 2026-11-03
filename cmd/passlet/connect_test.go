package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"passlet.dev/passlet/internal/passlet"
)

func endpointTestServer(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "pl-endpoint-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	socket := filepath.Join(dir, "socket")
	listener, err := passlet.ListenUnix(socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/health":
			_, _ = w.Write([]byte(`{"status":"ready"}`))
		case "POST /v1/doctor":
			_, _ = w.Write([]byte(`{"login":"alice"}`))
		case "POST /v1/grants":
			_ = json.NewEncoder(w).Encode(passlet.Grant{ID: "fixture", Token: "fixture-token", User: "alice", ExpiresAt: time.Now().Add(time.Hour), Repositories: []string{"alice/repo"}, Permissions: map[string]string{"contents": "read", "metadata": "read"}})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return "unix://" + socket
}

func TestConnectValidation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PASSLET_ENDPOINT", "")
	for _, args := range [][]string{nil, {"--unknown"}, {"unix:///tmp/a", "unix:///tmp/b"}, {"http://insecure.example"}, {"https://secret@mac.example"}, {"unix:relative"}, {""}} {
		if _, err := invokeCLI(t, "", append([]string{"connect"}, args...)...); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if _, err := invokeCLI(t, "", "connect", "--help"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".config", "passlet", "endpoint")); !os.IsNotExist(err) {
		t.Fatal("validation wrote configuration")
	}
}

func TestConnectCannotSave(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	endpoint := endpointTestServer(t)
	if err := os.WriteFile(filepath.Join(home, ".config"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	out, err := invokeCLI(t, "", "connect", endpoint)
	if err == nil || !strings.Contains(err.Error(), "could not save endpoint") || strings.Contains(out, "Connected to Conductor") {
		t.Fatalf("%q %v", out, err)
	}
}

func TestSavedEndpointUsedByCommands(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PASSLET_ENDPOINT", "")
	endpoint := endpointTestServer(t)
	if err := passlet.SaveEndpoint(endpoint); err != nil {
		t.Fatal(err)
	}
	out, err := invokeCLI(t, "", "doctor")
	if err != nil || !strings.Contains(out, "GitHub identity: alice") {
		t.Fatalf("saved doctor: %q %v", out, err)
	}
	grant := filepath.Join(t.TempDir(), "grant.json")
	if _, err := invokeCLI(t, "", "request", "--repos", "alice/repo", "--permissions", "contents:read", "--out", grant); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(os.Getenv("HOME"), ".config", "passlet", "endpoint")
	if err := os.WriteFile(config, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"doctor"}, {"request", "--repos", "alice/repo", "--permissions", "contents:read"}} {
		if _, err := invokeCLI(t, "", args...); err == nil || !strings.Contains(err.Error(), "invalid saved endpoint") {
			t.Fatalf("ignored bad config: %v", err)
		}
		args = append(args, "--endpoint", endpoint)
		if args[0] == "request" {
			args = append(args, "--out", filepath.Join(t.TempDir(), "grant.json"))
		}
		if _, err := invokeCLI(t, "", args...); err != nil {
			t.Fatalf("explicit override: %v", err)
		}
	}
	if _, err := invokeCLI(t, "", "exec", "--grant", grant, "--", "/bin/sh", "-c", "exit 0"); err != nil {
		t.Fatalf("saved grant should work without endpoint config: %v", err)
	}
	out, err = invokeCLI(t, "")
	if err != nil || !strings.Contains(out, "Request endpoint unavailable:") || strings.Contains(out, "Sending requests to:") {
		t.Fatalf("invalid config overview: %q %v", out, err)
	}
}
