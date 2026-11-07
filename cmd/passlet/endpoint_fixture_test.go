package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
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
	listener = pairedFixture(t, server, listener)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return "unix://" + socket
}
