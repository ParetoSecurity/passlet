package main

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"passlet.dev/passlet/internal/passlet"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestOverviewRemoteAddressValidation(t *testing.T) {
	f := overviewFixture{socket: "/tmp/passlet.sock", dns: "mac.example.ts.net"}
	got, _ := overviewRemoteAddresses(context.Background(), true, f.command)
	if !reflect.DeepEqual(got, []string{"https://mac.example.ts.net:17400"}) {
		t.Fatal(got)
	}
	f.dns = "user@mac.example.ts.net"
	if got, _ := overviewRemoteAddresses(context.Background(), true, f.command); len(got) != 0 {
		t.Fatal("unsafe endpoint advertised", got)
	}
	if got, _ := overviewRemoteAddresses(context.Background(), false, func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("queried disabled Tailscale")
		return nil, nil
	}); len(got) != 0 {
		t.Fatal(got)
	}
	f.dns = "mac.example.ts.net"
	if got, _ := overviewRemoteAddresses(context.Background(), true, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		if args[0] == "serve" {
			return []byte("invalid"), nil
		}
		return f.command(ctx, name, args...)
	}); len(got) != 0 {
		t.Fatal(got)
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
	if err != nil || out != commandHelp {
		t.Fatalf("%q %v", out, err)
	}
	out, err = invokeCLI(t, "", "approver")
	if err != nil || !strings.Contains(out, "Approver") || strings.Contains(out, "Sending requests to:") {
		t.Fatalf("local overview: %q %v", out, err)
	}
	for _, arg := range []string{"--help", "-h", "help"} {
		out, err := invokeCLI(t, "", arg)
		if err != nil || out != commandHelp {
			t.Fatalf("%q %v", out, err)
		}
	}
}

func TestOverviewHealthUsesDefaultSocket(t *testing.T) {
	approverTestPaths(t)
	listener, err := passlet.ListenUnix(passlet.DefaultSocket())
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})}
	listener = pairedFixture(t, server, listener)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	endpoint := "unix://" + passlet.DefaultSocket()
	t.Setenv("PASSLET_ENDPOINT", endpoint)
	f := overviewFixture{socket: strings.TrimPrefix(endpoint, "unix://")}
	var out bytes.Buffer
	if err := writeOverview(context.Background(), &out, os.Getenv("HOME"), f.command, passlet.Health); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "✓ Local socket: listening") || strings.Contains(out.String(), "WARNING!") {
		t.Fatal(out.String())
	}
}

func runOverviewFixture(t *testing.T, f *overviewFixture) string {
	t.Helper()
	output, err := invokeCLICommand(t, "", func() error {
		err := writeOverview(context.Background(), os.Stdout, os.Getenv("HOME"), f.command, f.health)
		if err != nil {
			writeCommandError(os.Stdout, err)
		}
		return err
	}, "approver")
	if strings.Contains(output, "Commands:") {
		t.Fatalf("unexpected command result: %q %v", output, err)
	}
	for _, call := range f.calls {
		if call != "tailscale status --json" && call != "tailscale serve status --json" {
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
		"Tailscale forwards to a different backend",
		"Tailscale forwarding terminates TLS",
	} {
		t.Run(condition, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("PASSLET_ENDPOINT", "")
			f := overviewFixture{socket: passlet.DefaultSocket(), dns: "mac.example.ts.net", condition: condition}
			dir := filepath.Join(os.Getenv("HOME"), ".config", "passlet")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "approver.toml"), []byte("tailscale = true\n"), 0600); err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			output := runOverviewFixture(t, &f)
			if time.Since(start) >= 3*time.Second {
				t.Fatal("overview exceeded lookup budget")
			}
			if !strings.Contains(output, "✓ Local socket: listening") || strings.Contains(output, "passlet connect https://") || strings.Contains(output, "Tell remote agents") {
				t.Fatal(output)
			}
		})
	}
}

func TestApproverOverviewIgnoresSelectedEndpoint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PASSLET_ENDPOINT", "invalid-endpoint")
	f := overviewFixture{socket: passlet.DefaultSocket()}
	output := runOverviewFixture(t, &f)
	if !strings.Contains(output, "✓ Local socket: listening") || !reflect.DeepEqual(f.healthCalls, []string{"unix://" + passlet.DefaultSocket()}) {
		t.Fatalf("%s, health calls %v", output, f.healthCalls)
	}
}

func TestApproverOverviewInvalidConfiguration(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := filepath.Join(os.Getenv("HOME"), ".config", "passlet")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "approver.toml"), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	f := overviewFixture{}
	output := runOverviewFixture(t, &f)
	if !strings.Contains(output, "cannot read Approver configuration:") || len(f.healthCalls) != 0 {
		t.Fatal(output)
	}
}
