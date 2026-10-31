package main

import (
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
		if run() != nil {
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

func TestDiagnosticsUseSelectedSocketWithoutApproving(t *testing.T) {
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
		if r.Method != "GET" || r.URL.Path != "/v1/health" {
			t.Error("diagnostic requested more than health")
			w.WriteHeader(403)
			return
		}
		w.Write([]byte(`{"status":"ready"}`))
	})}
	go server.Serve(listener)
	defer server.Close()
	for _, command := range []string{"doctor", "status"} {
		if err := diagnostics(context.Background(), command, []string{"--endpoint", "unix://" + path}); err != nil {
			t.Fatal(err)
		}
	}
	server.Close()
	if err := diagnostics(context.Background(), "doctor", []string{"--endpoint", "unix://" + path}); err == nil {
		t.Fatal("offline doctor succeeded")
	}
	if err := diagnostics(context.Background(), "doctor", []string{"--endpoint", "https://secret@example.test"}); err == nil {
		t.Fatal("credential-bearing endpoint accepted")
	}
}

func TestAuditCLIRequiresExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.sqlite")
	if err := auditCommand([]string{"--path", path}); err == nil {
		t.Fatal("missing audit accepted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("audit command created database")
	}
}
