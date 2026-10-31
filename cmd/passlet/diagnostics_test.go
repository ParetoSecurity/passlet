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

func TestDoctorAmbientCredentials(t *testing.T) {
	// A short path works with macOS's Unix socket length limit.
	dir, err := os.MkdirTemp("/tmp", "doctor-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "s")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/health" {
			t.Error("doctor requested more than health")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Write([]byte(`{"status":"ready"}`))
	})}
	go server.Serve(listener)
	defer server.Close()

	for _, tc := range []struct {
		name, command, gh, github, scoped string
		want                              []string
	}{
		{name: "unset", command: "doctor"},
		{name: "gh", command: "doctor", gh: "secret-gh", want: []string{"GH_TOKEN"}},
		{name: "github", command: "doctor", github: "secret-github", want: []string{"GITHUB_TOKEN"}},
		{name: "both", command: "doctor", gh: "secret-gh", github: "secret-github", want: []string{"GH_TOKEN", "GITHUB_TOKEN"}},
		{name: "exec environment", command: "doctor", gh: "secret-scoped", github: "secret-scoped", scoped: "secret-scoped"},
		{name: "overridden exec token", command: "doctor", gh: "secret-gh", github: "secret-scoped", scoped: "secret-scoped", want: []string{"GH_TOKEN"}},
		{name: "status unchanged", command: "status", gh: "secret-gh", github: "secret-github"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := fixture(t, tc.command, "--endpoint", "unix://"+socket)
			cmd.Env = append(cmd.Env, "GH_TOKEN="+tc.gh, "GITHUB_TOKEN="+tc.github, "PASSLET_GIT_TOKEN="+tc.scoped, "GH_TOKEN_CUSTOM=secret-custom")
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("healthy diagnostics failed: %v", err)
			}
			text := string(output)
			if strings.Contains(text, "secret-") || strings.Contains(text, "GH_TOKEN_CUSTOM") {
				t.Fatal("diagnostics disclosed a credential or inspected a custom variable")
			}
			if strings.Count(text, "Warning:") != len(tc.want) {
				t.Fatalf("unexpected warnings: %s", text)
			}
			for _, name := range tc.want {
				if !strings.Contains(text, "Warning: "+name+" is set;") {
					t.Fatalf("missing warning for %s", name)
				}
			}
		})
	}

	grant := filepath.Join(dir, "grant.json")
	data, err := json.Marshal(passlet.Grant{Token: "secret-exec", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(grant, data, 0600); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := fixture(t, "exec", "--grant", grant, "--", self, "doctor", "--endpoint", "unix://"+socket)
	cmd.Env = append(cmd.Env, "GH_TOKEN=secret-parent", "GITHUB_TOKEN=secret-parent")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("doctor through exec failed: %v", err)
	}
	if strings.Contains(string(output), "Warning:") || strings.Contains(string(output), "secret-") {
		t.Fatal("exec credentials caused a warning or were disclosed")
	}
}
