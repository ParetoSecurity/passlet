package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cucumber/godog"
	"passlet.dev/passlet/internal/passlet"
)

func TestHelpFeatures(t *testing.T) {
	suite := godog.TestSuite{
		Options: &godog.Options{Format: "pretty", Paths: []string{"features/help.feature"}, Strict: true, TestingT: t},
		ScenarioInitializer: func(s *godog.ScenarioContext) {
			var output string
			var result error
			s.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
				t.Setenv("HOME", t.TempDir())
				t.Setenv("PASSLET_ENDPOINT", "invalid-fixture-endpoint")
				dir := filepath.Join(os.Getenv("HOME"), ".config", "passlet")
				if err := os.MkdirAll(dir, 0700); err != nil {
					return ctx, err
				}
				if err := os.WriteFile(filepath.Join(dir, "approver.toml"), []byte("invalid-fixture-config"), 0600); err != nil {
					return ctx, err
				}
				output, result = "", nil
				return ctx, nil
			})
			s.Step("^I run `passlet(.*)`$", func(command string) {
				args := strings.Fields(command)
				output, result = invokeCLICommand(t, "", func() error {
					previous := os.Stderr
					os.Stderr = os.Stdout
					defer func() { os.Stderr = previous }()
					return runWithOverview(func(context.Context) error { t.Error("help inspected service configuration"); return nil })
				}, args...)
			})
			s.Step(`^the terminal shows:$`, func(doc *godog.DocString) error {
				expected := strings.NewReplacer(
					"{helper}", defaultHelper(),
					"{version}", version,
					"{state_dir}", passlet.StateDir(),
				).Replace(doc.Content) + "\n"
				// Render Go flag help tabs at terminal tab stops without mixing indentation in Gherkin.
				terminalOutput := strings.ReplaceAll(output, "\n    \t", "\n        ")
				if terminalOutput != expected {
					return fmt.Errorf("unexpected output: %q; want %q", output, expected)
				}
				return nil
			})
			s.After(func(ctx context.Context, _ *godog.Scenario, scenarioErr error) (context.Context, error) {
				if scenarioErr != nil {
					return ctx, scenarioErr
				}
				if result != nil {
					return ctx, fmt.Errorf("help failed: %w", result)
				}
				if _, err := os.Stat(passlet.StateDir()); !os.IsNotExist(err) {
					return ctx, fmt.Errorf("help created service state: %v", err)
				}
				return ctx, nil
			})
		},
	}
	if suite.Run() != 0 {
		t.Fatal("help scenarios failed")
	}
}

func TestCommandOverviewsMatchHelp(t *testing.T) {
	for _, command := range []string{"", "approver", "approver github"} {
		t.Run(strings.TrimSpace("passlet "+command), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("PASSLET_ENDPOINT", "invalid-ignored-endpoint")
			args := strings.Fields(command)
			overview, err := invokeCLI(t, "", args...)
			if err != nil {
				t.Fatal(err)
			}
			help, err := invokeCLI(t, "", append(args, "--help")...)
			if err != nil {
				t.Fatal(err)
			}
			if overview == "" || overview != help {
				t.Fatalf("overview %q does not match help %q", overview, help)
			}
		})
	}
}

func TestGitHubHelpBeforePlatformCheck(t *testing.T) {
	for _, command := range []string{"setup", "reset"} {
		var output bytes.Buffer
		deps := setupDependencies{hostOS: "linux", output: &output, helper: "/fixture-helper"}
		if err := setupWithDependencies(context.Background(), []string{command, "--help"}, deps); !errors.Is(err, flag.ErrHelp) || output.Len() == 0 {
			t.Fatalf("Linux help failed: %q, %v", output.String(), err)
		}
		want := errSetupRequiresMac.Error()
		if err := setupWithDependencies(context.Background(), []string{command}, deps); err == nil || err.Error() != want {
			t.Fatalf("Linux operation was not rejected: %v", err)
		}
	}
}
