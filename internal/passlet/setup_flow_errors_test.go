package passlet

import (
	"context"
	"errors"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type registrationProvider struct {
	setupProvider
	mode string
}

func (p *registrationProvider) ConvertManifest(ctx context.Context, code string) (AppRegistration, error) {
	app, err := p.setupProvider.ConvertManifest(ctx, code)
	switch p.mode {
	case "exchange":
		return AppRegistration{}, errors.New("registration_failed")
	case "wrong owner":
		app.Owner.Login = "bob"
	case "unknown permission":
		app.Permissions["unknown"] = "write"
	case "changed permission":
		app.Permissions["contents"] = "write"
	case "extra permission":
		app.Permissions["issues"] = "read"
	case "duplicate permission alias":
		app.Permissions["emails"] = "read"
		app.Permissions["email_addresses"] = "read"
	}
	return app, err
}

func TestSetupRegistrationFailuresDiscardSecrets(t *testing.T) {
	for _, mode := range []string{"exchange", "wrong owner", "unknown permission", "changed permission", "extra permission", "duplicate permission alias"} {
		t.Run(mode, func(t *testing.T) {
			s, err := NewSetupFlow("http://127.0.0.1:12345", "alice", map[string]string{"contents": "read"}, &registrationProvider{mode: mode}, func(context.Context, Credentials, AccountInfo) error {
				t.Fatal("failed registration saved credentials")
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			w := setupRequest(s, "/created?code=test&state="+s.state)
			if w.Code != 400 || s.phase != "failed" || s.pending != nil || s.app.ClientSecret != "" {
				t.Fatalf("registration failure retained state: %d %s", w.Code, s.phase)
			}
			if err := <-s.done; err == nil || strings.Contains(w.Body.String(), "hidden-") {
				t.Fatal("missing failure or leaked credential")
			}
		})
	}
}

func TestSetupFavicon(t *testing.T) {
	s, err := NewSetupFlow("http://127.0.0.1:12345", "alice", map[string]string{"metadata": "read"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", s.base+"/favicon.png", nil))
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/png" || !strings.HasPrefix(w.Body.String(), "\x89PNG\r\n\x1a\n") {
		t.Fatal("missing PNG favicon")
	}
}

func TestSetupOAuthExchangeFailureClearsState(t *testing.T) {
	s, err := NewSetupFlow("http://127.0.0.1:12345", "alice", map[string]string{"contents": "read"}, &setupProvider{fail: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	setupRequest(s, "/created?code=test&state="+s.state)
	setupRequest(s, "/authorize?state="+s.oauthState)
	w := setupRequest(s, "/authorized?code=test&state="+s.oauthState)
	if w.Code != 400 || s.app.ClientSecret != "" || s.pending != nil {
		t.Fatal("failed exchange retained secrets")
	}
}

func TestInstallationSetupRejectsInvalidCredentialsAndListener(t *testing.T) {
	c := Credentials{Account: AccountInfo{Login: "alice", AppID: 1, AppURL: "https://github.com/apps/passlet-alice", VerifiedAt: time.Now()}}
	if err := RunInstallationSetup(context.Background(), nil, c, nil, nil, nil); err == nil || err.Error() != "incomplete_credentials" {
		t.Fatalf("incomplete credentials accepted: %v", err)
	}
	c.ClientID, c.ClientSecret, c.AccessToken, c.RefreshToken = "id", "secret", "access", "refresh"
	c.ExpiresAt = time.Now().Add(time.Hour)
	l, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := RunInstallationSetup(context.Background(), l, c, nil, nil, func(string) error { t.Fatal("non-loopback setup opened browser"); return nil }); err == nil {
		t.Fatal("non-loopback setup accepted")
	}
}

func TestSetupInstallationHookFailureAndCancelledCallback(t *testing.T) {
	for _, mode := range []string{"hook fails", "callback cancelled"} {
		t.Run(mode, func(t *testing.T) {
			s, err := NewSetupFlow("http://127.0.0.1:12345", "alice", map[string]string{"contents": "read"}, &setupProvider{}, func(context.Context, Credentials, AccountInfo) error {
				t.Fatal("cancelled setup stored credentials")
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "hook fails" {
				s.installationStarted = func(context.Context) error { return errors.New("cancelled") }
			}
			setupRequest(s, "/created?code=test&state="+s.state)
			if w := setupRequest(s, "/start"); w.Code != 409 {
				t.Fatal("restarted active setup")
			}
			setupRequest(s, "/authorize?state="+s.oauthState)
			w := setupRequest(s, "/authorized?code=test&state="+s.oauthState)
			if mode == "callback cancelled" {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				w = httptest.NewRecorder()
				s.ServeHTTP(w, httptest.NewRequest("GET", s.base+"/github/installed?installation_id=1", nil).WithContext(ctx))
			}
			if w.Code != 400 || s.pending != nil || s.phase != "failed" {
				t.Fatalf("cancellation: %d %s", w.Code, s.phase)
			}
			if err := <-s.done; err == nil {
				t.Fatal("cancellation reported success")
			}
		})
	}
}

func TestSetupInvalidConfigurationDoesNotOpenBrowser(t *testing.T) {
	for _, tc := range []struct{ base, user string }{
		{"http://127.0.0.1:12345", "bad/name"},
		{"https://example.com", "alice"},
	} {
		if _, err := NewSetupFlow(tc.base, tc.user, map[string]string{"metadata": "read"}, nil, nil); err == nil {
			t.Fatal("invalid setup origin or handle accepted")
		}
	}
	opened := false
	err := testRunSetup(context.Background(), "bad/name", map[string]string{"metadata": "read"}, nil, nil, func(string) error { opened = true; return nil }, nil)
	if err == nil || opened {
		t.Fatal("invalid setup opened browser")
	}
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := runSetupBrowser(context.Background(), l.Addr().String(), nil, func(string) error { t.Fatal("occupied port opened browser"); return nil }, nil); err == nil {
		t.Fatal("occupied listener accepted")
	}
}

func TestSetupErrorPreservesCause(t *testing.T) {
	cause := errors.New("provider unavailable")
	if err := setupFailure(cause, ""); !errors.Is(err, cause) {
		t.Fatal("setup error lost cause")
	}
}
