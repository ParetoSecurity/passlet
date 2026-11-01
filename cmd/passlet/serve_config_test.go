package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPackageServiceSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "serve.json")
	if got, err := readTailscaleListener(path); got != "" || err != nil {
		t.Fatal("missing config must leave TCP disabled", err)
	}
	for _, tt := range []struct {
		contents string
		want     string
		valid    bool
	}{
		{`{}`, "", true},
		{`{"tailscale_listen":"127.0.0.1:8484"}`, "127.0.0.1:8484", true},
		{`{"tailscale_listen":42}`, "", false},
		{`{"token":"must-not-be-printed"}`, "", false},
		{`{} {}`, "", false},
	} {
		if err := os.WriteFile(path, []byte(tt.contents), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := readTailscaleListener(path)
		if (err == nil) != tt.valid || got != tt.want {
			t.Fatalf("incorrect config result: %q %v", got, err)
		}
		if err != nil && err.Error() != "invalid_serve_config_use_tailscale_listen_only" {
			t.Fatal("configuration error may expose contents")
		}
	}
}

func TestConfiguredTailscaleListener(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got, err := configuredTailscaleListener(); err != nil || got != "" {
		t.Fatalf("default should disable TCP: %q %v", got, err)
	}
	dir := filepath.Join(home, ".config", "passlet")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "serve.json"), []byte(`{"tailscale_listen":"127.0.0.1:8484"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := configuredTailscaleListener(); err != nil || got != "127.0.0.1:8484" {
		t.Fatalf("configured listener: %q %v", got, err)
	}
	t.Setenv("HOME", "")
	if _, err := configuredTailscaleListener(); err == nil {
		t.Fatal("accepted unavailable home")
	}
}

func TestServeConfigIOErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := readTailscaleListener(dir); err == nil || err.Error() != "cannot_read_serve_config" {
		t.Fatalf("directory read: %v", err)
	}
	loop := filepath.Join(dir, "loop")
	if err := os.Symlink(loop, loop); err != nil {
		t.Fatal(err)
	}
	if _, err := readTailscaleListener(loop); err == nil || err.Error() != "cannot_read_serve_config" {
		t.Fatalf("symlink loop: %v", err)
	}
}
