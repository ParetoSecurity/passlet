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
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"passlet.dev/passlet/assets"
)

type AppRegistration struct {
	InstallationID int64             `json:"-"`
	ID             int64             `json:"id"`
	ClientID       string            `json:"client_id"`
	ClientSecret   string            `json:"client_secret"`
	HTMLURL        string            `json:"html_url"`
	Permissions    map[string]string `json:"permissions"`
	Owner          struct {
		Login string `json:"login"`
	} `json:"owner"`
	// Intentionally no PEM or webhook-secret fields: discard these response
	// fields during decoding, never persist or use the unused signing key.
}

type SetupProvider interface {
	AppExists(context.Context, string) (bool, error)
	VerifyIdentity(context.Context, Credentials, string) error
	ConvertManifest(context.Context, string) (AppRegistration, error)
	AuthorizeCode(context.Context, AppRegistration, string, string, string) (Credentials, error)
	VerifySetup(context.Context, AppRegistration, Credentials) (AccountInfo, error)
}

func (g *GitHub) AppExists(ctx context.Context, name string) (bool, error) {
	var app struct {
		ID int64 `json:"id"`
	}
	err := g.call(ctx, "GET", "/apps/"+url.PathEscape(name), "", nil, nil, &app)
	if err != nil && err.Error() == "github_http_404" {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if app.ID <= 0 {
		return false, errors.New("github_invalid_response")
	}
	return true, nil
}
func (g *GitHub) VerifyIdentity(ctx context.Context, c Credentials, expected string) error {
	var user struct {
		Login string `json:"login"`
	}
	if err := g.call(ctx, "GET", "/user", c.AccessToken, nil, nil, &user); err != nil {
		return err
	}
	if !githubHandle.MatchString(user.Login) || len(user.Login) > 39 {
		return errors.New("github_invalid_response")
	}
	if !strings.EqualFold(user.Login, expected) {
		return identityMismatch(expected, user.Login)
	}
	return nil
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
	defer func() { _ = resp.Body.Close() }()
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
	foundInstallation := false
	missingPermissions := map[string]bool{}
	for page := 1; page <= 10; page++ {
		var installs struct {
			Installations []struct {
				ID          int64             `json:"id"`
				AppID       int64             `json:"app_id"`
				Permissions map[string]string `json:"permissions"`
				Account     struct {
					Type  string `json:"type"`
					Login string `json:"login"`
				} `json:"account"`
			} `json:"installations"`
		}
		if err := g.call(ctx, "GET", fmt.Sprintf("/user/installations?per_page=100&page=%d", page), c.AccessToken, nil, nil, &installs); err != nil {
			return AccountInfo{}, errors.New("github_installation_verification_failed")
		}
		for _, install := range installs.Installations {
			if install.AppID != app.ID || install.ID <= 0 || (app.InstallationID != 0 && install.ID != app.InstallationID) {
				continue
			}
			foundInstallation = true
			permissionsApproved := true
			for name, level := range app.Permissions {
				if permissionCatalog[name].Scope == "account" || (permissionCatalog[name].Scope == "organization" && install.Account.Type == "User") {
					continue
				}
				actual := install.Permissions[name]
				if !permissionCovers(name, actual, level) {
					permissionsApproved = false
					if _, known := permissionCatalog[name]; known {
						missingPermissions[name] = true
					}
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
				return AccountInfo{Login: user.Login, AppID: app.ID, AppURL: app.HTMLURL, VerifiedAt: time.Now().UTC(), InstallationOwner: install.Account.Login}, nil
			}
		}
		if len(installs.Installations) < 100 {
			break
		}
	}
	if !foundInstallation {
		return AccountInfo{}, errors.New("github_app_has_no_accessible_installation")
	}
	if len(missingPermissions) > 0 {
		names := make([]string, 0, len(missingPermissions))
		for name := range missingPermissions {
			names = append(names, name)
		}
		sort.Strings(names)
		return AccountInfo{}, fmt.Errorf("github_installation_missing_requested_permissions: %s", strings.Join(names, ", "))
	}
	return AccountInfo{}, errors.New("github_app_installation_has_no_accessible_repositories")
}

// SetupFlow is served only by a short-lived IPv4 loopback listener. All secret
// material stays in memory until the protected helper import. Host validation
// and unguessable paths prevent DNS rebinding and blind loopback callbacks.
type SetupFlow struct {
	initialURL                                string
	mu                                        sync.Mutex
	base, prefix, state, oauthState, verifier string
	appName                                   string
	githubUser                                string
	createdAppURL                             string
	permissions                               map[string]string
	provider                                  SetupProvider
	store                                     func(context.Context, Credentials, AccountInfo) error
	phase                                     string
	app                                       AppRegistration
	done                                      chan error
	installationStarted                       func(context.Context) error
	pending                                   *setupAuthorization
}

type setupAuthorization struct {
	app         AppRegistration
	credentials Credentials
}

func randomURLToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil { // coverage-ignore: crypto/rand.Read never returns an error on the pinned Go version.
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

var githubHandle = regexp.MustCompile(`^[a-zA-Z0-9]+(-[a-zA-Z0-9]+)*$`)

// GitHub limits app names to 34 characters, including our prefix.
func GitHubAppName(handle string) (string, error) {
	handle = strings.TrimPrefix(strings.TrimSpace(handle), "@")
	if len(handle) > 39 || !githubHandle.MatchString(handle) {
		return "", errors.New("invalid_GitHub_handle")
	}
	name := "passlet-" + strings.ToLower(handle)
	if len(name) > 34 {
		name = strings.TrimRight(name[:34], "-")
	}
	return name, nil
}

func NewSetupFlow(base, githubUser string, permissions map[string]string, provider SetupProvider, store func(context.Context, Credentials, AccountInfo) error) (*SetupFlow, error) {
	appName, err := GitHubAppName(githubUser)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("setup_requires_loopback_origin")
	}
	// App limits are independent of the smaller per-request native prompt limit.
	if len(permissions) == 0 {
		return nil, errors.New("invalid_permissions")
	}
	appPermissions := make(map[string]string, len(permissions)+1)
	for name, level := range permissions {
		if reason, excluded := manifestExcludedPermissions[name]; excluded {
			return nil, fmt.Errorf("setup_permission_%s_%s", name, reason)
		}
		if !validPermission(name, level) {
			return nil, errors.New("invalid_permission")
		}
		appPermissions[name] = level
	}
	appPermissions["metadata"] = "read"
	tokens := make([]string, 4)
	for i := range tokens {
		tokens[i], err = randomURLToken()
		if err != nil { // coverage-ignore: randomURLToken uses infallible crypto/rand.Read on the pinned Go version.
			return nil, err
		}
	}
	return &SetupFlow{githubUser: strings.TrimPrefix(strings.TrimSpace(githubUser), "@"), appName: appName, base: base, prefix: "/" + tokens[0], state: tokens[1], oauthState: tokens[2], verifier: tokens[3], permissions: appPermissions, provider: provider, store: store, phase: "register", done: make(chan error, 1), installationStarted: nil}, nil
}

func (s *SetupFlow) URL() string { return s.base + s.prefix + "/start" }

// Setup creates personally owned apps, whose settings use the app's slug.
func GitHubAppSettingsURL(appURL string) string {
	u, err := url.Parse(appURL)
	if err == nil && u.Scheme == "https" && u.Host == "github.com" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && strings.HasPrefix(u.Path, "/apps/") {
		slug := strings.TrimPrefix(u.Path, "/apps/")
		if component.MatchString(slug) {
			return "https://github.com/settings/apps/" + slug
		}
	}
	return "https://github.com/settings/apps"
}

func (s *SetupFlow) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", setupContentSecurityPolicy)
	if r.Host != strings.TrimPrefix(s.base, "http://") || r.Method != "GET" || len(r.URL.RawQuery) > 2048 {
		http.Error(w, "Invalid setup request", http.StatusBadRequest)
		return
	}
	if r.URL.Path == "/favicon.png" {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(assets.FaviconPNG)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	fail := func(err error) {
		s.phase = "failed"
		s.app = AppRegistration{}
		s.pending = nil
		failure := setupFailure(err, s.createdAppURL)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_ = setupFailurePage.Execute(w, strings.Split(failure.Browser, "\n"))
		err = failure
		select {
		case s.done <- err:
		default: // coverage-ignore: Defensive nonblocking send; phase validation prevents a second failure notification.
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
			http.Error(w, "Setup already started", http.StatusConflict)
			return
		}
		manifest, _ := json.Marshal(map[string]any{
			"name": s.appName, "url": "https://passlet.dev", "public": true,
			"hook_attributes": map[string]any{"url": "https://passlet.dev", "active": false},
			"redirect_url":    s.base + s.prefix + "/created", "callback_urls": []string{s.base + s.prefix + "/authorized"},
			"request_oauth_on_install": false,
			"setup_url":                s.base + "/github/installed", "setup_on_update": true,
			"default_permissions": manifestPermissions(s.permissions), "default_events": []string{},
		})
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = setupPage.Execute(w, map[string]any{"Manifest": string(manifest), "Action": "https://github.com/settings/apps/new?state=" + s.state})
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
		s.createdAppURL = app.HTMLURL
		if !strings.EqualFold(app.Owner.Login, s.githubUser) {
			fail(identityMismatch(s.githubUser, app.Owner.Login))
			return
		}
		permissions, err := normalizeRegistrationPermissions(app.Permissions)
		if err != nil {
			fail(err)
			return
		}
		app.Permissions = permissions
		for name, level := range s.permissions {
			if app.Permissions[name] != level {
				// Only the locally validated requested name enters diagnostics.
				fail(fmt.Errorf("app_permissions_differ_from_setup_request: %s", name))
				return
			}
		}
		if len(app.Permissions) != len(s.permissions) {
			fail(errors.New("app_permissions_differ_from_setup_request: unexpected_permissions"))
			return
		}
		s.app = app
		s.phase = "install"
		http.Redirect(w, r, s.prefix+"/authorize?state="+s.oauthState, http.StatusSeeOther)
	case s.prefix + "/authorize":
		if s.phase != "install" || !validState("state", s.oauthState) {
			http.Error(w, "Invalid or replayed authorization request", http.StatusBadRequest)
			return
		}
		// Continue starts authorization, not proof of installation. VerifySetup
		// checks the user's actual installation and repository access afterward.
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

		if err := s.provider.VerifyIdentity(r.Context(), c, s.githubUser); err != nil {
			fail(err)
			return
		}
		s.pending = &setupAuthorization{app: s.app, credentials: c}
		if s.installationStarted != nil {
			if err := s.installationStarted(r.Context()); err != nil {
				fail(err)
				return
			}
		}
		s.app = AppRegistration{}
		s.phase = "authorized"
		http.Redirect(w, r, s.pending.app.HTMLURL+"/installations/new", http.StatusSeeOther)

	case "/github/installed":
		id, err := strconv.ParseInt(params.Get("installation_id"), 10, 64)
		if s.phase != "authorized" || s.pending == nil || err != nil || id <= 0 || len(params["installation_id"]) != 1 {
			http.Error(w, "Invalid or unexpected installation callback", http.StatusBadRequest)
			return
		}
		s.phase = "verifying"
		authorization := *s.pending
		s.pending = nil
		authorization.app.InstallationID = id
		account, err := s.provider.VerifySetup(r.Context(), authorization.app, authorization.credentials)
		if err != nil {
			fail(err)
			return
		}
		if r.Context().Err() != nil {
			fail(errors.New("setup_cancelled"))
			return
		}
		authorization.credentials.Account = account
		if err := s.store(r.Context(), authorization.credentials, account); err != nil {
			fail(err)
			return
		}
		s.phase = "complete"
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = setupCompletePage.Execute(w, map[string]string{"SettingsURL": GitHubAppSettingsURL(authorization.app.HTMLURL)})
		s.done <- nil
	default:
		http.NotFound(w, r)
	}
}

func RunSetup(ctx context.Context, listener net.Listener, githubUser string, permissions map[string]string, provider SetupProvider, store func(context.Context, Credentials, AccountInfo) error, open func(string) error, installationStarted func(context.Context) error) error {
	return serveSetupBrowser(ctx, listener, func(base string) (*SetupFlow, error) {
		return NewSetupFlow(base, githubUser, permissions, provider, store)
	}, open, installationStarted)
}

// Reopening installation uses identity retrieved from the protected credential item.
func RunInstallationSetup(ctx context.Context, listener net.Listener, credentials Credentials, provider SetupProvider, verified func(context.Context, AccountInfo) error, open func(string) error) error {
	if err := credentials.Account.Validate(); err != nil {
		return err
	}
	if err := credentials.Validate(); err != nil {
		return err
	}
	return serveSetupBrowser(ctx, listener, func(base string) (*SetupFlow, error) {
		account := credentials.Account
		flow, err := NewSetupFlow(base, account.Login, map[string]string{"metadata": "read"}, provider, func(ctx context.Context, _ Credentials, result AccountInfo) error {
			return verified(ctx, result)
		})
		if err != nil {
			return nil, err
		}
		app := AppRegistration{ID: account.AppID, HTMLURL: account.AppURL, Permissions: map[string]string{"metadata": "read"}}
		app.Owner.Login = account.Login
		flow.pending = &setupAuthorization{app: app, credentials: credentials}
		flow.phase = "authorized"
		flow.initialURL = account.AppURL + "/installations/new"
		return flow, nil
	}, open, nil)
}

func runSetupBrowser(ctx context.Context, address string, create func(string) (*SetupFlow, error), open func(string) error, installationStarted func(context.Context) error) error {
	l, err := net.Listen("tcp4", address)
	if err != nil {
		return err
	}
	return serveSetupBrowser(ctx, l, create, open, installationStarted)
}

func serveSetupBrowser(ctx context.Context, l net.Listener, create func(string) (*SetupFlow, error), open func(string) error, installationStarted func(context.Context) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer func() { _ = l.Close() }()
	flow, err := create("http://" + l.Addr().String())
	if err != nil {
		return err
	}
	flow.installationStarted = installationStarted
	server := &http.Server{Handler: flow, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 90 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 8192, BaseContext: func(net.Listener) context.Context { return ctx }}
	defer func() { _ = server.Close() }()
	go func() { _ = server.Serve(l) }()
	startURL := flow.URL()
	if flow.initialURL != "" {
		startURL = flow.initialURL
	}
	if err := open(startURL); err != nil {
		return errors.New("setup_browser_unavailable")
	}
	select {
	case err := <-flow.done:
		// Let the final browser response flush before closing active connections.
		shutdown, done := context.WithTimeout(context.Background(), time.Second)
		defer done()
		_ = server.Shutdown(shutdown)
		return err
	case <-ctx.Done():
		return errors.New("setup_cancelled_restart_setup")
	}
}
