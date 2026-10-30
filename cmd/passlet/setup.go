package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"passlet.dev/passlet/internal/passlet"
)

func setupCommand(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "github" {
		return errors.New("usage: passlet setup github")
	}
	if runtime.GOOS != "darwin" {
		return errors.New("run_setup_on_your_Mac")
	}
	fs := flag.NewFlagSet("setup github", flag.ContinueOnError)
	helper := fs.String("helper", defaultHelper(), "signed helper path")
	permissions := fs.String("permissions", "contents:write,pull_requests:write,issues:write,actions:read,workflows:write", "maximum App permissions; each agent request is separately scoped")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 || !filepath.IsAbs(*helper) {
		return errors.New("setup_requires_absolute_helper")
	}
	state, err := passlet.KeychainStatus(ctx, *helper)
	if err != nil {
		return err
	}
	if state != "missing" {
		return errors.New("existing_or_protected_credentials_detected_setup_will_not_overwrite_Keychain")
	}
	perms := map[string]string{}
	for _, p := range strings.Split(*permissions, ",") {
		kv := strings.Split(p, ":")
		if len(kv) != 2 || perms[kv[0]] != "" {
			return errors.New("invalid_permissions")
		}
		perms[kv[0]] = kv[1]
	}
	return passlet.RunSetup(ctx, perms, passlet.NewGitHub(), func(ctx context.Context, c passlet.Credentials, account passlet.AccountInfo) error {
		if err := passlet.ImportCredentials(ctx, *helper, c); err != nil {
			return err
		}
		if err := passlet.SaveAccount(filepath.Join(passlet.StateDir(), "account.json"), account); err != nil {
			return errors.New("credentials_saved_but_account_metadata_write_failed")
		}
		fmt.Println("GitHub account verified:", account.Login)
		fmt.Println("Setup complete. Run passlet service install, then passlet doctor.")
		return nil
	}, func(url string) error {
		fmt.Println("Complete GitHub App creation, installation, and authorization in your browser within 10 minutes.")
		return exec.CommandContext(ctx, "/usr/bin/open", url).Run()
	})
}
