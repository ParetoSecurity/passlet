package passlet

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"html"
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
	missingInstallation              bool
}

func (p *setupProvider) AppExists(context.Context, string) (bool, error)           { return false, nil }
func (p *setupProvider) VerifyIdentity(context.Context, Credentials, string) error { return nil }

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
	if p.missingInstallation {
		return AccountInfo{}, errors.New("install_app_on_an_accessible_repository_and_approve_all_permissions_in_the_org")
	}
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
	s, err := NewSetupFlow("http://127.0.0.1:12345", "alice", map[string]string{"contents": "read"}, p, func(_ context.Context, c Credentials, a AccountInfo) error {
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
	manifestHTML := html.UnescapeString(page.Body.String())
	if !strings.Contains(manifestHTML, `"name":"passlet-alice"`) {
		t.Fatal("missing prefilled app name")
	}
	if !strings.Contains(manifestHTML, `"setup_url":"http://127.0.0.1:12345/github/installed"`) || !strings.Contains(manifestHTML, `"request_oauth_on_install":false`) {
		t.Fatal("manifest must set the stable installation callback and disable automatic authorization")
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
	if created.Code != 303 || created.Header().Get("Location") != s.prefix+"/authorize?state="+s.oauthState {
		t.Fatal("registration failed")
	}
	if setupRequest(s, "/created?code=test&state="+s.state).Code != 400 || p.conversions != 1 {
		t.Fatal("replayed manifest conversion")
	}

	for _, path := range []string{"/installed", "/authorize", "/authorize?state=wrong", "/authorize?state=" + s.oauthState + "&state=" + s.oauthState, "/authorized?code=unsolicited&state=" + s.oauthState} {
		if setupRequest(s, path).Code < 400 || p.exchanges != 0 || stored != 0 {
			t.Fatal("invalid transition accepted", path)
		}
	}
	installed := setupRequest(s, "/authorize?state="+s.oauthState)
	if installed.Code != 303 || p.exchanges != 0 || stored != 0 {
		t.Fatal("continuation must only start protected authorization")
	}
	if setupRequest(s, "/authorize?state="+s.oauthState).Code != 400 {
		t.Fatal("continuation replay accepted")
	}
	authorize, _ := url.Parse(installed.Header().Get("Location"))
	query := authorize.Query()
	if query.Get("scope") != "offline_access" || query.Get("code_challenge_method") != "S256" || query.Get("state") == s.state {
		t.Fatal("insecure OAuth request")
	}
	if setupRequest(s, "/authorized?code=test&state=wrong").Code != 400 || p.exchanges != 0 {
		t.Fatal("OAuth CSRF accepted")
	}
	complete := setupRequest(s, "/authorized?code=test&state="+s.oauthState)
	if complete.Code != http.StatusSeeOther || stored != 0 || p.verifies != 0 {
		t.Fatal("setup not completed", complete.Body.String())
	}

	hash := sha256.Sum256([]byte(p.verifier))
	if query.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(hash[:]) {
		t.Fatal("PKCE mismatch")
	}
	if setupRequest(s, "/authorized?code=test&state="+s.oauthState).Code != 400 || p.exchanges != 1 || stored != 0 {
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
	authorization := s.pending
	if authorization.credentials.AccessToken != "hidden-parent" {
		t.Fatal("missing private authorization result")
	}
}

func TestSetupDenialNeverImports(t *testing.T) {
	s, _ := NewSetupFlow("http://127.0.0.1:12345", "alice", map[string]string{"contents": "read"}, &setupProvider{}, func(context.Context, Credentials, AccountInfo) error {
		t.Fatal("denied setup imported credentials")
		return nil
	})
	setupRequest(s, "/created?code=test&state="+s.state)
	setupRequest(s, "/authorize?state="+s.oauthState)
	w := setupRequest(s, "/authorized?error=access_denied&state="+s.oauthState)
	if w.Code != 400 || <-s.done == nil {
		t.Fatal("denial not reported")
	}
}

func TestSetupContinueWithoutInstallationNeverImports(t *testing.T) {
	p := &setupProvider{missingInstallation: true}
	s, _ := NewSetupFlow("http://127.0.0.1:12345", "alice", map[string]string{"contents": "read"}, p, func(context.Context, Credentials, AccountInfo) error {
		t.Fatal("uninstalled app credentials saved")
		return nil
	})
	setupRequest(s, "/created?code=test&state="+s.state)
	setupRequest(s, "/authorize?state="+s.oauthState)
	w := setupRequest(s, "/authorized?code=test&state="+s.oauthState)
	if w.Code != http.StatusSeeOther || p.verifies != 0 || s.app.ClientSecret != "" {
		t.Fatal("missing installation must fail without saving credentials")
	}
	if strings.Contains(w.Body.String(), "hidden-") {
		t.Fatal("credentials exposed in verification failure")
	}
}

func TestSetupCancelledClosesListener(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var endpoint string
	err := testRunSetup(ctx, "alice", map[string]string{"contents": "read"}, &setupProvider{}, nil, func(u string) error { endpoint = u; cancel(); return nil }, nil)
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
	for _, variant := range []string{"valid", "no_refresh", "too_long", "wrong_user", "no_install", "wrong_app", "wrong_installation", "no_permissions", "no_repos", "user-error", "install-error", "repos-error"} {
		t.Run(variant, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == map[string]string{"user-error": "/user", "install-error": "/user/installations", "repos-error": "/user/installations/1/repositories"}[variant] {
					w.WriteHeader(503)
					return
				}
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
			app := AppRegistration{ID: 123, ClientID: "id", ClientSecret: "secret", Permissions: map[string]string{"contents": "read", "metadata": "read", "interaction_limits": "write"}}
			app.Owner.Login = "alice"
			if variant == "wrong_installation" {
				app.InstallationID = 999
			}
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

func TestGitHubAppName(t *testing.T) {
	for _, tc := range []struct{ handle, want string }{
		{"zupo", "passlet-zupo"}, {" @Alice-Smith ", "passlet-alice-smith"},
		{strings.Repeat("a", 39), "passlet-" + strings.Repeat("a", 26)},
	} {
		got, err := GitHubAppName(tc.handle)
		if err != nil || got != tc.want {
			t.Fatalf("unexpected name %q: %v", got, err)
		}
	}
	for _, handle := range []string{"", "../alice", "a b", "-alice", "alice-", "a--b", strings.Repeat("a", 40)} {
		if _, err := GitHubAppName(handle); err == nil {
			t.Fatalf("accepted invalid handle %q", handle)
		}
	}
}

func TestMaximumAppPermissionsDoNotWidenAgentRequests(t *testing.T) {
	permissions := map[string]string{}
	for _, item := range strings.Split(DefaultSetupPermissions, ",") {
		pair := strings.SplitN(item, ":", 2)
		permissions[pair[0]] = pair[1]
	}
	flow, err := NewSetupFlow("http://127.0.0.1:12345", "alice", permissions, &setupProvider{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(flow.permissions) != len(permissionCatalog)-len(manifestExcludedPermissions) || flow.permissions["metadata"] != "read" {
		t.Fatal("default setup does not cover supported permissions")
	}
	for name := range manifestExcludedPermissions {
		if _, present := flow.permissions[name]; present {
			t.Fatalf("unsupported manifest permission %s", name)
		}
		if _, err := NewSetupFlow("http://127.0.0.1:12345", "alice", map[string]string{name: "write"}, &setupProvider{}, nil); err == nil {
			t.Fatalf("explicit unsupported manifest permission accepted: %s", name)
		}
	}
	if permissions["metadata"] != "read" {
		t.Fatal("setup modified caller permissions")
	}
	req := Request{Repositories: []string{"alice/example"}, Permissions: flow.permissions}
	if req.Validate() == nil {
		t.Fatal("app setup bypassed native request limits")
	}
	for _, invalid := range []map[string]string{{}, {"metadata": "write"}, {"contents": "admin"}, {"unknown": "write"}, {"workflows": "read"}} {
		if _, err := NewSetupFlow("http://127.0.0.1:12345", "alice", invalid, &setupProvider{}, nil); err == nil {
			t.Fatal("invalid setup permissions accepted")
		}
	}
}

func TestSetupManifestUsesRegistrationPermissionNames(t *testing.T) {
	s, err := NewSetupFlow("http://127.0.0.1:12345", "alice", map[string]string{"email_addresses": "write", "git_ssh_keys": "write"}, &setupProvider{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	body := html.UnescapeString(setupRequest(s, "/start").Body.String())
	for _, expected := range []string{`"emails":"write"`, `"keys":"write"`, `"metadata":"read"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("manifest missing %s", expected)
		}
	}
	for _, unexpected := range []string{`"email_addresses"`, `"git_ssh_keys"`} {
		if strings.Contains(body, unexpected) {
			t.Fatalf("manifest contains API-only name %s", unexpected)
		}
	}
	if s.permissions["email_addresses"] != "write" || s.permissions["git_ssh_keys"] != "write" {
		t.Fatal("manifest translation changed expected API permissions")
	}
}

func TestExistingAppRejectsInvalidIdentity(t *testing.T) {
	for _, appURL := range []string{"https://attacker.invalid/apps/a", "https://github.com/apps/a?x=1", "https://github.com/apps/a/extra"} {
		c := Credentials{Account: AccountInfo{AppURL: appURL}}
		if err := RunInstallationSetup(context.Background(), nil, c, &setupProvider{}, nil, func(string) error { t.Fatal("opened untrusted URL"); return nil }); err == nil {
			t.Fatal("accepted invalid app URL")
		}
	}
}

func TestSetupBrowserFailureClosesListener(t *testing.T) {
	var endpoint string
	err := testRunSetup(context.Background(), "alice", map[string]string{"contents": "read"}, &setupProvider{},
		func(context.Context, Credentials, AccountInfo) error {
			t.Error("credentials saved after browser launch failed")
			return nil
		}, func(u string) error {
			endpoint = u
			return errors.New("browser launch failed")
		}, nil)
	if err == nil || err.Error() != "setup_browser_unavailable" {
		t.Fatalf("unexpected browser failure: %v", err)
	}
	if endpoint == "" {
		t.Fatal("browser launch was not attempted")
	}
	client := &http.Client{Timeout: time.Second}
	if resp, err := client.Get(endpoint); err == nil {
		resp.Body.Close()
		t.Fatal("callback server survived browser launch failure")
	}
}

// Parallel package test processes must not contend for the production port.
func testRunSetup(ctx context.Context, user string, permissions map[string]string, provider SetupProvider, store func(context.Context, Credentials, AccountInfo) error, open func(string) error, installationStarted func(context.Context) error) error {
	return runSetupBrowser(ctx, "127.0.0.1:0", func(base string) (*SetupFlow, error) {
		return NewSetupFlow(base, user, permissions, provider, store)
	}, open, installationStarted)
}

func TestInstallationCallbackRequiresActiveAuthorizationAndRejectsReplay(t *testing.T) {
	p := &setupProvider{}
	stored := 0
	s, err := NewSetupFlow("http://127.0.0.1:12345", "alice", map[string]string{"contents": "read"}, p, func(_ context.Context, c Credentials, a AccountInfo) error {
		if c.Account.Login != "alice" || p.verifies != 1 {
			t.Error("store ran before verified identity was attached")
		}
		stored++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	request := func(query string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("GET", s.base+"/github/installed"+query, nil))
		return w
	}
	if request("?installation_id=1").Code != 400 || stored != 0 {
		t.Fatal("unauthorized callback accepted")
	}
	setupRequest(s, "/created?code=test&state="+s.state)
	setupRequest(s, "/authorize?state="+s.oauthState)
	setupRequest(s, "/authorized?code=test&state="+s.oauthState)
	for _, query := range []string{"", "?installation_id=-1", "?installation_id=1&installation_id=2"} {
		if request(query).Code != 400 || stored != 0 {
			t.Fatal("invalid installation callback accepted")
		}
	}
	w := request("?installation_id=1")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "GitHub setup complete.") {
		t.Fatal("missing completion response")
	}
	for _, content := range []string{
		"Optional: add the Passlet logo",
		`href="https://github.com/settings/apps/example#:~:text=Display%20information"`,
		`src="https://passlet.dev/passlet-github-app-logo.png"`,
		`draggable="true"`,
		`target="_blank" rel="noopener noreferrer"`,
		"popup,width=900,height=850,noopener,noreferrer",
		"Drag the logo below",
	} {
		if !strings.Contains(w.Body.String(), content) {
			t.Fatal("missing app settings or draggable hosted logo")
		}
	}
	logoScriptHash := sha256.Sum256([]byte(setupLogoScript))
	policy := w.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "img-src 'self' https://passlet.dev;") || !strings.Contains(policy, "'sha256-"+base64.StdEncoding.EncodeToString(logoScriptHash[:])+"'") {
		t.Fatal("completion page logo or window script blocked by CSP")
	}
	if stored != 1 || p.verifies != 1 || s.pending != nil {
		t.Fatal("incorrect verification/store lifecycle")
	}
	if request("?installation_id=1").Code != 400 || stored != 1 {
		t.Fatal("replay stored credentials again")
	}
}
