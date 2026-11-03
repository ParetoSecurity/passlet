package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

type conductorConfig struct {
	Ssh       []string `json:"ssh"`
	Tailscale bool     `json:"tailscale"`
}

// Homebrew owns the LaunchAgent; user settings stay outside its managed plist.
func configuredConductor() (conductorConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return conductorConfig{}, err
	}
	return readConductorConfig(filepath.Join(home, ".config", "passlet", "conductor.json"))
}

func readConductorConfig(path string) (conductorConfig, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return conductorConfig{}, nil
	}
	if err != nil {
		return conductorConfig{}, errors.New("cannot_read_conductor_config")
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return conductorConfig{}, errors.New("cannot_read_conductor_config")
	}
	var config conductorConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if len(data) > 4096 || !json.Valid(data) || decoder.Decode(&config) != nil {
		return conductorConfig{}, errors.New("invalid_conductor_config_use_tailscale_and_ssh_labels")
	}
	label := regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	seen := make(map[string]bool)
	for _, name := range config.Ssh {
		if !label.MatchString(name) || seen[name] {
			return conductorConfig{}, errors.New("ssh_labels_must_be_unique_lowercase_names_up_to_32_characters_using_letters_digits_and_hyphens")
		}
		seen[name] = true
	}
	return config, nil
}
