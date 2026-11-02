// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package config loads and saves the CLI's server address and OAuth tokens.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultServer is used when neither a flag, the environment nor the config
// file names a server.
const DefaultServer = "https://tmphost.io"

// Environment variables the CLI reads.
const (
	EnvConfig = "TMPHOSTIO_CONFIG"
	EnvServer = "TMPHOSTIO_SERVER"
)

// userConfigDir is swapped in tests to exercise its failure path.
var userConfigDir = os.UserConfigDir

// Config is what the CLI persists between runs. The tokens belong to
// Server: they are never sent anywhere else.
type Config struct {
	Server       string    `json:"server,omitempty"`
	AccessToken  string    `json:"accessToken,omitempty"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	Expiry       time.Time `json:"expiry,omitzero"`
}

// LoggedIn reports whether c holds tokens.
func (c Config) LoggedIn() bool {
	return c.AccessToken != "" || c.RefreshToken != ""
}

// Path returns where the config file lives: $TMPHOSTIO_CONFIG when set,
// otherwise tmphost/config.json under the user's config directory.
func Path(getenv func(string) string) (string, error) {
	if p := getenv(EnvConfig); p != "" {
		return p, nil
	}
	dir, err := userConfigDir()
	if err != nil {
		return "", fmt.Errorf("locate config directory: %w", err)
	}
	return filepath.Join(dir, "tmphost", "config.json"), nil
}

// Load reads the config file; a missing file is an empty config.
func Load(getenv func(string) string) (Config, error) {
	var c Config
	p, err := Path(getenv)
	if err != nil {
		return c, err
	}
	b, err := os.ReadFile(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return c, nil
	case err != nil:
		return c, fmt.Errorf("read config: %w", err)
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, fmt.Errorf("parse config %s: %w", p, err)
	}
	c.Server = strings.TrimRight(c.Server, "/")
	return c, nil
}

// Save writes c to the config file. The file holds secrets, so it is only
// readable by the owner.
func Save(getenv func(string) string, c Config) error {
	p, err := Path(getenv)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	// Marshalling a struct of strings and a time cannot fail.
	b, _ := json.MarshalIndent(c, "", "  ")
	if err := os.WriteFile(p, append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	// WriteFile only applies the mode to new files; tighten an older one.
	if err := os.Chmod(p, 0o600); err != nil {
		return fmt.Errorf("protect config: %w", err)
	}
	return nil
}
