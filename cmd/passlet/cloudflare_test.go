package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"passlet.dev/passlet/internal/passlet"
)

type cfSetupVerifier struct {
	err          error
	calls        int
	refreshError error
	refreshed    bool
}

func (v *cfSetupVerifier) Verify(_ context.Context, token string) (passlet.Credentials, error) {
	v.calls++
	return passlet.Credentials{Provider: "cloudflare", AccessToken: token, CloudflareTokenID: strings.Repeat("a", 32)}, v.err
}

func (v *cfSetupVerifier) RefreshResources(_ context.Context, c passlet.Credentials) error {
	v.refreshed = true
	if c.Provider != "cloudflare" {
		panic("wrong provider")
	}
	return v.refreshError
}

func TestCloudflareSetup(t *testing.T) {
	for _, mode := range []string{"success", "browser failure", "cancel", "empty token", "verification failure", "storage failure", "discovery failure", "pairing failure", "reset", "reset failure", "missing reset", "status failure", "linux", "relative helper", "positional", "unknown flag", "unknown command", "help", "setup help"} {
		t.Run(mode, func(t *testing.T) {
			var out bytes.Buffer
			verifier := &cfSetupVerifier{}
			stored, paired, reset := false, false, false
			d := cloudflareSetupDependencies{hostOS: "darwin", helper: "/fixture/helper", out: &out, provider: verifier,
				status: func(context.Context, string) (string, error) { return "missing", nil },
				prompt: func() (string, error) { return " parent-secret ", nil }, open: func(url string) error {
					if url != "https://dash.cloudflare.com/profile/api-tokens" {
						t.Fatal(url)
					}
					return nil
				},
				store: func(_ context.Context, path string, c passlet.Credentials) error {
					stored = true
					if c.Provider != "cloudflare" || c.AccessToken != "parent-secret" || path != "/fixture/helper" {
						t.Fatal("incorrect import")
					}
					return nil
				},
				pair: func() error { paired = true; return nil }, reset: func(context.Context, string) error { reset = true; return nil },
			}
			args := []string{"setup"}
			wantError := true
			switch mode {
			case "success":
				wantError = false
			case "browser failure":
				wantError = false
				d.open = func(string) error { return errors.New("no browser") }
			case "cancel":
				d.prompt = func() (string, error) { return "", errors.New("parent-secret") }
			case "empty token":
				d.prompt = func() (string, error) { return " ", nil }
			case "verification failure":
				verifier.err = errors.New("cloudflare_http_401")
			case "storage failure":
				d.store = func(context.Context, string, passlet.Credentials) error { return errors.New("keychain unavailable") }
			case "discovery failure":
				verifier.refreshError = errors.New("discovery failed")
			case "pairing failure":
				d.pair = func() error { return errors.New("pair failed") }
			case "status failure":
				d.status = func(context.Context, string) (string, error) { return "", errors.New("helper unavailable") }
			case "missing reset":
				args = []string{"reset"}
				wantError = false
			case "reset":
				d.status = func(context.Context, string) (string, error) { return "configured", nil }
				args = []string{"reset"}
				wantError = false
			case "reset failure":
				d.status = func(context.Context, string) (string, error) { return "protected_or_unavailable", nil }
				args = []string{"reset"}
				d.reset = func(context.Context, string) error { return errors.New("denied") }
			case "linux":
				d.hostOS = "linux"
			case "relative helper":
				d.helper = "relative"
			case "positional":
				args = append(args, "extra")
			case "unknown flag":
				args = append(args, "--unknown")
			case "unknown command":
				args = []string{"unknown"}
			case "help":
				args = nil
				wantError = false
			case "setup help":
				args = append(args, "--help")
			}
			err := cloudflareSetup(context.Background(), args, d)
			if (err != nil) != wantError {
				t.Fatal(mode, err)
			}
			if strings.Contains(out.String(), "parent-secret") || err != nil && strings.Contains(err.Error(), "parent-secret") {
				t.Fatal("secret exposed")
			}
			if mode == "success" && (!stored || !paired || reset || verifier.calls != 1) {
				t.Fatal("incomplete setup")
			}
			if mode == "reset" && (!reset || stored || paired || verifier.calls != 0) {
				t.Fatal("reset touched other credentials")
			}
			if mode == "missing reset" && reset {
				t.Fatal("missing reset requested approval")
			}
			if mode == "verification failure" && stored || mode == "storage failure" && paired {
				t.Fatal("continued failed setup")
			}
		})
	}
}

func TestCloudflareExec(t *testing.T) {
	for _, name := range []string{"CF_API_EMAIL", "CF_API_KEY", "CF_API_TOKEN", "CF_ACCOUNT_ID", "CLOUDFLARE_API_EMAIL", "CLOUDFLARE_API_KEY", "CLOUDFLARE_API_TOKEN", "CLOUDFLARE_ACCOUNT_ID"} {
		t.Setenv(name, "ambient-secret")
	}
	t.Setenv("PASSLET_ENDPOINT", "unix:///missing/approver.sock")
	for _, account := range []string{"", strings.Repeat("b", 32)} {
		grant := passlet.Grant{Provider: "cloudflare", Token: "child-secret", Account: account, ExpiresAt: time.Now().Add(time.Hour)}
		script := `test "$CLOUDFLARE_API_TOKEN" = child-secret && test "${CF_API_TOKEN+x}" != x && test "${CF_API_KEY+x}" != x && test "${CLOUDFLARE_API_KEY+x}" != x && test "${CF_API_EMAIL+x}" != x && test "${CLOUDFLARE_API_EMAIL+x}" != x && test "${CF_ACCOUNT_ID+x}" != x`
		if account != "" {
			script += ` && test "$CLOUDFLARE_ACCOUNT_ID" = ` + account
		} else {
			script += ` && test "${CLOUDFLARE_ACCOUNT_ID+x}" != x`
		}
		if err := execCloudflare(context.Background(), grant, []string{"sh", "-c", script}); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(grant)
		path := filepath.Join(t.TempDir(), "grant.json")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := invokeCLI(t, "", "exec", "--grant", path, "--", "sh", "-c", script); err != nil {
			t.Fatal(err)
		}
	}
	if err := execCloudflare(context.Background(), passlet.Grant{}, []string{"sh", "-c", "exit 1"}); err == nil {
		t.Fatal("lost exit failure")
	}
	grant := passlet.Grant{Provider: "unknown", Token: "secret", ExpiresAt: time.Now().Add(time.Hour)}
	data, _ := json.Marshal(grant)
	path := filepath.Join(t.TempDir(), "grant.json")
	os.WriteFile(path, data, 0600)
	if _, err := invokeCLI(t, "", "exec", "--grant", path, "--", "true"); err == nil {
		t.Fatal("accepted unknown provider")
	}
}

func TestCloudflareCLIRequest(t *testing.T) {
	for _, args := range [][]string{
		{"approver", "cloudflare", "--help"},
		{"request", "--provider", "cloudflare", "--zones", strings.Repeat("a", 32), "--permissions", "dns:write", "--endpoint", "invalid"},
		{"request", "--provider", "github", "--repos", "alice/repo", "--permissions", "contents:read", "--endpoint", "invalid"},
	} {
		_, err := invokeCLI(t, "", args...)
		if args[0] == "approver" {
			if err != nil {
				t.Fatal(err)
			}
		} else if err == nil {
			t.Fatal("invalid endpoint accepted")
		}
	}
}

func TestCloudflareAuditFormatting(t *testing.T) {
	for _, account := range []string{"", "account-id"} {
		text := formatAudit([]passlet.AuditEntry{{Event: passlet.Event{Request: passlet.Request{Provider: "cloudflare", Account: account, Zones: []string{"zone-id"}}}}})
		if !strings.Contains(text, "Cloudflare") || account == "" && !strings.Contains(text, "zone-id") || account != "" && !strings.Contains(text, account) {
			t.Fatal(text)
		}
	}
}

func TestCloudflareDoctorCLI(t *testing.T) {
	for _, mode := range []string{"remote", "local", "missing", "revoked", "denied"} {
		t.Run(mode, func(t *testing.T) {
			_, _ = approverTestPaths(t)
			path := passlet.DefaultSocket()
			listener, err := passlet.ListenUnix(path)
			if err != nil {
				t.Fatal(err)
			}
			requests := 0
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/health" {
					fmt.Fprint(w, `{"status":"ready"}`)
					return
				}
				if r.URL.Path != "/v1/doctor" {
					t.Error("doctor requested a grant")
					w.WriteHeader(http.StatusNotFound)
					return
				}
				requests++
				var body map[string]string
				if json.NewDecoder(r.Body).Decode(&body) != nil || len(body) != 1 || body["provider"] != "cloudflare" {
					t.Error("wrong diagnostic request")
				}
				switch mode {
				case "missing":
					w.WriteHeader(http.StatusForbidden)
					fmt.Fprint(w, `{"error":"credentials_missing"}`)
				case "revoked":
					w.WriteHeader(http.StatusBadGateway)
					fmt.Fprint(w, `{"error":"cloudflare_credentials_verification_failed"}`)
				case "denied":
					w.WriteHeader(http.StatusForbidden)
					fmt.Fprint(w, `{"error":"approval_denied"}`)
				default:
					fmt.Fprintf(w, `{"provider":"cloudflare","cloudflare_token_id":%q}`, strings.Repeat("a", 32))
				}
			})}
			listener = pairedFixture(t, server, listener)
			go func() { _ = server.Serve(listener) }()
			defer server.Close()
			t.Setenv("PASSLET_ENDPOINT", "unix:///missing.sock")
			args := []string{"doctor", "--provider", "cloudflare", "--endpoint", "unix://" + path}
			if mode == "local" {
				args = []string{"approver", "doctor", "--provider", "cloudflare"}
			}
			output, err := invokeCLI(t, "", args...)
			if mode == "remote" || mode == "local" {
				if err != nil || !strings.Contains(output, "Cloudflare token-creation authority: valid") || !strings.Contains(output, "Cloudflare parent token: "+strings.Repeat("a", 32)) {
					t.Fatal(output, err)
				}
			} else {
				if err == nil || strings.Contains(output, "authority: valid") {
					t.Fatal("failure reported success")
				}
				if mode == "missing" && !strings.Contains(err.Error(), "passlet approver cloudflare setup") {
					t.Fatal(err)
				}
			}
			if requests != 1 || strings.Contains(output, "GitHub") {
				t.Fatal("incorrect diagnostic routing", output)
			}
		})
	}
	for _, args := range [][]string{{"doctor", "--provider", "unknown"}, {"approver", "doctor", "--provider", "unknown"}, {"doctor", "--provider", "github", "--endpoint", "invalid"}} {
		if _, err := invokeCLI(t, "", args...); err == nil {
			t.Fatal("invalid diagnostic arguments accepted")
		}
	}
	if err := diagnoseEndpoint(context.Background(), "invalid", "linux", "github"); err == nil {
		t.Fatal("invalid endpoint accepted")
	}
	for _, code := range []string{"cloudflare_credentials_verification_failed", "provider_unavailable", "credentials_missing", "invalid_credentials"} {
		message := doctorFailureMessage(errors.New(code), "cloudflare")
		if !strings.Contains(message, "Cloudflare") || strings.Contains(message, "github") || strings.Contains(message, "GitHub") {
			t.Fatal(message)
		}
	}
	text := formatAudit([]passlet.AuditEntry{{Event: passlet.Event{Operation: "doctor", Request: passlet.Request{Provider: "cloudflare"}}}})
	if !strings.Contains(text, "Cloudflare token-creation authority check") {
		t.Fatal(text)
	}
}

type cfSetupSession struct {
	credentials passlet.Credentials
	closed      bool
}

func (s *cfSetupSession) Credentials() passlet.Credentials { return s.credentials }
func (s *cfSetupSession) Store(passlet.Credentials) error {
	panic("setup must not replace saved credentials")
}
func (s *cfSetupSession) Close() { s.closed = true }

func TestCloudflareSetupReusesCredential(t *testing.T) {
	for _, mode := range []string{"configured", "protected_or_unavailable", "denied", "revoked", "wrong token", "discovery failure", "pair failure"} {
		t.Run(mode, func(t *testing.T) {
			var out bytes.Buffer
			verifier := &cfSetupVerifier{}
			session := &cfSetupSession{credentials: passlet.Credentials{Provider: "cloudflare", AccessToken: "saved-secret", CloudflareTokenID: strings.Repeat("a", 32)}}
			paired := false
			d := cloudflareSetupDependencies{
				hostOS: "darwin", helper: "/fixture/helper", out: &out, provider: verifier,
				status: func(context.Context, string) (string, error) { return mode, nil },
				approve: func(_ context.Context, path string) (passlet.CredentialSession, error) {
					if path != "/fixture/helper" {
						t.Fatal(path)
					}
					if mode == "denied" {
						return nil, errors.New("denied")
					}
					return session, nil
				},
				pair: func() error {
					if mode == "pair failure" {
						return errors.New("pair failed")
					}
					paired = true
					return nil
				},
			}
			if mode == "discovery failure" {
				verifier.refreshError = errors.New("discovery failed")
			}
			if mode == "revoked" {
				verifier.err = errors.New("cloudflare_http_401")
			}
			if mode == "wrong token" {
				session.credentials.CloudflareTokenID = strings.Repeat("b", 32)
			}
			err := cloudflareSetup(context.Background(), []string{"setup"}, d)
			wantSuccess := mode == "configured" || mode == "protected_or_unavailable"
			if (err == nil) != wantSuccess || paired != wantSuccess || session.closed != (mode != "denied") {
				t.Fatalf("setup outcome: %v, paired=%v, closed=%v", err, paired, session.closed)
			}
			if strings.Contains(out.String(), "saved-secret") {
				t.Fatal("credential exposed")
			}
		})
	}
}
