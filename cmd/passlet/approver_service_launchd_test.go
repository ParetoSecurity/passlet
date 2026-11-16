//go:build darwin

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestApproverServiceLaunchd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	domain := fmt.Sprintf("gui/%d", os.Geteuid())
	if err := exec.CommandContext(ctx, "/bin/launchctl", "print", domain).Run(); err != nil {
		t.Skip("requires a logged-in macOS desktop")
	}
	home := t.TempDir()
	label := "dev.passlet.service-test." + filepath.Base(home) + fmt.Sprint(time.Now().UnixNano())
	target := domain + "/" + label
	plist := filepath.Join(home, "Library", "LaunchAgents", approverServiceLabel+".plist")
	if err := os.MkdirAll(filepath.Dir(plist), 0700); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(home, "service.sh")
	stopped := filepath.Join(home, "stopped")
	ready := filepath.Join(home, "ready")
	if err := os.WriteFile(script, []byte(fmt.Sprintf("#!/bin/sh\ntrap 'sleep 1; touch %s; exit 0' TERM\ntouch %s\nwhile :; do sleep 0.1; done\n", stopped, ready)), 0700); err != nil {
		t.Fatal(err)
	}
	data := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>Label</key><string>%s</string><key>ProgramArguments</key><array><string>/bin/sh</string><string>%s</string></array><key>RunAtLoad</key><true/><key>KeepAlive</key><true/></dict></plist>`, label, script)
	if err := os.WriteFile(plist, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = exec.Command("/bin/launchctl", "bootout", "--wait", target).Run() })
	run := func(ctx context.Context, args ...string) error {
		for i, arg := range args {
			if arg == domain+"/"+approverServiceLabel {
				args[i] = target
			}
		}
		if args[0] == "bootout" {
			output, err := exec.CommandContext(ctx, "/bin/launchctl", "print", target).Output()
			if err != nil {
				t.Fatal(err)
			}
			match := regexp.MustCompile(`(?m)\bpid = (\d+)`).FindStringSubmatch(string(output))
			if len(match) != 2 {
				t.Fatal("missing service PID")
			}
			oldPid, _ := strconv.Atoi(match[1])
			_ = os.Remove(stopped)
			if err := launchctlCommand(ctx, args...); err != nil {
				return err
			}
			if _, err := os.Stat(stopped); err != nil {
				t.Fatal("bootout returned before slow shutdown completed", err)
			}
			if err := syscall.Kill(oldPid, 0); !errors.Is(err, syscall.ESRCH) {
				t.Fatal("old process still exists", err)
			}
			_ = os.Remove(ready)
			return nil
		}
		return launchctlCommand(ctx, args...)
	}
	pid := func() string {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
			output, _ := exec.CommandContext(ctx, "/bin/launchctl", "print", target).Output()
			match := regexp.MustCompile(`(?m)\bpid = (\d+)`).FindStringSubmatch(string(output))
			if _, err := os.Stat(ready); len(match) > 1 && err == nil {
				return match[1]
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("disposable service did not start")
		return ""
	}
	control := func(action string) {
		t.Helper()
		if err := controlApproverService(ctx, action, home, os.Geteuid(), run); err != nil {
			t.Fatal(err)
		}
	}
	control("restart")
	first := pid()
	control("restart")
	if pid() == first {
		t.Fatal("restart kept old process")
	}
	control("stop")
	if output, err := exec.CommandContext(ctx, "/bin/launchctl", "print", target).CombinedOutput(); err == nil {
		t.Fatal("stopped service is still registered", string(output))
	}
	control("stop")
	control("restart")
	_ = pid()
	if contents, err := os.ReadFile(plist); err != nil || !strings.Contains(string(contents), label) {
		t.Fatal("package plist changed", err)
	}
}
