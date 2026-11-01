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
	for _, mode := range []string{"ok", "not-installed", "broadened", "changed-permission", "no-expiry", "wrong-user", "parent-token", "missing-user", "no-installations", "user-error", "installations-error", "repositories-error", "mint-error"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				failurePath := map[string]string{"user-error": "/user", "installations-error": "/user/installations", "repositories-error": "/user/installations/42/repositories", "mint-error": "/applications/client/token/scoped"}[mode]
				if r.URL.Path == failurePath {
					w.WriteHeader(503)
					return
				}
				switch r.URL.Path {
				case "/user":
					if mode == "missing-user" {
						w.Write([]byte(`{}`))
						return
					}
					json.NewEncoder(w).Encode(map[string]any{"login": "zupo"})
				case "/user/installations":
					if mode == "no-installations" {
						w.Write([]byte(`{"installations":[]}`))
						return
					}
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
					if mode == "changed-permission" {
						perms["contents"] = "write"
					}
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

func TestMintExplicitOrganizationAndAccountScopes(t *testing.T) {
	for _, scope := range []string{"organization", "account"} {
		for _, mode := range []string{"ok", "broader", "wrong-target-type", "wrong-account", "wrong-selection"} {
			t.Run(scope+"/"+mode, func(t *testing.T) {
				target, kind, permission := "acme", "Organization", "organization_secrets"
				req := Request{Organization: target, Permissions: map[string]string{permission: "write"}}
				if scope == "account" {
					target, kind, permission = "alice", "User", "git_ssh_keys"
					req = Request{Account: target, Permissions: map[string]string{permission: "write"}}
				}
				h := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/user":
						login := "alice"
						if mode == "wrong-account" && scope == "account" {
							login = "bob"
						}
						json.NewEncoder(w).Encode(map[string]string{"login": login})
					case "/user/installations":
						typ := kind
						if mode == "wrong-target-type" {
							typ = "Wrong"
						}
						json.NewEncoder(w).Encode(map[string]any{"installations": []any{map[string]any{"id": 42, "account": map[string]string{"login": target, "type": typ}}}})
					case "/applications/client/token/scoped":
						var body map[string]any
						json.NewDecoder(r.Body).Decode(&body)
						if body["target"] != target || body["repository_ids"] != nil {
							t.Error("incorrect wide scope")
						}
						perms := map[string]string{permission: "write", "metadata": "read"}
						if mode == "broader" {
							perms["contents"] = "write"
						}
						selection := "all"
						if mode == "wrong-selection" {
							selection = "selected"
						}
						json.NewEncoder(w).Encode(map[string]any{"token": "child", "expires_at": time.Now().Add(time.Hour), "user": map[string]string{"login": "alice"}, "installation": map[string]any{"repository_selection": selection, "permissions": perms, "account": map[string]string{"login": target}}})
					default:
						t.Error("unexpected endpoint", r.URL.Path)
						w.WriteHeader(404)
					}
				}))
				defer h.Close()
				g := NewGitHub()
				g.API = h.URL
				grant, err := g.Mint(context.Background(), Credentials{ClientID: "client", ClientSecret: "secret", AccessToken: "parent"}, req)
				wantOK := mode == "ok" || (mode == "wrong-account" && scope == "organization")
				if (err == nil) != wantOK {
					t.Fatalf("unexpected mint result %v", err)
				}
				if err == nil && (grant.Organization != req.Organization || grant.Account != req.Account) {
					t.Fatal("grant lost scope")
				}
			})
		}
	}
}
