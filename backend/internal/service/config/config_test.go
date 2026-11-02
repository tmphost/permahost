// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type appCfg struct {
	Port    int    `yaml:"port"`
	BaseURL string `yaml:"baseUrl"`
	Secure  bool   `yaml:"secure"`
	Ratio   float64
	Name    string   `yaml:"name"`
	Hosts   []string `yaml:"hosts"`
	Empty   string   `yaml:"empty"`
	Bad     string   `yaml:"bad"`
}

const _raw = `
app:
  port: ${PORT:3000}
  baseUrl: http://${HOST:localhost}:${PORT:3000}
  secure: ${SECURE:false}
  ratio: ${RATIO:0.5}
  name: ${NAME:tmphost}
  hosts:
    - ${HOST:localhost}
    - static
  empty: ${EMPTY}
  bad: ${BAD}
  count: 3
`

func TestNew_ExpandsPlaceholders(t *testing.T) {
	env := map[string]string{"PORT": "8080", "SECURE": "true", "BAD": "[unclosed"}
	svc, err := New([]byte(_raw), func(k string) string { return env[k] })
	require.NoError(t, err)

	var cfg appCfg
	require.NoError(t, svc.Populate("app", &cfg))
	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, "http://localhost:8080", cfg.BaseURL)
	assert.True(t, cfg.Secure)
	assert.Equal(t, 0.5, cfg.Ratio)
	assert.Equal(t, "tmphost", cfg.Name)
	assert.Equal(t, []string{"localhost", "static"}, cfg.Hosts)
	assert.Equal(t, "", cfg.Empty)
	assert.Equal(t, "[unclosed", cfg.Bad)
}

func TestNew_DefaultsToOSGetenv(t *testing.T) {
	t.Setenv("TMPHOSTIO_CONFIG_TEST", "fromenv")
	svc, err := New([]byte("app:\n  name: ${TMPHOSTIO_CONFIG_TEST}\n"), nil)
	require.NoError(t, err)
	var cfg appCfg
	require.NoError(t, svc.Populate("app", &cfg))
	assert.Equal(t, "fromenv", cfg.Name)
}

func TestNew_ParseError(t *testing.T) {
	_, err := New([]byte("app: [unclosed"), nil)
	assert.Error(t, err)
}

func TestPopulate_MissingKeyIsNoop(t *testing.T) {
	svc, err := New([]byte(_raw), func(string) string { return "" })
	require.NoError(t, err)
	cfg := appCfg{Name: "keep"}
	require.NoError(t, svc.Populate("missing", &cfg))
	assert.Equal(t, "keep", cfg.Name)
}

func TestPopulate_DecodeError(t *testing.T) {
	svc, err := New([]byte("app:\n  port: notanumber\n"), func(string) string { return "" })
	require.NoError(t, err)
	var cfg appCfg
	assert.Error(t, svc.Populate("app", &cfg))
}
