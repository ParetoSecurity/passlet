package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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
	listener = pairedFixture(t, server, listener)
	go server.Serve(listener)
	defer server.Close()
	if err := passlet.Health(context.Background(), "unix://"+passlet.DefaultSocket()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(passlet.DefaultSocket()), 0755); err != nil {
		t.Fatal(err)
	}
	output, err := invokeCLICommand(t, "", func() error {
		return diagnosticsForOS(context.Background(), nil, "darwin")
	})
	if err == nil || !strings.Contains(err.Error(), "socket directory is unsafe") || strings.Contains(output, "Approve the diagnostic check") {
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

func TestLocalDoctorRejectsEndpointAndArguments(t *testing.T) {
	for _, args := range [][]string{{"--endpoint", "https://other.example.ts.net:17400"}, {"extra"}} {
		if err := localDiagnostics(context.Background(), args); err == nil {
			t.Fatal("local doctor accepted remote endpoint or arguments")
		}
	}
}

func TestDoctorConnectionHints(t *testing.T) {
	for _, tc := range []struct {
		endpoint string
		cause    error
		want     string
	}{
		{"unix:///tmp/forwarded.sock", os.ErrPermission, "ownership and permissions"},
		{"unix:///tmp/forwarded.sock", syscall.EPERM, "allow access to this Unix socket"},
		{"unix://" + passlet.DefaultSocket(), os.ErrNotExist, "Your SSH tunnel may not be running"},
		{"unix://" + passlet.DefaultSocket(), syscall.ECONNREFUSED, "Restart your SSH tunnel"},
		{"https://mac.example.ts.net:17400", syscall.ECONNREFUSED, "Tailscale endpoint"},
	} {
		got := doctorConnectionHint(tc.endpoint, "linux", fmt.Errorf("connection: %w", tc.cause))
		if !strings.Contains(got, tc.want) {
			t.Fatal(got)
		}
	}
}

func TestDoctorFailureMessages(t *testing.T) {
	for _, tc := range []struct {
		codes []string
		want  string
	}{
		{[]string{"approval_denied"}, "Approval was denied."},
		{[]string{"approval_cancelled", "request_cancelled"}, "The diagnostic check was canceled."},
		{[]string{"approval_timeout"}, "Approval timed out. Try again when you can approve on your Mac."},
		{[]string{"credentials_missing"}, "GitHub credentials are not configured.\nRun `passlet approver github setup` on your Mac."},
		{[]string{"refresh_failed", "invalid_credentials", "reauthorization_required", "invalid_protected_account_identity"}, "Saved GitHub authorization could not be verified.\nRun `passlet approver github setup` on your Mac."},
		{[]string{"credential_rotation_not_saved"}, "Updated GitHub credentials could not be saved.\nRun `passlet approver github setup` on your Mac."},
		{[]string{"github_identity_verification_failed"}, "GitHub identity could not be verified."},
		{[]string{"helper_failed", "helper_unavailable"}, "The approval helper is unavailable. Reinstall Passlet on your Mac."},
		{[]string{"keychain_unavailable"}, "The macOS Keychain could not be accessed."},
		{[]string{"audit_unavailable"}, "The Approver could not write to its audit log."},
		{[]string{"identity_unavailable"}, "The Approver could not identify the caller."},
		{[]string{"approver_busy"}, "The Approver is handling another approval request. Try again when it finishes."},
		{[]string{"approver_unavailable_or_connection_lost"}, "The connection to the Approver was lost during the diagnostic check."},
		{[]string{"doctor_requires_updated_approver"}, "Update Passlet on your Approver Mac to use diagnostics."},
		{[]string{"invalid_doctor_response"}, "The Approver returned an invalid diagnostic response."},
		{[]string{"doctor_failed", "unexpected response containing fixture-secret"}, "The diagnostic check failed."},
	} {
		for _, code := range tc.codes {
			t.Run(code, func(t *testing.T) {
				if got := doctorFailureMessage(errors.New(code)); got != tc.want {
					t.Fatalf("got %q, want %q", got, tc.want)
				}
			})
		}
	}
}

func TestLocalDoctorConnectionHints(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, tc := range []struct {
		cause error
		want  string
	}{
		{os.ErrNotExist, "local Approver service is not running"},
		{syscall.ECONNREFUSED, "local Approver service is not running"},
		{os.ErrPermission, "Cannot access socket " + passlet.DefaultSocket() + ". Check its ownership and permissions. If running in a sandbox, allow access to this Unix socket."},
		{context.DeadlineExceeded, "The local Approver health check timed out."},
		{context.Canceled, "The local Approver health check was canceled."},
		{errors.New("approver_health_check_failed"), "The local socket did not return a healthy Approver response."},
	} {
		t.Run(tc.cause.Error(), func(t *testing.T) {
			got := doctorConnectionHint("unix://"+passlet.DefaultSocket(), "darwin", fmt.Errorf("health check: %w", tc.cause))
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDoctorExplainsDeniedCodexConnection(t *testing.T) {
	t.Setenv("CODEX_SANDBOX_NETWORK_DISABLED", "1")
	for _, endpoint := range []string{"unix:///tmp/forwarded.sock", "https://mac.example.ts.net:17400"} {
		err := &net.OpError{Op: "dial", Err: syscall.EPERM}
		if got := doctorConnectionHint(endpoint, "linux", err); got != passlet.SandboxNetworkProblem(err) {
			t.Fatal(got)
		}
	}
	if got := doctorConnectionHint("unix:///tmp/missing.sock", "linux", os.ErrNotExist); !strings.Contains(got, "No forwarded socket") {
		t.Fatal(got)
	}
}
