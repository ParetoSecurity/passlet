package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"passlet.dev/passlet/internal/passlet"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupRejectsInvalidInputBeforeBrowser(t *testing.T) {
	for _, tc := range []struct {
		name          string
		args          []string
		input         string
		missingHelper bool
	}{
		{"missing command", nil, "", false},
		{"unknown flag", []string{"setup", "--unknown"}, "", false},
		{"relative helper", []string{"setup", "--helper", "relative"}, "", false},
		{"missing helper", []string{"setup"}, "", true},
		{"missing reset helper", []string{"reset"}, "", true},
		{"unsupported permission override", []string{"setup", "--permissions", "contents:read"}, "", false},
		{"missing handle confirmation", []string{"setup"}, "", false},
		{"invalid handle", []string{"setup", "--handle", "bad/name"}, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "helper")
			if !tc.missingHelper {
				if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s' '{\"status\":\"missing\"}'\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			var out bytes.Buffer
			err := setupWithDependencies(context.Background(), tc.args, setupDependencies{helper: path, output: &out, input: bufio.NewReader(strings.NewReader(tc.input)), open: func(string) error { t.Fatal("invalid input opened browser"); return nil }})
			if err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
}

func TestSetupLookupFailureDoesNotOpenBrowser(t *testing.T) {
	f := newSetupFixture(t, "success")
	f.github.server.Close()
	result := f.run()
	if result.err == nil || result.browserOpens != 0 || result.imported {
		t.Fatalf("lookup failure continued setup: %+v", result)
	}
}

func TestExistingSetupBrowserFailureShowsManualURL(t *testing.T) {
	f := newSetupFixture(t, "existing")
	f.action = func(step string) error {
		if step == "install" {
			f.cancel()
			return errors.New("browser failed")
		}
		return nil
	}
	result := f.run()
	if result.err == nil || result.keychain != "existing" || !strings.Contains(result.output, "Could not open the browser automatically. Open the URL above to continue.") || !strings.Contains(result.output, "https://github.com/apps/passlet-alice/installations/new") {
		t.Fatalf("missing manual browser fallback: %+v", result)
	}
}

func TestSetupDefaultApprovalAndNativeCommandWiring(t *testing.T) {
	f := newSetupFixture(t, "protected")
	err := setupWithDependencies(f.ctx, []string{"setup"}, setupDependencies{helper: f.helper, output: &f.output})
	if err == nil || err.Error() != "approval_denied" {
		t.Fatalf("default native approval bypassed: %v", err)
	}
	// No real helper or browser is invoked: argument validation fails first.
	_, err = invokeCLI(t, "", "github", "setup", "--helper", "relative")
	if err == nil {
		t.Fatal("native command accepted relative helper")
	}
}

type invalidAccountSession struct{}

func (invalidAccountSession) Credentials() passlet.Credentials { return passlet.Credentials{} }
func (invalidAccountSession) Store(passlet.Credentials) error  { return nil }
func (invalidAccountSession) Close()                           {}

func TestSetupRejectsInvalidProtectedIdentity(t *testing.T) {
	f := newSetupFixture(t, "existing")
	err := setupWithDependencies(f.ctx, []string{"setup"}, setupDependencies{helper: f.helper, output: &f.output, approve: func(context.Context, string) (passlet.CredentialSession, error) { return invalidAccountSession{}, nil }})
	if err == nil || err.Error() != "invalid_protected_account_identity" {
		t.Fatalf("invalid identity used: %v", err)
	}
	if !strings.Contains(f.output.String(), "passlet github reset") {
		t.Fatal("missing account recovery guidance")
	}
}

func TestSetupCancellationBeforeBrowser(t *testing.T) {
	f := newSetupFixture(t, "success")
	f.action = func(step string) error {
		if step == "browser" {
			f.cancel()
		}
		return nil
	}
	if result := f.run(); !errors.Is(result.err, context.Canceled) || result.browserOpens != 0 {
		t.Fatalf("cancelled setup opened browser: %+v", result)
	}
}

func TestGitHubSetupFailuresDoNotSaveCredentials(t *testing.T) {
	scenarios := []struct {
		name      string
		fixture   string
		wantError string
	}{
		{"user declines unlocking existing credentials", "protected", "approval_denied"},
		{"user denies GitHub authorization", "oauth_denied", "github_authorization_denied"},
		{"app has not been installed", "missing_installation", "github_app_has_no_accessible_installation"},
		{"helper cannot save credentials", "storage_failure", "keychain_import_failed"},
		{"user cancels during installation", "cancel", "setup_cancelled_restart_setup"},
		{"terminal closes before confirmation", "input_closed", "setup_terminal_confirmation_required"},
		{"browser cannot open", "browser_failure", "setup_cancelled_restart_setup"},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			result := newSetupFixture(t, scenario.fixture).run()

			if result.err == nil || result.err.Error() != scenario.wantError {
				t.Fatalf("error = %v; want %q", result.err, scenario.wantError)
			}
			if result.imported {
				t.Error("failed setup imported credentials")
			}
			if result.accountSaved {
				t.Error("failed setup saved account identity")
			}
			if strings.Contains(result.output, "GitHub setup complete.") {
				t.Error("failed setup reported success")
			}
			if scenario.fixture == "protected" && result.browserOpens != 0 {
				t.Error("inconclusive Keychain status must not start app creation")
			}
			assertSetupOutputContainsNoSecrets(t, result)
		})
	}
}

func assertSetupOutputContainsNoSecrets(t *testing.T, result setupResult) {
	t.Helper()
	output := result.output + fmt.Sprint(result.err)
	for _, secret := range []string{
		"fixture-parent-secret", "fixture-refresh-secret", "fixture-client-secret",
		"fixture-unused-private-key", "fixture-browser-secret",
	} {
		if strings.Contains(output, secret) {
			t.Errorf("CLI output disclosed %q", secret)
		}
	}
}

func TestGitHubResetRequiresOnlyNativeApproval(t *testing.T) {
	for _, mode := range []string{"existing", "protected"} {
		t.Run(mode, func(t *testing.T) {
			f := newSetupFixture(t, mode)
			err := setupWithDependencies(f.ctx, []string{"reset"}, setupDependencies{
				helper: f.helper, output: &f.output,
				prompt: func(string, string) (string, error) {
					t.Fatal("reset requested terminal confirmation")
					return "", nil
				},
			})
			_, statErr := os.Stat(f.marker)
			if mode == "protected" {
				if err == nil || err.Error() != "approval_denied" || statErr != nil {
					t.Fatalf("denied approval removed credentials: %v, %v", err, statErr)
				}
			} else if err != nil || !os.IsNotExist(statErr) {
				t.Fatalf("confirmed removal failed: %v, %v", err, statErr)
			}
		})
	}
}
