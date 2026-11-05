package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"passlet.dev/passlet/internal/passlet"
)

// This fixture exists only in the test executable. Production still launches
// the signed helper and uses its real JSON-over-pipes protocol.
func TestSetupHelperFixture(t *testing.T) {
	mode := os.Getenv("PASSLET_SETUP_FIXTURE")
	if mode == "" {
		return
	}
	op := os.Args[len(os.Args)-1]
	if op == "--status" {
		state := "missing"
		if mode == "existing" || mode == "revoked" {
			state = "configured"
		}
		if mode == "protected" {
			state = "protected_or_unavailable"
		}
		fmt.Printf("{\"status\":%q}\n", state)
		os.Exit(0)
	}
	if mode == "existing" || mode == "revoked" || mode == "protected" {
		scanner := bufio.NewScanner(os.Stdin)
		if !scanner.Scan() {
			os.Exit(2)
		}
		if mode == "protected" {
			fmt.Println(`{"ok":false,"error":"approval_denied"}`)
			os.Exit(0)
		}
		credentials := passlet.Credentials{ClientID: "fixture-client", ClientSecret: "fixture-client-secret", AccessToken: "fixture-parent-secret", RefreshToken: "fixture-refresh-secret", ExpiresAt: time.Now().Add(time.Hour), Account: passlet.AccountInfo{Login: "alice", AppID: 123, AppURL: "https://github.com/apps/passlet-alice", VerifiedAt: time.Now()}}
		json.NewEncoder(os.Stdout).Encode(map[string]any{"ok": true, "credentials": credentials})
		for scanner.Scan() {
			var msg struct {
				Op string `json:"op"`
			}
			if json.Unmarshal(scanner.Bytes(), &msg) != nil || msg.Op != "delete" {
				os.Exit(5)
			}
			if err := os.Remove(os.Getenv("PASSLET_SETUP_MARKER")); err != nil {
				os.Exit(6)
			}
			fmt.Println(`{"ok":true}`)
		}
		os.Exit(0)
	}
	if op != "--import" {
		os.Exit(2)
	}
	var request struct {
		Credentials passlet.Credentials `json:"credentials"`
	}
	if json.NewDecoder(os.Stdin).Decode(&request) != nil || request.Credentials.Validate() != nil || request.Credentials.Account.Validate() != nil || request.Credentials.Account.Login != "alice" || request.Credentials.AccessToken != "fixture-parent-secret" {
		os.Exit(3)
	}
	if mode == "storage_failure" {
		fmt.Println(`{"ok":false,"error":"fixture-parent-secret"}`)
		os.Exit(1)
	}
	if err := os.WriteFile(os.Getenv("PASSLET_SETUP_MARKER"), []byte("imported"), 0600); err != nil {
		os.Exit(4)
	}
	fmt.Println(`{"ok":true}`)
	os.Exit(0)
}

// setupFixture wires the real CLI and GitHub client to disposable test resources.
// Only the signed Swift process, browser/user actions, and GitHub are simulated.
type setupFixture struct {
	hostOS              string
	listenerStarts      int
	command             string
	prompts             int
	suggestedHandle     string
	installCallback     string
	handle              string
	startURL            string
	pageHTML            string
	browserHTML         string
	lastURL             string
	promptInitial       string
	t                   *testing.T
	mode                string
	dir, helper, marker string
	github              *setupGitHubFixture
	ctx                 context.Context
	cancel              context.CancelFunc
	output              bytes.Buffer
	opens               int
	action              func(string) error
}

type setupResult struct {
	keychain     string
	err          error
	output       string
	imported     bool
	accountSaved bool
	exchanges    int32
	browserOpens int
}

func newSetupFixture(t *testing.T, mode string) *setupFixture {
	t.Helper()
	f := &setupFixture{hostOS: "darwin", t: t, mode: mode, dir: t.TempDir(), handle: "alice", command: "setup"}
	if err := os.Chmod(f.dir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", f.dir)
	f.marker = filepath.Join(f.dir, "imported")
	f.helper = f.writeHelperLauncher()
	if mode == "existing" || mode == "protected" || mode == "revoked" {
		os.WriteFile(f.marker, []byte("existing"), 0600)
	}

	f.github = newSetupGitHubFixture(t, mode)
	f.ctx, f.cancel = context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(f.cancel)
	return f
}

func (f *setupFixture) writeHelperLauncher() string {
	f.t.Helper()
	self, err := os.Executable()
	if err != nil {
		f.t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	script := "#!/bin/sh\n: > " + quote(filepath.Join(f.dir, "helper-invoked")) + "\nGORACE=atexit_sleep_ms=0 PASSLET_SETUP_FIXTURE=" + quote(f.mode) + " PASSLET_SETUP_MARKER=" + quote(f.marker) + " exec " + quote(self) + " -test.run='^TestSetupHelperFixture$' -- \"$@\"\n"
	path := filepath.Join(f.dir, "helper")
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		f.t.Fatal(err)
	}
	return path
}

func (f *setupFixture) run() setupResult {
	f.t.Helper()
	err := setupWithDependencies(f.ctx, []string{f.command}, setupDependencies{
		hostOS: f.hostOS,
		listen: func(network, address string) (net.Listener, error) {
			f.listenerStarts++
			return net.Listen(network, address)
		},
		input: bufio.NewReader(strings.NewReader("")), output: &f.output,
		approve: func(ctx context.Context, prompt string) (passlet.CredentialSession, error) {
			if f.action != nil {
				if err := f.action("keychain"); err != nil {
					return nil, err
				}
			}
			return (passlet.NativeHelper{Path: f.helper}).Approve(ctx, prompt)
		},
		suggest: func(context.Context) string { return f.suggestedHandle },
		prompt: func(prompt, initial string) (string, error) {
			f.prompts++
			fmt.Fprintln(&f.output, prompt+initial)
			if f.action == nil {
				if prompt != "" {
					return "alice", nil
				}
				if f.mode == "input_closed" {
					return "", errors.New("setup_terminal_confirmation_required")
				}
				return "", nil
			}
			if prompt != "" {
				f.promptInitial = initial
				if err := f.action("handle"); err != nil {
					return "", err
				}
				return f.handle, nil
			}
			return "", f.action("browser")
		},
		helper: f.helper, provider: f.github.client(), open: f.openBrowser,
	})
	_, importErr := os.Stat(f.marker)
	if importErr != nil && !os.IsNotExist(importErr) {
		f.t.Fatal(importErr)
	}

	marker, _ := os.ReadFile(f.marker)
	return setupResult{keychain: string(marker), err: err, output: f.output.String(), imported: string(marker) == "imported",
		accountSaved: string(marker) == "imported",
		exchanges:    f.github.exchanges.Load(), browserOpens: f.opens}
}

// A local HTTP server exercises production request encoding and response parsing.
type setupGitHubFixture struct {
	t                        *testing.T
	mode                     string
	owner                    string
	server                   *httptest.Server
	exchanges, verifications atomic.Int32
	requests                 atomic.Int32
	lookupName               atomic.Value
}

func newSetupGitHubFixture(t *testing.T, mode string) *setupGitHubFixture {
	t.Helper()
	f := &setupGitHubFixture{t: t, mode: mode, owner: "ACME"}
	f.server = httptest.NewServer(http.HandlerFunc(f.serveHTTP))
	t.Cleanup(f.server.Close)
	return f
}

func (f *setupGitHubFixture) client() *passlet.GitHub {
	return &passlet.GitHub{HTTP: f.server.Client(), API: f.server.URL, OAuth: f.server.URL + "/oauth"}
}

func (f *setupGitHubFixture) serveHTTP(w http.ResponseWriter, r *http.Request) {
	f.requests.Add(1)
	w.Header().Set("Content-Type", "application/json")
	permissions := map[string]string{}
	for _, permission := range strings.Split(passlet.DefaultSetupPermissions, ",") {
		name, level, _ := strings.Cut(permission, ":")
		permissions[name] = level
	}
	encoded, _ := json.Marshal(permissions)
	switch r.URL.Path {
	case "/apps/passlet-alice", "/apps/passlet-bob":
		f.lookupName.Store(strings.TrimPrefix(r.URL.Path, "/apps/"))
		if f.mode == "name_conflict" {
			io.WriteString(w, `{"id":123}`)
		} else {
			http.NotFound(w, r)
		}
	case "/app-manifests/fixture-code/conversions":
		if r.Method != "POST" {
			f.t.Error("wrong conversion method")
		}
		fmt.Fprintf(w, `{"id":123,"client_id":"fixture-client","client_secret":"fixture-client-secret","html_url":"https://github.com/apps/passlet-alice","owner":{"login":"alice"},"permissions":%s,"pem":"fixture-unused-private-key"}`, encoded)
	case "/oauth":
		f.exchanges.Add(1)
		if err := r.ParseForm(); err != nil {
			f.t.Error(err)
		}
		if r.Form.Get("code_verifier") == "" || r.Form.Get("client_secret") != "fixture-client-secret" {
			f.t.Error("missing protected OAuth exchange parameters")
		}
		io.WriteString(w, `{"access_token":"fixture-parent-secret","refresh_token":"fixture-refresh-secret","expires_in":28800,"token_type":"bearer"}`)
	case "/user":
		f.verifications.Add(1)
		if r.Header.Get("Authorization") != "Bearer fixture-parent-secret" {
			f.t.Error("wrong authorization")
		}
		if f.mode == "revoked" {
			w.WriteHeader(401)
			return
		}
		login := "alice"
		if f.mode == "wrong_account" {
			login = "bob"
		}
		fmt.Fprintf(w, `{"login":%q}`, login)
	case "/user/installations":
		if f.mode == "missing_installation" {
			io.WriteString(w, `{"installations":[]}`)
			return
		}
		fmt.Fprintf(w, `{"installations":[{"id":1,"app_id":123,"account":{"type":"Organization","login":%q},"permissions":%s}]}`, f.owner, encoded)
	case "/user/installations/1/repositories":
		if f.mode == "no_repositories" {
			io.WriteString(w, `{"repositories":[]}`)
			return
		}
		io.WriteString(w, `{"repositories":[{"full_name":"ACME/storefront"}]}`)
	default:
		f.t.Error("unexpected GitHub endpoint", r.URL.Path)
		http.NotFound(w, r)
	}
}

// Follow local callbacks as a browser would, but never contact real GitHub.
func (f *setupFixture) openBrowser(u string) error {
	f.opens++
	f.lastURL = u
	if f.mode == "existing" {
		if f.action != nil {
			if err := f.action("install"); err != nil {
				return err
			}
		}
		if u != "https://github.com/apps/passlet-alice/installations/new" {
			f.t.Error("wrong existing app URL")
		}
		f.installCallback = "http://127.0.0.1:17402/github/installed?installation_id=1"
		resp, err := http.Get(f.installCallback)
		if err != nil {
			return err
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		f.browserHTML = string(body)
		if resp.StatusCode != 200 {
			return errors.New("existing installation callback failed")
		}
		return nil
	}
	if f.mode == "browser_failure" {
		f.cancel()
		return errors.New("fixture-browser-secret")
	}

	f.startURL = u

	// Script only localhost callbacks; never follow a link to real GitHub.
	resp, err := http.Get(u)
	if err != nil {
		return err
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	f.pageHTML = string(body)
	matches := regexp.MustCompile(`action="([^"]+)"`).FindSubmatch(body)
	if matches == nil {
		return errors.New("missing_manifest_form")
	}
	action, _ := url.Parse(html.UnescapeString(string(matches[1])))
	base := strings.TrimSuffix(u, "/start")
	local, _ := url.Parse(u)
	browserClient := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Host != local.Host {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	if f.action != nil {
		if err := f.action("create"); err != nil {
			return err
		}
	}
	resp, err = browserClient.Get(base + "/created?code=fixture-code&state=" + action.Query().Get("state"))
	if err != nil {
		return err
	}
	auth, _ := url.Parse(resp.Header.Get("Location"))
	resp.Body.Close()
	if auth.Host != "github.com" || auth.Query().Get("code_challenge_method") != "S256" {
		return errors.New("invalid_authorization_redirect")
	}
	callback := auth.Query().Get("redirect_uri") + "?state=" + auth.Query().Get("state")
	if f.action != nil {
		if err := f.action("authorize"); err != nil {
			return err
		}
	}
	if f.mode == "oauth_denied" {
		callback += "&error=access_denied"
	} else {
		callback += "&code=fixture-oauth-code"
	}
	resp, err = browserClient.Get(callback)
	if err != nil {
		return err
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(body), "fixture-parent-secret") {
		f.t.Error("browser leaked credential")
	}
	if f.mode == "oauth_denied" || f.mode == "wrong_account" {
		f.browserHTML = string(body)
		return nil
	}
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "https://github.com/apps/passlet-alice/installations/new" {
		return errors.New("authorization did not redirect to installation")
	}
	f.lastURL = resp.Header.Get("Location")
	if f.github.verifications.Load() != 1 {
		f.t.Error("identity must be verified before installation callback")
	}
	if f.action != nil {
		if err := f.action("install"); err != nil {
			return err
		}
	}
	if f.mode == "cancel" {
		f.cancel()
		return nil
	}
	f.installCallback = "http://127.0.0.1:17402/github/installed?installation_id=1"
	resp, err = browserClient.Get(f.installCallback)
	if err != nil {
		return err
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	f.browserHTML = string(body)
	return nil
}
