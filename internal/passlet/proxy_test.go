package passlet

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestProxyEnvironment(t *testing.T) {
	if os.Getenv("PASSLET_PROXY_ENV_TEST") == "1" {
		target, _ := url.Parse("https://approver.example:17400")
		proxy, err := environmentProxy(&http.Request{URL: target})
		if err != nil {
			t.Fatal(err)
		}
		if (proxy == nil) != (os.Getenv("NO_PROXY") != "") {
			t.Fatal("unexpected proxy selection")
		}
		return
	}
	for _, bypass := range []string{"", "approver.example", "*"} {
		t.Run("no_proxy="+bypass, func(t *testing.T) {
			t.Setenv("HTTPS_PROXY", "http://127.0.0.1:12345")
			t.Setenv("NO_PROXY", bypass)
			t.Setenv("no_proxy", "")
			t.Setenv("REQUEST_METHOD", "")
			self, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(self, "-test.run=^TestProxyEnvironment$")
			cmd.Env = append(os.Environ(), "PASSLET_PROXY_ENV_TEST=1")
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%s: %v", output, err)
			}
		})
	}
}

func TestProxyConfigurationErrors(t *testing.T) {
	original := environmentProxy
	t.Cleanup(func() { environmentProxy = original })
	t.Setenv("HOME", t.TempDir())
	endpoint, _ := url.Parse("https://approver.example")
	proxy, _ := url.Parse("http://127.0.0.1:12345")
	environmentProxy = func(*http.Request) (*url.URL, error) { return proxy, nil }
	if _, _, err := EndpointClient(endpoint.String()); err == nil {
		t.Fatal("missing Agent configuration accepted")
	}
	sentinel := errors.New("invalid URL containing fixture-proxy-secret")
	environmentProxy = func(*http.Request) (*url.URL, error) { return nil, sentinel }
	if err := routeThroughProxy(&http.Transport{}, endpoint, func() (*tls.Config, error) {
		t.Fatal("TLS config requested after proxy selection failed")
		return nil, nil
	}); err == nil || strings.Contains(err.Error(), "fixture-proxy-secret") {
		t.Fatal(err)
	}
	code, err := CreateInvitation()
	if err != nil {
		t.Fatal(err)
	}
	if err := PairAgent(context.Background(), endpoint.String(), code, "alice@proxy"); err == nil || strings.Contains(err.Error(), "fixture-proxy-secret") {
		t.Fatal(err)
	}
}
