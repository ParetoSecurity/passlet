package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/peterh/liner"

	"passlet.dev/passlet/internal/passlet"
)

func githubCommand(ctx context.Context, args []string) error { // coverage-ignore: OS adapter assembles native terminal/browser dependencies; setupWithDependencies is exercised on both platforms.
	if len(args) == 0 || (args[0] != "setup" && args[0] != "reset") {
		return errors.New("usage: passlet github setup | reset")
	}
	if runtime.GOOS != "darwin" {
		return errors.New("run_setup_on_your_Mac")
	}
	return setupWithDependencies(ctx, args, setupDependencies{
		input: bufio.NewReader(os.Stdin), output: os.Stdout, provider: passlet.NewGitHub(),
		helper:  defaultHelper(),
		suggest: configuredGitHubHandle,
		prompt:  promptSetupLine,
		open: func(u string) error {
			return exec.CommandContext(ctx, "/usr/bin/open", u).Run()
		},
	})
}

// Dependencies are supplied by the command, never by environment-based test modes.
type setupDependencies struct {
	approve  func(context.Context, string) (passlet.CredentialSession, error)
	input    *bufio.Reader
	output   io.Writer
	provider passlet.SetupProvider
	helper   string
	open     func(string) error
	suggest  func(context.Context) string
	prompt   func(string, string) (string, error)
}

func setupWithDependencies(ctx context.Context, args []string, deps setupDependencies) (result error) {
	defer func() {
		var failure *passlet.SetupError
		if errors.As(result, &failure) {
			writeSetupText(deps.output, failure.Terminal)
		}
		if result != nil && (result.Error() == "approval_denied" || result.Error() == "approval_cancelled") {
			command := "setup"
			if len(args) > 0 {
				command = args[0]
			}
			writeSetupText(deps.output, "GitHub "+command+" canceled.")
		}
	}()
	if len(args) == 0 || (args[0] != "setup" && args[0] != "reset") {
		return errors.New("usage: passlet github setup | reset")
	}
	fs := flag.NewFlagSet("github "+args[0], flag.ContinueOnError)
	fs.SetOutput(deps.output)
	githubUser := fs.String("github-user", "", "GitHub handle used to pre-fill the App name (prompted if omitted)")
	helper := fs.String("helper", deps.helper, "signed helper path")
	permissions := fs.String("permissions", passlet.DefaultSetupPermissions, "maximum App permissions; each agent request is separately scoped")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 || !filepath.IsAbs(*helper) {
		return errors.New("setup_requires_absolute_helper")
	}
	if args[0] == "reset" {
		return resetGitHub(ctx, *helper, deps)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:45821")
	if err != nil {
		writeSetupText(deps.output, "Cannot start GitHub setup because localhost port 45821 is already in use.\nClose the application using that port, then run `passlet github setup` again.")
		return errors.New("setup_listener_unavailable")
	}
	defer func() { _ = listener.Close() }()
	state, err := passlet.KeychainStatus(ctx, *helper)
	if err != nil {
		return err
	}
	if state != "missing" {
		approve := deps.approve
		if approve == nil {
			approve = (passlet.NativeHelper{Path: *helper}).Approve
		}
		session, err := approve(ctx, "view your configured Passlet GitHub App")
		if err != nil {
			return err
		}
		credentials := session.Credentials()
		account := credentials.Account
		session.Close()
		if err := account.Validate(); err != nil {
			return err
		}

		if err := deps.provider.VerifyIdentity(ctx, credentials, account.Login); err != nil {
			if err.Error() == "github_http_401" {
				writeSetupText(deps.output, fmt.Sprintf("GitHub no longer accepts the saved credentials for %s. The app may have been deleted or its authorization revoked.\nRun `passlet github reset`, then `passlet github setup` to set up again.", strings.TrimPrefix(account.AppURL, "https://github.com/apps/")))
			}
			return err
		}

		writeSetupText(deps.output, fmt.Sprintf("Using your GitHub App: %s\nSigned in as: %s", strings.TrimPrefix(account.AppURL, "https://github.com/apps/"), account.Login))
		writeSetupText(deps.output, "Choose an organization in your browser, then select \"All repositories\".")
		return passlet.RunInstallationSetup(ctx, listener, credentials, deps.provider, func(_ context.Context, verified passlet.AccountInfo) error {
			writeSetupText(deps.output, fmt.Sprintf("GitHub access enabled for %s.\n\nRun `passlet github setup` again to add another organization.", verified.InstallationOwner))
			return nil
		}, func(u string) error {
			writeSetupText(deps.output, fmt.Sprintf("If your browser didn't open:\n%s", u))
			writeSetupText(deps.output, "Waiting for installation…")
			if err := deps.open(u); err != nil {
				_, _ = fmt.Fprintln(deps.output, "Could not open the browser automatically. Open the URL above to continue.")
			}
			return nil
		})
	}
	perms := map[string]string{}
	for _, p := range strings.Split(*permissions, ",") {
		kv := strings.Split(p, ":")
		if len(kv) != 2 || perms[kv[0]] != "" {
			return errors.New("invalid_permissions")
		}
		perms[kv[0]] = kv[1]
	}

	if *githubUser == "" {
		initial := ""
		if deps.suggest != nil {
			initial = deps.suggest(ctx)
		}
		line, err := setupReadLine(deps, "Please enter your GitHub handle: ", initial)
		if err != nil {
			return err
		}
		*githubUser = strings.TrimSpace(line)
	}
	name, err := passlet.GitHubAppName(*githubUser)
	if err != nil {
		return err
	}
	exists, err := deps.provider.AppExists(ctx, name)
	if err != nil {
		return err
	}
	if exists {
		writeSetupText(deps.output, fmt.Sprintf("A GitHub App named %s already exists, but its credentials aren't saved in this Mac's Keychain.\n\nIf you own this app, review it at https://github.com/settings/apps/%s/advanced before continuing. It may still be in use on another Mac.", name, name))
		return errors.New("github_app_name_taken")
	}
	writeSetupIntroduction(deps.output)
	if _, err := setupReadLine(deps, "", ""); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	open := func(u string) error {
		writeSetupText(deps.output, fmt.Sprintf("If your browser didn't open, open this URL:\n%s", u))
		if err := deps.open(u); err != nil {
			_, _ = fmt.Fprintln(deps.output, "Could not open the browser automatically. Open the URL above to continue.")
		}
		return nil
	}

	return passlet.RunSetup(ctx, listener, *githubUser, perms, deps.provider, func(ctx context.Context, c passlet.Credentials, account passlet.AccountInfo) error {
		writeSetupVerification(deps.output, account)
		if err := passlet.ImportCredentials(ctx, *helper, c); err != nil {
			return err
		}

		writeSetupText(deps.output, fmt.Sprintf("GitHub setup complete.\n\nApp: %s\nAccount: %s\n\nRun `passlet github setup` again to add another organization.", strings.TrimPrefix(account.AppURL, "https://github.com/apps/"), account.Login))
		return nil
	}, open, func(ctx context.Context) error {
		writeInstallationInstructions(deps.output)
		return nil
	})
}

const setupIntroduction = `Passlet creates short-lived scoped tokens for your agents by acting as a GitHub App. Let's create one for you!

This GitHub App is yours alone. Its credentials are stored in your Mac’s Keychain, protected by Touch ID, and used only to authenticate with GitHub. They are never sent to Passlet’s servers.

Agents get only the short-lived, scoped access you approve with Touch ID—never your app’s full credentials.

Passlet will start a temporary listener on localhost to receive GitHub's callbacks and verify your app setup. The listener runs only while ` + "`passlet github setup`" + ` is running.

Press Enter to create your personal GitHub App in your browser.`

func promptSetupLine(prompt, initial string) (string, error) { // coverage-ignore: Interactive liner terminal adapter; editable input and Ctrl+C require the manual terminal checklist.
	line := liner.NewLiner()
	defer func() { _ = line.Close() }()
	line.SetCtrlCAborts(true)
	value, err := line.PromptWithSuggestion(prompt, initial, len([]rune(initial)))
	if err != nil {
		return "", errors.New("setup_cancelled")
	}
	return value, nil
}

func writeInstallationInstructions(out io.Writer) {
	writeSetupText(out, "Follow the instructions in your browser to install your app for an organization.\n\nSelect \"All repositories\" so your agents can request access to any repository in that organization.\n\nWaiting for installation…")
}
func writeSetupVerification(out io.Writer, account passlet.AccountInfo) {
	writeSetupText(out, fmt.Sprintf("Verified: %s can access repositories in %s through %s.", account.Login, account.InstallationOwner, strings.TrimPrefix(account.AppURL, "https://github.com/apps/")))
}

func setupReadLine(deps setupDependencies, prompt, initial string) (string, error) {
	if deps.prompt != nil {
		return deps.prompt(prompt, initial)
	}
	_, _ = fmt.Fprint(deps.output, prompt)
	line, err := deps.input.ReadString('\n')
	if err != nil {
		return "", errors.New("setup_terminal_confirmation_required")
	}
	line = strings.TrimSpace(line)
	if line == "" {
		line = initial
	}
	return line, nil
}
func resetGitHub(ctx context.Context, helper string, deps setupDependencies) error {
	state, err := passlet.KeychainStatus(ctx, helper)
	if err != nil {
		return err
	}
	if state == "missing" {
		writeSetupText(deps.output, "No GitHub credentials are saved in Keychain. Nothing to do.")
		return nil
	}
	writeSetupText(deps.output, "This removes Passlet's GitHub credentials from your Mac's Keychain.\nYour GitHub App and its installations will remain on GitHub.")
	answer, err := setupReadLine(deps, "Remove GitHub credentials? [y/N]", "")
	if err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(answer), "y") {
		writeSetupText(deps.output, "GitHub reset canceled.")
		return nil
	}
	account, err := (passlet.NativeHelper{Path: helper}).Reset(ctx)
	if err != nil {
		return err
	}
	writeSetupText(deps.output, "GitHub credentials removed. Run `passlet github setup` to set up again.\n\nYour previous GitHub App still exists. To remove it, open "+passlet.GitHubAppSettingsURL(account.AppURL)+"/advanced and click \"Delete GitHub App\".")
	return nil
}
