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
)

func TestSetupProviderTransportAndManifestFailures(t *testing.T) {
	for _, tc := range []struct {
		name, api, body string
		fail            bool
	}{
		{"exchange failure", "https://api.example", "", true},
		{"invalid manifest", "https://api.example", `{"id":1}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGitHub()
			g.API = tc.api
			g.HTTP.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
				if tc.fail {
					return nil, errors.New("network failed")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			if _, err := g.ConvertManifest(context.Background(), "code"); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
	for _, endpoint := range []string{"://invalid", "https://oauth.example"} {
		g := NewGitHub()
		g.OAuth = endpoint
		g.HTTP.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("network failed") })
		if _, err := g.AuthorizeCode(context.Background(), AppRegistration{}, "code", "http://127.0.0.1/callback", "verifier"); err == nil || err.Error() != "github_authorization_failed" {
			t.Fatalf("authorization failure: %v", err)
		}
	}
}

func TestAppLookupRejectsInvalidResponses(t *testing.T) {
	for _, tc := range []struct {
		name, body   string
		status       int
		exists, fail bool
	}{
		{"exists", `{"id":123}`, 200, true, false},
		{"missing", `{}`, 404, false, false},
		{"no ID", `{}`, 200, false, true},
		{"negative ID", `{"id":-1}`, 200, false, true},
		{"malformed", `not JSON`, 200, false, true},
		{"service failure", `{"message":"fixture-secret"}`, 500, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/apps/passlet-alice" || r.Header.Get("Authorization") != "" {
					t.Error("app lookup must not send credentials")
				}
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer server.Close()
			g := NewGitHub()
			g.API = server.URL
			exists, err := g.AppExists(context.Background(), "passlet-alice")
			if exists != tc.exists || (err != nil) != tc.fail {
				t.Fatalf("exists=%v, error=%v", exists, err)
			}
			if err != nil && strings.Contains(err.Error(), "fixture-secret") {
				t.Fatal("provider response disclosed")
			}
		})
	}
}

func TestVerifyIdentityRejectsUntrustedLogin(t *testing.T) {
	for _, tc := range []struct {
		name, login string
		status      int
		fail        bool
	}{
		{"case insensitive", "ALICE", 200, false},
		{"other account", "bob", 200, true},
		{"empty", "", 200, true},
		{"control characters", "alice\nfixture-secret", 200, true},
		{"too long", strings.Repeat("a", 40), 200, true},
		{"revoked", "alice", 401, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/user" || r.Header.Get("Authorization") != "Bearer fixture-parent" {
					t.Error("wrong identity request")
				}
				w.WriteHeader(tc.status)
				json.NewEncoder(w).Encode(map[string]string{"login": tc.login})
			}))
			defer server.Close()
			g := NewGitHub()
			g.API = server.URL
			err := g.VerifyIdentity(context.Background(), Credentials{AccessToken: "fixture-parent"}, "alice")
			if (err != nil) != tc.fail {
				t.Fatalf("unexpected identity result: %v", err)
			}
			if err != nil && (strings.Contains(err.Error(), "fixture-") || strings.Contains(setupFailure(err, "").Terminal, "fixture-")) {
				t.Fatal("untrusted provider value disclosed")
			}
		})
	}
}
