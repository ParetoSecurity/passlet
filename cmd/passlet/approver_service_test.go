package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestApproverServiceLifecycle(t *testing.T) {
	missing := exec.Command("sh", "-c", "exit 113").Run()
	for _, symlink := range []bool{false, true} {
		t.Run(fmt.Sprint("HomeManager=", symlink), func(t *testing.T) {
			home := t.TempDir()
			plist := filepath.Join(home, "Library", "LaunchAgents", approverServiceLabel+".plist")
			if err := os.MkdirAll(filepath.Dir(plist), 0700); err != nil {
				t.Fatal(err)
			}
			source := filepath.Join(home, "package.plist")
			if err := os.WriteFile(source, []byte("package managed"), 0400); err != nil {
				t.Fatal(err)
			}
			if symlink {
				if err := os.Symlink(source, plist); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(plist, []byte("package managed"), 0400); err != nil {
				t.Fatal(err)
			}
			loaded := false
			var calls []string
			run := func(_ context.Context, args ...string) error {
				calls = append(calls, strings.Join(args, " "))
				switch args[0] {
				case "print":
					if len(strings.Split(args[1], "/")) == 3 && !loaded {
						return missing
					}
				case "bootstrap":
					loaded = true
				case "bootout":
					loaded = false
				}
				return nil
			}
			for _, action := range []string{"restart", "restart", "stop", "stop", "restart"} {
				if err := controlApproverService(context.Background(), action, home, 501, run); err != nil {
					t.Fatal(err)
				}
			}
			target := "gui/501/" + approverServiceLabel
			var mutations []string
			for _, call := range calls {
				if !strings.HasPrefix(call, "print ") {
					mutations = append(mutations, call)
				}
			}
			want := []string{"bootstrap gui/501 " + plist, "kickstart " + target, "bootout --wait " + target, "bootstrap gui/501 " + plist, "kickstart " + target, "bootout --wait " + target, "bootstrap gui/501 " + plist, "kickstart " + target}
			if !reflect.DeepEqual(mutations, want) {
				t.Fatalf("commands %v; want %v", mutations, want)
			}
			if data, err := os.ReadFile(plist); err != nil || string(data) != "package managed" {
				t.Fatal("changed package plist", err)
			}
			if symlink {
				if _, err := os.Readlink(plist); err != nil {
					t.Fatal("replaced Home Manager symlink", err)
				}
			}
		})
	}
}

func TestApproverServiceFailures(t *testing.T) {
	home := t.TempDir()
	plist := filepath.Join(home, "Library", "LaunchAgents", approverServiceLabel+".plist")
	if err := os.MkdirAll(filepath.Dir(plist), 0700); err != nil {
		t.Fatal(err)
	}
	never := func(context.Context, ...string) error { t.Fatal("unexpected launchctl call"); return nil }
	if err := controlApproverService(context.Background(), "restart", home, 0, never); err == nil {
		t.Fatal("allowed root")
	}
	if err := controlApproverService(context.Background(), "restart", home, 501, never); err == nil {
		t.Fatal("allowed missing installation")
	}
	if err := os.Mkdir(plist, 0700); err != nil {
		t.Fatal(err)
	}
	if err := controlApproverService(context.Background(), "restart", home, 501, never); err == nil {
		t.Fatal("allowed directory plist")
	}
	if err := os.Remove(plist); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plist, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	for failAt := 1; failAt <= 5; failAt++ {
		t.Run(strconv.Itoa(failAt), func(t *testing.T) {
			calls := 0
			failure := errors.New("permission denied")
			run := func(_ context.Context, args ...string) error {
				calls++
				if calls == failAt {
					return failure
				}
				return nil
			}
			err := controlApproverService(context.Background(), "restart", home, 501, run)
			if !errors.Is(err, failure) || calls != failAt {
				t.Fatalf("continued after failure: %d calls, %v", calls, err)
			}
		})
	}
}

func TestApproverServiceCommandArguments(t *testing.T) {
	for _, action := range []string{"stop", "restart"} {
		if err := approverServiceCommand(context.Background(), action, []string{"--help"}); err == nil {
			t.Fatal("expected help sentinel")
		}
		if err := approverServiceCommand(context.Background(), action, []string{"--helper", "/tmp/helper"}); err == nil {
			t.Fatal("accepted foreground option")
		}
		if err := approverServiceCommand(context.Background(), action, []string{"extra"}); err == nil {
			t.Fatal("accepted positional argument")
		}
		if runtime.GOOS != "darwin" {
			if err := approverServiceCommand(context.Background(), action, nil); err == nil || !strings.Contains(err.Error(), "requires_macOS") {
				t.Fatal(err)
			}
		}
	}
}

func TestApproverServicePlatformAndOutput(t *testing.T) {
	var output bytes.Buffer
	deps := approverServiceDependencies{goos: "linux", uid: 501, output: &output}
	if err := approverServiceWithDependencies(context.Background(), "stop", nil, deps); err == nil {
		t.Fatal("allowed Linux")
	}
	deps.goos = "darwin"
	failed := errors.New("missing home")
	deps.home = func() (string, error) { return "", failed }
	if err := approverServiceWithDependencies(context.Background(), "stop", nil, deps); !errors.Is(err, failed) {
		t.Fatal(err)
	}
	deps.home = func() (string, error) { return t.TempDir(), nil }
	deps.launchctl = func(context.Context, ...string) error { return failed }
	if err := approverServiceWithDependencies(context.Background(), "stop", nil, deps); !errors.Is(err, failed) {
		t.Fatal(err)
	}
	deps.launchctl = func(context.Context, ...string) error { return nil }
	if err := approverServiceWithDependencies(context.Background(), "stop", nil, deps); err != nil {
		t.Fatal(err)
	}
	if output.String() != "Approver service stopped.\n" {
		t.Fatal(output.String())
	}
}
