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
func configuredTailscaleListener() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return readTailscaleListener(filepath.Join(home, ".config", "passlet", "serve.json"))
}

func readTailscaleListener(path string) (string, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", errors.New("cannot_read_serve_config")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return "", errors.New("cannot_read_serve_config")
	}
	var config struct {
		TailscaleListen string `json:"tailscale_listen"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if len(data) > 4096 || !json.Valid(data) || decoder.Decode(&config) != nil {
		return "", errors.New("invalid_serve_config_use_tailscale_listen_only")
	}
	return config.TailscaleListen, nil
}
