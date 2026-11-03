package main

import (
	"context"
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
