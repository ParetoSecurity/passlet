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

func TestDoctorRejectsUnsafeSocketDirectory(t *testing.T) {
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
	if err := os.Chmod(filepath.Dir(passlet.DefaultSocket()), 0755); err != nil {
		t.Fatal(err)
	}
	output, err := invokeCLICommand(t, "", func() error {
		return diagnosticsForOS(context.Background(), nil, "darwin")
	})
	if err == nil || !strings.Contains(output, "Socket directory: unsafe") {
		t.Fatalf("unsafe directory: %q %v", output, err)
	}
}

func TestDiagnosticArgumentValidation(t *testing.T) {
	for _, args := range [][]string{{"--unknown"}, {"extra"}} {
		if err := diagnostics(context.Background(), args); err == nil {
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

func TestDoctorReportsVerificationFailures(t *testing.T) {
	for _, code := range []string{"credentials_missing", "approval_denied"} {
		t.Run(code, func(t *testing.T) {
			dir, err := os.MkdirTemp("/tmp", "pl-doctor-error-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			socket := filepath.Join(dir, "socket")
			listener, err := passlet.ListenUnix(socket)
			if err != nil {
				t.Fatal(err)
			}
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/health":
					_, _ = w.Write([]byte(`{"status":"ready"}`))
				case "/v1/doctor":
					w.WriteHeader(http.StatusForbidden)
					_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
				default:
					t.Errorf("unexpected request: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			})}
			go func() { _ = server.Serve(listener) }()
			defer server.Close()
			output, err := invokeCLI(t, "", "doctor", "--endpoint", "unix://"+socket)
			if err == nil || err.Error() != code || !strings.Contains(output, "GitHub credentials: verification failed ("+code+").") {
				t.Fatalf("unexpected failure: %q %v", output, err)
			}
			if strings.Contains(output, "Run passlet github setup on your Mac.") != (code == "credentials_missing") {
				t.Fatalf("incorrect recovery instructions: %q", output)
			}
			if strings.Contains(output, "GitHub authorization: valid") {
				t.Fatal("failed verification was reported as successful")
			}
		})
	}
}
