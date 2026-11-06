package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cucumber/godog"
	"passlet.dev/passlet/internal/passlet"
)

func TestConnectFeatures(t *testing.T) {
	const override = "https://override.example.ts.net:17400"
	suite := godog.TestSuite{
		Options: &godog.Options{Format: "pretty", Paths: []string{"features/connect.feature"}, Strict: true, TestingT: t},
		ScenarioInitializer: func(s *godog.ScenarioContext) {
			var endpoint, previous, output, dir string
			var result error
			var failureExpected bool
			var expectedHealth int32
			var server *http.Server
			var health, other atomic.Int32
			var unrelated atomic.Bool
			s.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
				t.Setenv("HOME", t.TempDir())
				t.Setenv("PASSLET_ENDPOINT", "")
				server = nil
				output = ""
				previous = ""
				result = nil
				failureExpected = false
				expectedHealth = 1
				health.Store(0)
				other.Store(0)
				unrelated.Store(false)
				var err error
				dir, err = os.MkdirTemp("/tmp", "pl-connect-")
				return ctx, err
			})
			s.After(func(ctx context.Context, _ *godog.Scenario, scenarioErr error) (context.Context, error) {
				if server != nil {
					_ = server.Close()
				}
				cleanupErr := os.RemoveAll(dir)
				if scenarioErr != nil {
					return ctx, errors.Join(scenarioErr, cleanupErr)
				}
				if (result != nil) != failureExpected {
					return ctx, errors.Join(fmt.Errorf("expected failure=%t, got %v", failureExpected, result), cleanupErr)
				}
				if health.Load() != expectedHealth || other.Load() != 0 {
					return ctx, errors.Join(fmt.Errorf("health=%d, want %d; other=%d", health.Load(), expectedHealth, other.Load()), cleanupErr)
				}
				return ctx, cleanupErr
			})
			s.Step(`^a reachable Approver$`, func() error {
				socket := filepath.Join(dir, "socket")
				listener, err := passlet.ListenUnix(socket)
				if err != nil {
					return err
				}
				server = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != "GET" || r.URL.Path != "/v1/health" {
						other.Add(1)
						w.WriteHeader(403)
						return
					}
					health.Add(1)
					if unrelated.Load() {
						_, _ = w.Write([]byte(`{"status":"unrelated"}`))
						return
					}
					_, _ = w.Write([]byte(`{"status":"ready"}`))
				})}
				listener = pairedFixture(t, server, listener)
				go func() { _ = server.Serve(listener) }()
				endpoint = "unix://" + socket
				return nil
			})
			s.Step(`^the saved endpoint is "([^"]*)"$`, func(saved string) error {
				previous = saved
				return passlet.SaveEndpoint(saved)
			})
			s.Step(`^that Approver stops$`, func() error { expectedHealth = 0; return server.Close() })
			s.Step(`^the service does not return Passlet health information$`, func() { unrelated.Store(true) })
			s.Step(`^PASSLET_ENDPOINT selects another Approver$`, func() { t.Setenv("PASSLET_ENDPOINT", override); expectedHealth = 0 })
			s.Step("^I run `passlet connect \\{endpoint\\}`$", func() {
				output, result = invokeCLICommand(t, "", func() error {
					err := run()
					if err != nil {
						writeCommandError(os.Stdout, err)
					}
					return err
				}, "connect", endpoint)
			})
			s.Step(`^the command exits unsuccessfully$`, func() { failureExpected = true })
			s.Step(`^the endpoint is saved for future commands$`, func() error {
				data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".config", "passlet", "endpoint"))
				if err != nil {
					return err
				}
				if string(data) != endpoint+"\n" {
					return fmt.Errorf("saved %q", data)
				}
				return nil
			})
			s.Step(`^the saved endpoint remains unchanged$`, func() error {
				data, err := os.ReadFile(filepath.Join(os.Getenv("HOME"), ".config", "passlet", "endpoint"))
				if err != nil {
					return err
				}
				got := string(data)
				if got != previous+"\n" {
					return fmt.Errorf("previous endpoint replaced: %s", got)
				}
				return nil
			})
			s.Step(`^the terminal shows:$`, func(doc *godog.DocString) error {
				want := strings.ReplaceAll(doc.Content, "{endpoint}", endpoint) + "\n"
				if output != want {
					return fmt.Errorf("unexpected output: %q; want %q", output, want)
				}
				return nil
			})
			s.Step(`^requests still use the environment endpoint$`, func() error {
				got, err := passlet.ResolveEndpoint("")
				if err != nil {
					return err
				}
				if got != override {
					return errors.New("environment override ignored")
				}
				return nil
			})
		},
	}
	if suite.Run() != 0 {
		t.Fatal("connect scenarios failed")
	}
}
