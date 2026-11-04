package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/pelletier/go-toml/v2"
)

type conductorConfig struct {
	Ssh       []string `toml:"ssh"`
	Tailscale bool     `toml:"tailscale"`
}

// Homebrew owns the LaunchAgent; user settings stay outside its managed plist.
func configuredConductor() (conductorConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return conductorConfig{}, err
	}
	return readConductorConfig(filepath.Join(home, ".config", "passlet", "conductor.toml"))
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
	decoder := toml.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if len(data) > 4096 || decoder.Decode(&config) != nil {
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
