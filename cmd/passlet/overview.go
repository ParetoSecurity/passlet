package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"passlet.dev/passlet/internal/passlet"
)

const commandHelp = `Give your remote agents scoped access to services, approved on-the-fly from your Mac.

Run ` + "`passlet approver`" + ` on your Mac to manage approvals and credentials.
Your Agents then run ` + "`passlet request`" + `.

Usage: passlet <command>

Commands:
  approver      Manage the local Approver on your Mac
  doctor        Check your connection and verify GitHub access
  exec          Run a command with scoped access
  pair          Pair with an Approver
  request       Request scoped access

Run ` + "`passlet <command> --help`" + ` for command options.
`

type overviewCommand func(context.Context, string, ...string) ([]byte, error)

func overviewOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 100 * time.Millisecond
	return cmd.Output()
}

func overview(ctx context.Context) error {
	home, _ := os.UserHomeDir()
	return writeOverview(ctx, os.Stdout, home, overviewOutput, overviewHealth)
}

func writeOverview(ctx context.Context, out io.Writer, home string, command overviewCommand, health func(context.Context, string) error) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	settings, err := readApproverConfig(filepath.Join(home, ".config", "passlet", "approver.toml"))
	if err != nil {
		return fmt.Errorf("cannot read Approver configuration: %w", err)
	}
	socket := passlet.DefaultSocket()
	if health(ctx, "unix://"+socket) != nil {
		return errors.New("local Approver is not reachable")
	}
	_, _ = fmt.Fprintln(out, "\n✓ Local socket: listening")
	var socketProblems []string
	addresses, tailscaleProblem := overviewRemoteAddresses(ctx, settings.Tailscale, command)
	if !settings.Tailscale {
		_, _ = fmt.Fprintln(out, "  Tailscale: disabled")
	} else if err := health(ctx, tailscaleTarget); err != nil {
		socketProblems = append(socketProblems, "Tailscale loopback listener is unavailable: "+tailscaleTarget)
		_, _ = fmt.Fprintln(out, "  Tailscale: unavailable")
		addresses = nil
	} else if len(addresses) == 0 {
		_, _ = fmt.Fprintln(out, "  Tailscale: unavailable")
		addresses = nil
	} else {
		_, _ = fmt.Fprintln(out, "✓ Tailscale: listening")
	}
	if !settings.Tailscale {
		_, _ = fmt.Fprintln(out, "\nEnable Tailscale: https://github.com/ParetoSecurity/passlet/blob/main/docs/SETUP.md#tailscale")
	}
	for _, problem := range socketProblems {
		_, _ = fmt.Fprintln(out, "\n"+problem)
	}
	if tailscaleProblem != "" {
		_, _ = fmt.Fprintln(out, "\n"+tailscaleProblem)
	}
	if settings.Tailscale && len(addresses) == 0 {
		_, _ = fmt.Fprintln(out, "\nTailscale troubleshooting: https://github.com/ParetoSecurity/passlet/blob/main/docs/SETUP.md#tailscale")
	}
	if len(addresses) != 0 {
		_, _ = fmt.Fprintln(out, "\nOn Tailscale agents, run this once:")
		_, _ = fmt.Fprintln(out, "  passlet pair "+addresses[0]+" --code <code>")
	}
	if settings.Tailscale && len(addresses) == 0 {
		return errors.New("one or more configured listeners are unavailable")
	}
	return nil
}

func overviewRemoteAddresses(ctx context.Context, enabled bool, command overviewCommand) ([]string, string) {
	if !enabled {
		return nil, ""
	}
	status, err := tailscaleNode(ctx, command)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, "Can't find `tailscale` command. Please add it to your PATH."
		}
		if status.BackendState != "" && status.BackendState != "Running" {
			return nil, fmt.Sprintf("Tailscale is not running (state: %s).", status.BackendState)
		}
		return nil, "Tailscale status could not be read or did not include a running node with a device name."
	}
	config, err := readTailscaleServe(ctx, command)
	if err != nil {
		return nil, "Tailscale Serve configuration could not be read."
	}
	if !config.forwardsTo(status.Self.DNSName, tailscaleTarget) {
		if !config.occupiesPort() {
			return nil, "No Tailscale Serve forwarding is configured on port " + tailscalePort + "."
		}
		proxy := config.TCP[tailscalePort].TCPForward
		expected := tailscaleBackend
		if proxy != "" && proxy != expected {
			return nil, fmt.Sprintf("Tailscale Serve forwards to %s instead of %s.", proxy, expected)
		}
		return nil, "Tailscale Serve configuration does not match the required TCP forwarding on port " + tailscalePort + "."
	}
	address := "https://" + status.Self.DNSName + ":" + tailscalePort
	if _, err := passlet.EndpointURL(address, "/v1/health"); err != nil {
		return nil, "Tailscale reported an invalid device address."
	}
	return []string{address}, ""
}

func overviewHealth(ctx context.Context, endpoint string) error {
	if endpoint == tailscaleTarget {
		endpoint = "https://" + tailscaleBackend
	}
	return passlet.Health(ctx, endpoint)
}
