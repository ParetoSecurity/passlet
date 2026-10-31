package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"passlet.dev/passlet/internal/passlet"
)

func diagnostics(ctx context.Context, command string, args []string) error {
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
	if command == "doctor" {
		fmt.Println(cliAvailability())
	}
	fmt.Println("Endpoint:", *endpoint)
	failed := false
	if err := passlet.Health(ctx, *endpoint); err != nil {
		fmt.Println("Service: unavailable; run passlet service status on the Mac and check the SSH tunnel or Tailscale endpoint.")
		failed = true
	} else {
		fmt.Println("Service: ready (connectivity only; no credentials unlocked)")
	}
	// These checks describe this Mac only; never imply that local metadata
	// describes a remote endpoint chosen by the user.
	if *endpoint == "unix://"+passlet.DefaultSocket() && runtime.GOOS == "darwin" {
		if err := passlet.CheckPrivateDir(filepath.Dir(passlet.DefaultSocket())); err != nil {
			fmt.Println("Socket directory: unsafe; it must be owned by you with mode 0700.")
			failed = true
		}
		account, err := passlet.LoadAccount(filepath.Join(passlet.StateDir(), "account.json"))
		if err != nil {
			fmt.Println("GitHub account: no setup metadata; run passlet setup github.")
			failed = true
		} else {
			fmt.Printf("GitHub account: %s (locally recorded at %s; not freshly verified)\n", account.Login, account.VerifiedAt.Format("2006-01-02T15:04:05Z07:00"))
		}
		state, err := passlet.KeychainStatus(ctx, *helper)
		if err != nil {
			fmt.Println("Helper: unavailable; install the signed Passlet app or set --helper.")
			failed = true
		} else {
			fmt.Println("Keychain:", state, "(no authentication performed)")
			if state == "missing" {
				fmt.Println("Run passlet setup github.")
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
	entries, err := passlet.ReadAudit(*path, *limit)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(entries)
}

// GUI apps inherit a restricted PATH. Distinguish an installed CLI from one
// available to this process, without running binaries or evaluating shell files.
func cliAvailability() string {
	var dirs []string
	if runtime.GOOS == "darwin" {
		home, _ := os.UserHomeDir()
		dirs = []string{filepath.Join(home, ".local/bin"), "/usr/local/bin", "/opt/homebrew/bin"}
	}
	return cliAvailabilityIn(dirs)
}

func cliAvailabilityIn(dirs []string) string {
	if _, err := exec.LookPath("passlet"); err == nil {
		return "CLI: available on PATH."
	}
	for _, dir := range dirs {
		if _, err := exec.LookPath(filepath.Join(dir, "passlet")); err == nil {
			return "CLI: installed in " + dir + "; this process PATH does not include it. Ensure your agent's PATH includes this directory."
		}
	}
	if runtime.GOOS == "darwin" {
		return "Warning: CLI missing from PATH. Choose Install Command Line Tool… in the Passlet menu, or link the app's CLI through Home Manager."
	}
	return "Warning: CLI missing from PATH. Install the Passlet CLI and add its directory to your agent's PATH."
}
