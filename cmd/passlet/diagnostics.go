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
	fs := commandFlags("doctor", "Check your Approver connection and verify provider access. Requires approval on your Approver Mac.")
	provider := fs.String("provider", "", "credential provider: github (default) or cloudflare")
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
	return diagnoseEndpoint(ctx, *endpoint, hostOS, *provider)
}

func localDiagnostics(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("approver doctor", flag.ContinueOnError)
	provider := fs.String("provider", "", "credential provider: github (default) or cloudflare")
	fs.Usage = func() {
		_, _ = fmt.Fprintln(fs.Output(), `Check service health and credentials on this Mac. Inspects the shared local/SSH socket and Tailscale connection.

Requires Touch ID approval to verify saved credentials.
Use --provider cloudflare to check Cloudflare instead of GitHub.`)
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected_arguments")
	}
	return diagnoseEndpoint(ctx, "unix://"+passlet.DefaultSocket(), "darwin", *provider)
}

func diagnoseEndpoint(ctx context.Context, endpoint, hostOS, provider string) error {
	if provider == "github" {
		provider = ""
	}
	if provider != "" && provider != "cloudflare" {
		return errors.New("invalid_provider")
	}
	fmt.Println("passlet", version)
	fmt.Println("Endpoint:", endpoint)
	if err := passlet.Health(ctx, endpoint); err != nil {
		return errors.New(doctorConnectionHint(endpoint, hostOS, err))
	}
	fmt.Println("Approver: running")
	if endpoint == "unix://"+passlet.DefaultSocket() && hostOS == "darwin" {
		if err := passlet.CheckPrivateDir(filepath.Dir(passlet.DefaultSocket())); err != nil {
			return errors.New("socket directory is unsafe: it must be owned by you with mode 0700")
		}
	}
	fmt.Println("Approve the diagnostic check on your Mac with Touch ID.")
	result, err := passlet.Doctor(ctx, endpoint, provider)
	if err != nil {
		return errors.New(doctorFailureMessage(err, provider))
	}
	fmt.Println("Keychain: unlocked")
	if provider == "cloudflare" {
		fmt.Println("Cloudflare token-creation authority: valid")
		fmt.Println("Cloudflare parent token:", result.CloudflareTokenID)
	} else {
		fmt.Println("GitHub authorization: valid")
		fmt.Println("GitHub identity:", result.Login)
	}
	return nil
}

func doctorFailureMessage(err error, provider string) (message string) {
	defer func() {
		if provider == "cloudflare" {
			message = strings.NewReplacer("GitHub", "Cloudflare", "github setup", "cloudflare setup").Replace(message)
		}
	}()
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
	case "cloudflare_credentials_verification_failed":
		return "Saved Cloudflare token-creation authority could not be verified. Check the parent token in Cloudflare. If replacing it, run `passlet approver cloudflare reset`, then `passlet approver cloudflare setup` on your Mac."
	case "provider_unavailable":
		return "Update Passlet on your Approver Mac to enable Cloudflare diagnostics."
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

func doctorConnectionHint(endpoint, hostOS string, err error) string {
	if problem := passlet.PairingProblem(err); problem != "" {
		return problem
	}
	if problem := passlet.SandboxNetworkProblem(err); problem != "" {
		return problem
	}
	if strings.HasPrefix(endpoint, "unix://") && (errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EPERM)) {
		return fmt.Sprintf("Cannot access socket %s. Check its ownership and permissions. If running in a sandbox, allow access to this Unix socket.", strings.TrimPrefix(endpoint, "unix://"))
	}
	if hostOS == "darwin" && endpoint == "unix://"+passlet.DefaultSocket() {
		switch {
		case errors.Is(err, os.ErrNotExist), errors.Is(err, syscall.ECONNREFUSED):
			return "local Approver service is not running"
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
