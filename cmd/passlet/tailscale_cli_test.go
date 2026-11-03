package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"passlet.dev/passlet/internal/passlet"
)

func TestTailscaleCliPreservesUnrelatedRoutes(t *testing.T) {
	binary, err := exec.LookPath("tailscale")
	if err != nil {
		t.Skip("Tailscale CLI unavailable")
	}
	dir, err := os.MkdirTemp("/tmp", "pl-ts-api-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "api.sock")
	listener, err := passlet.ListenUnix(socket)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	revision := 0
	config := map[string]any{
		"TCP": map[string]any{"443": map[string]any{"HTTPS": true}},
		"Web": map[string]any{"mac.example.ts.net:443": map[string]any{"Handlers": map[string]any{"/": map[string]any{"Text": "keep me"}}}},
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/localapi/v0/status":
			_, _ = w.Write([]byte(`{"Version":"1.104.4","BackendState":"Running","Self":{"DNSName":"mac.example.ts.net.","TailscaleIPs":["100.100.0.1"],"CapMap":{"https":[]}},"CurrentTailnet":{"MagicDNSSuffix":"example.ts.net"}}`))
		case "/localapi/v0/serve-config":
			etag := fmt.Sprintf(`"%d"`, revision)
			if r.Method == "GET" {
				w.Header().Set("ETag", etag)
				_ = json.NewEncoder(w).Encode(config)
				return
			}
			if r.Method != "POST" {
				t.Errorf("unexpected method %s", r.Method)
				w.WriteHeader(405)
				return
			}
			if r.Header.Get("If-Match") != etag {
				w.WriteHeader(412)
				return
			}
			if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return
			}
			revision++
		default:
			t.Errorf("unexpected LocalAPI request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	})}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	command := func(ctx context.Context, _ string, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, binary, append([]string{"--socket=" + socket}, args...)...)
		output, err := cmd.Output()
		if err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				return nil, fmt.Errorf("CLI failed: %s", exit.Stderr)
			}
			return nil, err
		}
		return output, nil
	}
	for range 2 {
		if err := ensureTailscale(context.Background(), "/tmp/passlet.sock.tailscale", command); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if revision != 1 || !strings.Contains(string(data), "keep me") {
		t.Fatalf("unrelated route changed or matching route rewritten: %d %s", revision, data)
	}
	var result tailscaleServeConfig
	if err := json.Unmarshal(data, &result); err != nil || !result.forwardsTo("mac.example.ts.net", "/tmp/passlet.sock.tailscale") {
		t.Fatalf("unexpected configuration: %s %v", data, err)
	}
}
