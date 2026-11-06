package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
	"passlet.dev/passlet/internal/passlet"
)

type approverConfig struct {
	PrivateKeyFile string `toml:"private_key_file"`
	Agents         map[string]struct {
		PublicKey string `toml:"public_key"`
	} `toml:"agents"`
	Tailscale bool `toml:"tailscale"`
}

func (c approverConfig) pairing() (*passlet.Pairing, error) {
	path := c.PrivateKeyFile
	if path == "" {
		path = filepath.Join(passlet.StateDir(), "approver.pem")
	}
	key, err := passlet.LoadOrCreatePairingKey(path, filepath.Join(passlet.StateDir(), "approver.pub"))
	if err != nil {
		return nil, err
	}
	agents := make(map[string]string)
	for name, agent := range c.Agents {
		agents[name] = agent.PublicKey
	}
	return passlet.NewPairing(key, agents)
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
