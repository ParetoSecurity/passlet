package passlet

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMintChecksRepositoriesAndReturnedPermissions(t *testing.T) {
	for _, mode := range []string{"ok", "not-installed", "broadened", "no-expiry", "wrong-user", "parent-token"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/user":
					json.NewEncoder(w).Encode(map[string]any{"login": "zupo"})
				case "/user/installations":
					json.NewEncoder(w).Encode(map[string]any{"installations": []any{map[string]any{"id": 42, "account": map[string]string{"login": "zupo"}}}})
				case "/user/installations/42/repositories":
					name := "zupo/test-a"
					if mode == "not-installed" {
						name = "zupo/other"
					}
					json.NewEncoder(w).Encode(map[string]any{"repositories": []any{map[string]any{"id": 123, "full_name": name}}})
				case "/applications/client/token/scoped":
					calls++
					u, p, ok := r.BasicAuth()
					if !ok || u != "client" || p != "client-secret" {
						t.Error("wrong auth")
					}
					var body struct {
						IDs    []int64 `json:"repository_ids"`
						Access string  `json:"access_token"`
					}
					json.NewDecoder(r.Body).Decode(&body)
					if len(body.IDs) != 1 || body.IDs[0] != 123 || body.Access != "parent-secret" {
						t.Error("request not ID-scoped")
					}
					perms := map[string]string{"contents": "read", "metadata": "read"}
					if mode == "broadened" {
						perms["issues"] = "write"
					}
					expiry := time.Now().Add(8 * time.Hour)
					if mode == "no-expiry" {
						expiry = time.Time{}
					}
					user := "zupo"
					if mode == "wrong-user" {
						user = "bot"
					}
					token := "scoped-secret"
					if mode == "parent-token" {
						token = "parent-secret"
					}
					json.NewEncoder(w).Encode(map[string]any{"token": token, "expires_at": expiry, "user": map[string]string{"login": user}, "installation": map[string]any{"repository_selection": "selected", "permissions": perms, "account": map[string]string{"login": "zupo"}}})
				default:
					t.Error("unexpected path", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer h.Close()
			g := NewGitHub()
			g.API = h.URL
			req := Request{Repositories: []string{"zupo/test-a"}, Permissions: map[string]string{"contents": "read"}}
			req.Validate()
			_, err := g.Mint(context.Background(), Credentials{ClientID: "client", ClientSecret: "client-secret", AccessToken: "parent-secret"}, req)
			if (mode == "ok") != (err == nil) {
				t.Fatalf("mode %s error %v", mode, err)
			}
			if mode == "not-installed" && calls != 0 {
				t.Fatal("minted without installation membership")
			}
		})
	}
}
func TestRefreshAndProviderErrorsDoNotLeak(t *testing.T) {
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		w.Write([]byte(`{"message":"secret-parent-token"}`))
	}))
	defer h.Close()
	g := NewGitHub()
	g.OAuth = h.URL
	g.API = h.URL
	c := Credentials{RefreshToken: "secret-parent-token"}
	_, err := g.Refresh(context.Background(), c)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("refresh leaked error")
	}
	err = g.call(context.Background(), "GET", "/", c.AccessToken, nil, nil, nil)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("API leaked error")
	}
}

func TestRefreshRotatesCredentials(t *testing.T) {
	calls := 0
	h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("refresh_token") != "old-refresh" || r.Form.Get("client_secret") != "client-secret" {
			t.Error("incorrect refresh request")
		}
		json.NewEncoder(w).Encode(map[string]any{"access_token": "new-parent", "refresh_token": "new-refresh", "expires_in": 28800})
	}))
	defer h.Close()
	g := NewGitHub()
	g.OAuth = h.URL
	old := Credentials{ClientID: "client", ClientSecret: "client-secret", AccessToken: "old-parent", RefreshToken: "old-refresh", ExpiresAt: time.Now().Add(-time.Minute)}
	updated, err := g.Refresh(context.Background(), old)
	if err != nil || updated.AccessToken != "new-parent" || updated.RefreshToken != "new-refresh" || time.Until(updated.ExpiresAt) < 7*time.Hour {
		t.Fatal("rotation failed", err)
	}
	same, err := g.Refresh(context.Background(), updated)
	if err != nil || same != updated || calls != 1 {
		t.Fatal("unnecessary refresh")
	}
}
