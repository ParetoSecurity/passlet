package main

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
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
	"passlet.dev/passlet/testutil/testkeys"
)

func TestDoctorFeatures(t *testing.T) {
	suite := godog.TestSuite{
		Options: &godog.Options{Format: "pretty", Paths: []string{"features/doctor.feature"}, Strict: true, TestingT: t},
		ScenarioInitializer: func(s *godog.ScenarioContext) {
			var dir, endpoint, output, helperError string
			var result error
			var expectFailure, reachable bool
			var cleanup func()
			var showStatus func(context.Context) error
			var tailscaleFixture *overviewFixture
			var socketListeners map[string]net.Listener
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
				showStatus = overview
				tailscaleFixture = nil
				socketListeners = make(map[string]net.Listener)
				expectFailure, reachable = false, false
				providerCalls.Store(0)
				t.Setenv("HOME", dir)
				t.Setenv("PASSLET_ENDPOINT", "")
				return ctx, testkeys.SaveEndpoint(endpoint)
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
					return ctx, fmt.Errorf("unreachable Approver requested approval: %v", err)
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
			s.Step(`^I am checking the local Approver$`, func() error {
				endpoint = "unix://" + passlet.DefaultSocket()
				t.Setenv("PASSLET_ENDPOINT", "invalid-ignored-endpoint")
				return testkeys.SaveEndpoint("https://other.example.ts.net:17400")
			})
			s.Step(`^the (?:forwarded|local) socket is stale$`, func() error {
				if err := os.MkdirAll(filepath.Dir(strings.TrimPrefix(endpoint, "unix://")), 0700); err != nil {
					return err
				}
				listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: strings.TrimPrefix(endpoint, "unix://"), Net: "unix"})
				if err != nil {
					return err
				}
				listener.SetUnlinkOnClose(false)
				return listener.Close()
			})
			s.Step(`^the (?:forwarded|local) socket returns no healthy Approver response$`, func() error {
				listener, err := passlet.ListenUnix(strings.TrimPrefix(endpoint, "unix://"))
				if err != nil {
					return err
				}
				server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != "GET" || r.URL.Path != "/v1/health" {
						t.Error("requested more than health")
					}
					w.WriteHeader(http.StatusBadGateway)
				})}
				listener = pairedFixture(t, server, listener)
				go func() { _ = server.Serve(listener) }()
				cleanup = func() { _ = server.Close() }
				return nil
			})
			s.Step(`^the selected Approver is unreachable$`, func() {})
			s.Step(`^the local Approver service is stopped$`, func() error {
				endpoint = "unix://" + passlet.DefaultSocket()
				return testkeys.SaveEndpoint(endpoint)
			})
			s.Step(`^an Approver (with valid|without) GitHub credentials$`, func(state string) error {
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
				listener, err := passlet.ListenUnix(strings.TrimPrefix(endpoint, "unix://"))
				if err != nil {
					return err
				}
				server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/v1/health" && r.URL.Path != "/v1/doctor" {
						t.Error("doctor requested a grant")
					}
					handler.ServeHTTP(w, r)
				})}
				listener = pairedFixture(t, server, listener)
				go func() { _ = server.Serve(listener) }()
				cleanup = func() { _ = server.Close(); api.Close(); _ = audit.Close() }
				return nil
			})
			s.Step(`^the Agent pairing (configuration is missing|pin is incorrect|key is not trusted|key has not been generated)$`, func(state string) error {
				reachable = false
				configDir := filepath.Join(dir, ".local", "share", "passlet")
				if state == "configuration is missing" {
					t.Setenv("PASSLET_ENDPOINT", endpoint)
					return os.Remove(filepath.Join(configDir, "connection.json"))
				}
				if state == "key has not been generated" {
					return os.Remove(filepath.Join(configDir, "agent.pem"))
				}
				if state == "key is not trusted" {
					data, err := x509.MarshalPKCS8PrivateKey(testkeys.Private("unknown"))
					if err != nil {
						return err
					}
					return os.WriteFile(filepath.Join(configDir, "agent.pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: data}), 0600)
				}
				data := `{"endpoint":"` + endpoint + `","approver":"` + base64.RawURLEncoding.EncodeToString(testkeys.Private("other-approver").Public().(ed25519.PublicKey)) + `"}`
				return os.WriteFile(filepath.Join(configDir, "connection.json"), []byte(data), 0600)
			})
			s.Step(`^the Approver has (?:an? )?(listening|unavailable) Tailscale listener$`, func(state string) error {
				configDir := filepath.Join(dir, ".config", "passlet")
				if err := os.MkdirAll(configDir, 0700); err != nil {
					return err
				}
				config := "tailscale = true\n"
				sockets := []string{tailscaleBackend}
				if err := os.WriteFile(filepath.Join(configDir, "approver.toml"), []byte(config), 0600); err != nil {
					return err
				}
				for _, socket := range sockets {
					if state == "unavailable" {
						continue
					}
					var listener net.Listener
					var err error
					if socket == tailscaleBackend {
						listener, err = net.Listen("tcp4", socket)
					} else {
						listener, err = passlet.ListenUnix(socket)
					}
					if err != nil {
						return err
					}
					server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.Method != "GET" || r.URL.Path != "/v1/health" {
							t.Error("listener check requested more than health")
						}
						fmt.Fprint(w, `{"status":"ready"}`)
					})}
					socketListeners[socket] = listener
					listener = pairedFixture(t, server, listener)
					go func() { _ = server.Serve(listener) }()
					previousCleanup := cleanup
					cleanup = func() { _ = server.Close(); previousCleanup() }
				}
				tailscaleFixture = &overviewFixture{socket: passlet.DefaultSocket(), dns: "alice-mac.example.ts.net"}
				showStatus = func(ctx context.Context) error {
					return writeOverview(ctx, os.Stdout, dir, tailscaleFixture.command, overviewHealth)
				}
				return nil
			})
			s.Step(`^Tailscale (is not installed|is not running|has no forwarding configured|forwards to a different backend)$`, func(condition string) {
				tailscaleFixture.condition = "Tailscale " + condition
			})
			s.Step(`^I (approve|deny) the diagnostic check$`, func(decision string) {
				if decision == "deny" {
					helperError = "approval_denied"
				}
			})
			s.Step("^I run `passlet ((?:approver )?doctor)`$", func(command string) error {
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
					err := runWithOverview(showStatus)
					if err != nil {
						writeCommandError(os.Stdout, err)
					}
					return err
				}, strings.Fields(command)...)
				for _, secret := range []string{credentials.ClientSecret, credentials.AccessToken, credentials.RefreshToken} {
					if strings.Contains(output, secret) {
						return fmt.Errorf("protected credential leaked to terminal")
					}
				}
				return nil
			})
			s.Step(`^the terminal shows:$`, func(doc *godog.DocString) error {
				want := strings.NewReplacer("{version}", version, "{endpoint}", endpoint, "{state_dir}", passlet.StateDir()).Replace(doc.Content) + "\n"
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
