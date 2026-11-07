// Package testkeys contains synthetic keys for fixtures, never user credentials.
package testkeys

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func Private(name string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("passlet test fixture: " + name))
	return ed25519.NewKeyFromSeed(seed[:])
}

func Public(name string) string {
	der, _ := x509.MarshalPKIXPublicKey(Private(name).Public())
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func Write(t testing.TB, home string) string {
	t.Helper()
	return WriteNamed(t, home, "")
}

func WriteNamed(t testing.TB, home, prefix string) string {
	t.Helper()
	dir := filepath.Join(home, ".local", "share", "passlet")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"agent", "approver"} {
		der, _ := x509.MarshalPKCS8PrivateKey(Private(prefix + name))
		if err := os.WriteFile(filepath.Join(dir, name+".pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0600); err != nil {
			t.Fatal(err)
		}
	}
	endpoint := "https://fixture.example:17400"
	if len(filepath.Join(dir, "passlet.sock")) <= 103 {
		endpoint = "unix://" + filepath.Join(dir, "passlet.sock")
	}
	var previous map[string]string
	if existing, err := os.ReadFile(filepath.Join(dir, "connection.json")); err == nil && json.Unmarshal(existing, &previous) == nil {
		endpoint = previous["endpoint"]
	}
	data, _ := json.Marshal(map[string]string{"endpoint": endpoint, "approver": base64.RawURLEncoding.EncodeToString(Private(prefix + "approver").Public().(ed25519.PublicKey))})
	if err := os.WriteFile(filepath.Join(dir, "connection.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	id := sha256.Sum256(Private(prefix + "agent").Public().(ed25519.PublicKey))
	data, _ = json.Marshal(map[string]any{"agents": map[string]any{hex.EncodeToString(id[:]): map[string]string{"name": "test-agent", "public_key": base64.RawURLEncoding.EncodeToString(Private(prefix + "agent").Public().(ed25519.PublicKey))}}})
	if err := os.WriteFile(filepath.Join(dir, "pairings.json"), data, 0600); err != nil {
		t.Fatal(err)
	}

	return filepath.Join(dir, "approver.pem")
}

func SaveEndpoint(endpoint string) error {
	path := filepath.Join(os.Getenv("HOME"), ".local", "share", "passlet", "connection.json")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	config := map[string]string{"approver": base64.RawURLEncoding.EncodeToString(Private("approver").Public().(ed25519.PublicKey))}
	if len(data) > 0 {
		if err = json.Unmarshal(data, &config); err != nil {
			return err
		}
	}
	os.MkdirAll(filepath.Dir(path), 0700)
	config["endpoint"] = endpoint
	data, err = json.Marshal(config)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}
