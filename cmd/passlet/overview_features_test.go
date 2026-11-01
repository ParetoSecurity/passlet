package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cucumber/godog"
	"passlet.dev/passlet/internal/passlet"
)

type overviewFixture struct {
	hostOS, socket, dns, condition      string
	tailscale                           bool
	conductor                           bool
	stopped, unavailable, healthTimeout bool
	healthCalls                         []string
	calls                               []string
}

func (f *overviewFixture) health(ctx context.Context, endpoint string) error {
	f.healthCalls = append(f.healthCalls, endpoint)
	if f.stopped {
		return passlet.Health(ctx, endpoint)
	}
	if f.healthTimeout {
		<-ctx.Done()
		return ctx.Err()
	}
	if f.unavailable {
		return errors.New("unavailable")
	}
	return nil
}

func (f *overviewFixture) command(ctx context.Context, name string, args ...string) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	if name == "/usr/bin/plutil" {
		if !f.conductor {
			return nil, errors.New("no LaunchAgent")
		}
		arguments := []string{"/passlet", "conductor", "--socket", f.socket, "--tailscale=" + strconv.FormatBool(f.tailscale)}
		return json.Marshal(map[string]any{"Label": "dev.passlet.agent", "ProgramArguments": arguments})
	}
	if name != "tailscale" {
		return nil, fmt.Errorf("unexpected command: %s", call)
	}
	switch f.condition {
	case "Tailscale is not installed":
		return nil, errors.New("not found")
	case "querying Tailscale times out":
		<-ctx.Done()
		return nil, ctx.Err()
	case "Tailscale returns invalid configuration":
		return []byte("invalid"), nil
	}
	if strings.Join(args, " ") == "status --json" {
		state := "Running"
		if f.condition == "Tailscale is not running" {
			state = "Stopped"
		}
		return json.Marshal(map[string]any{"BackendState": state, "Self": map[string]string{"DNSName": f.dns + "."}})
	}
	if strings.Join(args, " ") != "serve status --json" {
		return nil, fmt.Errorf("unexpected command: %s", call)
	}
	if f.condition == "Tailscale has no forwarding configured" {
		return []byte(`{}`), nil
	}
	socket := tailscaleSocket(f.socket)
	https := true
	switch f.condition {
	case "Tailscale forwards to a different socket":
		socket = "/tmp/other.sock"
	case "Tailscale forwarding does not use HTTPS":
		https = false
	}
	return json.Marshal(map[string]any{
		"TCP": map[string]any{tailscalePort: map[string]any{"HTTPS": https}},
		"Web": map[string]any{f.dns + ":" + tailscalePort: map[string]any{"Handlers": map[string]any{"/": map[string]string{"Proxy": "unix:" + socket}}}},
	})
}

func TestOverviewHelpAndVersionFeatures(t *testing.T) {
	suite := godog.TestSuite{
		Options: &godog.Options{Format: "pretty", Paths: []string{"features/help.feature", "features/overview.feature", "features/version.feature"}, Strict: true, TestingT: t},
		ScenarioInitializer: func(s *godog.ScenarioContext) {
			var f overviewFixture
			var output string
			var conductorDir, helper string
			var result error
			var overviewRequested bool
			s.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
				f = overviewFixture{hostOS: "darwin", socket: "/Users/alice/.local/share/passlet/passlet.sock", dns: "alice-mac.example.ts.net"}
				t.Setenv("PASSLET_ENDPOINT", "")
				t.Setenv("HOME", t.TempDir())
				output, result = "", nil
				overviewRequested = false
				conductorDir, helper = "", ""
				return ctx, nil
			})
			s.Step(`^Passlet is installed$`, func() { f.hostOS = "linux"; f.unavailable = true })
			s.Step(`^Passlet is installed and configured as Conductor$`, func() {
				f.conductor = true
				f.socket = passlet.DefaultSocket()
			})
			s.Step(`^the Conductor is configured for Tailscale$`, func() {
				f.conductor = true
				f.socket = passlet.DefaultSocket()
				f.tailscale = true
			})
			s.Step(`^this (Linux|Mac) has no local Conductor configured$`, func(machine string) {
				f.conductor = false
				if machine == "Linux" {
					f.hostOS = "linux"
				}
			})
			s.Step("^I have run `passlet connect ([^`]+)`$", func(endpoint string) error {
				f.hostOS = "linux"
				return passlet.SaveEndpoint(endpoint)
			})
			s.Step(`^PASSLET_ENDPOINT is "([^"]*)"$`, func(v string) { t.Setenv("PASSLET_ENDPOINT", v) })
			s.Step(`^the selected Conductor is unreachable$`, func() { f.unavailable = true })
			s.Step(`^the local Conductor is stopped$`, func() error {
				if !f.conductor {
					return errors.New("configure a Conductor first")
				}
				conductorDir, helper = conductorTestPaths(t)
				f.socket = filepath.Join(conductorDir, "socket")
				ctx, cancel := context.WithCancel(context.Background())
				done := make(chan error, 1)
				go func() {
					defer close(done)
					done <- conductor(ctx, []string{"--helper", helper, "--socket", f.socket, "--audit", filepath.Join(conductorDir, "audit.sqlite"), "--tailscale=false"})
				}()
				defer func() {
					cancel()
					select {
					case <-done:
					case <-time.After(5 * time.Second):
						t.Error("test Conductor did not stop")
					}
				}()
				deadline := time.Now().Add(5 * time.Second)
				for {
					healthCtx, stop := context.WithTimeout(ctx, 100*time.Millisecond)
					err := passlet.Health(healthCtx, "unix://"+f.socket)
					stop()
					if err == nil {
						break
					}
					select {
					case err := <-done:
						return fmt.Errorf("test Conductor exited before becoming reachable: %v", err)
					default:
					}
					if time.Now().After(deadline) {
						return fmt.Errorf("test Conductor never became reachable: %w", err)
					}
					time.Sleep(10 * time.Millisecond)
				}
				cancel()
				select {
				case err := <-done:
					if err != nil {
						return err
					}
				case <-time.After(5 * time.Second):
					return errors.New("test Conductor shutdown timed out")
				}
				f.stopped = true
				t.Setenv("PASSLET_ENDPOINT", "unix://"+f.socket)
				if err := passlet.Health(context.Background(), "unix://"+f.socket); err == nil {
					return errors.New("stopped Conductor is still reachable")
				}
				return nil
			})
			s.Step("^I run `passlet( --help| --version)?`$", func(arg string) {
				overviewRequested = arg == ""
				var args []string
				if arg != "" {
					args = []string{strings.TrimSpace(arg)}
				}
				output, result = invokeCLICommand(t, "", func() error {
					return runWithOverview(func(ctx context.Context) {
						writeOverview(ctx, os.Stdout, f.hostOS, "/fixture-home", f.command, f.health)
					})
				}, args...)
			})
			s.Step(`^the terminal shows:$`, func(doc *godog.DocString) error {
				if output != strings.NewReplacer("{socket}", f.socket, "{version}", version).Replace(doc.Content)+"\n" {
					return fmt.Errorf("unexpected output: %q", output)
				}
				return nil
			})
			s.After(func(ctx context.Context, _ *godog.Scenario, scenarioErr error) (context.Context, error) {
				if scenarioErr != nil {
					return ctx, scenarioErr
				}
				if result != nil {
					return ctx, fmt.Errorf("command failed: %w", result)
				}
				if overviewRequested && strings.Contains(output, "Commands:") {
					return ctx, errors.New("overview must not include the command list")
				}
				if conductorDir != "" {
					if _, err := os.Stat(filepath.Join(conductorDir, "invoked")); !os.IsNotExist(err) {
						return ctx, errors.New("approval helper was invoked")
					}
				}
				for _, call := range f.calls {
					if !strings.HasPrefix(call, "/usr/bin/plutil ") && call != "tailscale status --json" && call != "tailscale serve status --json" {
						return ctx, fmt.Errorf("unexpected operation: %s", call)
					}
				}
				return ctx, nil
			})
			s.Step(`^no listening addresses are shown$`, func() error {
				if strings.Contains(output, "Local agents on this Mac connect automatically.") || strings.Contains(output, "\n  unix://") || strings.Contains(output, "\n  passlet connect https://") {
					return errors.New("unavailable Conductor advertised listening addresses")
				}
				return nil
			})
			s.Step(`^no request destination is shown$`, func() error {
				if strings.Contains(output, "Sending requests to:") {
					return errors.New("unreachable Conductor advertised as a request destination")
				}
				return nil
			})
			s.Step(`^the overview does not claim the Conductor is running or ready$`, func() error {
				if strings.Contains(output, "Conductor: running") || strings.Contains(output, "Conductor: ready") {
					return errors.New("claimed readiness")
				}
				return nil
			})
			s.Step(`^no local service or Tailscale configuration is queried$`, func() error {
				if len(f.calls) != 0 || len(f.healthCalls) != 0 {
					return fmt.Errorf("unexpected lookups: %v", f.calls)
				}
				return nil
			})
		},
	}
	if suite.Run() != 0 {
		t.Fatal("overview, help, or version scenarios failed")
	}
}
