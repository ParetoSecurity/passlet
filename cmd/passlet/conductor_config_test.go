package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPackageServiceSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conductor.json")
	if got, err := readTailscale(path); got != false || err != nil {
		t.Fatal("missing config must leave Tailscale disabled", err)
	}
	for _, tt := range []struct {
		contents string
		want     bool
		valid    bool
	}{
		{`{}`, false, true},
		{`{"tailscale":true}`, true, true},
		{`{"tailscale":"yes"}`, false, false},
		{`{"token":"must-not-be-printed"}`, false, false},
		{`{} {}`, false, false},
	} {
		if err := os.WriteFile(path, []byte(tt.contents), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := readTailscale(path)
		if (err == nil) != tt.valid || got != tt.want {
			t.Fatalf("incorrect config result: %v %v", got, err)
		}
		if err != nil && err.Error() != "invalid_conductor_config_use_tailscale_only" {
			t.Fatal("configuration error may expose contents")
		}
	}
}

func TestConfiguredTailscale(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got, err := configuredTailscale(); err != nil || got != false {
		t.Fatalf("default should disable Tailscale: %v %v", got, err)
	}
	dir := filepath.Join(home, ".config", "passlet")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "conductor.json"), []byte(`{"tailscale":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := configuredTailscale(); err != nil || got != true {
		t.Fatalf("configured listener: %v %v", got, err)
	}
	t.Setenv("HOME", "")
	if _, err := configuredTailscale(); err == nil {
		t.Fatal("accepted unavailable home")
	}
}

func TestConductorConfigIOErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := readTailscale(dir); err == nil || err.Error() != "cannot_read_conductor_config" {
		t.Fatalf("directory read: %v", err)
	}
	loop := filepath.Join(dir, "loop")
	if err := os.Symlink(loop, loop); err != nil {
		t.Fatal(err)
	}
	if _, err := readTailscale(loop); err == nil || err.Error() != "cannot_read_conductor_config" {
		t.Fatalf("symlink loop: %v", err)
	}
}
