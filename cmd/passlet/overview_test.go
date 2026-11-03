package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"passlet.dev/passlet/internal/passlet"
)

func TestOverviewConductorConfiguration(t *testing.T) {
	home := t.TempDir()
	configDir := filepath.Join(home, ".config", "passlet")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "conductor.toml"), []byte(`tailscale = true`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, data, socket string
		enabled            bool
	}{
		{"Homebrew", `{"Label":"dev.passlet.agent","ProgramArguments":["/passlet","conductor","--helper","/helper"]}`, passlet.DefaultSocket(), true},
		{"removed Tailscale flag", `{"Label":"dev.passlet.agent","ProgramArguments":["/passlet","conductor","--tailscale=false"]}`, "", false},
		{"invalid JSON", `{`, "", false},
		{"unrelated label", `{"Label":"other","ProgramArguments":["/passlet","conductor"]}`, "", false},
		{"missing command", `{"Label":"dev.passlet.agent","ProgramArguments":["/passlet"]}`, "", false},
		{"wrong command", `{"Label":"dev.passlet.agent","ProgramArguments":["/passlet","doctor"]}`, "", false},
		{"invalid flag", `{"Label":"dev.passlet.agent","ProgramArguments":["/passlet","conductor","--unknown"]}`, "", false},
		{"positional argument", `{"Label":"dev.passlet.agent","ProgramArguments":["/passlet","conductor","unexpected"]}`, "", false},
		{"removed socket flag", `{"Label":"dev.passlet.agent","ProgramArguments":["/passlet","conductor","--socket=relative"]}`, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			socket, listener := overviewConductor(context.Background(), "darwin", home, func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name != "/usr/bin/plutil" || args[len(args)-1] != filepath.Join(home, "Library", "LaunchAgents", "dev.passlet.agent.plist") {
					t.Fatalf("unexpected plist lookup: %s %v", name, args)
				}
				return []byte(tc.data), nil
			})
			if socket != tc.socket || listener != tc.enabled {
				t.Fatalf("got %q %v", socket, listener)
			}
		})
	}
	for _, hostOS := range []string{"linux", "darwin"} {
		socket, _ := overviewConductor(context.Background(), hostOS, "", func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("unexpected lookup")
			return nil, nil
		})
		if socket != "" {
			t.Fatal("claimed a Conductor without configuration")
		}
	}
}

func TestOverviewRemoteAddressValidation(t *testing.T) {
	f := overviewFixture{socket: "/tmp/passlet.sock", dns: "mac.example.ts.net"}
	got := overviewRemoteAddresses(context.Background(), f.socket, true, f.command)
	if !reflect.DeepEqual(got, []string{"https://mac.example.ts.net:17400"}) {
		t.Fatal(got)
	}
	f.dns = "user@mac.example.ts.net"
	if got := overviewRemoteAddresses(context.Background(), f.socket, true, f.command); len(got) != 0 {
		t.Fatal("unsafe endpoint advertised", got)
	}
	if got := overviewRemoteAddresses(context.Background(), f.socket, false, func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("queried disabled Tailscale")
		return nil, nil
	}); len(got) != 0 {
		t.Fatal(got)
	}
	f.dns = "mac.example.ts.net"
	if got := overviewRemoteAddresses(context.Background(), f.socket, true, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if args[0] == "serve" {
			return []byte("invalid"), nil
		}
		return f.command(ctx, name, args...)
	}); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestOverviewEndpointOverride(t *testing.T) {
	t.Setenv("PASSLET_ENDPOINT", "https://other.example.ts.net:17400")
	f := overviewFixture{conductor: true, socket: passlet.DefaultSocket()}
	var out bytes.Buffer
	writeOverview(context.Background(), &out, "darwin", t.TempDir(), f.command, f.health)
	if !strings.Contains(out.String(), "Configured as a Conductor.") || !strings.Contains(out.String(), "Sending requests to: https://other.example.ts.net:17400") {
		t.Fatal(out.String())
	}
}

func TestOverviewCommandAndDispatcher(t *testing.T) {
	data, err := overviewOutput(context.Background(), "/bin/sh", "-c", "printf fixture")
	if err != nil || string(data) != "fixture" {
		t.Fatalf("%q %v", data, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := overviewOutput(ctx, "/bin/sh", "-c", "exec sleep 10"); err == nil || time.Since(start) > time.Second {
		t.Fatal("command timeout not enforced")
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PASSLET_ENDPOINT", "unix:///tmp/forwarded.sock")
	out, err := invokeCLI(t, "")
	if err != nil || !strings.HasPrefix(out, "Configured as an Agent.") || strings.Contains(out, overviewHelpHint) {
		t.Fatalf("%q %v", out, err)
	}
	for _, arg := range []string{"--help", "-h", "help"} {
		out, err := invokeCLI(t, "", arg)
		if err != nil || out != commandHelp {
			t.Fatalf("%q %v", out, err)
		}
	}
}

func TestOverviewHomeManagerWrapper(t *testing.T) {
	for _, script := range []string{
		`/bin/wait4path /nix/store && exec /nix/store/package/bin/passlet conductor --helper /nix/store/package/helper`,
		`/bin/wait4path /nix/store && exec '/nix/store/package/bin/passlet' 'conductor' '--helper' '/nix/store/package/helper'`,
	} {
		data, err := json.Marshal(map[string]any{"Label": "dev.passlet.agent", "ProgramArguments": []string{"/bin/sh", "-c", script}})
		if err != nil {
			t.Fatal(err)
		}
		socket, listener := overviewConductor(context.Background(), "darwin", t.TempDir(), func(context.Context, string, ...string) ([]byte, error) { return data, nil })
		if socket == "" || listener {
			t.Fatalf("wrapper not recognized: %q %v", socket, listener)
		}
	}
	for _, script := range []string{`exec /passlet conductor`, `/bin/wait4path /nix/store && exec /passlet conductor; touch /tmp/unwanted`} {
		data, _ := json.Marshal(map[string]any{"Label": "dev.passlet.agent", "ProgramArguments": []string{"/bin/sh", "-c", script}})
		socket, _ := overviewConductor(context.Background(), "darwin", t.TempDir(), func(context.Context, string, ...string) ([]byte, error) { return data, nil })
		if socket != "" {
			t.Fatal("accepted unsupported shell script")
		}
	}
}

func TestOverviewLiteralShellArguments(t *testing.T) {
	for _, tc := range []struct {
		script string
		want   []string
	}{
		{`/passlet conductor --helper ''`, []string{"/passlet", "conductor", "--helper", ""}},
		{`'/path with spaces/it'\''s/passlet' conductor`, []string{"/path with spaces/it's/passlet", "conductor"}},
		{`/passlet conductor; malicious`, nil},
		{`$(malicious) conductor`, nil},
		{`'/unclosed`, nil},
		{`/passlet "conductor"`, nil},
	} {
		if got := overviewShellArguments(tc.script); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%q: %v, want %v", tc.script, got, tc.want)
		}
	}
}

func TestOverviewHealthUsesDefaultSocket(t *testing.T) {
	conductorTestPaths(t)
	listener, err := passlet.ListenUnix(passlet.DefaultSocket())
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	endpoint := "unix://" + passlet.DefaultSocket()
	t.Setenv("PASSLET_ENDPOINT", endpoint)
	f := overviewFixture{conductor: true, socket: strings.TrimPrefix(endpoint, "unix://")}
	var out bytes.Buffer
	writeOverview(context.Background(), &out, "darwin", os.Getenv("HOME"), f.command, passlet.Health)
	if !strings.Contains(out.String(), "Local agents on this Mac connect automatically.") || strings.Contains(out.String(), "WARNING!") {
		t.Fatal(out.String())
	}
}

func TestOverviewExpiredLookupDoesNotClaimUnavailability(t *testing.T) {
	t.Setenv("PASSLET_ENDPOINT", "https://other.example.ts.net:17400")
	f := overviewFixture{conductor: true, socket: passlet.DefaultSocket(), tailscale: true, condition: "querying Tailscale times out"}
	var out bytes.Buffer
	start := time.Now()
	writeOverview(context.Background(), &out, "darwin", t.TempDir(), f.command, f.health)
	if !strings.Contains(out.String(), "Local agents on this Mac connect automatically.") || !strings.Contains(out.String(), "WARNING! The Passlet Conductor could not be checked before the overview timed out.") {
		t.Fatal(out.String())
	}
	if time.Since(start) >= 3*time.Second || len(f.healthCalls) != 1 || f.healthCalls[0] != "unix://"+passlet.DefaultSocket() {
		t.Fatalf("unexpected health lookups: %v", f.healthCalls)
	}
}

func TestOverviewDoctorTargetsUnavailableLocalConductor(t *testing.T) {
	t.Setenv("PASSLET_ENDPOINT", "https://other.example.ts.net:17400")
	f := overviewFixture{conductor: true, socket: passlet.DefaultSocket()}
	var out bytes.Buffer
	writeOverview(context.Background(), &out, "darwin", t.TempDir(), f.command, func(_ context.Context, endpoint string) error {
		if endpoint == "unix://"+f.socket {
			return errors.New("unavailable")
		}
		if endpoint != "https://other.example.ts.net:17400" {
			t.Fatalf("unexpected endpoint %q", endpoint)
		}
		return nil
	})
	if !strings.Contains(out.String(), "WARNING! The Passlet Conductor is not reachable") || !strings.Contains(out.String(), "Run `passlet doctor --endpoint 'unix://"+passlet.DefaultSocket()+"'` for more information.") || !strings.Contains(out.String(), "Selected Conductor: reachable") || strings.Contains(out.String(), "Local agents on this Mac connect automatically.") {
		t.Fatal(out.String())
	}
}

func runOverviewFixture(t *testing.T, f *overviewFixture) string {
	t.Helper()
	output, err := invokeCLICommand(t, "", func() error {
		return runWithOverview(func(ctx context.Context) {
			writeOverview(ctx, os.Stdout, f.hostOS, os.Getenv("HOME"), f.command, f.health)
		})
	})
	if err != nil || strings.Contains(output, "Commands:") || (strings.Contains(output, "WARNING!") && strings.Contains(output, overviewHelpHint)) {
		t.Fatalf("unexpected command result: %q %v", output, err)
	}
	for _, call := range f.calls {
		if !strings.HasPrefix(call, "/usr/bin/plutil ") && call != "tailscale status --json" && call != "tailscale serve status --json" {
			t.Fatalf("unexpected operation: %s", call)
		}
	}
	return output
}

func TestOverviewUnverifiedTailscaleForwarding(t *testing.T) {
	for _, condition := range []string{
		"Tailscale is not installed",
		"Tailscale is not running",
		"querying Tailscale times out",
		"Tailscale returns invalid configuration",
		"Tailscale has no forwarding configured",
		"Tailscale forwards to a different socket",
		"Tailscale forwarding does not use HTTPS",
	} {
		t.Run(condition, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("PASSLET_ENDPOINT", "")
			f := overviewFixture{hostOS: "darwin", conductor: true, socket: passlet.DefaultSocket(), tailscale: true, dns: "mac.example.ts.net", condition: condition}
			start := time.Now()
			output := runOverviewFixture(t, &f)
			if time.Since(start) >= 3*time.Second {
				t.Fatal("overview exceeded lookup budget")
			}
			if !strings.Contains(output, "Configured as a Conductor.") || !strings.Contains(output, "Local agents on this Mac connect automatically.") || strings.Contains(output, "https://") || strings.Contains(output, "Tell remote agents") {
				t.Fatal(output)
			}
		})
	}
}

func TestOverviewForwardedSocketRemainsAgent(t *testing.T) {
	for _, hostOS := range []string{"darwin", "linux"} {
		t.Run(hostOS, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("PASSLET_ENDPOINT", "unix:///home/alice/.local/share/passlet/mac.sock")
			f := overviewFixture{hostOS: hostOS}
			output := runOverviewFixture(t, &f)
			if !strings.HasPrefix(output, "Configured as an Agent.") || !strings.Contains(output, "Sending requests to: "+os.Getenv("PASSLET_ENDPOINT")) || strings.Contains(output, "Configured as a Conductor") || strings.Contains(output, "Local agents on this Mac") {
				t.Fatal(output)
			}
		})
	}
}

func TestOverviewHealthTimeout(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PASSLET_ENDPOINT", "https://mac.example.ts.net:17400")
	f := overviewFixture{hostOS: "linux", healthTimeout: true}
	start := time.Now()
	output := runOverviewFixture(t, &f)
	if time.Since(start) >= 3*time.Second {
		t.Fatal("overview exceeded lookup budget")
	}
	if !strings.HasPrefix(output, "Configured as an Agent.") || !strings.Contains(output, "WARNING! The Passlet Conductor is not reachable.\nRun `passlet doctor` for more information.") || strings.Contains(output, "Sending requests to:") {
		t.Fatal(output)
	}
	if !reflect.DeepEqual(f.healthCalls, []string{os.Getenv("PASSLET_ENDPOINT")}) {
		t.Fatalf("unexpected health lookups: %v", f.healthCalls)
	}
}

func TestOverviewConfiguredAgent(t *testing.T) {
	for _, source := range []string{"environment", "saved HTTPS", "saved local socket"} {
		t.Run(source, func(t *testing.T) {
			home, err := os.MkdirTemp("/tmp", "pl-agent-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(home) })
			t.Setenv("HOME", home)
			t.Setenv("PASSLET_ENDPOINT", "")
			endpoint := "https://mac.example.ts.net:17400"
			if source == "environment" {
				t.Setenv("PASSLET_ENDPOINT", endpoint)
			} else {
				if source == "saved local socket" {
					endpoint = "unix://" + passlet.DefaultSocket()
				}
				if err := passlet.SaveEndpoint(endpoint); err != nil {
					t.Fatal(err)
				}
			}
			f := overviewFixture{hostOS: "linux"}
			output := runOverviewFixture(t, &f)
			if !strings.Contains(output, "Sending requests to: "+endpoint) || strings.Contains(output, "Run `passlet` on your Conductor machine") {
				t.Fatal(output)
			}
		})
	}
}

func TestOverviewAgentChecksDefaultLocalConductor(t *testing.T) {
	for _, reachable := range []bool{false, true} {
		t.Run(fmt.Sprint(reachable), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("PASSLET_ENDPOINT", "")
			f := overviewFixture{hostOS: "linux", unavailable: !reachable}
			output := runOverviewFixture(t, &f)
			endpoint := "unix://" + passlet.DefaultSocket()
			if !reflect.DeepEqual(f.healthCalls, []string{endpoint}) {
				t.Fatalf("unexpected health lookups: %v", f.healthCalls)
			}
			if reachable {
				if !strings.Contains(output, "Sending requests to: "+endpoint) || strings.Contains(output, "WARNING!") || strings.Contains(output, "Run `passlet` on your Conductor machine") {
					t.Fatal(output)
				}
			} else if !strings.Contains(output, "WARNING! No local Conductor found.") || !strings.Contains(output, "Run `passlet` on your Conductor machine to see how to connect.") || strings.Contains(output, "Sending requests to:") {
				t.Fatal(output)
			}
		})
	}
}
