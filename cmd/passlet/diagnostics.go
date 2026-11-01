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

	"passlet.dev/passlet/internal/passlet"
)

func diagnostics(ctx context.Context, args []string) error {
	return diagnosticsForOS(ctx, args, runtime.GOOS)
}

func diagnosticsForOS(ctx context.Context, args []string, hostOS string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
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
	fmt.Println("Endpoint:", *endpoint)
	if err := passlet.Health(ctx, *endpoint); err != nil {
		fmt.Println("Conductor: unavailable. Check the service on your Mac and your SSH tunnel or Tailscale endpoint.")
		return errors.New("doctor_found_problems")
	}
	fmt.Println("Conductor: running")
	if *endpoint == "unix://"+passlet.DefaultSocket() && hostOS == "darwin" {
		if err := passlet.CheckPrivateDir(filepath.Dir(passlet.DefaultSocket())); err != nil {
			fmt.Println("Socket directory: unsafe; it must be owned by you with mode 0700.")
			return errors.New("doctor_found_problems")
		}
	}
	fmt.Println("Approve the diagnostic check on your Mac with Touch ID or your password.")
	result, err := passlet.Doctor(ctx, *endpoint)
	if err != nil {
		fmt.Println("GitHub credentials: verification failed (" + err.Error() + ").")
		if err.Error() == "credentials_missing" {
			fmt.Println("Run passlet github setup on your Mac.")
		}
		return err
	}
	fmt.Println("Keychain: unlocked")
	fmt.Println("GitHub authorization: valid")
	fmt.Println("GitHub identity:", result.Login)
	fmt.Println("No agent token was issued.")
	return nil
}

func auditCommand(args []string) error {
	fs := flag.NewFlagSet("audit", flag.ContinueOnError)
	path := fs.String("path", filepath.Join(passlet.StateDir(), "audit.sqlite"), "local audit database")
	limit := fs.Int("limit", 20, "recent events, 1–1000")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected_arguments")
	}
	entries, skipped, err := passlet.ReadAudit(*path, *limit)
	if err != nil {
		return err
	}
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "Warning: audit log contains corrupted entries (%d skipped).\n", skipped)
	}
	return json.NewEncoder(os.Stdout).Encode(entries)
}
