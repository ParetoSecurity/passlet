package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"passlet.dev/passlet/internal/passlet"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func init() {
	if os.Getenv("PASSLET_CLI_FIXTURE") == "1" {
		if err := run(); err != nil {
			writeCommandError(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
}
func fixture(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, args...)
	cmd.Env = append(os.Environ(), "PASSLET_CLI_FIXTURE=1", "GORACE=atexit_sleep_ms=0")
	return cmd
}
func TestGitHelperRestrictsHostAndOperation(t *testing.T) {
	for _, tc := range []struct {
		host, protocol, op string
		want               bool
	}{{"github.com", "https", "get", true}, {"evil.example", "https", "get", false}, {"github.com.evil.example", "https", "get", false}, {"github.com", "http", "get", false}, {"github.com", "https", "store", false}} {
		cmd := fixture(t, "git-credential", tc.op)
		cmd.Env = append(cmd.Env, "PASSLET_GIT_TOKEN=test-child")
		cmd.Stdin = strings.NewReader("protocol=" + tc.protocol + "\nhost=" + tc.host + "\n\n")
		output, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(output), "test-child") != tc.want {
			t.Fatal("credential escaped allowed host/operation")
		}
	}
}
func TestExecOverridesCredentialsWithoutPersisting(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	dir := t.TempDir()
	grant := filepath.Join(dir, "grant.json")
	data, _ := json.Marshal(passlet.Grant{Token: "scoped-test-child", ExpiresAt: time.Now().Add(time.Hour)})
	os.WriteFile(grant, data, 0600)
	global := filepath.Join(dir, "gitconfig")
	os.WriteFile(global, []byte("[credential]\n helper = !echo password=unrelated-parent\n"), 0600)
	cmd := fixture(t, "exec", "--grant", grant, "--", "git", "credential", "fill")
	cmd.Env = append(cmd.Env, "GH_TOKEN=unrelated-parent", "GITHUB_TOKEN=unrelated-parent", "GIT_CONFIG_GLOBAL="+global)
	cmd.Stdin = strings.NewReader("protocol=https\nhost=github.com\n\n")
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(output), "scoped-test-child") || strings.Contains(string(output), "unrelated-parent") {
		t.Fatal("wrong Git credential")
	}
	content, _ := os.ReadFile(global)
	if strings.Contains(string(content), "scoped-test-child") {
		t.Fatal("persisted token in global Git config")
	}
}
func TestExecRejectsExpiredGrant(t *testing.T) {
	path := filepath.Join(t.TempDir(), "grant.json")
	data, _ := json.Marshal(passlet.Grant{Token: "expired", ExpiresAt: time.Now().Add(-time.Hour)})
	os.WriteFile(path, data, 0600)
	if fixture(t, "exec", "--grant", path, "--", "true").Run() == nil {
		t.Fatal("expired grant executed")
	}
}

func TestDoctorUsesSelectedApprover(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "passlet-cli-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "s")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/v1/doctor" {
			w.Write([]byte(`{"login":"alice"}`))
			return
		}
		if r.Method != "GET" || r.URL.Path != "/v1/health" {
			t.Error("diagnostic requested more than health")
			w.WriteHeader(403)
			return
		}
		w.Write([]byte(`{"status":"ready"}`))
	})}
	listener = pairedFixture(t, server, listener)
	go server.Serve(listener)
	defer server.Close()
	if err := diagnostics(context.Background(), []string{"--endpoint", "unix://" + path}); err != nil {
		t.Fatal(err)
	}
	server.Close()
	if err := diagnostics(context.Background(), []string{"--endpoint", "unix://" + path}); err == nil {
		t.Fatal("offline doctor succeeded")
	}
	if err := diagnostics(context.Background(), []string{"--endpoint", "https://secret@example.test"}); err == nil {
		t.Fatal("credential-bearing endpoint accepted")
	}
}

func TestExecProcessSubstitutionPreservesStdin(t *testing.T) {
	t.Setenv("PASSLET_ENDPOINT", endpointTestServer(t))
	for _, permissions := range []string{"contents:read", "invalid"} {
		t.Run(permissions, func(t *testing.T) {
			cli := fixture(t)
			cmd := exec.Command("bash", "--noprofile", "--norc", "-c", `
"$1" exec --grant <("$1" request --repos alice/repo --permissions "$2") -- /bin/sh -c '
  test "$GH_TOKEN" = fixture-token || exit 9
  printf "child started\n"
  cat
'`, "passlet-process-substitution", cli.Path, permissions)
			cmd.Env = cli.Env
			cmd.Stdin = strings.NewReader("input for the child\n")
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			output, err := cmd.Output()
			if permissions == "contents:read" {
				if err != nil || string(output) != "child started\ninput for the child\n" {
					t.Fatalf("process substitution failed: %q, %v, %s", output, err, stderr.String())
				}
			} else if err == nil || len(output) != 0 {
				t.Fatalf("failed request started child: %q, %v", output, err)
			}
			if strings.Contains(stderr.String(), "fixture-token") || strings.Contains(string(output), "fixture-token") {
				t.Fatal("grant token leaked to command output")
			}
		})
	}
}

func TestExecPreservesProxyAuthenticationWithoutCredentials(t *testing.T) {
	dir := t.TempDir()
	grant := filepath.Join(dir, "grant.json")
	data, _ := json.Marshal(passlet.Grant{Token: "scoped-test-child", ExpiresAt: time.Now().Add(time.Hour)})
	if err := os.WriteFile(grant, data, 0600); err != nil {
		t.Fatal(err)
	}
	global := filepath.Join(dir, "gitconfig")
	if err := os.WriteFile(global, []byte("[http]\n proxyAuthMethod = basic\n[credential]\n helper = !echo password=unrelated-parent\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, injected, explicit, want string }{
		{"global", "", "", "basic"},
		{"sandbox", "digest", "", "digest"},
		{"explicit environment", "digest", "ntlm", "ntlm"},
		{"invalid method", "fixture-secret", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("GIT_CONFIG_GLOBAL", global)
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			t.Setenv("GIT_CONFIG_COUNT", "0")
			t.Setenv("GIT_CONFIG_PARAMETERS", "")
			t.Setenv("GIT_HTTP_PROXY_AUTHMETHOD", tc.explicit)
			if tc.injected != "" {
				t.Setenv("GIT_CONFIG_COUNT", "2")
				t.Setenv("GIT_CONFIG_KEY_0", "http.proxyAuthMethod")
				t.Setenv("GIT_CONFIG_VALUE_0", tc.injected)
				t.Setenv("GIT_CONFIG_KEY_1", "credential.helper")
				t.Setenv("GIT_CONFIG_VALUE_1", "!echo password=unrelated-parent")
			}
			got, err := invokeCLI(t, "", "exec", "--grant", grant, "--", "/bin/sh", "-c", `printf '%s' "$GIT_HTTP_PROXY_AUTHMETHOD"`)
			if err != nil || got != tc.want {
				t.Fatalf("proxy method: %q %v", got, err)
			}
			cmd := fixture(t, "exec", "--grant", grant, "--", "/bin/sh", "-c", `printf '%s\n' "$GIT_HTTP_PROXY_AUTHMETHOD"; printf 'protocol=https\nhost=github.com\n\n' | git credential fill`)
			cmd.Dir = dir
			output, err := cmd.Output()
			if err != nil || !strings.HasPrefix(string(output), tc.want+"\n") || !strings.Contains(string(output), "password=scoped-test-child") || strings.Contains(string(output), "unrelated-parent") || strings.Contains(string(output), "fixture-secret") {
				t.Fatalf("proxy/credential isolation: %q %v", output, err)
			}
		})
	}
}
