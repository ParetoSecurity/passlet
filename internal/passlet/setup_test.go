package passlet

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type setupProvider struct {
	conversions, exchanges, verifies int
	verifier                         string
	fail                             bool
}

func (p *setupProvider) ConvertManifest(context.Context, string) (AppRegistration, error) {
	p.conversions++
	a := AppRegistration{ID: 123, ClientID: "test-client", ClientSecret: "hidden-client-secret", HTMLURL: "https://github.com/apps/example", Permissions: map[string]string{"contents": "read", "metadata": "read"}}
	a.Owner.Login = "alice"
	return a, nil
}
func (p *setupProvider) AuthorizeCode(_ context.Context, _ AppRegistration, _, _, verifier string) (Credentials, error) {
	p.exchanges++
	p.verifier = verifier
	if p.fail {
		return Credentials{}, errors.New("github_authorization_failed")
	}
	return Credentials{ClientID: "test-client", ClientSecret: "hidden-client-secret", AccessToken: "hidden-parent", RefreshToken: "hidden-refresh", ExpiresAt: time.Now().Add(time.Hour)}, nil
}
func (p *setupProvider) VerifySetup(context.Context, AppRegistration, Credentials) (AccountInfo, error) {
	p.verifies++
	return AccountInfo{Login: "alice", AppID: 123, AppURL: "https://github.com/apps/example", VerifiedAt: time.Now()}, nil
}

func setupRequest(s *SetupFlow, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", s.base+s.prefix+path, nil))
	return w
}

func TestSetupFlowStatePKCEAndSecretHandling(t *testing.T) {
	p := &setupProvider{}
	stored := 0
	s, err := NewSetupFlow("http://127.0.0.1:12345", map[string]string{"contents": "read"}, p, func(_ context.Context, c Credentials, a AccountInfo) error {
		stored++
		if c.AccessToken != "hidden-parent" || a.Login != "alice" {
			t.Fatal("wrong import")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	page := setupRequest(s, "/start")
	if page.Code != 200 || !strings.Contains(page.Body.String(), "Create my GitHub App") {
		t.Fatal("no manifest page")
	}
	wrongHost := httptest.NewRequest("GET", s.base+s.prefix+"/start", nil)
	wrongHost.Host = "attacker.invalid"
	w := httptest.NewRecorder()
	s.ServeHTTP(w, wrongHost)
	if w.Code != 400 {
		t.Fatal("DNS rebinding host accepted")
	}
	if setupRequest(s, "/created?code=test&state=wrong").Code != 400 || p.conversions != 0 {
		t.Fatal("CSRF accepted")
	}
	created := setupRequest(s, "/created?code=test&state="+s.state)
	if created.Code != 303 || created.Header().Get("Location") != "https://github.com/apps/example/installations/new" {
		t.Fatal("registration failed")
	}
	if setupRequest(s, "/created?code=test&state="+s.state).Code != 400 || p.conversions != 1 {
		t.Fatal("replayed manifest conversion")
	}
	installed := setupRequest(s, "/installed")
	authorize, _ := url.Parse(installed.Header().Get("Location"))
	query := authorize.Query()
	if query.Get("scope") != "offline_access" || query.Get("code_challenge_method") != "S256" || query.Get("state") == s.state {
		t.Fatal("insecure OAuth request")
	}
	if setupRequest(s, "/authorized?code=test&state=wrong").Code != 400 || p.exchanges != 0 {
		t.Fatal("OAuth CSRF accepted")
	}
	complete := setupRequest(s, "/authorized?code=test&state="+s.oauthState)
	if complete.Code != 200 || stored != 1 || p.verifies != 1 {
		t.Fatal("setup not completed", complete.Body.String())
	}
	hash := sha256.Sum256([]byte(p.verifier))
	if query.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(hash[:]) {
		t.Fatal("PKCE mismatch")
	}
	if setupRequest(s, "/authorized?code=test&state="+s.oauthState).Code != 400 || p.exchanges != 1 || stored != 1 {
		t.Fatal("OAuth replay accepted")
	}
	for _, response := range []*httptest.ResponseRecorder{page, created, installed, complete} {
		if strings.Contains(response.Body.String(), "hidden-") || strings.Contains(response.Header().Get("Location"), "hidden-") {
			t.Fatal("secret exposed to browser")
		}
	}
	if s.app.ClientSecret != "" {
		t.Fatal("App credential retained after completion")
	}
	if err := <-s.done; err != nil {
		t.Fatal(err)
	}
}

func TestSetupDenialNeverImports(t *testing.T) {
	s, _ := NewSetupFlow("http://127.0.0.1:12345", map[string]string{"contents": "read"}, &setupProvider{}, func(context.Context, Credentials, AccountInfo) error {
		t.Fatal("denied setup imported credentials")
		return nil
	})
	setupRequest(s, "/created?code=test&state="+s.state)
	setupRequest(s, "/installed")
	w := setupRequest(s, "/authorized?error=access_denied&state="+s.oauthState)
	if w.Code != 400 || <-s.done == nil {
		t.Fatal("denial not reported")
	}
}

func TestSetupCancelledClosesListener(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var endpoint string
	err := RunSetup(ctx, map[string]string{"contents": "read"}, &setupProvider{}, nil, func(u string) error { endpoint = u; cancel(); return nil })
	if err == nil {
		t.Fatal("cancelled setup succeeded")
	}
	client := &http.Client{Timeout: time.Second}
	if resp, err := client.Get(endpoint); err == nil {
		resp.Body.Close()
		t.Fatal("callback server survived cancellation")
	}
}

func TestSetupProviderRejectsNonExpiringAndUnverifiedCredentials(t *testing.T) {
	for _, variant := range []string{"valid", "no_refresh", "too_long", "wrong_user", "no_install", "wrong_app", "no_permissions", "no_repos"} {
		t.Run(variant, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/oauth":
					r.ParseForm()
					if r.Form.Get("code_verifier") != "verifier" || r.Form.Get("client_secret") != "secret" {
						t.Error("missing OAuth parameters")
					}
					refresh := "refresh"
					expires := 28800
					if variant == "no_refresh" {
						refresh = ""
					}
					if variant == "too_long" {
						expires = 28801
					}
					json.NewEncoder(w).Encode(map[string]any{"access_token": "parent", "refresh_token": refresh, "expires_in": expires, "token_type": "bearer"})
				case "/user":
					login := "alice"
					if variant == "wrong_user" {
						login = "mallory"
					}
					json.NewEncoder(w).Encode(map[string]string{"login": login})
				case "/user/installations":
					if variant == "no_install" {
						w.Write([]byte(`{"installations":[]}`))
						return
					}
					id := 123
					if variant == "wrong_app" {
						id = 999
					}
					permissions := map[string]string{"metadata": "read", "contents": "read"}
					if variant == "no_permissions" {
						delete(permissions, "contents")
					}
					json.NewEncoder(w).Encode(map[string]any{"installations": []any{map[string]any{"id": 1, "app_id": id, "permissions": permissions}}})
				case "/user/installations/1/repositories":
					if variant == "no_repos" {
						w.Write([]byte(`{"repositories":[]}`))
					} else {
						w.Write([]byte(`{"repositories":[{"full_name":"alice/test"}]}`))
					}
				default:
					t.Error("unexpected endpoint", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer srv.Close()
			g := &GitHub{HTTP: srv.Client(), API: srv.URL, OAuth: srv.URL + "/oauth"}
			app := AppRegistration{ID: 123, ClientID: "id", ClientSecret: "secret", Permissions: map[string]string{"contents": "read", "metadata": "read"}}
			app.Owner.Login = "alice"
			c, err := g.AuthorizeCode(context.Background(), app, "code", "http://127.0.0.1:1/callback", "verifier")
			if err == nil {
				_, err = g.VerifySetup(context.Background(), app, c)
			}
			if (err == nil) != (variant == "valid") {
				t.Fatal("unexpected setup verification", err)
			}
		})
	}
}

func TestManifestDiscardsPrivateKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/app-manifests/code/conversions" {
			t.Error("wrong conversion request")
		}
		w.Write([]byte(`{"id":123,"client_id":"id","client_secret":"secret","html_url":"https://github.com/apps/example","owner":{"login":"alice"},"pem":"unused-signing-key","webhook_secret":"unused-webhook-secret"}`))
	}))
	defer srv.Close()
	g := &GitHub{HTTP: srv.Client(), API: srv.URL}
	app, err := g.ConvertManifest(context.Background(), "code")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(app)
	if strings.Contains(string(data), "unused-") {
		t.Fatal("unused App secret retained")
	}
}
