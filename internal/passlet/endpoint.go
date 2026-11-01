package passlet

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func endpointFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "passlet", "endpoint"), nil
}

func ResolveEndpoint(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if endpoint := os.Getenv("PASSLET_ENDPOINT"); endpoint != "" {
		return endpoint, nil
	}
	path, err := endpointFile()
	if err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return "unix://" + DefaultSocket(), nil
	}
	if err != nil {
		return "", fmt.Errorf("cannot read saved endpoint: %w", err)
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return "", fmt.Errorf("cannot read saved endpoint: %w", err)
	}
	endpoint := strings.TrimSpace(string(data))
	if _, err := endpointURL(endpoint); err != nil || len(data) > 4096 {
		return "", errors.New("invalid saved endpoint; run passlet connect with a valid endpoint")
	}
	return endpoint, nil
}

func SaveEndpoint(endpoint string) error {
	if _, err := endpointURL(endpoint); err != nil {
		return err
	}
	if len(endpoint)+1 > 4096 {
		return errors.New("endpoint_too_long")
	}
	path, err := endpointFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".endpoint-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if _, err := file.WriteString(endpoint + "\n"); err != nil { // coverage-ignore: Requires disk failure after creating the private temporary file.
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil { // coverage-ignore: Requires filesystem failure while closing the newly written file.
		return err
	}
	return os.Rename(file.Name(), path)
}
