package passlet

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
)

func LoadOrCreatePairingKey(path, publicPath string) (ed25519.PrivateKey, error) {
	if !filepath.IsAbs(path) || !filepath.IsAbs(publicPath) || filepath.Clean(path) == filepath.Clean(publicPath) {
		return nil, authenticationError("invalid_pairing_key_path")
	}
	// Lstat preserves dangling secret-manager symlinks instead of replacing them.
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		_, private, err := ed25519.GenerateKey(rand.Reader)
		if err != nil { // coverage-ignore: Requires failure in the system-backed cryptographic random source.
			return nil, authenticationError("pairing_key_creation_failed")
		}
		der, _ := x509.MarshalPKCS8PrivateKey(private)
		data := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
		if err := writePairingKey(path, data, false); err != nil {
			return nil, authenticationError("pairing_key_creation_failed")
		}
	}
	private, err := ReadPrivateKey(path)
	if err != nil {
		return nil, err
	}
	der, _ := x509.MarshalPKIXPublicKey(private.Public())
	public := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	if info, err := os.Stat(publicPath); err == nil {
		source, err := os.Stat(path)
		if err != nil || os.SameFile(info, source) {
			return nil, authenticationError("invalid_pairing_key_path")
		}
	}
	existing, _ := os.ReadFile(publicPath)
	if !bytes.Equal(existing, public) {
		if err := writePairingKey(publicPath, public, true); err != nil {
			return nil, authenticationError("pairing_public_key_export_failed")
		}
	}
	return private, nil
}

func writePairingKey(path string, data []byte, replace bool) error {
	if err := PrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".pairing-key-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if _, err := file.Write(data); err != nil { // coverage-ignore: Requires filesystem failure while writing a small regular file.
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil { // coverage-ignore: Requires filesystem failure while syncing the key.
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil { // coverage-ignore: Requires filesystem failure after writing the key.
		return err
	}
	if replace {
		return os.Rename(file.Name(), path)
	}
	// Publish a complete key without replacing one created by another process.
	if err := os.Link(file.Name(), path); err != nil && !os.IsExist(err) { // coverage-ignore: Other link failures require filesystem changes after the temporary file was written.
		return err
	}
	return nil
}
