package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"passlet.dev/passlet/internal/passlet"
)

func TestDoctorLocalKeychainStatus(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "pl-doctor-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("HOME", dir)
	t.Setenv("PASSLET_ENDPOINT", "")
	listener, err := passlet.ListenUnix(passlet.DefaultSocket())
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"status":"ready"}`)) })}
	go server.Serve(listener)
	defer server.Close()
	for _, tc := range []struct {
		status, want string
		fail         bool
	}{
		{"configured", "Keychain: configured", false},
		{"missing", "Run passlet github setup.", true},
		{"protected_or_unavailable", "Credential availability requires a real approved request", false},
		{"unknown", "Helper: unavailable", true},
	} {
		t.Run(tc.status, func(t *testing.T) {
			helper := filepath.Join(t.TempDir(), "helper")
			script := "#!/bin/sh\n[ \"$1\" = --status ] || exit 2\nprintf '%s' '{\"status\":\"" + tc.status + "\"}'\n"
			if err := os.WriteFile(helper, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			output, err := invokeCLICommand(t, "", func() error {
				return diagnosticsForOS(context.Background(), "doctor", []string{"--helper", helper}, "darwin")
			})
			if (err != nil) != tc.fail || !strings.Contains(output, tc.want) || !strings.Contains(output, "Service: ready") {
				t.Fatalf("doctor: %q %v", output, err)
			}
		})
	}
	if err := os.Chmod(filepath.Dir(passlet.DefaultSocket()), 0755); err != nil {
		t.Fatal(err)
	}
	output, err := invokeCLICommand(t, "", func() error {
		return diagnosticsForOS(context.Background(), "doctor", []string{"--helper", filepath.Join(dir, "missing")}, "darwin")
	})
	if err == nil || !strings.Contains(output, "Socket directory: unsafe") {
		t.Fatalf("unsafe directory: %q %v", output, err)
	}
}

func TestDiagnosticArgumentValidation(t *testing.T) {
	for _, args := range [][]string{{"--unknown"}, {"extra"}} {
		if err := diagnostics(context.Background(), "doctor", args); err == nil {
			t.Fatal("invalid arguments accepted")
		}
	}
	if err := auditCommand([]string{"extra"}); err == nil {
		t.Fatal("audit accepted extra arguments")
	}
}

func TestAuditCommandPrintsEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.sqlite")
	audit, err := passlet.OpenAudit(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := audit.Record(passlet.Event{RequestID: "test-request", Outcome: "approved"}); err != nil {
		t.Fatal(err)
	}
	audit.Close()
	output, err := invokeCLI(t, "", "audit", "--path", path)
	if err != nil || !strings.Contains(output, `"request_id":"test-request"`) {
		t.Fatalf("audit output: %q %v", output, err)
	}
}

func TestAuditCommandWarnsWhenSkippingCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.sqlite")
	audit, err := passlet.OpenAudit(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := audit.Record(passlet.Event{RequestID: "valid", Outcome: "approved"}); err != nil {
		t.Fatal(err)
	}
	audit.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO events(at,request_id,outcome,data) VALUES('now','broken','approved','corrupted-fixture')`); err != nil {
		t.Fatal(err)
	}
	stderr, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	defer stderr.Close()
	oldStderr := os.Stderr
	os.Stderr = stderr
	defer func() { os.Stderr = oldStderr }()
	output, err := invokeCLI(t, "", "audit", "--path", path)
	if err != nil {
		t.Fatal(err)
	}
	var entries []passlet.AuditEntry
	if err := json.Unmarshal([]byte(output), &entries); err != nil || len(entries) != 1 || entries[0].Event.RequestID != "valid" {
		t.Fatalf("valid JSON entries lost: %q %v", output, err)
	}
	warning, err := os.ReadFile(stderr.Name())
	if err != nil {
		t.Fatal(err)
	}
	if string(warning) != "Warning: audit log contains corrupted entries (1 skipped).\n" {
		t.Fatalf("missing corruption warning: %q", warning)
	}
}
