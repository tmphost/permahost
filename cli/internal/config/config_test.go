// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func failConfigDir(t *testing.T) {
	t.Helper()
	orig := userConfigDir
	userConfigDir = func() (string, error) { return "", errors.New("no home") }
	t.Cleanup(func() { userConfigDir = orig })
}

func TestPath(t *testing.T) {
	p, err := Path(envOf(map[string]string{EnvConfig: "/x/c.json"}))
	require.NoError(t, err)
	assert.Equal(t, "/x/c.json", p)

	orig := userConfigDir
	userConfigDir = func() (string, error) { return "/home/u/.config", nil }
	p, err = Path(envOf(nil))
	userConfigDir = orig
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/home/u/.config", "tmphost", "config.json"), p)

	failConfigDir(t)
	_, err = Path(envOf(nil))
	assert.ErrorContains(t, err, "no home")
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	env := envOf(map[string]string{EnvConfig: p})

	c, err := Load(env)
	require.NoError(t, err)
	assert.Equal(t, Config{}, c)
	assert.False(t, c.LoggedIn())

	require.NoError(t, os.WriteFile(p, []byte(`{"server":"https://a.example/","accessToken":"at","refreshToken":"rt","expiry":"2026-10-18T12:00:00Z"}`), 0o600))
	c, err = Load(env)
	require.NoError(t, err)
	assert.Equal(t, Config{Server: "https://a.example", AccessToken: "at", RefreshToken: "rt", Expiry: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)}, c)
	assert.True(t, c.LoggedIn())
	assert.True(t, Config{RefreshToken: "rt"}.LoggedIn())

	require.NoError(t, os.WriteFile(p, []byte(`{`), 0o600))
	_, err = Load(env)
	assert.ErrorContains(t, err, "parse config")

	_, err = Load(envOf(map[string]string{EnvConfig: dir}))
	assert.ErrorContains(t, err, "read config")

	failConfigDir(t)
	_, err = Load(envOf(nil))
	assert.Error(t, err)
}

func TestSave(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "config.json")
	env := envOf(map[string]string{EnvConfig: p})
	want := Config{Server: "https://a.example", AccessToken: "at", RefreshToken: "rt", Expiry: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)}

	require.NoError(t, Save(env, want))
	info, err := os.Stat(p)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	dinfo, err := os.Stat(filepath.Dir(p))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dinfo.Mode().Perm())
	c, err := Load(env)
	require.NoError(t, err)
	assert.Equal(t, want, c)

	// An existing world-readable file is tightened, and empty fields vanish.
	require.NoError(t, os.Chmod(p, 0o644))
	require.NoError(t, Save(env, Config{Server: "https://a.example"}))
	info, err = os.Stat(p)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	b, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.JSONEq(t, `{"server":"https://a.example"}`, string(b))
}

func TestSaveErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	err := Save(envOf(map[string]string{EnvConfig: filepath.Join(file, "config.json")}), Config{})
	assert.ErrorContains(t, err, "create config directory")

	err = Save(envOf(map[string]string{EnvConfig: dir}), Config{})
	assert.ErrorContains(t, err, "write config")

	failConfigDir(t)
	assert.Error(t, Save(envOf(nil), Config{}))
}

func TestSaveChmodError(t *testing.T) {
	// /dev/stdout-like targets accept writes but refuse chmod for an
	// unprivileged user; /dev/null is the portable one on Unix.
	if os.Geteuid() == 0 {
		t.Skip("root may chmod /dev/null")
	}
	err := Save(envOf(map[string]string{EnvConfig: os.DevNull}), Config{})
	assert.ErrorContains(t, err, "protect config")
}
