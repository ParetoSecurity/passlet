package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"passlet.dev/passlet/internal/passlet"
)

func TestAccessFeatures(t *testing.T) {
	suite := godog.TestSuite{
		Options: &godog.Options{Format: "pretty", Paths: []string{"features/access.feature"}, Strict: true, TestingT: t},
		ScenarioInitializer: func(s *godog.ScenarioContext) {
			var dir, grantPath, input, output, errorsOutput string
			var cleanup func()
			var commandErr error
			var server *http.Server
			var requests atomic.Int32
			var expectedFailure bool
			s.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
				var err error
				dir, err = os.MkdirTemp("/tmp", "pl-access-")
				if err != nil {
					return ctx, err
				}
				grantPath = filepath.Join(dir, "grant.json")
				input, output = "", ""
				commandErr, server, cleanup = nil, nil, nil
				expectedFailure = false
				requests.Store(0)
				t.Setenv("HOME", dir)
				t.Setenv("PASSLET_ENDPOINT", "unix://"+filepath.Join(dir, "socket"))
				t.Setenv("CLAUDECODE", "1")
				t.Setenv("CODEX_THREAD_ID", "")
				t.Setenv("GH_TOKEN", "ambient-secret")
				t.Setenv("GITHUB_TOKEN", "ambient-secret")
				return ctx, nil
			})
			s.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
				if server != nil {
					_ = server.Close()
				}
				if cleanup != nil {
					cleanup()
				}
				err := os.RemoveAll(dir)
				if !expectedFailure && commandErr != nil {
					return ctx, fmt.Errorf("command failed: %w", commandErr)
				}
				return ctx, err
			})
			s.Step(`^an Approver with GitHub access for alice$`, func() error {
				api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					w.Header().Set("Content-Type", "application/json")
					if r.Method == "GET" && r.Header.Get("Authorization") != "Bearer fixture-parent-secret" {
						t.Error("provider lookup did not use the protected credential")
						w.WriteHeader(401)
						return
					}
					switch r.Method + " " + r.URL.Path {
					case "GET /user":
						fmt.Fprint(w, `{"login":"alice"}`)
					case "GET /user/installations":
						fmt.Fprint(w, `{"installations":[{"id":1,"account":{"login":"alice","type":"User"}}]}`)
					case "GET /user/installations/1/repositories":
						fmt.Fprint(w, `{"repositories":[{"id":42,"full_name":"alice/repo"}]}`)
					case "POST /applications/fixture-client/token/scoped":
						var body struct {
							AccessToken string            `json:"access_token"`
							Target      string            `json:"target"`
							IDs         []int             `json:"repository_ids"`
							Permissions map[string]string `json:"permissions"`
						}
						user, password, ok := r.BasicAuth()
						if json.NewDecoder(r.Body).Decode(&body) != nil || !ok || user != "fixture-client" || password != "fixture-client-secret" || body.AccessToken != "fixture-parent-secret" || body.Target != "alice" || !reflect.DeepEqual(body.IDs, []int{42}) || !reflect.DeepEqual(body.Permissions, map[string]string{"contents": "read", "metadata": "read"}) {
							t.Error("unexpected grant scope or provider authentication")
							w.WriteHeader(400)
							return
						}
						fmt.Fprintf(w, `{"token":"fixture-scoped-token","expires_at":%q,"user":{"login":"alice"},"installation":{"permissions":{"contents":"read","metadata":"read"},"repository_selection":"selected","account":{"login":"alice"}}}`, time.Now().Add(time.Hour).Format(time.RFC3339))
					default:
						t.Errorf("unexpected provider call: %s %s", r.Method, r.URL.Path)
						w.WriteHeader(404)
					}
				}))
				cleanup = api.Close
				audit, err := passlet.OpenAudit(filepath.Join(dir, "audit.sqlite"))
				if err != nil {
					return err
				}
				cleanup = func() { api.Close(); _ = audit.Close() }
				provider := &passlet.GitHub{HTTP: api.Client(), API: api.URL, OAuth: api.URL + "/unexpected-refresh"}
				handler := passlet.NewServer(passlet.NativeHelper{Path: filepath.Join(dir, "helper")}, provider, audit)
				listener, err := passlet.ListenUnix(filepath.Join(dir, "socket"))
				if err != nil {
					return err
				}
				server = &http.Server{Handler: handler, ConnContext: passlet.ConnectionContext}
				go func(srv *http.Server) { _ = srv.Serve(listener) }(server)
				child := `#!/bin/sh
: > "$HOME/child-started"
test "$GH_TOKEN" = fixture-scoped-token || exit 9
test "$GITHUB_TOKEN" = fixture-scoped-token || exit 10
printf 'Scoped credentials received\n'
cat
`
				return os.WriteFile(filepath.Join(dir, "check-access"), []byte(child), 0700)
			})
			s.Step(`^I (approve|deny) the access request$`, func(decision string) error {
				reply := []byte(`{"ok":false,"error":"approval_denied"}`)
				if decision == "approve" {
					credentials := passlet.Credentials{ClientID: "fixture-client", ClientSecret: "fixture-client-secret", AccessToken: "fixture-parent-secret", RefreshToken: "fixture-refresh-secret", ExpiresAt: time.Now().Add(time.Hour)}
					var err error
					reply, err = json.Marshal(map[string]any{"ok": true, "credentials": credentials})
					if err != nil {
						return err
					}
				}
				script := "#!/bin/sh\nIFS= read -r request || exit 1\nprintf '%s\\n' '" + string(reply) + "'\ncat >/dev/null\n"
				return os.WriteFile(filepath.Join(dir, "helper"), []byte(script), 0700)
			})
			s.Step(`^the command's standard input is "([^"]*)"$`, func(value string) { input = value + "\n" })
			s.Step("^I run `passlet ([^`]+)`$", func(command string) error {
				cli := fixture(t)
				launcher := "#!/bin/sh\nexec '" + strings.ReplaceAll(cli.Path, "'", "'\\''") + "' \"$@\"\n"
				if err := os.WriteFile(filepath.Join(dir, "passlet"), []byte(launcher), 0700); err != nil {
					return err
				}
				command = strings.ReplaceAll(command, "{grant}", grantPath)
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				defer cancel()
				cmd := exec.CommandContext(ctx, "bash", "--noprofile", "--norc", "-c", "passlet "+command)
				cmd.Env = append(cli.Env, "PATH="+dir+":"+os.Getenv("PATH"))
				cmd.Stdin = strings.NewReader(input)
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				data, err := cmd.Output()
				output, commandErr, errorsOutput = string(data), err, stderr.String()
				for _, secret := range []string{"fixture-scoped-token", "fixture-parent-secret", "fixture-client-secret", "fixture-refresh-secret", "ambient-secret"} {
					if strings.Contains(output+stderr.String(), secret) {
						return fmt.Errorf("credential leaked to terminal output")
					}
				}
				return nil
			})
			s.Step(`^a private grant is saved for alice/repo with contents:read$`, func() error {
				if commandErr != nil {
					return commandErr
				}
				data, err := os.ReadFile(grantPath)
				if err != nil {
					return err
				}
				var grant passlet.Grant
				if err := json.Unmarshal(data, &grant); err != nil {
					return err
				}
				info, err := os.Stat(grantPath)
				if err != nil {
					return err
				}
				if info.Mode().Perm() != 0600 || grant.Token != "fixture-scoped-token" || grant.ID == "" || grant.User != "alice" || !grant.ExpiresAt.After(time.Now()) || !reflect.DeepEqual(grant.Repositories, []string{"alice/repo"}) || !reflect.DeepEqual(grant.Permissions, map[string]string{"contents": "read", "metadata": "read"}) {
					return fmt.Errorf("incorrect saved grant or file permissions")
				}
				for _, secret := range []string{"fixture-parent-secret", "fixture-client-secret", "fixture-refresh-secret"} {
					if bytes.Contains(data, []byte(secret)) {
						return fmt.Errorf("protected credential leaked into grant")
					}
				}
				return nil
			})
			s.Step(`^the terminal shows:$`, func(doc *godog.DocString) error {
				if commandErr != nil || output != doc.Content+"\n" {
					return fmt.Errorf("output %q, error %v; want %q", output, commandErr, doc.Content+"\n")
				}
				return nil
			})
			s.Step(`^the Approver goes offline$`, func() error { return server.Close() })
			s.Step(`^the saved grant expires$`, func() error {
				if commandErr != nil {
					return commandErr
				}
				data, err := os.ReadFile(grantPath)
				if err != nil {
					return err
				}
				var grant passlet.Grant
				if err := json.Unmarshal(data, &grant); err != nil {
					return err
				}
				grant.ExpiresAt = time.Now().Add(-time.Hour)
				data, err = json.Marshal(grant)
				if err != nil {
					return err
				}
				return os.WriteFile(grantPath, data, 0600)
			})
			s.Step(`^the command fails without starting check-access$`, func() error {
				expectedFailure = true
				if commandErr == nil || output != "" {
					return fmt.Errorf("expected failure with no stdout, got %q, %v", output, commandErr)
				}
				if _, err := os.Stat(filepath.Join(dir, "child-started")); !os.IsNotExist(err) {
					return fmt.Errorf("command started or marker could not be checked: %v", err)
				}
				return nil
			})
			s.Step(`^no grant file is saved$`, func() error {
				if _, err := os.Stat(grantPath); !os.IsNotExist(err) {
					return fmt.Errorf("unexpected grant file: %v", err)
				}
				return nil
			})
			s.Step("^the error includes `([^`]+)`$", func(message string) error {
				if !strings.Contains(errorsOutput, message) {
					return fmt.Errorf("error output %q does not contain %q", errorsOutput, message)
				}
				return nil
			})
			s.Step(`^GitHub is not contacted$`, func() error {
				if requests.Load() != 0 {
					return fmt.Errorf("provider received %d calls after denial", requests.Load())
				}
				return nil
			})
		},
	}
	if suite.Run() != 0 {
		t.Fatal("access scenarios failed")
	}
}
