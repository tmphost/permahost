// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package repository

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/tmphost/backend/internal/data/model"
	"github.com/tmphost/tmphost/backend/internal/service/encryption"
)

func testEncryption(t *testing.T, key string) encryption.Service {
	t.Helper()
	enc, err := encryption.New(strings.Repeat(key, encryption.MinMasterKeyLength))
	require.NoError(t, err)
	return enc
}

func TestSettingsRepository_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.json")
	repo := NewSettingsRepository(path, testEncryption(t, "k"))

	_, err := repo.LoadSettings()
	assert.ErrorIs(t, err, ErrNotFound)

	want := model.Settings{
		Credits:  model.CreditSettings{MonthlyGrant: 99},
		Database: model.DatabaseSettings{Driver: "postgres", Postgres: model.PostgresSettings{DSN: "postgres://u:pg-secret@db/x"}},
		Auth:     model.AuthSettings{Google: model.OAuthClientSettings{ClientID: "id", ClientSecret: "oauth-secret"}},
	}
	require.NoError(t, repo.SaveSettings(want))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "holds secrets")
	_, err = os.Stat(path + ".tmp")
	assert.True(t, os.IsNotExist(err))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	for _, plain := range []string{"pg-secret", "oauth-secret", "monthlyGrant", "postgres"} {
		assert.NotContains(t, string(raw), plain, "the file is encrypted")
	}
	var env model.EncryptedSettings
	require.NoError(t, json.Unmarshal(raw, &env))
	assert.Equal(t, 1, env.Version)
	assert.Equal(t, encryption.Algorithm, env.Algorithm)

	got, err := repo.LoadSettings()
	require.NoError(t, err)
	assert.Equal(t, want, got)

	_, err = NewSettingsRepository(path, testEncryption(t, "x")).LoadSettings()
	assert.ErrorIs(t, err, encryption.ErrDecrypt, "a wrong master key never yields settings")
}

func TestSettingsRepository_LoadErrors(t *testing.T) {
	dir := t.TempDir()
	enc := testEncryption(t, "k")
	load := func(content string) error {
		path := filepath.Join(dir, "s.json")
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
		_, err := NewSettingsRepository(path, enc).LoadSettings()
		return err
	}
	envelope := func(data string) string {
		b, _ := json.Marshal(model.EncryptedSettings{Version: 1, Algorithm: encryption.Algorithm, Data: data})
		return string(b)
	}

	assert.ErrorContains(t, load("{nope"), "parse settings")
	assert.ErrorContains(t, load(`{"credits":{"monthlyGrant":5}}`), "unsupported format", "plaintext settings are refused")
	assert.ErrorContains(t, load(`{"version":2,"algorithm":"AES-256-GCM","data":""}`), "unsupported format")
	assert.ErrorIs(t, load(envelope("!!not base64")), encryption.ErrDecrypt)
	assert.ErrorIs(t, load(envelope("c2hvcnQ=")), encryption.ErrDecrypt)

	// Sealed with the right key and AAD, but not settings JSON.
	sealed := enc.Encrypt([]byte("not json"), []byte(_settingsAAD))
	assert.ErrorContains(t, load(envelope(encodeBase64(sealed))), "parse settings")

	// Reading a directory fails with something other than not-exist.
	_, err := NewSettingsRepository(dir, enc).LoadSettings()
	assert.ErrorContains(t, err, "read settings")
}

func TestSettingsRepository_SaveErrors(t *testing.T) {
	dir := t.TempDir()
	enc := testEncryption(t, "k")

	file := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))
	assert.ErrorContains(t, NewSettingsRepository(filepath.Join(file, "x", "s.json"), enc).SaveSettings(model.Settings{}), "create settings dir")

	readonly := filepath.Join(dir, "ro")
	require.NoError(t, os.Mkdir(readonly, 0o500))
	assert.ErrorContains(t, NewSettingsRepository(filepath.Join(readonly, "s.json"), enc).SaveSettings(model.Settings{}), "write settings")

	target := filepath.Join(dir, "target")
	require.NoError(t, os.MkdirAll(filepath.Join(target, "child"), 0o700))
	assert.ErrorContains(t, NewSettingsRepository(target, enc).SaveSettings(model.Settings{}), "replace settings")
}

func encodeBase64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
