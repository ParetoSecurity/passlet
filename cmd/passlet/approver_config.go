package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

type approverConfig struct {
	SSHAgentSocket string `toml:"ssh_agent_socket"`
	Tailscale      bool   `toml:"tailscale"`
}

// Homebrew owns the LaunchAgent; user settings stay outside its managed plist.
func configuredApprover() (approverConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return approverConfig{}, err
	}
	return readApproverConfig(filepath.Join(home, ".config", "passlet", "approver.toml"))
}

func readApproverConfig(path string) (approverConfig, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return approverConfig{}, nil
	}
	if err != nil {
		return approverConfig{}, errors.New("cannot_read_approver_config")
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil {
		return approverConfig{}, errors.New("cannot_read_approver_config")
	}
	var config approverConfig
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if len(data) > 65536 || decoder.Decode(&config) != nil {
		return approverConfig{}, errors.New("invalid_approver_configuration")
	}
	return config, nil
}
