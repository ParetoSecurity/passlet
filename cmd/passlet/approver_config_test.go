package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPackageServiceSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "approver.toml")
	if got, err := readApproverConfig(path); got.Tailscale != false || err != nil {
		t.Fatal("missing config must leave Tailscale disabled", err)
	}
	for _, tt := range []struct {
		contents string
		want     bool
		valid    bool
	}{
		{``, false, true},
		{`tailscale = true`, true, true},
		{"# Remote agents\ntailscale = true # Enable forwarding\n", true, true},
		{`tailscale = "yes"`, false, false},
		{`token = "must-not-be-printed"`, false, false},
		{"tailscale = true\ntailscale = false", false, false},
	} {
		if err := os.WriteFile(path, []byte(tt.contents), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := readApproverConfig(path)
		if (err == nil) != tt.valid || got.Tailscale != tt.want {
			t.Fatalf("incorrect config result: %v %v", got, err)
		}
		if err != nil && err.Error() != "invalid_approver_configuration" {
			t.Fatal("configuration error may expose contents")
		}
	}
}

func TestConfiguredApprover(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got, err := configuredApprover(); err != nil || got.Tailscale != false {
		t.Fatalf("default should disable Tailscale: %v %v", got, err)
	}
	dir := filepath.Join(home, ".config", "passlet")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "approver.toml"), []byte(`tailscale = true`), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := configuredApprover(); err != nil || got.Tailscale != true {
		t.Fatalf("configured listener: %v %v", got, err)
	}
	t.Setenv("HOME", "")
	if _, err := configuredApprover(); err == nil {
		t.Fatal("accepted unavailable home")
	}
}

func TestApproverConfigIOErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := readApproverConfig(dir); err == nil || err.Error() != "cannot_read_approver_config" {
		t.Fatalf("directory read: %v", err)
	}
	loop := filepath.Join(dir, "loop")
	if err := os.Symlink(loop, loop); err != nil {
		t.Fatal(err)
	}
	if _, err := readApproverConfig(loop); err == nil || err.Error() != "cannot_read_approver_config" {
		t.Fatalf("symlink loop: %v", err)
	}
}
