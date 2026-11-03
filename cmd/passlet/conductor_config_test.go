package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPackageServiceSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conductor.json")
	if got, err := readConductorConfig(path); got.Tailscale != false || err != nil {
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
		got, err := readConductorConfig(path)
		if (err == nil) != tt.valid || got.Tailscale != tt.want {
			t.Fatalf("incorrect config result: %v %v", got, err)
		}
		if err != nil && err.Error() != "invalid_conductor_config_use_tailscale_and_ssh_labels" {
			t.Fatal("configuration error may expose contents")
		}
	}
}

func TestConfiguredConductor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got, err := configuredConductor(); err != nil || got.Tailscale != false {
		t.Fatalf("default should disable Tailscale: %v %v", got, err)
	}
	dir := filepath.Join(home, ".config", "passlet")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "conductor.json"), []byte(`{"tailscale":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got, err := configuredConductor(); err != nil || got.Tailscale != true {
		t.Fatalf("configured listener: %v %v", got, err)
	}
	t.Setenv("HOME", "")
	if _, err := configuredConductor(); err == nil {
		t.Fatal("accepted unavailable home")
	}
}

func TestConductorConfigIOErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := readConductorConfig(dir); err == nil || err.Error() != "cannot_read_conductor_config" {
		t.Fatalf("directory read: %v", err)
	}
	loop := filepath.Join(dir, "loop")
	if err := os.Symlink(loop, loop); err != nil {
		t.Fatal(err)
	}
	if _, err := readConductorConfig(loop); err == nil || err.Error() != "cannot_read_conductor_config" {
		t.Fatalf("symlink loop: %v", err)
	}
}

func TestConductorSshLabels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conductor.json")
	for _, tc := range []struct {
		name, contents string
		want           []string
		valid          bool
	}{
		{"two routes", `{"ssh":["cruncher","other-server"],"tailscale":true}`, []string{"cruncher", "other-server"}, true},
		{"empty", `{"ssh":[]}`, []string{}, true},
		{"duplicate", `{"ssh":["cruncher","cruncher"]}`, nil, false},
		{"path traversal", `{"ssh":["../cruncher"]}`, nil, false},
		{"uppercase", `{"ssh":["Cruncher"]}`, nil, false},
		{"empty label", `{"ssh":[""]}`, nil, false},
		{"leading hyphen", `{"ssh":["-cruncher"]}`, nil, false},
		{"too long", `{"ssh":["` + strings.Repeat("a", 33) + `"]}`, nil, false},
		{"wrong type", `{"ssh":"cruncher"}`, nil, false},
		{"oversized config", `{"ssh":[]}` + strings.Repeat(" ", 4096), nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tc.contents), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := readConductorConfig(path)
			if (err == nil) != tc.valid || !reflect.DeepEqual(got.Ssh, tc.want) {
				t.Fatalf("config=%+v err=%v", got, err)
			}
		})
	}
}
