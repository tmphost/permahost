// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())
	return port
}

func envFor(t *testing.T, port int) map[string]string {
	t.Helper()
	return map[string]string{
		"TMPHOSTIO_PORT":               strconv.Itoa(port),
		"TMPHOSTIO_AUTH_JWT_SECRET":    "secret",
		"TMPHOSTIO_MASTER_KEY":         "a-master-key-of-at-least-32-characters",
		"TMPHOSTIO_SQLITE_DSN":         filepath.Join(t.TempDir(), "tmphost.db"),
		"TMPHOSTIO_SETTINGS_FILE":      filepath.Join(t.TempDir(), "settings.json"),
		"TMPHOSTIO_STORAGE_LOCAL_PATH": filepath.Join(t.TempDir(), "files"),
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	raw, err := bundled.ReadFile("_config/base.yaml")
	require.NoError(t, err)
	cfg, err := loadConfig(raw, func(string) string { return "" })
	require.NoError(t, err)
	assert.Equal(t, 2027, cfg.App.Port)
	assert.Equal(t, "http://localhost:2027", cfg.App.BaseURL)
	assert.Empty(t, cfg.App.SitesDomain)
	assert.False(t, cfg.TLS.Enabled)
	assert.Equal(t, 443, cfg.TLS.Port)
	assert.False(t, cfg.TLS.Lego.Enabled)
	assert.False(t, cfg.App.CookieSecure)
	assert.True(t, cfg.SQLite.Enabled)
	assert.Contains(t, cfg.SQLite.DSN, "./_storage/tmphost.db?")
	assert.Equal(t, time.Hour, cfg.SQLite.MaxConnLifetime)
	assert.False(t, cfg.Postgres.Enabled)
	assert.Empty(t, cfg.Postgres.DSN)
	assert.Empty(t, cfg.MasterKey)
	assert.Equal(t, 250, cfg.Credits.MonthlyGrant)
	assert.Equal(t, "local", cfg.Storage.Driver)
	assert.Equal(t, "./_storage/files", cfg.Storage.LocalPath)
	assert.False(t, cfg.Files.Subdomains)
	assert.Empty(t, cfg.Files.PublicDomain)
	assert.Equal(t, 432000, cfg.Files.MaxTTLMinutes)
	assert.Equal(t, 16, cfg.Files.MaxUploadMB)
	assert.Equal(t, "./_storage/settings.json", cfg.App.SettingsFile)
	assert.Equal(t, "", cfg.Auth.Google.ClientID)
}

func TestLoadConfig_Errors(t *testing.T) {
	_, err := loadConfig([]byte("app: [unclosed"), func(string) string { return "" })
	assert.Error(t, err)

	raw, _ := bundled.ReadFile("_config/base.yaml")
	_, err = loadConfig(raw, func(k string) string {
		if k == "TMPHOSTIO_PORT" {
			return "not-a-port"
		}
		return ""
	})
	assert.Error(t, err)
}

func TestRun_ServesUntilCancelled(t *testing.T) {
	port := freePort(t)
	env := envFor(t, port)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- run(ctx, func(k string) string { return env[k] }) }()

	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/auth/config"
	require.Eventually(t, func() bool {
		res, err := http.Get(url)
		if err != nil {
			return false
		}
		_ = res.Body.Close()
		return res.StatusCode == http.StatusOK
	}, 5*time.Second, 20*time.Millisecond)

	cancel()
	assert.NoError(t, <-done)
}

func TestRun_Errors(t *testing.T) {
	t.Run("bad config", func(t *testing.T) {
		assert.Error(t, run(context.Background(), func(k string) string {
			if k == "TMPHOSTIO_PORT" {
				return "x"
			}
			return ""
		}))
	})

	t.Run("app fails", func(t *testing.T) {
		assert.Error(t, run(context.Background(), func(string) string { return "" }), "missing JWT secret")
	})

	t.Run("listen fails", func(t *testing.T) {
		l, err := net.Listen("tcp", ":0")
		require.NoError(t, err)
		defer func() { _ = l.Close() }()
		env := envFor(t, l.Addr().(*net.TCPAddr).Port)
		assert.Error(t, run(context.Background(), func(k string) string { return env[k] }))
	})
}

func TestMain_StopsOnSignal(t *testing.T) {
	port := freePort(t)
	for k, v := range envFor(t, port) {
		t.Setenv(k, v)
	}
	var fatalErr []any
	fatal = func(v ...any) { fatalErr = v }
	t.Cleanup(func() { fatal = log.Fatal })

	done := make(chan struct{})
	go func() { main(); close(done) }()
	require.Eventually(t, func() bool {
		c, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port))
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	}, 5*time.Second, 20*time.Millisecond)

	require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGINT))
	<-done
	assert.Nil(t, fatalErr)
}

func TestMain_Fatal(t *testing.T) {
	t.Setenv("TMPHOSTIO_AUTH_JWT_SECRET", "")
	var fatalErr []any
	fatal = func(v ...any) { fatalErr = v }
	t.Cleanup(func() { fatal = log.Fatal })
	main()
	assert.NotEmpty(t, fatalErr)
}
