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
