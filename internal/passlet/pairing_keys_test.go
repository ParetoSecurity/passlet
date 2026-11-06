package passlet

import (
	"bytes"
	"crypto/ed25519"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"passlet.dev/passlet/testutil/testkeys"
)

func TestPairingKeyGeneration(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	path, publicPath := filepath.Join(dir, "keys", "agent.pem"), filepath.Join(dir, "exports", "agent.pub")
	const processes = 12
	keys := make(chan ed25519.PrivateKey, processes)
	var workers sync.WaitGroup
	for range processes {
		workers.Go(func() {
			key, err := LoadOrCreatePairingKey(path, publicPath)
			if err != nil {
				t.Error(err)
				return
			}
			keys <- key
		})
	}
	workers.Wait()
	close(keys)
	stored, err := ReadPrivateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	for key := range keys {
		if !bytes.Equal(key, stored) {
			t.Fatal("concurrent starts replaced the private key")
		}
	}
	data, err := os.ReadFile(publicPath)
	if err != nil {
		t.Fatal(err)
	}
	public, err := ParsePublicKey(string(data))
	if err != nil || !bytes.Equal(public, stored.Public().(ed25519.PublicKey)) {
		t.Fatal("public key does not match private key", err)
	}
	before, _ := os.ReadFile(path)
	for _, file := range []string{path, publicPath} {
		info, err := os.Stat(file)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("unsafe key permissions: %v", err)
		}
	}
	if _, err := LoadOrCreatePairingKey(path, publicPath); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("existing private key changed")
	}
	// Recover a missing public export without rotating its private key.
	os.Remove(publicPath)
	if _, err := LoadOrCreatePairingKey(path, publicPath); err != nil {
		t.Fatal(err)
	}
	after, _ = os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("export recovery rotated private key")
	}
}

func TestPairingKeyFailuresPreserveExistingFiles(t *testing.T) {
	dir := t.TempDir()
	os.Chmod(dir, 0700)
	path := testkeys.Write(t, dir)
	publicPath := filepath.Join(dir, "approver.pub")
	for _, invalid := range []string{"relative", publicPath} {
		if _, err := LoadOrCreatePairingKey(invalid, publicPath); err == nil {
			t.Fatal("invalid path accepted")
		}
	}
	if _, err := LoadOrCreatePairingKey(path, "relative"); err == nil {
		t.Fatal("relative export accepted")
	}
	alias := filepath.Join(dir, "key-alias")
	os.Symlink(path, alias)
	if _, err := LoadOrCreatePairingKey(path, alias); err == nil {
		t.Fatal("export aliased to private key")
	}
	original, _ := os.ReadFile(path)
	os.WriteFile(path, []byte("invalid existing key"), 0600)
	if _, err := LoadOrCreatePairingKey(path, publicPath); err == nil {
		t.Fatal("invalid key replaced")
	}
	unchanged, _ := os.ReadFile(path)
	if string(unchanged) != "invalid existing key" {
		t.Fatal("invalid key overwritten")
	}
	os.WriteFile(path, original, 0600)
	dangling := filepath.Join(dir, "dangling")
	os.Symlink(filepath.Join(dir, "missing-secret"), dangling)
	if _, err := LoadOrCreatePairingKey(dangling, publicPath); err == nil {
		t.Fatal("dangling secret link accepted")
	}
	if _, err := os.Readlink(dangling); err != nil {
		t.Fatal("secret-manager link replaced")
	}
	linked := filepath.Join(dir, "linked")
	os.Symlink(path, linked)
	if _, err := LoadOrCreatePairingKey(linked, publicPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Readlink(linked); err != nil {
		t.Fatal("existing secret link replaced")
	}
	unsafe := filepath.Join(dir, "unsafe")
	os.Mkdir(unsafe, 0755)
	if _, err := LoadOrCreatePairingKey(filepath.Join(unsafe, "key"), publicPath); err == nil || PairingProblem(err) == "" {
		t.Fatal("shared directory accepted", err)
	}
	os.Remove(publicPath)
	os.Mkdir(publicPath, 0700)
	if _, err := LoadOrCreatePairingKey(path, publicPath); err == nil || PairingProblem(err) == "" {
		t.Fatal("unwritable export accepted", err)
	}
}

func TestAgentGeneratesDefaultKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".config", "passlet")
	os.MkdirAll(dir, 0700)
	os.WriteFile(filepath.Join(dir, "agent.toml"), []byte("approver_public_key = '''"+testkeys.Public("approver")+"'''\n"), 0600)
	if _, err := configuredAgentTLS(); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPrivateKey(filepath.Join(StateDir(), "agent.pem")); err != nil {
		t.Fatal(err)
	}
}

func TestPairingKeyReadOnlyDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses filesystem write permissions")
	}
	dir := t.TempDir()
	os.Chmod(dir, 0500)
	defer os.Chmod(dir, 0700)
	if _, err := LoadOrCreatePairingKey(filepath.Join(dir, "agent.pem"), filepath.Join(dir, "agent.pub")); err == nil || err.Error() != "pairing_key_creation_failed" {
		t.Fatalf("unexpected result: %v", err)
	}
}
