package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"passlet.dev/passlet/internal/passlet"
)

const commandHelp = `Give your remote agents scoped access to services, approved on-the-fly from your Mac.

Run ` + "`passlet approver`" + ` on your Mac to manage approvals and credentials.
Your Agents then run ` + "`passlet request`" + `.

Usage: passlet <command>

Commands:
  approver      Manage the local Approver on your Mac
  connect       Check and save an Approver endpoint
  doctor        Check your connection and verify GitHub access
  exec          Run a command with scoped access
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
	if len(settings.Ssh) == 0 {
		_, _ = fmt.Fprintln(out, "  SSH sockets: disabled")
	}
	var socketProblems []string
	sshUnavailable := false
	for _, label := range settings.Ssh {
		path := filepath.Join(filepath.Dir(socket), "ssh-"+label+".sock")
		if err := health(ctx, "unix://"+path); err != nil {
			socketProblems = append(socketProblems, listenerProblem(path, err))
			sshUnavailable = true
			_, _ = fmt.Fprintf(out, "  SSH socket (%s): unavailable\n", label)
			continue
		}
		_, _ = fmt.Fprintf(out, "✓ SSH socket (%s): listening\n", label)
	}
	addresses, tailscaleProblem := overviewRemoteAddresses(ctx, socket, settings.Tailscale, command)
	if !settings.Tailscale {
		_, _ = fmt.Fprintln(out, "  Tailscale: disabled")
	} else if err := health(ctx, "unix://"+tailscaleSocket(socket)); err != nil {
		socketProblems = append(socketProblems, listenerProblem(tailscaleSocket(socket), err))
		_, _ = fmt.Fprintln(out, "  Tailscale: unavailable")
		addresses = nil
	} else if len(addresses) == 0 {
		_, _ = fmt.Fprintln(out, "  Tailscale: unavailable")
		addresses = nil
	} else {
		_, _ = fmt.Fprintln(out, "✓ Tailscale: listening")
	}
	if len(settings.Ssh) == 0 || !settings.Tailscale {
		_, _ = fmt.Fprintln(out)
	}
	if len(settings.Ssh) == 0 {
		_, _ = fmt.Fprintln(out, "Enable SSH sockets: https://github.com/ParetoSecurity/passlet/blob/main/docs/SETUP.md#ssh-tunnel-labels")
	}
	if !settings.Tailscale {
		_, _ = fmt.Fprintln(out, "Enable Tailscale: https://github.com/ParetoSecurity/passlet/blob/main/docs/SETUP.md#tailscale")
	}
	for _, problem := range socketProblems {
		_, _ = fmt.Fprintln(out, "\n"+problem)
	}
	if sshUnavailable {
		_, _ = fmt.Fprintln(out, "\nSSH troubleshooting: https://github.com/ParetoSecurity/passlet/blob/main/docs/SETUP.md#ssh-tunnel-labels")
	}
	if tailscaleProblem != "" {
		_, _ = fmt.Fprintln(out, "\n"+tailscaleProblem)
	}
	if settings.Tailscale && len(addresses) == 0 {
		_, _ = fmt.Fprintln(out, "\nTailscale troubleshooting: https://github.com/ParetoSecurity/passlet/blob/main/docs/SETUP.md#tailscale")
	}
	if len(addresses) != 0 {
		_, _ = fmt.Fprintln(out, "\nOn Tailscale agents, run this once:")
		_, _ = fmt.Fprintln(out, "  passlet connect "+addresses[0])
	}
	if sshUnavailable || (settings.Tailscale && len(addresses) == 0) {
		return errors.New("one or more configured listeners are unavailable")
	}
	return nil
}

func overviewRemoteAddresses(ctx context.Context, socket string, enabled bool, command overviewCommand) ([]string, string) {
	if !enabled {
		return nil, ""
	}
	status, err := tailscaleNode(ctx, command)
	if err != nil {
		if status.BackendState != "" && status.BackendState != "Running" {
			return nil, fmt.Sprintf("Tailscale is not running (state: %s).", status.BackendState)
		}
		return nil, "Tailscale status could not be read or did not include a running node with a device name."
	}
	config, err := readTailscaleServe(ctx, command)
	if err != nil {
		return nil, "Tailscale Serve configuration could not be read."
	}
	if !config.forwardsTo(status.Self.DNSName, tailscaleSocket(socket)) {
		if !config.occupiesPort() {
			return nil, "No Tailscale Serve forwarding is configured on port " + tailscalePort + "."
		}
		proxy := config.Web[status.Self.DNSName+":"+tailscalePort].Handlers["/"].Proxy
		expected := "unix:" + tailscaleSocket(socket)
		if strings.HasPrefix(proxy, "unix:") && proxy != expected {
			return nil, fmt.Sprintf("Tailscale Serve forwards to %s instead of %s.", proxy, expected)
		}
		return nil, "Tailscale Serve configuration does not match the required private HTTPS forwarding on port " + tailscalePort + "."
	}
	address := "https://" + status.Self.DNSName + ":" + tailscalePort
	if _, err := passlet.EndpointURL(address, "/v1/health"); err != nil {
		return nil, "Tailscale reported an invalid device address."
	}
	return []string{address}, ""
}

func overviewHealth(ctx context.Context, endpoint string) error {
	if endpoint != "unix://"+tailscaleSocket(passlet.DefaultSocket()) {
		return passlet.Health(ctx, endpoint)
	}
	// Serve supplies identity headers, so a local readiness check only opens its private socket.
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", tailscaleSocket(passlet.DefaultSocket()))
	if err != nil {
		return err
	}
	return conn.Close()
}

func listenerProblem(path string, err error) string {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "Socket file is missing: " + path
	case errors.Is(err, syscall.ECONNREFUSED):
		return "No service is listening on socket: " + path
	case errors.Is(err, os.ErrPermission):
		return "Cannot access socket: " + path + "\nIts directory must be owned by you with mode 0700, and the socket with mode 0600."
	case errors.Is(err, context.DeadlineExceeded):
		return "Socket check timed out: " + path
	default:
		return "Socket did not return a healthy Approver response: " + path
	}
}
