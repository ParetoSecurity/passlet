package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"passlet.dev/passlet/internal/passlet"
)

func TestDoctorFeatures(t *testing.T) {
	suite := godog.TestSuite{
		Options: &godog.Options{Format: "pretty", Paths: []string{"features/doctor.feature"}, Strict: true, TestingT: t},
		ScenarioInitializer: func(s *godog.ScenarioContext) {
			var dir, endpoint, output, helperError string
			var result error
			var expectFailure, reachable bool
			var cleanup func()
			var providerCalls atomic.Int32
			s.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
				var err error
				dir, err = os.MkdirTemp("/tmp", "pl-doctor-")
				if err != nil {
					return ctx, err
				}
				endpoint = "unix://" + filepath.Join(dir, "socket")
				output, helperError = "", ""
				result, cleanup = nil, nil
				expectFailure, reachable = false, false
				providerCalls.Store(0)
				t.Setenv("HOME", dir)
				t.Setenv("PASSLET_ENDPOINT", "")
				return ctx, passlet.SaveEndpoint(endpoint)
			})
			s.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
				if cleanup != nil {
					cleanup()
				}
				defer os.RemoveAll(dir)
				if (result != nil) != expectFailure {
					return ctx, fmt.Errorf("unexpected command result: %v", result)
				}
				_, err := os.Stat(filepath.Join(dir, "helper-invoked"))
				if reachable && err != nil {
					return ctx, fmt.Errorf("diagnostic did not request approval: %v", err)
				}
				if !reachable && !os.IsNotExist(err) {
					return ctx, fmt.Errorf("unreachable Conductor requested approval: %v", err)
				}
				want := int32(0)
				if reachable && helperError == "" {
					want = 1
				}
				if providerCalls.Load() != want {
					return ctx, fmt.Errorf("got %d provider calls, want %d", providerCalls.Load(), want)
				}
				return ctx, nil
			})
			s.Step(`^the selected Conductor is unreachable$`, func() {})
			s.Step(`^the local Conductor service is stopped$`, func() error {
				endpoint = "unix://" + passlet.DefaultSocket()
				return passlet.SaveEndpoint(endpoint)
			})
			s.Step(`^a Conductor (with valid|without) GitHub credentials$`, func(state string) error {
				reachable = true
				if state == "without" {
					helperError = "credentials_missing"
				}
				api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					providerCalls.Add(1)
					if r.Method != "GET" || r.URL.Path != "/user" || r.Header.Get("Authorization") != "Bearer fixture-parent-secret" {
						t.Error("doctor called an unexpected provider endpoint or attempted to issue a token")
						w.WriteHeader(400)
						return
					}
					fmt.Fprint(w, `{"login":"alice"}`)
				}))
				cleanup = api.Close
				audit, err := passlet.OpenAudit(filepath.Join(dir, "audit.sqlite"))
				if err != nil {
					return err
				}
				cleanup = func() { api.Close(); _ = audit.Close() }
				provider := &passlet.GitHub{HTTP: api.Client(), API: api.URL, OAuth: api.URL + "/unexpected-refresh"}
				handler := passlet.NewServer(passlet.NativeHelper{Path: filepath.Join(dir, "helper")}, provider, audit)
				listener, err := passlet.ListenUnix(filepath.Join(dir, "socket"))
				if err != nil {
					return err
				}
				server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/v1/health" && r.URL.Path != "/v1/doctor" {
						t.Error("doctor requested a grant")
					}
					handler.ServeHTTP(w, r)
				}), ConnContext: passlet.ConnectionContext}
				go func() { _ = server.Serve(listener) }()
				cleanup = func() { _ = server.Close(); api.Close(); _ = audit.Close() }
				return nil
			})
			s.Step(`^I (approve|deny) the diagnostic check$`, func(decision string) {
				if decision == "deny" {
					helperError = "approval_denied"
				}
			})
			s.Step("^I run `passlet doctor`$", func() error {
				credentials := passlet.Credentials{ClientID: "fixture-client", ClientSecret: "fixture-client-secret", AccessToken: "fixture-parent-secret", RefreshToken: "fixture-refresh-secret", ExpiresAt: time.Now().Add(time.Hour), Account: passlet.AccountInfo{Login: "alice", AppID: 123, AppURL: "https://github.com/apps/passlet-alice", VerifiedAt: time.Now()}}
				reply := map[string]any{"ok": true, "credentials": credentials}
				if helperError != "" {
					reply = map[string]any{"ok": false, "error": helperError}
				}
				data, err := json.Marshal(reply)
				if err != nil {
					return err
				}
				script := "#!/bin/sh\nIFS= read -r request || exit 1\n: > \"$HOME/helper-invoked\"\nprintf '%s\\n' '" + string(data) + "'\ncat >/dev/null\n"
				if err := os.WriteFile(filepath.Join(dir, "helper"), []byte(script), 0700); err != nil {
					return err
				}
				output, result = invokeCLICommand(t, "", func() error {
					err := run()
					if err != nil {
						writeCommandError(os.Stdout, err)
					}
					return err
				}, "doctor")
				for _, secret := range []string{credentials.ClientSecret, credentials.AccessToken, credentials.RefreshToken} {
					if strings.Contains(output, secret) {
						return fmt.Errorf("protected credential leaked to terminal")
					}
				}
				return nil
			})
			s.Step(`^the terminal shows:$`, func(doc *godog.DocString) error {
				want := strings.ReplaceAll(doc.Content, "{endpoint}", endpoint) + "\n"
				if output != want {
					return fmt.Errorf("terminal output:\n%s\nwant:\n%s", output, want)
				}
				return nil
			})
			s.Step(`^the command fails$`, func() error {
				expectFailure = true
				if result == nil {
					return fmt.Errorf("command succeeded")
				}
				return nil
			})
		},
	}
	if suite.Run() != 0 {
		t.Fatal("doctor scenarios failed")
	}
}
