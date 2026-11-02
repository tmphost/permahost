// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package repository

//go:generate mockgen -source=settings.go -destination=mocks/settings.go -package=mocks

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/tmphost/tmphost/backend/internal/data/model"
	"github.com/tmphost/tmphost/backend/internal/service/encryption"
)

const (
	_settingsVersion = 1
	// _settingsAAD binds the ciphertext to its purpose, so another sealed
	// blob cannot be swapped in as settings.
	_settingsAAD = "tmphost.io/settings/v1"
)

type (
	// SettingsRepository persists the setup page settings. They live in a
	// file, not the database, because the database choice is a setting.
	SettingsRepository interface {
		// LoadSettings returns the saved settings, or ErrNotFound when none
		// were saved yet.
		LoadSettings() (model.Settings, error)
		// SaveSettings atomically replaces the saved settings.
		SaveSettings(s model.Settings) error
	}

	settingsRepository struct {
		path string
		enc  encryption.Service
	}
)

// NewSettingsRepository stores settings at path, encrypted with enc. The
// plaintext never touches the disk.
func NewSettingsRepository(path string, enc encryption.Service) SettingsRepository {
	return &settingsRepository{path: path, enc: enc}
}

func (r *settingsRepository) LoadSettings() (model.Settings, error) {
	b, err := os.ReadFile(r.path)
	if errors.Is(err, fs.ErrNotExist) {
		return model.Settings{}, ErrNotFound
	}
	if err != nil {
		return model.Settings{}, fmt.Errorf("read settings: %w", err)
	}
	var env model.EncryptedSettings
	if err := json.Unmarshal(b, &env); err != nil {
		return model.Settings{}, fmt.Errorf("parse settings %s: %w", r.path, err)
	}
	if env.Version != _settingsVersion || env.Algorithm != encryption.Algorithm {
		return model.Settings{}, fmt.Errorf("settings %s: unsupported format (version %d, %q); expected an encrypted settings file", r.path, env.Version, env.Algorithm)
	}
	sealed, err := base64.StdEncoding.DecodeString(env.Data)
	if err != nil {
		return model.Settings{}, fmt.Errorf("settings %s: %w", r.path, encryption.ErrDecrypt)
	}
	plain, err := r.enc.Decrypt(sealed, []byte(_settingsAAD))
	if err != nil {
		return model.Settings{}, fmt.Errorf("settings %s: %w", r.path, err)
	}
	var s model.Settings
	if err := json.Unmarshal(plain, &s); err != nil {
		return model.Settings{}, fmt.Errorf("parse settings %s: %w", r.path, err)
	}
	return s, nil
}

func (r *settingsRepository) SaveSettings(s model.Settings) error {
	// Structs of plain fields always encode.
	plain, _ := json.Marshal(s)
	b, _ := json.MarshalIndent(model.EncryptedSettings{
		Version:   _settingsVersion,
		Algorithm: encryption.Algorithm,
		Data:      base64.StdEncoding.EncodeToString(r.enc.Encrypt(plain, []byte(_settingsAAD))),
	}, "", "  ")
	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create settings dir: %w", err)
	}
	// The file holds secrets: owner-only, and written via rename so a crash
	// never leaves it half written.
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}
	// Gone already after a successful rename; this only cleans up a failed one.
	defer func() { _ = os.Remove(tmp) }()
	if err := os.Rename(tmp, r.path); err != nil {
		return fmt.Errorf("replace settings: %w", err)
	}
	return nil
}
