package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"

	"passlet.dev/passlet/internal/passlet"
)

func serviceCommand(ctx context.Context, args []string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("login_service_requires_macos")
	}
	if len(args) == 0 {
		return errors.New("usage: passlet service install|uninstall|status")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	file := filepath.Join(home, "Library", "LaunchAgents", passlet.ServiceLabel+".plist")
	domain := "gui/" + strconv.Itoa(os.Getuid())
	run := func(args ...string) error {
		cmd := exec.CommandContext(ctx, "/bin/launchctl", args...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		return cmd.Run()
	}
	switch args[0] {
	case "status":
		if len(args) != 1 {
			return errors.New("unexpected_arguments")
		}
		return run("print", domain+"/"+passlet.ServiceLabel)
	case "uninstall":
		if len(args) != 1 {
			return errors.New("unexpected_arguments")
		}
		if err := run("bootout", domain+"/"+passlet.ServiceLabel); err != nil {
			// A stopped or never-bootstrapped service can still have a plist.
			if exec.CommandContext(ctx, "/bin/launchctl", "print", domain+"/"+passlet.ServiceLabel).Run() == nil {
				return errors.New("service_stop_failed")
			}
		}
		if err := os.Remove(file); err != nil && !os.IsNotExist(err) {
			return err
		}
		fmt.Println("Login service removed. Keychain credentials and audit history retained.")
		return nil
	case "install":
		fs := flag.NewFlagSet("service install", flag.ContinueOnError)
		helper := fs.String("helper", defaultHelper(), "absolute signed helper path")
		tcp := fs.String("tailscale-listen", "", "optional loopback PROXY v2 listener")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 0 {
			return errors.New("unexpected_arguments")
		}
		if *tcp != "" {
			host, _, err := net.SplitHostPort(*tcp)
			if err != nil || !net.ParseIP(host).IsLoopback() {
				return errors.New("tailscale_listener_must_be_loopback")
			}
		}
		binary, err := os.Executable()
		if err != nil {
			return err
		}
		if info, err := os.Stat(*helper); err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			return errors.New("helper_unavailable_install_signed_Passlet_app")
		}
		if err := passlet.PrivateDir(passlet.StateDir()); err != nil {
			return err
		}
		data, err := passlet.LaunchAgent(binary, *helper, passlet.DefaultSocket(), filepath.Join(passlet.StateDir(), "audit.sqlite"), filepath.Join(passlet.StateDir(), "service.log"), *tcp, os.Getenv("PATH"))
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			return err
		}
		f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return errors.New("service_plist_exists_or_unwritable_uninstall_before_replacing")
		}
		_, writeErr := f.Write(data)
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			os.Remove(file)
			return errors.New("service_plist_write_failed")
		}
		if err := run("bootstrap", domain, file); err != nil {
			return errors.New("service_bootstrap_failed_check_logged_in_GUI_session_then_uninstall_and_retry")
		}
		fmt.Println("Passlet will start at login and restart after crashes. Run passlet doctor to verify.")
		return nil
	default:
		return errors.New("usage: passlet service install|uninstall|status")
	}
}
