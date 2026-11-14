package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/peterh/liner"
	"passlet.dev/passlet/internal/passlet"
)

func cloudflareCommand(ctx context.Context, args []string) error { // coverage-ignore: Native terminal/browser adapter; cloudflareSetup tests the flow with injected dependencies.
	return cloudflareSetup(ctx, args, cloudflareSetupDependencies{
		hostOS: runtime.GOOS, helper: defaultHelper(), out: os.Stdout, provider: passlet.NewCloudflare(),
		prompt: func() (string, error) {
			line := liner.NewLiner()
			defer func() { _ = line.Close() }()
			line.SetCtrlCAborts(true)
			return line.PasswordPrompt("Paste the Cloudflare token (hidden): ")
		},
		open:  func(url string) error { return exec.CommandContext(ctx, "/usr/bin/open", url).Run() },
		store: passlet.ImportCredentials, pair: passlet.PairLocalAgent,
		status: func(ctx context.Context, path string) (string, error) {
			return (passlet.NativeHelper{Path: path, Provider: "cloudflare"}).Status(ctx)
		},
		approve: func(ctx context.Context, path string) (passlet.CredentialSession, error) {
			return (passlet.NativeHelper{Path: path, Provider: "cloudflare"}).Approve(ctx, "verify your saved Cloudflare credential and refresh account and zone names using temporary read-only tokens")
		},
		reset: func(ctx context.Context, path string) error {
			_, err := (passlet.NativeHelper{Path: path, Provider: "cloudflare"}).Reset(ctx)
			return err
		},
	})
}

type cloudflareSetupDependencies struct {
	hostOS, helper string
	out            io.Writer
	provider       interface {
		Verify(context.Context, string) (passlet.Credentials, error)
		RefreshResources(context.Context, passlet.Credentials) error
	}
	prompt  func() (string, error)
	open    func(string) error
	store   func(context.Context, string, passlet.Credentials) error
	pair    func() error
	reset   func(context.Context, string) error
	status  func(context.Context, string) (string, error)
	approve func(context.Context, string) (passlet.CredentialSession, error)
}

func cloudflareSetup(ctx context.Context, args []string, d cloudflareSetupDependencies) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		_, _ = fmt.Fprintln(d.out, "Manage Cloudflare credentials on your Approver Mac.\n\nUsage: passlet approver cloudflare <setup|reset>")
		return nil
	}
	if args[0] != "setup" && args[0] != "reset" {
		return errors.New("usage: passlet approver cloudflare setup | reset")
	}
	fs := commandFlags("approver cloudflare "+args[0], "Configure a Cloudflare token-creation credential in protected Keychain, or remove it. Agents receive only explicitly approved scoped tokens.")
	fs.SetOutput(d.out)
	helper := fs.String("helper", d.helper, "absolute signed helper path")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if d.hostOS != "darwin" {
		return errors.New("cloudflare_setup_requires_Mac")
	}
	if fs.NArg() != 0 || !filepath.IsAbs(*helper) {
		return errors.New("setup_requires_absolute_helper")
	}
	state, err := d.status(ctx, *helper)
	if err != nil {
		return err
	}
	if args[0] == "reset" {
		if state == "missing" {
			_, _ = fmt.Fprintln(d.out, "No Cloudflare credentials are saved in Keychain. Nothing to do.")
			return nil
		}
		if err := d.reset(ctx, *helper); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(d.out, "Cloudflare credentials removed from this Mac. Existing tokens remain valid until expiry or revocation in Cloudflare.")
		return nil
	}
	if state != "missing" {
		session, err := d.approve(ctx, *helper)
		if err != nil {
			return err
		}
		credentials := session.Credentials()
		session.Close()
		verified, err := d.provider.Verify(ctx, credentials.AccessToken)
		if err != nil {
			_, _ = fmt.Fprintln(d.out, "Saved Cloudflare credentials could not be verified. Run `passlet approver cloudflare reset`, then `passlet approver cloudflare setup` to replace them.")
			return err
		}
		if verified.Validate() != nil || verified.Provider != "cloudflare" || verified.CloudflareTokenID != credentials.CloudflareTokenID {
			return errors.New("cloudflare_credentials_verification_failed")
		}
		if err := d.provider.RefreshResources(ctx, verified); err != nil {
			return err
		}
		if err := d.pair(); err != nil {
			return err
		}
		_, _ = fmt.Fprintln(d.out, "Using your saved Cloudflare credential. Local Agent paired and connected to this Mac.")
		return nil
	}
	const url = "https://dash.cloudflare.com/profile/api-tokens"
	_, _ = fmt.Fprintln(d.out, "Create a token using the 'Create additional tokens' template in Cloudflare. Keep its API Tokens: Edit permission and broad resource access, with no additional permissions or Passlet allowlist.\n\nPasslet stores this credential in Touch ID-protected Keychain. Each agent receives only the exact account or zone permissions you approve, for one hour. Setup discovers account and zone names using temporary read-only tokens, revokes them, and caches only names and IDs on this Mac.\n\n"+url)
	if err := d.open(url); err != nil {
		_, _ = fmt.Fprintln(d.out, "Open the URL above to continue.")
	}
	token, err := d.prompt()
	if err != nil {
		return errors.New("cloudflare_setup_cancelled")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("cloudflare_token_required")
	}
	credentials, err := d.provider.Verify(ctx, token)
	if err != nil {
		return err
	}
	if err := d.store(ctx, *helper, credentials); err != nil {
		return err
	}
	if err := d.provider.RefreshResources(ctx, credentials); err != nil {
		return err
	}
	if err := d.pair(); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(d.out, "Cloudflare setup complete. Local Agent paired and connected to this Mac.")
	return nil
}

func execCloudflare(ctx context.Context, grant passlet.Grant, args []string) error {
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "CF_API_EMAIL", "CF_API_KEY", "CF_API_TOKEN", "CF_ACCOUNT_ID", "CLOUDFLARE_API_EMAIL", "CLOUDFLARE_API_KEY", "CLOUDFLARE_API_TOKEN", "CLOUDFLARE_ACCOUNT_ID":
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	cmd.Env = append(cmd.Env, "CLOUDFLARE_API_TOKEN="+grant.Token)
	if grant.Account != "" {
		cmd.Env = append(cmd.Env, "CLOUDFLARE_ACCOUNT_ID="+grant.Account)
	}
	if err := cmd.Run(); err != nil {
		return errors.New("command_failed")
	}
	return nil
}
