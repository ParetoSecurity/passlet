package passlet

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

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
					http.Redirect(w, r, "https://elsewhere.invalid", 302)
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
