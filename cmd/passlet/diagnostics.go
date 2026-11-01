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

func diagnostics(ctx context.Context, command string, args []string) error {
	return diagnosticsForOS(ctx, command, args, runtime.GOOS)
}

func diagnosticsForOS(ctx context.Context, command string, args []string, hostOS string) error {
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	endpoint := fs.String("endpoint", passlet.DefaultEndpoint(), "local Unix socket or remote HTTPS origin")
	helper := fs.String("helper", defaultHelper(), "signed helper path (Mac only)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected_arguments")
	}
	if _, err := passlet.EndpointURL(*endpoint, "/v1/health"); err != nil {
		return errors.New("invalid_endpoint_use_unix_absolute_socket_or_Tailscale_HTTPS_origin")
	}
	fmt.Println("Endpoint:", *endpoint)
	failed := false
	if err := passlet.Health(ctx, *endpoint); err != nil {
		fmt.Println("Service: unavailable; check the Homebrew or Home Manager service on the Mac and check the SSH tunnel or Tailscale endpoint.")
		failed = true
	} else {
		fmt.Println("Service: ready (connectivity only; no credentials unlocked)")
	}
	// These checks describe this Mac only; never imply that local Keychain state
	// describes a remote endpoint chosen by the user.
	if *endpoint == "unix://"+passlet.DefaultSocket() && hostOS == "darwin" {
		if err := passlet.CheckPrivateDir(filepath.Dir(passlet.DefaultSocket())); err != nil {
			fmt.Println("Socket directory: unsafe; it must be owned by you with mode 0700.")
			failed = true
		}

		state, err := passlet.KeychainStatus(ctx, *helper)
		if err != nil {
			fmt.Println("Helper: unavailable; install Passlet through Homebrew or Home Manager.")
			failed = true
		} else {
			fmt.Println("Keychain:", state, "(no authentication performed)")
			if state == "missing" {
				fmt.Println("Run passlet github setup.")
				failed = true
			}
			if state == "protected_or_unavailable" {
				fmt.Println("Credential availability requires a real approved request to verify.")
			}
		}
	} else {
		fmt.Println("GitHub setup and Keychain: check on the approving Mac; not inspected remotely.")
	}
	if command == "doctor" && failed {
		return errors.New("doctor_found_problems")
	}
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
