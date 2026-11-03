package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"passlet.dev/passlet/internal/passlet"
)

const overviewHelpHint = "See `passlet --help` for available commands.\n"

const commandHelp = `Give your remote agents scoped access to services, approved on-the-fly from your laptop. Run passlet without arguments to see how this machine is configured.

Usage: passlet <command>

Commands:
  connect       Check and save a Conductor endpoint
  github setup  Configure GitHub access on your Conductor Mac
  github reset  Remove saved GitHub credentials
  conductor     Run the Conductor
  request       Request scoped access
  exec          Run a command with scoped access
  doctor        Verify Conductor and GitHub access with approval
  audit         Show local audit history

Run passlet <command> --help for command options.
`

type overviewCommand func(context.Context, string, ...string) ([]byte, error)

func overviewOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 100 * time.Millisecond
	return cmd.Output()
}

func overview(ctx context.Context) {
	home, _ := os.UserHomeDir()
	writeOverview(ctx, os.Stdout, runtime.GOOS, home, overviewOutput, passlet.Health)
}

func writeOverview(ctx context.Context, out io.Writer, hostOS, home string, command overviewCommand, health func(context.Context, string) error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	socket, enabled := overviewConductor(ctx, hostOS, home, command)
	endpoint, endpointErr := passlet.ResolveEndpoint("")
	showHelp := endpointErr == nil
	if socket == "" {
		_, _ = fmt.Fprintln(out, "Configured as an Agent. Requests are sent to another Passlet running in Conductor mode for approval.")
		configHome, _ := os.UserHomeDir()
		_, savedErr := os.Stat(filepath.Join(configHome, ".config", "passlet", "endpoint"))
		if endpointErr == nil && os.Getenv("PASSLET_ENDPOINT") == "" && os.IsNotExist(savedErr) {
			if ctx.Err() == nil && health(ctx, endpoint) == nil {
				_, _ = fmt.Fprintln(out, "\nSending requests to:", endpoint)
			} else {
				showHelp = false
				_, _ = fmt.Fprintln(out, "\nWARNING! No local Conductor found.")
				_, _ = fmt.Fprintln(out, "\nRun `passlet` on your Conductor machine to see how to connect.")
			}
		} else if endpointErr == nil {
			showHelp = writeOverviewHealth(ctx, out, "", endpoint, "passlet doctor", health)
			if showHelp {
				_, _ = fmt.Fprintln(out, "\nSending requests to:", endpoint)
			}
		}
	} else {
		_, _ = fmt.Fprintln(out, "Configured as a Conductor. Agents send requests here for your approval.")
		doctor := "passlet doctor"
		if endpointErr != nil || endpoint != "unix://"+socket {
			doctor += " --endpoint '" + strings.ReplaceAll("unix://"+socket, "'", "'\\''") + "'"
		}
		reachable := writeOverviewHealth(ctx, out, "", "unix://"+socket, doctor, health)
		showHelp = showHelp && reachable
		if reachable {
			_, _ = fmt.Fprintln(out, "\nLocal agents on this Mac connect automatically.")
			addresses := overviewRemoteAddresses(ctx, socket, enabled, command)
			if len(addresses) != 0 {
				_, _ = fmt.Fprintln(out, "\nTell remote agents how to connect:")
				_, _ = fmt.Fprintln(out, "  passlet connect "+addresses[0])
			}
		}
	}
	if endpointErr != nil {
		_, _ = fmt.Fprintln(out, "Request endpoint unavailable:", endpointErr)
	} else if socket != "" && endpoint != "unix://"+socket {
		reachable := writeOverviewHealth(ctx, out, "Selected Conductor", endpoint, "passlet doctor", health)
		showHelp = showHelp && reachable
		if reachable {
			_, _ = fmt.Fprintln(out, "Sending requests to:", endpoint)
		}
	}
	if showHelp {
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprint(out, overviewHelpHint)
	}
}

func writeOverviewHealth(ctx context.Context, out io.Writer, label, endpoint, doctor string, health func(context.Context, string) error) bool {
	if ctx.Err() != nil {
		_, _ = fmt.Fprintf(out, "\nWARNING! The Passlet Conductor could not be checked before the overview timed out.\nRun `%s` for more information.\n", doctor)
	} else if err := health(ctx, endpoint); err != nil {
		_, _ = fmt.Fprintf(out, "\nWARNING! The Passlet Conductor is not reachable.\nRun `%s` for more information.\n", doctor)
	} else {
		if label != "" {
			_, _ = fmt.Fprintf(out, "%s: reachable\n", label)
		}
		return true
	}
	return false
}

func overviewConductor(ctx context.Context, hostOS, home string, command overviewCommand) (string, bool) {
	if hostOS != "darwin" || home == "" {
		return "", false
	}
	path := filepath.Join(home, "Library", "LaunchAgents", "dev.passlet.agent.plist")
	data, err := command(ctx, "/usr/bin/plutil", "-convert", "json", "-o", "-", path)
	if err != nil {
		return "", false
	}
	var config struct {
		Label            string
		ProgramArguments []string
	}
	if json.Unmarshal(data, &config) != nil || config.Label != "dev.passlet.agent" {
		return "", false
	}

	args := config.ProgramArguments
	if len(args) == 3 && args[0] == "/bin/sh" && args[1] == "-c" {
		const prefix = "/bin/wait4path /nix/store && exec "
		if !strings.HasPrefix(args[2], prefix) {
			return "", false
		}
		args = overviewShellArguments(strings.TrimPrefix(args[2], prefix))
	}
	if len(args) < 2 || args[1] != "conductor" {
		return "", false
	}
	fs := flag.NewFlagSet("conductor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.String("helper", "", "")
	if fs.Parse(args[2:]) != nil || fs.NArg() != 0 {
		return "", false
	}
	settings, _ := readConductorConfig(filepath.Join(home, ".config", "passlet", "conductor.toml"))
	return passlet.DefaultSocket(), settings.Tailscale
}

func overviewRemoteAddresses(ctx context.Context, socket string, enabled bool, command overviewCommand) []string {
	if !enabled {
		return nil
	}
	status, err := tailscaleNode(ctx, command)
	if err != nil {
		return nil
	}
	config, err := readTailscaleServe(ctx, command)
	if err != nil || !config.forwardsTo(status.Self.DNSName, tailscaleSocket(socket)) {
		return nil
	}
	address := "https://" + status.Self.DNSName + ":" + tailscalePort
	if _, err := passlet.EndpointURL(address, "/v1/health"); err != nil {
		return nil
	}
	return []string{address}
}

// Home Manager wraps launchd commands to wait for the Nix store at login.
// Accept its literal argument quoting without evaluating shell expressions.
func overviewShellArguments(script string) []string {
	word := regexp.MustCompile(`^(?:[a-zA-Z0-9_./:=+,@%-]+|'[^']*'|\\')+`)
	part := regexp.MustCompile(`'([^']*)'|\\'|[^'\\]+`)
	var args []string
	for script = strings.TrimSpace(script); script != ""; script = strings.TrimLeft(script, " \t") {
		token := word.FindString(script)
		if token == "" {
			return nil
		}
		var value strings.Builder
		for _, piece := range part.FindAllString(token, -1) {
			if strings.HasPrefix(piece, "'") {
				value.WriteString(piece[1 : len(piece)-1])
			} else if piece == `\'` {
				value.WriteByte('\'')
			} else {
				value.WriteString(piece)
			}
		}
		args = append(args, value.String())
		script = script[len(token):]
		if script != "" && script[0] != ' ' && script[0] != '\t' {
			return nil
		}
	}
	return args
}
