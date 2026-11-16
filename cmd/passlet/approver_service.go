//go:build !agentonly

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const approverServiceLabel = "dev.passlet.agent"

type approverServiceDependencies struct {
	goos      string
	uid       int
	home      func() (string, error)
	launchctl func(context.Context, ...string) error
	output    io.Writer
}

func approverServiceCommand(ctx context.Context, action string, args []string) error { // coverage-ignore: OS adapter; lifecycle and CLI behavior are tested with injected platform dependencies.
	return approverServiceWithDependencies(ctx, action, args, approverServiceDependencies{
		goos: runtime.GOOS, uid: os.Geteuid(), home: os.UserHomeDir,
		launchctl: launchctlCommand, output: os.Stdout,
	})
}

func approverServiceWithDependencies(ctx context.Context, action string, args []string, deps approverServiceDependencies) error {
	fs := flag.NewFlagSet("approver "+action, flag.ContinueOnError)
	fs.Usage = func() {
		_, _ = fmt.Fprintf(fs.Output(), "%s the installed Passlet Approver service on this Mac.\n\nSupports Homebrew and Home Manager installations. Activate Nix configuration changes before restarting. Stop is temporary until restart, package activation, or your next login. For checkout testing, stop the service and use `passlet approver start`.\n", strings.ToUpper(action[:1])+action[1:])
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("approver_service_requires_no_arguments")
	}
	if deps.goos != "darwin" {
		return errors.New("approver_service_requires_macOS")
	}
	home, err := deps.home()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := controlApproverService(ctx, action, home, deps.uid, deps.launchctl); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(deps.output, "Approver service %s.\n", map[string]string{"stop": "stopped", "restart": "restarted"}[action])
	return nil
}

func launchctlCommand(ctx context.Context, args ...string) error { // coverage-ignore: Native launchctl adapter; exercised by the disposable macOS launchd test.
	output, err := exec.CommandContext(ctx, "/bin/launchctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl %s: %w: %s", args[0], err, strings.TrimSpace(string(output)))
	}
	return nil
}

func controlApproverService(ctx context.Context, action, home string, uid int, run func(context.Context, ...string) error) error {
	if uid == 0 {
		return errors.New("run_as_your_logged_in_Mac_user_not_root")
	}
	domain := "gui/" + strconv.Itoa(uid)
	target := domain + "/" + approverServiceLabel
	plist := filepath.Join(home, "Library", "LaunchAgents", approverServiceLabel+".plist")
	if action != "stop" {
		info, err := os.Stat(plist)
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("approver_service_not_installed_install_with_Homebrew_or_activate_Home_Manager")
		}
	}
	if err := run(ctx, "print", domain); err != nil {
		return fmt.Errorf("log_into_your_Mac_desktop_before_managing_Approver: %w", err)
	}
	err := run(ctx, "print", target)
	loaded := err == nil
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 113 {
			return err
		}
	}
	if loaded {
		if err := run(ctx, "bootout", "--wait", target); err != nil {
			return err
		}
	}
	if action == "stop" {
		return nil
	}
	if err := run(ctx, "bootstrap", domain, plist); err != nil {
		return err
	}
	return run(ctx, "kickstart", target)
}
