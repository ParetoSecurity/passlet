package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cucumber/godog"
	"passlet.dev/passlet/internal/passlet"
)

func TestAuditFeatures(t *testing.T) {
	suite := godog.TestSuite{
		Options: &godog.Options{Format: "pretty", Paths: []string{"features/audit.feature"}, Strict: true, TestingT: t},
		ScenarioInitializer: func(s *godog.ScenarioContext) {
			var path, output, stdout, stderr string
			var expectFailure bool
			var result error
			s.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
				t.Setenv("HOME", t.TempDir())
				t.Setenv("PASSLET_ENDPOINT", "invalid-fixture-endpoint")
				path = filepath.Join(passlet.StateDir(), "audit.sqlite")
				output, stdout, stderr, result = "", "", "", nil
				expectFailure = false
				return ctx, nil
			})
			empty := func() error {
				audit, err := passlet.OpenAudit(path)
				if err != nil {
					return err
				}
				return audit.Close()
			}
			s.Step(`^the audit log is empty$`, empty)
			s.Step(`^the audit log contains:$`, func(table *godog.Table) error {
				if err := empty(); err != nil {
					return err
				}
				db, err := sql.Open("sqlite", path)
				if err != nil {
					return err
				}
				defer db.Close()
				for _, row := range table.Rows[1:] {
					event := passlet.Event{
						RequestID: row.Cells[1].Value, Outcome: row.Cells[2].Value, Approver: "human",
						Caller:  passlet.Caller{Name: "cruncher", Transport: "ssh"},
						Request: passlet.Request{Agent: "Claude", Repositories: []string{"alice/repo"}, Permissions: map[string]string{"contents": "write", "metadata": "read"}},
					}
					data, err := json.Marshal(event)
					if err != nil {
						return err
					}
					if _, err := db.Exec("INSERT INTO events(at,request_id,outcome,data) VALUES(?,?,?,?)", row.Cells[0].Value, event.RequestID, event.Outcome, string(data)); err != nil {
						return err
					}
				}
				return nil
			})
			s.Step(`^no audit database (?:exists|is created)$`, func() error {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					return fmt.Errorf("audit database exists: %v", err)
				}
				return nil
			})
			s.Step(`^the audit log contains a corrupted record$`, func() error {
				db, err := sql.Open("sqlite", path)
				if err != nil {
					return err
				}
				defer db.Close()
				_, err = db.Exec(`INSERT INTO events(at,request_id,outcome,data) VALUES('2026-11-03T12:01:00Z','broken','requested','corrupted-fixture')`)
				return err
			})
			s.Step("^I run `passlet (approver audit[^`]*)`$", func(command string) error {
				errorFile, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
				if err != nil {
					return err
				}
				defer errorFile.Close()
				stdout, result = invokeCLICommand(t, "", func() error {
					previous := os.Stderr
					os.Stderr = errorFile
					defer func() { os.Stderr = previous }()
					err := run()
					if err != nil {
						writeCommandError(os.Stderr, err)
					}
					return err
				}, strings.Fields(command)...)
				data, err := os.ReadFile(errorFile.Name())
				stderr = string(data)
				output = stderr + stdout
				return err
			})
			s.Step(`^the command exits unsuccessfully$`, func() error {
				expectFailure = true
				if result == nil {
					return fmt.Errorf("audit unexpectedly succeeded")
				}
				return nil
			})
			s.Step(`^the warning is written to standard error$`, func() error {
				if stderr != "Warning: audit log contains corrupted entries (1 skipped).\n" || strings.Contains(stdout, "Warning:") {
					return fmt.Errorf("audit warning used the wrong output stream")
				}
				return nil
			})
			s.Step(`^standard output is valid JSON$`, func() error {
				if !json.Valid([]byte(stdout)) {
					return fmt.Errorf("invalid JSON on stdout: %q", stdout)
				}
				return nil
			})
			s.Step(`^the terminal shows:$`, func(doc *godog.DocString) error {
				if output != doc.Content+"\n" {
					return fmt.Errorf("terminal output: %q; want %q", output, doc.Content+"\n")
				}
				return nil
			})
			s.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
				if (result != nil) != expectFailure {
					return ctx, fmt.Errorf("unexpected command result: %v", result)
				}
				return ctx, nil
			})
		},
	}
	if suite.Run() != 0 {
		t.Fatal("audit scenarios failed")
	}
}
