package passlet

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func socketPath(t *testing.T) string {
	t.Helper()
	// t.TempDir can exceed Darwin's 104-byte sockaddr_un limit.
	dir, err := os.MkdirTemp("/tmp", "pl-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "s")
}

func TestSocketLifecycleAndPermissions(t *testing.T) {
	path := socketPath(t)
	l, err := ListenUnix(path)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatal("socket is not private")
	}
	if second, err := ListenUnix(path); err == nil {
		second.Close()
		t.Fatal("duplicate listener accepted")
	}
	l.Close()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatal("socket not cleaned up")
	}
	// Model a killed process: its lock is released but the socket survives.
	stale, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	stale.SetUnlinkOnClose(false)
	stale.Close()
	restarted, err := ListenUnix(path)
	if err != nil {
		t.Fatal("crash recovery failed:", err)
	}
	restarted.Close()
}

func TestSocketRejectsUnsafePaths(t *testing.T) {
	for _, variant := range []string{"file", "symlink", "shared_directory", "lock_symlink", "relative"} {
		t.Run(variant, func(t *testing.T) {
			path := socketPath(t)
			switch variant {
			case "file":
				os.WriteFile(path, []byte("keep me"), 0600)
			case "symlink":
				os.Symlink("missing", path)
			case "shared_directory":
				os.Chmod(filepath.Dir(path), 0755)
			case "lock_symlink":
				os.Symlink("missing", path+".lock")
			case "relative":
				path = "relative.sock"
			}
			if l, err := ListenUnix(path); err == nil {
				l.Close()
				t.Fatal("unsafe path accepted")
			}
			if variant == "file" {
				data, _ := os.ReadFile(path)
				if string(data) != "keep me" {
					t.Fatal("overwrote existing file")
				}
			}
		})
	}
}

func unixServer(t *testing.T, handler *Server) string {
	t.Helper()
	path := socketPath(t)
	l, err := ListenUnix(path)
	if err != nil {
		t.Fatal(err)
	}
	s := &http.Server{Handler: handler, ConnContext: ConnectionContext}
	done := make(chan struct{})
	go func() { defer close(done); s.Serve(l) }()
	t.Cleanup(func() { s.Close(); <-done })
	return "unix://" + path
}

func TestUnixGrantAndIdentity(t *testing.T) {
	audit := &memoryAudit{}
	s := NewServer(&testApprover{session: &testSession{}}, &testMinter{}, audit)
	endpoint := unixServer(t, s)
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	grant, err := RequestGrant(context.Background(), endpoint, Request{Repositories: []string{"zupo/test"}, Permissions: map[string]string{"contents": "read"}})
	if err != nil || grant.Token == "" {
		t.Fatal("Unix request failed:", err)
	}
	audit.mu.Lock()
	defer audit.mu.Unlock()
	for _, e := range audit.events {
		if e.Caller.Transport != "unix" || e.Caller.Name != "local/SSH socket client" || e.Caller.User != "" {
			t.Fatal("invented client identity", e.Caller)
		}
		data, _ := json.Marshal(e)
		if strings.Contains(string(data), grant.Token) {
			t.Fatal("audit leaked token")
		}
	}
}

func TestUnixDisconnectCancelsApproval(t *testing.T) {
	entered := make(chan struct{}, 1)
	audit := &memoryAudit{}
	s := NewServer(&testApprover{entered: entered, wait: true}, &testMinter{}, audit)
	endpoint := unixServer(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := RequestGrant(ctx, endpoint, Request{Repositories: []string{"zupo/test"}, Permissions: map[string]string{"contents": "read"}})
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("no approval")
	}
	cancel()
	if err := <-done; err == nil {
		t.Fatal("cancelled client succeeded")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		audit.mu.Lock()
		last := audit.events[len(audit.events)-1].Outcome
		audit.mu.Unlock()
		if last == "request_cancelled" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server approval survived disconnect")
}

func TestUnixNoFallbackAndDefault(t *testing.T) {
	t.Setenv("PASSLET_ENDPOINT", "")
	if DefaultEndpoint() != "unix://"+DefaultSocket() {
		t.Fatal("not local by default")
	}
	for _, endpoint := range []string{"unix://remote/tmp/x", "unix:relative", "unix:///tmp/../x", "unix:///tmp/x?secret=y", "unix:///"} {
		if _, _, err := EndpointClient(endpoint); err == nil {
			t.Fatal("bad Unix endpoint accepted", endpoint)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := RequestGrant(ctx, "unix://"+socketPath(t), Request{Repositories: []string{"zupo/test"}, Permissions: map[string]string{"contents": "read"}}); err == nil {
		t.Fatal("missing socket accepted")
	}
}
