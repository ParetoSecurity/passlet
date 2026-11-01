package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// Homebrew owns the LaunchAgent; user settings stay outside its managed plist.
func configuredTailscale() (bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, err
	}
	return readTailscale(filepath.Join(home, ".config", "passlet", "conductor.json"))
}

func readTailscale(path string) (bool, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, errors.New("cannot_read_conductor_config")
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return false, errors.New("cannot_read_conductor_config")
	}
	var config struct {
		Tailscale bool `json:"tailscale"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if len(data) > 4096 || !json.Valid(data) || decoder.Decode(&config) != nil {
		return false, errors.New("invalid_conductor_config_use_tailscale_only")
	}
	return config.Tailscale, nil
}
