package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"passlet.dev/passlet/internal/passlet"
)

func TestPairCommandValidation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PASSLET_ENDPOINT", "")
	for _, args := range [][]string{nil, {"--bad"}, {"--code", "x"}, {"--code", "x", "a", "b"}, {"--code", "x", "https://example.test"}, {"--code", "x", "--endpoint", "http://unsafe"}} {
		if err := pair(context.Background(), args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if _, err := invokeCLI(t, "", "connect", "https://example.test"); err == nil {
		t.Fatal("obsolete connect accepted")
	}
	t.Setenv("PASSLET_ENDPOINT", "https://override.test")
	if err := pair(context.Background(), []string{"--code", "x"}); err == nil {
		t.Fatal("environment override accepted")
	}
}

func TestApproverPairCommands(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	out, err := invokeCLI(t, "", "approver", "pair")
	if err != nil || !strings.Contains(out, "passlet pair --code ") {
		t.Fatal(out, err)
	}
	if strings.Contains(out, "<endpoint>") {
		t.Fatal("printed unusable command")
	}
	for _, args := range [][]string{{"approver", "pair", "extra"}, {"approver", "pair", "--bad"}, {"approver", "agents"}, {"approver", "agents", "revoke", "missing"}} {
		if _, err := invokeCLI(t, "", args...); err == nil {
			t.Fatal("accepted", args)
		}
	}
	for _, args := range [][]string{{"approver", "pair", "--help"}, {"approver", "agents", "--help"}, {"approver", "agents", "list"}} {
		if _, err := invokeCLI(t, "", args...); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(passlet.StateDir(), "pairings.json"), []byte(`{"agents":{"fixture":{"name":"alice@vm","public_key":"fixture"}}}`), 0600)
	out, err = invokeCLI(t, "", "approver", "agents", "list")
	if err != nil || out != "alice@vm\n" {
		t.Fatal(out, err)
	}
	out, err = invokeCLI(t, "", "approver", "agents", "revoke", "alice@vm")
	if err != nil || out != "Revoked Agent: alice@vm\n" {
		t.Fatal(out, err)
	}
}

func TestPairingStateErrors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PASSLET_ENDPOINT", "")
	os.MkdirAll(passlet.StateDir(), 0700)
	os.WriteFile(filepath.Join(passlet.StateDir(), "connection.json"), []byte("broken"), 0600)
	for _, args := range [][]string{{"doctor"}, {"request", "--repos", "alice/repo", "--permissions", "contents:read"}} {
		if _, err := invokeCLI(t, "", args...); err == nil {
			t.Fatal("invalid saved connection accepted")
		}
	}
	os.WriteFile(filepath.Join(passlet.StateDir(), "pairings.json"), []byte("broken"), 0600)
	if _, err := invokeCLI(t, "", "approver", "agents", "list"); err == nil {
		t.Fatal("invalid saved Agents accepted")
	}
	if _, err := invokeCLI(t, "", "approver", "pair"); err == nil {
		t.Fatal("invalid saved state accepted")
	}
	os.Remove(filepath.Join(passlet.StateDir(), "pairings.json"))
	dir := filepath.Join(os.Getenv("HOME"), ".config", "passlet")
	os.MkdirAll(dir, 0700)
	os.WriteFile(filepath.Join(dir, "approver.toml"), []byte("broken"), 0600)
	if _, err := invokeCLI(t, "", "approver", "pair"); err == nil {
		t.Fatal("invalid service config accepted")
	}
}
