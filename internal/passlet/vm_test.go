package passlet

// This entire fixture is compiled only by `go test -c` for NixOS VM tests.
// No environment variable can enable it in the production CLI or Swift helper.
import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/cgi"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type vmApproval struct{ user, modeFile string }

func (a vmApproval) Approve(ctx context.Context, _ string) (CredentialSession, error) {
	mode, _ := os.ReadFile(a.modeFile)
	switch strings.TrimSpace(string(mode)) {
	case "deny":
		return nil, errors.New("approval_denied")
	case "wait":
		<-ctx.Done()
		return nil, errors.New("approval_cancelled")
	}
	return &testSession{c: Credentials{ClientID: "fixture", ClientSecret: "fixture-client-secret", AccessToken: "parent-" + a.user, RefreshToken: "fixture-refresh", ExpiresAt: time.Now().Add(time.Hour)}}, nil
}

func TestVMFixture(t *testing.T) {
	role := os.Getenv("PASSLET_VM_ROLE")
	if role == "" {
		t.Skip("NixOS VM fixture only")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	defer cancel()
	if role == "github" {
		runVMGitHub(t, ctx)
		return
	}
	if role != "alice" && role != "bob" {
		t.Fatal("invalid fixture role")
	}
	path := filepath.Join(StateDir(), "passlet.sock")
	l, err := ListenUnix(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	a, err := OpenAudit(filepath.Join(StateDir(), "audit.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	g := NewGitHub()
	g.API = "https://api.github.com"
	s := NewServer(vmApproval{user: role, modeFile: filepath.Join(StateDir(), "mode")}, g, a)
	// Short deadline for deterministic cancellation tests; only this test
	// binary changes it. The production server retains 120 seconds.
	s.Deadline = 15 * time.Second
	h := &http.Server{Handler: s, ConnContext: ConnectionContext, BaseContext: func(net.Listener) context.Context { return ctx }}
	go func() { <-ctx.Done(); h.Close() }()
	if err := h.Serve(l); err != nil && err != http.ErrServerClosed {
		t.Fatal(err)
	}
}

func runVMGitHub(t *testing.T, ctx context.Context) {
	var mu sync.Mutex
	grants := map[string]Grant{}
	backend := &cgi.Handler{Path: os.Getenv("PASSLET_TEST_GIT_BACKEND"), Env: []string{"GIT_PROJECT_ROOT=/var/lib/test-github", "GIT_HTTP_EXPORT_ALL=1", "REMOTE_USER=fixture"}}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if strings.HasPrefix(r.Header.Get("Authorization"), "token ") {
			bearer = strings.TrimPrefix(r.Header.Get("Authorization"), "token ")
		}
		user := strings.TrimPrefix(bearer, "parent-")
		parent := strings.HasPrefix(bearer, "parent-") && (user == "alice" || user == "bob")
		writeJSON := func(value any) { w.Header().Set("Content-Type", "application/json"); json.NewEncoder(w).Encode(value) }
		if r.URL.Path == "/__expire" {
			mu.Lock()
			for token, g := range grants {
				g.ExpiresAt = time.Now().Add(-time.Hour)
				grants[token] = g
			}
			mu.Unlock()
			w.WriteHeader(204)
			return
		}
		if r.URL.Path == "/applications/fixture/token/scoped" {
			id, secret, ok := r.BasicAuth()
			var request struct {
				Access      string            `json:"access_token"`
				Target      string            `json:"target"`
				IDs         []int64           `json:"repository_ids"`
				Permissions map[string]string `json:"permissions"`
			}
			if !ok || id != "fixture" || secret != "fixture-client-secret" || json.NewDecoder(r.Body).Decode(&request) != nil || request.Target != "acme" {
				http.Error(w, "forbidden", 403)
				return
			}
			user := strings.TrimPrefix(request.Access, "parent-")
			if user != "alice" && user != "bob" {
				http.Error(w, "forbidden", 403)
				return
			}
			repos := []string{}
			for _, id := range request.IDs {
				if id == 1 {
					repos = append(repos, "acme/test-a")
				} else if id == 2 {
					repos = append(repos, "acme/test-b")
				} else {
					http.Error(w, "bad repo", 422)
					return
				}
			}
			token, _ := randomURLToken()
			expiry := time.Now().Add(time.Hour)
			mu.Lock()
			grants[token] = Grant{Token: token, User: user, ExpiresAt: expiry, Repositories: repos, Permissions: request.Permissions}
			mu.Unlock()
			writeJSON(map[string]any{"token": token, "expires_at": expiry, "user": map[string]string{"login": user}, "installation": map[string]any{"repository_selection": "selected", "permissions": request.Permissions, "account": map[string]string{"login": "acme"}}})
			return
		}
		if parent {
			switch r.URL.Path {
			case "/user":
				writeJSON(map[string]string{"login": user})
				return
			case "/user/installations":
				writeJSON(map[string]any{"installations": []any{map[string]any{"id": 1, "account": map[string]string{"login": "acme"}}}})
				return
			case "/user/installations/1/repositories":
				writeJSON(map[string]any{"repositories": []any{map[string]any{"id": 1, "full_name": "acme/test-a"}, map[string]any{"id": 2, "full_name": "acme/test-b"}}})
				return
			}
		}
		_, password, basic := r.BasicAuth()
		if basic {
			bearer = password
		}
		mu.Lock()
		grant, ok := grants[bearer]
		mu.Unlock()
		if !ok || !grant.ExpiresAt.After(time.Now()) {
			w.Header().Set("WWW-Authenticate", `Basic realm="fixture"`)
			http.Error(w, "authentication required", 401)
			return
		}
		repo := ""
		for _, name := range grant.Repositories {
			if r.URL.Path == "/repos/"+name || strings.HasPrefix(r.URL.Path, "/"+name+".git/") {
				repo = name
			}
		}
		if repo == "" {
			http.Error(w, "repository outside grant", 403)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/repos/") {
			writeJSON(map[string]any{"full_name": repo, "actor": grant.User})
			return
		}
		write := strings.Contains(r.URL.Path, "git-receive-pack") || r.URL.Query().Get("service") == "git-receive-pack"
		if write && grant.Permissions["contents"] != "write" {
			http.Error(w, "write outside grant", 403)
			return
		}
		if grant.Permissions["contents"] == "" {
			http.Error(w, "contents outside grant", 403)
			return
		}
		backend.ServeHTTP(w, r)
	})
	port := 443
	if value := os.Getenv("PASSLET_TEST_PORT"); value != "" {
		port, _ = strconv.Atoi(value)
	}
	s := &http.Server{Addr: ":" + strconv.Itoa(port), Handler: handler}
	go func() { <-ctx.Done(); s.Close() }()
	if err := s.ListenAndServeTLS(os.Getenv("PASSLET_TEST_CERT"), os.Getenv("PASSLET_TEST_KEY")); err != nil && err != http.ErrServerClosed {
		t.Fatal(err)
	}
}
