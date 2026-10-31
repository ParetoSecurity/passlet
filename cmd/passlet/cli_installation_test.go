package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIAvailability(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	if !strings.Contains(cliAvailabilityIn(nil), "CLI missing") {
		t.Fatal("missing CLI not reported")
	}
	target := filepath.Join(dir, "passlet")
	if err := os.WriteFile(target, []byte("not executable"), 0600); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cliAvailabilityIn(nil), "CLI missing") {
		t.Fatal("nonexecutable accepted")
	}
	if err := os.Chmod(target, 0700); err != nil {
		t.Fatal(err)
	}
	if cliAvailabilityIn(nil) != "CLI: available on PATH." {
		t.Fatal("CLI on PATH not detected")
	}
	t.Setenv("PATH", t.TempDir())
	if !strings.Contains(cliAvailabilityIn([]string{dir}), "CLI: installed in ") {
		t.Fatal("GUI PATH caused missing-CLI warning")
	}
	linkDir := t.TempDir()
	if err := os.Symlink(target, filepath.Join(linkDir, "passlet")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", linkDir)
	if cliAvailabilityIn(nil) != "CLI: available on PATH." {
		t.Fatal("managed symlink not detected")
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(cliAvailabilityIn([]string{dir}), "CLI missing") {
		t.Fatal("broken symlink accepted")
	}
}
