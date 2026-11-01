package passlet

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestClientResponseErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		status           int
	}{
		{"oversized", strings.Repeat("x", 65537), "invalid_response", 200},
		{"forbidden", `{"error":"github_http_403"}`, "github_access_denied_check_App_permissions_installation_approval_and_org_policy", 403},
		{"expired authorization", `{"error":"github_http_401"}`, "reauthorization_required", 401},
		{"denied approval", `{"error":"approval_denied"}`, "approval_denied", 403},
		{"malformed error", "not JSON", "grant_failed", 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}
			_, err := requestGrant(context.Background(), "https://mac.example", Request{Repositories: []string{"alice/repo"}, Permissions: map[string]string{"contents": "read"}}, client)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("want %s, got %v", tc.want, err)
			}
		})
	}
}

func TestClientRejectsEqualSizedScopeSubstitution(t *testing.T) {
	for _, scope := range []string{"repository", "permission"} {
		t.Run(scope, func(t *testing.T) {
			grant := Grant{ID: "id", Token: "token", User: "alice", ExpiresAt: time.Now().Add(time.Hour), Repositories: []string{"alice/repo"}, Permissions: map[string]string{"contents": "read", "metadata": "read"}}
			if scope == "repository" {
				grant.Repositories[0] = "alice/other"
			} else {
				grant.Permissions["contents"] = "write"
			}
			data, err := json.Marshal(grant)
			if err != nil {
				t.Fatal(err)
			}
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(data)))}, nil
			})}
			_, err = requestGrant(context.Background(), "https://mac.example", Request{Repositories: []string{"alice/repo"}, Permissions: map[string]string{"contents": "read"}}, client)
			if err == nil || err.Error() != "unexpected_grant_scope" {
				t.Fatalf("scope substitution accepted: %v", err)
			}
		})
	}
}

func TestClientValidatesBeforeSending(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Error("invalid input reached transport")
		return nil, errors.New("unexpected request")
	})}
	valid := Request{Repositories: []string{"alice/repo"}, Permissions: map[string]string{"contents": "read"}}
	for _, tc := range []struct {
		ctx      context.Context
		endpoint string
		request  Request
	}{
		{context.Background(), "https://mac.example", Request{}},
		{context.Background(), "http://mac.example", valid},
		{nil, "https://mac.example", valid},
	} {
		if _, err := requestGrant(tc.ctx, tc.endpoint, tc.request, client); err == nil {
			t.Fatal("invalid request accepted")
		}
	}
}

func TestClientHTTPS(t *testing.T) {
	for _, variant := range []string{"valid", "broader", "expired", "parent_field", "provider_secret", "redirect"} {
		t.Run(variant, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/grants" || r.Method != "POST" {
					t.Error("incorrect request")
				}
				if variant == "provider_secret" {
					w.WriteHeader(500)
					io.WriteString(w, `{"error":"secret-parent-value"}`)
					return
				}
				if variant == "redirect" {
					http.Redirect(w, r, "https://elsewhere.invalid", http.StatusFound)
					return
				}
				var requested Request
				json.NewDecoder(r.Body).Decode(&requested)
				if _, ok := requested.Permissions["metadata"]; !ok {
					t.Error("intrinsic permission missing")
				}
				grant := Grant{ID: "test", Token: "child-only", User: "zupo", ExpiresAt: time.Now().Add(time.Hour), Repositories: requested.Repositories, Permissions: requested.Permissions}
				if variant == "broader" {
					grant.Permissions["issues"] = "write"
				}
				if variant == "expired" {
					grant.ExpiresAt = time.Now().Add(-time.Hour)
				}
				if variant == "parent_field" {
					io.WriteString(w, `{"refresh_token":"secret-parent-value"}`)
					return
				}
				json.NewEncoder(w).Encode(grant)
			}))
			defer server.Close()
			client := server.Client()
			client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			grant, err := requestGrant(context.Background(), server.URL, Request{Repositories: []string{"zupo/test"}, Permissions: map[string]string{"contents": "read"}}, client)
			if variant == "valid" {
				if err != nil || grant.Token != "child-only" {
					t.Fatal("valid response rejected", err)
				}
			} else if err == nil || strings.Contains(err.Error(), "secret-parent-value") {
				t.Fatal("bad response accepted or secret leaked")
			}
		})
	}
}
func TestClientRejectsUntrustedTLS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("untrusted server reached") }))
	defer server.Close()
	_, err := RequestGrant(context.Background(), server.URL, Request{Repositories: []string{"zupo/test"}, Permissions: map[string]string{"contents": "read"}})
	if err == nil {
		t.Fatal("untrusted TLS accepted")
	}
}
func TestClientRejectsEndpoints(t *testing.T) {
	for _, endpoint := range []string{"http://localhost", "https://user:secret@example.com", "https://example.com/path", "https://example.com?token=x", "https://example.com#x"} {
		_, err := RequestGrant(context.Background(), endpoint, Request{Repositories: []string{"zupo/test"}, Permissions: map[string]string{"contents": "read"}})
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("invalid endpoint or leaked credential")
		}
	}
}
func TestClientCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := RequestGrant(ctx, "https://127.0.0.1:1", Request{Repositories: []string{"zupo/test"}, Permissions: map[string]string{"contents": "read"}})
	if err == nil || err.Error() != "request_cancelled" {
		t.Fatal(err)
	}
}

func TestClientRejectsChangedWideScope(t *testing.T) {
	for _, scope := range []string{"organization", "account"} {
		for _, changed := range []bool{false, true} {
			req := Request{Organization: "acme", Permissions: map[string]string{"members": "write"}}
			if scope == "account" {
				req = Request{Account: "alice", Permissions: map[string]string{"git_ssh_keys": "write"}}
			}
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var got Request
				json.NewDecoder(r.Body).Decode(&got)
				grant := Grant{ID: "test", Token: "child", User: "alice", ExpiresAt: time.Now().Add(time.Hour), Organization: got.Organization, Account: got.Account, Permissions: got.Permissions}
				if changed {
					if scope == "organization" {
						grant.Organization = "other"
					} else {
						grant.Account = "other"
					}
				}
				json.NewEncoder(w).Encode(grant)
			}))
			_, err := requestGrant(context.Background(), server.URL, req, server.Client())
			server.Close()
			if (err != nil) != changed {
				t.Fatalf("%s changed=%v error=%v", scope, changed, err)
			}
		}
	}
}
