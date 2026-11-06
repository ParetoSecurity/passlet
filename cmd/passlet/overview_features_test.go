package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/cucumber/godog"
)

type overviewFixture struct {
	tailUnavailable, unavailable bool
	socket, dns, condition       string
	healthCalls, calls           []string
}

func (f *overviewFixture) health(ctx context.Context, endpoint string) error {
	f.healthCalls = append(f.healthCalls, endpoint)
	if f.tailUnavailable && endpoint == tailscaleTarget {
		return errors.New("offline")
	}

	if f.unavailable {
		return errors.New("unavailable")
	}
	return nil
}

func (f *overviewFixture) command(ctx context.Context, name string, args ...string) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	if name != "tailscale" {
		return nil, fmt.Errorf("unexpected command: %s", call)
	}
	switch f.condition {
	case "Tailscale is not installed":
		return nil, exec.ErrNotFound
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
	target := tailscaleBackend
	terminateTLS := ""
	switch f.condition {
	case "Tailscale forwards to a different backend":
		target = "127.0.0.1:9999"
	case "Tailscale forwarding terminates TLS":
		terminateTLS = f.dns
	}
	return json.Marshal(map[string]any{
		"TCP": map[string]any{tailscalePort: map[string]any{"TCPForward": target, "TerminateTLS": terminateTLS}},
	})
}

func TestOverviewFeatures(t *testing.T) {
	suite := godog.TestSuite{
		Options: &godog.Options{Format: "pretty", Paths: []string{"features/overview.feature"}, Strict: true, TestingT: t},
		ScenarioInitializer: func(s *godog.ScenarioContext) {
			var output string
			var result error
			s.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
				t.Setenv("HOME", t.TempDir())
				t.Setenv("PASSLET_ENDPOINT", "invalid-ignored-endpoint")
				output, result = "", nil
				return ctx, nil
			})
			s.Step("^I run `passlet(?: (approver(?: github)?))?`$", func(arg string) {
				output, result = invokeCLICommand(t, "", func() error {
					return runWithOverview(func(ctx context.Context) error {
						t.Error("command discovery inspected service configuration")
						return nil
					})
				}, strings.Fields(arg)...)
			})
			s.Step(`^the terminal shows:$`, func(doc *godog.DocString) error {
				want := doc.Content + "\n"
				if output != want {
					return fmt.Errorf("output %q, want %q", output, want)
				}
				return nil
			})
			s.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) { return ctx, result })
		},
	}
	if suite.Run() != 0 {
		t.Fatal("overview scenarios failed")
	}
}
