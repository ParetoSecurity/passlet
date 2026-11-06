// Package testkeys contains synthetic keys for fixtures, never user credentials.
package testkeys

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
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
	dir := filepath.Join(home, ".config", "passlet")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"agent", "approver"} {
		der, _ := x509.MarshalPKCS8PrivateKey(Private(prefix + name))
		if err := os.WriteFile(filepath.Join(dir, name+".pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0600); err != nil {
			t.Fatal(err)
		}
	}
	data := fmt.Sprintf("private_key_file = %q\napprover_public_key = %q\n", filepath.Join(dir, "agent.pem"), Public(prefix+"approver"))
	if err := os.WriteFile(filepath.Join(dir, "agent.toml"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "approver.pem")
}
