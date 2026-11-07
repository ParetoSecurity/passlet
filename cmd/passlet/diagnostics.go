package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"passlet.dev/passlet/internal/passlet"
)

func diagnostics(ctx context.Context, args []string) error {
	return diagnosticsForOS(ctx, args, runtime.GOOS)
}

func diagnosticsForOS(ctx context.Context, args []string, hostOS string) error {
	fs := commandFlags("doctor", "Check your Approver connection and verify GitHub access. Requires approval on your Approver Mac.")
	endpoint := fs.String("endpoint", "", "override the environment, saved endpoint, or default local socket")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected_arguments")
	}
	resolved, err := passlet.ResolveEndpoint(*endpoint)
	if err != nil {
		return err
	}
	*endpoint = resolved
	if _, err := passlet.EndpointURL(*endpoint, "/v1/health"); err != nil {
		return errors.New("invalid_endpoint_use_unix_absolute_socket_or_Tailscale_HTTPS_origin")
	}
	return diagnoseEndpoint(ctx, *endpoint, hostOS)
}

func localDiagnostics(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("approver doctor", flag.ContinueOnError)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(fs.Output(), `Check service health and credentials on this Mac. Inspects the shared local/SSH socket and Tailscale connection.

Requires Touch ID approval to verify saved credentials.`)
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected_arguments")
	}
	return diagnoseEndpoint(ctx, "unix://"+passlet.DefaultSocket(), runtime.GOOS)
}

func diagnoseEndpoint(ctx context.Context, endpoint, hostOS string) error {
	fmt.Println("passlet", version)
	fmt.Println("Endpoint:", endpoint)
	if err := passlet.Health(ctx, endpoint); err != nil {
		return errors.New(doctorConnectionHint(endpoint, err))
	}
	fmt.Println("Approver: running")
	if endpoint == "unix://"+passlet.DefaultSocket() && hostOS == "darwin" {
		if err := passlet.CheckPrivateDir(filepath.Dir(passlet.DefaultSocket())); err != nil {
			return errors.New("socket directory is unsafe: it must be owned by you with mode 0700")
		}
	}
	fmt.Println("Approve the diagnostic check on your Mac with Touch ID.")
	result, err := passlet.Doctor(ctx, endpoint)
	if err != nil {
		return errors.New(doctorFailureMessage(err))
	}
	fmt.Println("Keychain: unlocked")
	fmt.Println("GitHub authorization: valid")
	fmt.Println("GitHub identity:", result.Login)
	return nil
}

func doctorFailureMessage(err error) string {
	if problem := passlet.PairingProblem(err); problem != "" {
		return problem
	}
	switch err.Error() {
	case "approval_denied":
		return "Approval was denied."
	case "approval_cancelled", "request_cancelled":
		return "The diagnostic check was canceled."
	case "approval_timeout":
		return "Approval timed out. Try again when you can approve on your Mac."
	case "credentials_missing":
		return "GitHub credentials are not configured.\nRun `passlet approver github setup` on your Mac."
	case "refresh_failed", "invalid_credentials", "reauthorization_required", "invalid_protected_account_identity":
		return "Saved GitHub authorization could not be verified.\nRun `passlet approver github setup` on your Mac."
	case "credential_rotation_not_saved":
		return "Updated GitHub credentials could not be saved.\nRun `passlet approver github setup` on your Mac."
	case "github_identity_verification_failed":
		return "GitHub identity could not be verified."
	case "helper_failed", "helper_unavailable":
		return "The approval helper is unavailable. Reinstall Passlet on your Mac."
	case "keychain_unavailable":
		return "The macOS Keychain could not be accessed."
	case "audit_unavailable":
		return "The Approver could not write to its audit log."
	case "identity_unavailable":
		return "The Approver could not identify the caller."
	case "approver_busy":
		return "The Approver is handling another approval request. Try again when it finishes."
	case "approver_unavailable_or_connection_lost":
		return "The connection to the Approver was lost during the diagnostic check."
	case "doctor_requires_updated_approver":
		return "Update Passlet on your Approver Mac to use diagnostics."
	case "invalid_doctor_response":
		return "The Approver returned an invalid diagnostic response."
	default:
		return "The diagnostic check failed."
	}
}

func doctorConnectionHint(endpoint string, err error) string {
	if problem := passlet.PairingProblem(err); problem != "" {
		return problem
	}
	if passlet.CodexSandboxBlocksNetwork() {
		return passlet.CodexSandboxHint
	}
	if endpoint == "unix://"+passlet.DefaultSocket() {
		switch {
		case errors.Is(err, os.ErrNotExist), errors.Is(err, syscall.ECONNREFUSED):
			return "local Approver service is not running"
		case errors.Is(err, os.ErrPermission):
			return "Cannot access the local Approver socket. Check its ownership and permissions."
		case errors.Is(err, context.DeadlineExceeded):
			return "The local Approver health check timed out."
		case errors.Is(err, context.Canceled):
			return "The local Approver health check was canceled."
		default:
			return "The local socket did not return a healthy Approver response."
		}
	}
	if !strings.HasPrefix(endpoint, "unix://") {
		return "Approver: unavailable. Check the service on your Mac and your Tailscale endpoint."
	}
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "No forwarded socket found. Your SSH tunnel may not be running."
	case errors.Is(err, syscall.ECONNREFUSED):
		return "The forwarded socket is not accepting connections. Restart your SSH tunnel."
	case errors.Is(err, os.ErrPermission):
		return "Cannot access the forwarded socket. Check its ownership and permissions."
	default:
		return "The socket did not return a healthy Approver response. Check that Passlet Approver is running on your Mac and that the SSH tunnel forwards to its socket."
	}
}

func auditCommand(args []string) error {
	fs := commandFlags("approver audit", "Show recent access events from the local audit log.")
	jsonOutput := fs.Bool("json", false, "print structured records as JSON")
	limit := fs.Int("limit", 20, "recent events, 1–1000")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected_arguments")
	}
	entries, skipped, err := passlet.ReadAudit(filepath.Join(passlet.StateDir(), "audit.sqlite"), *limit)
	if err != nil {
		return err
	}
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "Warning: audit log contains corrupted entries (%d skipped).\n", skipped)
	}
	if *jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(entries)
	}
	_, err = fmt.Fprint(os.Stdout, formatAudit(entries))
	return err
}
