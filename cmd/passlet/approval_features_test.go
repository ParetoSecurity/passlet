package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cucumber/godog"
	"passlet.dev/passlet/internal/passlet"
)

func TestApprovalFeatures(t *testing.T) {
	suite := godog.TestSuite{
		Options: &godog.Options{Format: "pretty", Paths: []string{"features/approval.feature"}, Strict: true, TestingT: t},
		ScenarioInitializer: func(s *godog.ScenarioContext) {
			var caller passlet.Caller
			var capture string
			var repositories []string
			s.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
				caller = passlet.Caller{}
				capture = ""
				repositories = nil
				t.Setenv("HOME", t.TempDir())
				t.Setenv("PASSLET_ENDPOINT", "")
				t.Setenv("CLAUDECODE", "")
				t.Setenv("CODEX_THREAD_ID", "")
				return ctx, nil
			})
			s.Step(`^the request is made from Claude Code$`, func() {
				t.Setenv("CLAUDECODE", "1")
			})
			s.Step(`^the request is made from Codex$`, func() {
				t.Setenv("CODEX_THREAD_ID", "fixture-thread")
			})
			s.Step("^(?:an Agent on this Mac|an Approver configured for GitHub account `alice`)$", func() {
				caller = passlet.Caller{Transport: "unix", Name: "local/SSH socket client"}
			})
			s.Step(`^an Agent connected through the "([^"]*)" SSH tunnel$`, func(label string) {
				caller = passlet.Caller{Transport: "ssh", Name: label}
			})
			s.Step(`^an Agent connected through Tailscale as "([^"]*)"$`, func(host string) {
				caller = passlet.Caller{Transport: "tailscale", Name: host, NodeID: "fixture-node"}
			})
			s.Step("^repositories `alice/repository-001` through `alice/repository-100`$", func() {
				for i := 1; i <= 100; i++ {
					repositories = append(repositories, fmt.Sprintf("alice/repository-%03d", i))
				}
			})
			s.Step("^I run `passlet (request [^`]+|doctor)`$", func(command string) error {
				command = strings.ReplaceAll(command, "{repositories}", strings.Join(repositories, ","))
				dir, err := os.MkdirTemp("/tmp", "pl-approval-")
				if err != nil {
					return err
				}
				t.Cleanup(func() { _ = os.RemoveAll(dir) })
				capture = filepath.Join(dir, "prompt.json")
				helper := filepath.Join(dir, "helper")
				quotedCapture := "'" + strings.ReplaceAll(capture, "'", "'\\''") + "'"
				// Capture the real helper message and deny before any credentials can be accessed.
				script := "#!/bin/sh\nIFS= read -r request || exit 1\nprintf '%s\\n' \"$request\" > " + quotedCapture + "\nprintf '%s\\n' '{\"ok\":false,\"error\":\"approval_denied\"}'\n"
				if err := os.WriteFile(helper, []byte(script), 0700); err != nil {
					return err
				}
				audit, err := passlet.OpenAudit(filepath.Join(dir, "audit.sqlite"))
				if err != nil {
					return err
				}
				defer audit.Close()
				handler := passlet.NewServer(passlet.NativeHelper{Path: helper}, nil, audit)
				identity := caller
				handler.Identity = func(context.Context, string) (passlet.Caller, error) { return identity, nil }
				socket := filepath.Join(dir, "socket")
				listener, err := passlet.ListenUnix(socket)
				if err != nil {
					return err
				}
				server := &http.Server{Handler: handler}
				go func() { _ = server.Serve(listener) }()
				defer server.Close()
				grant := filepath.Join(dir, "grant.json")
				args := append(strings.Fields(command), "--endpoint", "unix://"+socket)
				denial := "approval_denied"
				if command == "doctor" {
					denial = "Approval was denied."
				} else {
					args = append(args, "--out", grant)
				}
				_, err = invokeCLI(t, "", args...)
				if err == nil || err.Error() != denial {
					return fmt.Errorf("expected fixture denial, got %v", err)
				}
				if _, err := os.Stat(grant); !os.IsNotExist(err) {
					return fmt.Errorf("denied preview created a grant: %v", err)
				}
				return nil
			})
			s.Step(`^the Touch ID pane shows:$`, func(doc *godog.DocString) error {
				data, err := os.ReadFile(capture)
				if err != nil {
					return err
				}
				var message struct{ Op, Prompt string }
				if err := json.Unmarshal(data, &message); err != nil {
					return err
				}
				// Include the English macOS wrapper so scenarios describe the visible sentence.
				paneText := "Passlet is trying to " + message.Prompt + "."
				if message.Op != "approve" || paneText != doc.Content {
					return fmt.Errorf("unexpected helper message: op=%q pane=%q; want %q", message.Op, paneText, doc.Content)
				}
				return nil
			})
		},
	}
	if suite.Run() != 0 {
		t.Fatal("approval text scenarios failed")
	}
}
