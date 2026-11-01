package passlet

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestGitHubCallErrorsAndEmptyResponse(t *testing.T) {
	for _, tc := range []struct {
		name, api, body, want string
		payload               any
		networkError          bool
	}{
		{name: "invalid body", api: "https://api.example", payload: make(chan int), want: "json: unsupported type"},
		{name: "invalid URL", api: "://bad", want: "github_request_failed"},
		{name: "network error", api: "https://api.example", networkError: true, want: "github_unavailable"},
		{name: "invalid JSON", api: "https://api.example", body: "bad", want: "github_invalid_response"},
		{name: "no response expected", api: "https://api.example"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGitHub()
			g.API = tc.api
			g.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if tc.networkError {
					return nil, errors.New("secret detail")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})
			var out any
			var target any
			if tc.name != "no response expected" {
				target = &out
			}
			err := g.call(context.Background(), "GET", "/", "", nil, tc.payload, target)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %s, got %v", tc.want, err)
			}
		})
	}
}

func TestGitHubRefreshTransportFailuresKeepCredentials(t *testing.T) {
	for _, endpoint := range []string{"://bad", "https://github.example/token"} {
		g := NewGitHub()
		g.OAuth = endpoint
		g.HTTP.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("secret detail") })
		old := Credentials{AccessToken: "old", RefreshToken: "refresh"}
		got, err := g.Refresh(context.Background(), old)
		if err == nil || err.Error() != "refresh_failed" || got != old {
			t.Fatalf("refresh modified credentials on failure: %v", err)
		}
	}
}

func TestGitHubClientsRefuseRedirects(t *testing.T) {
	g := NewGitHub()
	if err := g.HTTP.CheckRedirect(&http.Request{}, nil); err != http.ErrUseLastResponse {
		t.Fatalf("redirect policy: %v", err)
	}
	client, close, err := EndpointClient("https://mac.example")
	if err != nil {
		t.Fatal(err)
	}
	defer close()
	if err := client.CheckRedirect(&http.Request{}, nil); err != http.ErrUseLastResponse {
		t.Fatalf("endpoint redirect policy: %v", err)
	}
	if _, err := g.Mint(context.Background(), Credentials{}, Request{}); err == nil {
		t.Fatal("mint accepted empty scope")
	}
}
