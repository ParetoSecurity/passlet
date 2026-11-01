package main

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"passlet.dev/passlet/internal/passlet"
)

// Invoke the real dispatcher in-process so its coverage is recorded. No test
// using process-wide arguments or streams may run in parallel.
func invokeCLI(t *testing.T, input string, args ...string) (string, error) {
	t.Helper()
	return invokeCLICommand(t, input, run, args...)
}

func invokeCLICommand(t *testing.T, input string, command func() error, args ...string) (string, error) {
	t.Helper()
	oldArgs, oldIn, oldOut := os.Args, os.Stdin, os.Stdout
	defer func() { os.Args, os.Stdin, os.Stdout = oldArgs, oldIn, oldOut }()
	dir := t.TempDir()
	in, err := os.Create(filepath.Join(dir, "stdin"))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.WriteString(input); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(filepath.Join(dir, "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	os.Args, os.Stdin, os.Stdout = append([]string{"passlet"}, args...), in, out
	runErr := command()
	data, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(data), runErr
}

func TestCommandValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"missing command", nil, "usage:"},
		{"unknown command", []string{"unknown"}, "unknown command"},
		{"missing github action", []string{"github"}, "usage:"},
		{"relative helper", []string{"import", "--helper", "relative"}, "absolute --helper required"},
		{"malformed import", []string{"import", "--helper", "/unused"}, "invalid_credentials_json"},
		{"unknown import flag", []string{"import", "--unknown"}, "flag provided but not defined"},
		{"malformed permission", []string{"request", "--permissions", "contents"}, "permissions must be"},
		{"duplicate permission", []string{"request", "--permissions", "contents:read,contents:write"}, "duplicate_permission"},
		{"request cannot use grant", []string{"request", "--grant", "/unused"}, "--grant is exclusive"},
		{"grant excludes scope", []string{"exec", "--grant", "/unused", "--repos", "alice/repo"}, "--grant is exclusive"},
		{"missing grant", []string{"exec", "--grant", filepath.Join(t.TempDir(), "missing")}, "no such file"},
		{"unknown request flag", []string{"request", "--unknown"}, "flag provided but not defined"},
		{"serve routing", []string{"serve", "--helper", "relative"}, "serve_requires_absolute_helper"},
		{"audit routing", []string{"audit", "--unknown"}, "flag provided but not defined"},
		{"status routing", []string{"status", "--endpoint", "http://invalid"}, "invalid_endpoint_use"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := invokeCLI(t, "not JSON", tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v; want %s", err, tc.want)
			}
		})
	}
	for _, command := range []string{"version", "--version"} {
		out, err := invokeCLI(t, "", command)
		if err != nil || out != "passlet "+version+"\n" {
			t.Fatalf("version: %q, %v", out, err)
		}
	}
}

func TestGrantExecutionValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "grant.json")
	for _, tc := range []struct {
		name, token string
		expiry      time.Time
		args        []string
		want        string
	}{
		{"expired", "fixture-secret", time.Now().Add(-time.Hour), nil, "grant_expired_or_invalid"},
		{"empty token", "", time.Now().Add(time.Hour), nil, "grant_expired_or_invalid"},
		{"missing command", "fixture-secret", time.Now().Add(time.Hour), nil, "exec requires"},
		{"out is request only", "fixture-secret", time.Now().Add(time.Hour), []string{"--out", "unused"}, "--out is request-only"},
		{"child fails", "fixture-secret", time.Now().Add(time.Hour), []string{"--", "/bin/sh", "-c", "exit 7"}, "command_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, _ := json.Marshal(passlet.Grant{Token: tc.token, ExpiresAt: tc.expiry})
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			out, err := invokeCLI(t, "", append([]string{"exec", "--grant", path}, tc.args...)...)
			if err == nil || !strings.Contains(err.Error(), tc.want) || strings.Contains(out, "fixture-secret") {
				t.Fatalf("unexpected execution result: %q %v", out, err)
			}
		})
	}
}

func TestRequestWritesPrivateGrantWithoutOverwriting(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "passlet-request-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "socket")
	l, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/grants" {
			t.Error("unexpected request")
			w.WriteHeader(404)
			return
		}
		var request passlet.Request
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		json.NewEncoder(w).Encode(passlet.Grant{ID: "fixture", Token: "fixture-child", User: "alice", ExpiresAt: time.Now().Add(time.Hour), Repositories: request.Repositories, Permissions: request.Permissions})
	})}
	go server.Serve(l)
	defer server.Close()
	path := filepath.Join(dir, "grant.json")
	args := []string{"request", "--endpoint", "unix://" + socket, "--repos", "alice/repo", "--permissions", "contents:read", "--out", path}
	out, err := invokeCLI(t, "", args...)
	if err != nil || out != "" {
		t.Fatalf("request: %q %v", out, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatal("grant file is not private")
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var grant passlet.Grant
	if json.Unmarshal(original, &grant) != nil || grant.Token != "fixture-child" {
		t.Fatal("wrong grant saved")
	}
	if _, err := invokeCLI(t, "", args...); !os.IsExist(err) {
		t.Fatalf("existing grant was not protected: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(original) {
		t.Fatal("existing grant overwritten")
	}
	output, err := invokeCLI(t, "", args[:len(args)-2]...)
	if err != nil || json.Unmarshal([]byte(output), &grant) != nil || grant.Token != "fixture-child" {
		t.Fatalf("grant stdout: %q %v", output, err)
	}
}

func TestGitCredentialScope(t *testing.T) {
	t.Setenv("PASSLET_GIT_TOKEN", "fixture-token")
	for _, tc := range []struct {
		name, input string
		args        []string
		want        string
	}{
		{"missing operation", "", nil, ""},
		{"store ignored", "", []string{"store"}, ""},
		{"wrong host", "protocol=https\nhost=evil.example\n", []string{"get"}, ""},
		{"HTTP forbidden", "protocol=http\nhost=github.com\n", []string{"get"}, ""},
		{"GitHub HTTPS", "protocol=https\nhost=github.com\n\n", []string{"get"}, "username=x-access-token\npassword=fixture-token\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := invokeCLI(t, tc.input, append([]string{"git-credential"}, tc.args...)...)
			if err != nil || got != tc.want {
				t.Fatalf("credential output: %q %v", got, err)
			}
		})
	}
	t.Setenv("PASSLET_GIT_TOKEN", "")
	if got, err := invokeCLI(t, "protocol=https\nhost=github.com\n", "git-credential", "get"); err != nil || got != "" {
		t.Fatalf("missing token: %q %v", got, err)
	}
}

func TestExecReplacesAmbientCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "grant.json")
	data, err := json.Marshal(passlet.Grant{Token: "fixture-token", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN", "PASSLET_GIT_TOKEN", "GIT_CONFIG_COUNT", "GIT_CONFIG_VALUE_0", "GIT_ASKPASS", "SSH_ASKPASS", "GIT_TERMINAL_PROMPT", "GH_HOST"} {
		t.Setenv(name, "untrusted-fixture")
	}
	script := `[ "$GH_TOKEN" = fixture-token ] && [ "$GITHUB_TOKEN" = fixture-token ] && [ "$PASSLET_GIT_TOKEN" = fixture-token ] && [ "$GIT_CONFIG_COUNT" = 2 ] && [ "$GIT_CONFIG_VALUE_0" = '' ] && [ -z "${GIT_ASKPASS+x}" ] && [ -z "${SSH_ASKPASS+x}" ] && [ -z "${GH_HOST+x}" ] && [ "$GIT_TERMINAL_PROMPT" = 0 ] && [ -x "$GIT_CONFIG_VALUE_1" ] && printf 'ok'`
	got, err := invokeCLI(t, "", "exec", "--grant", path, "--", "/bin/sh", "-c", script)
	if err != nil || got != "ok" {
		t.Fatalf("exec environment: %q %v", got, err)
	}
}

func TestImportValidJSONStillValidatesCredentials(t *testing.T) {
	if _, err := invokeCLI(t, `{}`, "import", "--helper", "/unused"); err == nil || err.Error() != "invalid_protected_account_identity" {
		t.Fatalf("import bypassed identity validation: %v", err)
	}
	if _, err := invokeCLI(t, "", "request", "--endpoint", "http://invalid", "--repos", "alice/repo", "--permissions", "contents:read"); err == nil {
		t.Fatal("invalid endpoint accepted")
	}
}

func TestExecUnavailableTemporaryDirectory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "grant.json")
	data, err := json.Marshal(passlet.Grant{Token: "fixture-token", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", filepath.Join(dir, "missing"))
	if _, err := invokeCLI(t, "", "exec", "--grant", path, "--", "/bin/true"); err == nil {
		t.Fatal("exec ignored temporary directory failure")
	}
}

func TestCredentialInputReadFailure(t *testing.T) {
	input, err := os.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	oldArgs, oldInput := os.Args, os.Stdin
	os.Args, os.Stdin = []string{"passlet", "git-credential", "get"}, input
	defer func() { os.Args, os.Stdin = oldArgs, oldInput }()
	if err := run(); err == nil {
		t.Fatal("credential helper ignored input read failure")
	}
}
