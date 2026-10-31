package passlet

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type AppRegistration struct {
	ID           int64             `json:"id"`
	ClientID     string            `json:"client_id"`
	ClientSecret string            `json:"client_secret"`
	HTMLURL      string            `json:"html_url"`
	Permissions  map[string]string `json:"permissions"`
	Owner        struct {
		Login string `json:"login"`
	} `json:"owner"`
	// Intentionally no PEM or webhook-secret fields: discard these response
	// fields during decoding, never persist or use the unused signing key.
}

type SetupProvider interface {
	ConvertManifest(context.Context, string) (AppRegistration, error)
	AuthorizeCode(context.Context, AppRegistration, string, string, string) (Credentials, error)
	VerifySetup(context.Context, AppRegistration, Credentials) (AccountInfo, error)
}

func (g *GitHub) ConvertManifest(ctx context.Context, code string) (AppRegistration, error) {
	var app AppRegistration
	err := g.call(ctx, "POST", "/app-manifests/"+url.PathEscape(code)+"/conversions", "", nil, nil, &app)
	if err != nil {
		return app, errors.New("app_registration_exchange_failed_restart_setup")
	}
	u, err := url.Parse(app.HTMLURL)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !strings.HasPrefix(u.Path, "/apps/") || !component.MatchString(strings.TrimPrefix(u.Path, "/apps/")) || app.ID <= 0 || app.ClientID == "" || app.ClientSecret == "" || !component.MatchString(app.Owner.Login) {
		return AppRegistration{}, errors.New("app_registration_invalid")
	}
	return app, nil
}

func (g *GitHub) AuthorizeCode(ctx context.Context, app AppRegistration, code, redirect, verifier string) (Credentials, error) {
	form := url.Values{"client_id": {app.ClientID}, "client_secret": {app.ClientSecret}, "code": {code}, "redirect_uri": {redirect}, "code_verifier": {verifier}}
	req, err := http.NewRequestWithContext(ctx, "POST", g.OAuth, strings.NewReader(form.Encode()))
	if err != nil {
		return Credentials{}, errors.New("github_authorization_failed")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return Credentials{}, errors.New("github_authorization_failed")
	}
	defer resp.Body.Close()
	var token struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		TokenType    string `json:"token_type"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&token) != nil || token.AccessToken == "" || token.RefreshToken == "" || token.ExpiresIn <= 0 || token.ExpiresIn > 28800 || !strings.EqualFold(token.TokenType, "bearer") {
		return Credentials{}, errors.New("github_authorization_requires_expiring_token_restart_setup")
	}
	return Credentials{ClientID: app.ClientID, ClientSecret: app.ClientSecret, AccessToken: token.AccessToken, RefreshToken: token.RefreshToken, ExpiresAt: time.Now().UTC().Add(time.Duration(token.ExpiresIn) * time.Second)}, nil
}

func (g *GitHub) VerifySetup(ctx context.Context, app AppRegistration, c Credentials) (AccountInfo, error) {
	var user struct {
		Login string `json:"login"`
	}
	if err := g.call(ctx, "GET", "/user", c.AccessToken, nil, nil, &user); err != nil {
		return AccountInfo{}, errors.New("github_identity_verification_failed")
	}
	if !strings.EqualFold(user.Login, app.Owner.Login) {
		return AccountInfo{}, errors.New("authorize_with_the_GitHub_account_that_created_the_app")
	}
	for page := 1; page <= 10; page++ {
		var installs struct {
			Installations []struct {
				ID          int64             `json:"id"`
				AppID       int64             `json:"app_id"`
				Permissions map[string]string `json:"permissions"`
			} `json:"installations"`
		}
		if err := g.call(ctx, "GET", fmt.Sprintf("/user/installations?per_page=100&page=%d", page), c.AccessToken, nil, nil, &installs); err != nil {
			return AccountInfo{}, errors.New("github_installation_verification_failed")
		}
		for _, install := range installs.Installations {
			if install.AppID != app.ID || install.ID <= 0 {
				continue
			}
			permissionsApproved := true
			for name, level := range app.Permissions {
				actual := install.Permissions[name]
				if actual != level && !(level == "read" && actual == "write") {
					permissionsApproved = false
				}
			}
			if !permissionsApproved {
				continue
			}
			var repos struct {
				Repositories []struct {
					FullName string `json:"full_name"`
				} `json:"repositories"`
			}
			if err := g.call(ctx, "GET", "/user/installations/"+strconv.FormatInt(install.ID, 10)+"/repositories?per_page=1", c.AccessToken, nil, nil, &repos); err != nil {
				return AccountInfo{}, errors.New("github_repository_verification_failed")
			}
			if len(repos.Repositories) > 0 && install.Permissions["metadata"] == "read" {
				return AccountInfo{Login: user.Login, AppID: app.ID, AppURL: app.HTMLURL, VerifiedAt: time.Now().UTC()}, nil
			}
		}
		if len(installs.Installations) < 100 {
			break
		}
	}
	return AccountInfo{}, errors.New("install_app_on_an_accessible_repository_and_approve_all_permissions_in_the_org")
}

// SetupFlow is served only by a short-lived IPv4 loopback listener. All secret
// material stays in memory until the protected helper import. Host validation
// and unguessable paths prevent DNS rebinding and blind loopback callbacks.
type SetupFlow struct {
	mu                                        sync.Mutex
	base, prefix, state, oauthState, verifier string
	permissions                               map[string]string
	provider                                  SetupProvider
	store                                     func(context.Context, Credentials, AccountInfo) error
	phase                                     string
	app                                       AppRegistration
	done                                      chan error
}

func randomURLToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func NewSetupFlow(base string, permissions map[string]string, provider SetupProvider, store func(context.Context, Credentials, AccountInfo) error) (*SetupFlow, error) {
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("setup_requires_loopback_origin")
	}
	r := Request{Repositories: []string{"setup/validation"}, Permissions: permissions}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	tokens := make([]string, 4)
	for i := range tokens {
		tokens[i], err = randomURLToken()
		if err != nil {
			return nil, err
		}
	}
	return &SetupFlow{base: base, prefix: "/" + tokens[0], state: tokens[1], oauthState: tokens[2], verifier: tokens[3], permissions: r.Permissions, provider: provider, store: store, phase: "register", done: make(chan error, 1)}, nil
}

func (s *SetupFlow) URL() string { return s.base + s.prefix + "/start" }

var setupPage = template.Must(template.New("setup").Parse(`<!doctype html><html><meta charset="utf-8"><title>Passlet GitHub setup</title><h1>Your GitHub App</h1><p>Create an App owned by your GitHub account. Choose only the repositories you need when installing it. Organization approval may be required.</p><p>Maximum permissions:</p><pre>{{.Permissions}}</pre><form method="post" action="{{.Action}}"><input type="hidden" name="manifest" value="{{.Manifest}}"><button type="submit">Create my GitHub App</button></form></html>`))

func (s *SetupFlow) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; form-action https://github.com; frame-ancestors 'none'; base-uri 'none'")
	if r.Host != strings.TrimPrefix(s.base, "http://") || r.Method != "GET" || len(r.URL.RawQuery) > 2048 {
		http.Error(w, "Invalid setup request", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fail := func(err error) {
		s.phase = "failed"
		s.app = AppRegistration{}
		// Errors are fixed internal codes, never raw provider responses or URLs.
		http.Error(w, err.Error()+". Return to the terminal. Remove the incomplete App before restarting setup.", http.StatusBadRequest)
		select {
		case s.done <- err:
		default:
		}
	}
	params := r.URL.Query()
	validState := func(name, expected string) bool {
		return len(params[name]) == 1 && subtle.ConstantTimeCompare([]byte(params.Get(name)), []byte(expected)) == 1
	}
	code := params.Get("code")
	validCode := code != "" && len(code) <= 256 && !strings.ContainsAny(code, "/\x00\r\n") && len(params["code"]) == 1
	switch r.URL.Path {
	case s.prefix + "/start":
		if s.phase != "register" {
			http.Error(w, "Setup already started", 409)
			return
		}
		manifest, _ := json.Marshal(map[string]any{
			"url": "https://passlet.dev", "public": true,
			"hook_attributes": map[string]any{"url": "https://passlet.dev", "active": false},
			"redirect_url":    s.base + s.prefix + "/created", "callback_urls": []string{s.base + s.prefix + "/authorized"},
			"setup_url": s.base + s.prefix + "/installed", "request_oauth_on_install": false,
			"default_permissions": s.permissions, "default_events": []string{},
		})
		permissions, _ := json.MarshalIndent(s.permissions, "", "  ")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		setupPage.Execute(w, map[string]string{"Manifest": string(manifest), "Permissions": string(permissions), "Action": "https://github.com/settings/apps/new?state=" + s.state})
	case s.prefix + "/created":
		if s.phase != "register" || !validState("state", s.state) || !validCode {
			http.Error(w, "Invalid or replayed registration callback", 400)
			return
		}
		s.phase = "converting"
		app, err := s.provider.ConvertManifest(r.Context(), code)
		if err != nil {
			fail(err)
			return
		}
		if len(app.Permissions) != len(s.permissions) {
			fail(errors.New("app_permissions_differ_from_setup_request"))
			return
		}
		for name, level := range s.permissions {
			if app.Permissions[name] != level {
				fail(errors.New("app_permissions_differ_from_setup_request"))
				return
			}
		}
		s.app = app
		s.phase = "install"
		http.Redirect(w, r, app.HTMLURL+"/installations/new", http.StatusSeeOther)
	case s.prefix + "/installed":
		if s.phase != "install" {
			http.Error(w, "Unexpected installation callback", 400)
			return
		}
		s.phase = "authorize"
		challenge := sha256.Sum256([]byte(s.verifier))
		query := url.Values{"client_id": {s.app.ClientID}, "redirect_uri": {s.base + s.prefix + "/authorized"}, "state": {s.oauthState}, "scope": {"offline_access"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(challenge[:])}, "code_challenge_method": {"S256"}, "login": {s.app.Owner.Login}}
		http.Redirect(w, r, "https://github.com/login/oauth/authorize?"+query.Encode(), http.StatusSeeOther)
	case s.prefix + "/authorized":
		if s.phase != "authorize" || !validState("state", s.oauthState) {
			http.Error(w, "Invalid or replayed authorization callback", 400)
			return
		}
		if params.Get("error") != "" || !validCode {
			fail(errors.New("github_authorization_denied"))
			return
		}
		s.phase = "saving"
		c, err := s.provider.AuthorizeCode(r.Context(), s.app, code, s.base+s.prefix+"/authorized", s.verifier)
		if err != nil {
			fail(err)
			return
		}
		account, err := s.provider.VerifySetup(r.Context(), s.app, c)
		if err != nil {
			fail(err)
			return
		}
		if r.Context().Err() != nil {
			fail(errors.New("setup_cancelled"))
			return
		}
		if err := s.store(r.Context(), c, account); err != nil {
			fail(err)
			return
		}
		s.app = AppRegistration{}
		s.phase = "complete"
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, "GitHub setup complete. Credentials are stored in your protected Mac Keychain. Return to the terminal.")
		s.done <- nil
	default:
		http.NotFound(w, r)
	}
}

func RunSetup(ctx context.Context, permissions map[string]string, provider SetupProvider, store func(context.Context, Credentials, AccountInfo) error, open func(string) error) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return errors.New("setup_callback_listener_unavailable")
	}
	defer l.Close()
	flow, err := NewSetupFlow("http://"+l.Addr().String(), permissions, provider, store)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: flow, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 90 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 8192, BaseContext: func(net.Listener) context.Context { return ctx }}
	defer server.Close()
	go server.Serve(l)
	if err := open(flow.URL()); err != nil {
		return errors.New("setup_browser_unavailable")
	}
	select {
	case err := <-flow.done:
		// Let the final browser response flush before closing active connections.
		shutdown, done := context.WithTimeout(context.Background(), time.Second)
		defer done()
		server.Shutdown(shutdown)
		return err
	case <-ctx.Done():
		return errors.New("setup_cancelled_or_timed_out_restart_setup")
	}
}
