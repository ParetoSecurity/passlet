package passlet

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalPairingLifecycle(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "pl-local-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("HOME", dir)
	t.Setenv("PASSLET_ENDPOINT", "")
	if err := PairLocalAgent(); err != nil {
		t.Fatal(err)
	}
	key, err := os.ReadFile(filepath.Join(StateDir(), "agent.pem"))
	if err != nil {
		t.Fatal(err)
	}
	name, err := localAgentName()
	if err != nil {
		t.Fatal(err)
	}
	agents, err := PairingAgents()
	if err != nil || len(agents) != 1 {
		t.Fatal(agents, err)
	}
	for _, label := range agents {
		if label != name {
			t.Fatal(label)
		}
	}
	endpoint := enrollmentServer(t, func(context.Context, string) error { return nil })
	if err := Health(context.Background(), endpoint); err != nil {
		t.Fatal(err)
	}
	// A saved remote connection is deliberately replaced by local setup.
	if err := os.WriteFile(connectionPath(), []byte(`{"endpoint":"https://remote.example:17400","approver":"remote"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := PairLocalAgent(); err != nil {
		t.Fatal(err)
	}
	saved, err := readConnection()
	if err != nil || saved.Endpoint != "unix://"+DefaultSocket() {
		t.Fatal(saved, err)
	}
	again, _ := os.ReadFile(filepath.Join(StateDir(), "agent.pem"))
	if string(key) != string(again) {
		t.Fatal("setup replaced Agent key")
	}
	if err := RevokeAgent(name); err != nil {
		t.Fatal(err)
	}
	if err := PairLocalAgent(); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatal(err)
	}
	if err := Health(context.Background(), endpoint); err == nil {
		t.Fatal("revoked Agent authenticated")
	}
	code, err := CreateInvitation()
	if err != nil {
		t.Fatal(err)
	}
	if err := PairAgent(context.Background(), endpoint, code, name); err != nil {
		t.Fatal(err)
	}
	if err := PairLocalAgent(); err != nil {
		t.Fatal(err)
	}
	if err := Health(context.Background(), endpoint); err != nil {
		t.Fatal(err)
	}
}

func TestLocalPairingFailures(t *testing.T) {
	for _, filename := range []string{"agent.pem", "approver.pem", "pairings.json", "connection.json"} {
		t.Run(filename, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			if err := PrivateDir(StateDir()); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(StateDir(), filename), 0700); err != nil {
				t.Fatal(err)
			}
			if err := PairLocalAgent(); err == nil {
				t.Fatal("accepted inaccessible " + filename)
			}
		})
	}
	t.Run("duplicate label", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		if err := PairLocalAgent(); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(StateDir(), "agent.pem")); err != nil {
			t.Fatal(err)
		}
		if err := PairLocalAgent(); err == nil || !strings.Contains(err.Error(), "name already paired") {
			t.Fatal(err)
		}
	})
}
