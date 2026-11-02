// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tmphost/tmphost/cli/internal/config"
)

// now is the fixed clock every command test runs at.
var now = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

const meJSON = `{"user":{"id":"u1","email":"ada@example.com","name":"Ada","credits":40,"creditsLimit":100,"creditsRenewAt":"2026-11-01T00:00:00Z"}}`

const limitsJSON = `{"defaultTtlMinutes":30,"maxTtlMinutes":432000,"maxUploadBytes":16,"billingBlockMinutes":30,"billingUnitBytes":1048576}`

// harness runs commands against a fake server: mux holds the API and OAuth
// handlers, and API requests must carry the access token in validToken.
type harness struct {
	t          *testing.T
	mux        *http.ServeMux
	srv        *httptest.Server
	env        map[string]string
	validToken string
	browser    func(string) error
	stdout     bytes.Buffer
	stderr     bytes.Buffer
}

// newHarness returns a harness logged in to its server with a fresh token.
func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, mux: http.NewServeMux(), validToken: "at"}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Header.Get("Authorization") != "Bearer "+h.validToken {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":"invalid token"}`)
			return
		}
		h.mux.ServeHTTP(w, r)
	}))
	t.Cleanup(h.srv.Close)
	h.env = map[string]string{"TMPHOSTIO_CONFIG": filepath.Join(t.TempDir(), "config.json")}
	h.saveConfig(config.Config{Server: h.srv.URL, AccessToken: "at", RefreshToken: "rt", Expiry: now.Add(time.Hour)})
	return h
}

func (h *harness) saveConfig(c config.Config) {
	h.t.Helper()
	require.NoError(h.t, config.Save(h.getenv, c))
}

func (h *harness) loadConfig() config.Config {
	h.t.Helper()
	c, err := config.Load(h.getenv)
	require.NoError(h.t, err)
	return c
}

func (h *harness) getenv(k string) string { return h.env[k] }

// reply registers a canned response for an http.ServeMux pattern.
func (h *harness) reply(pattern string, status int, body string) {
	h.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
}

// run executes the CLI and returns the exit code; output is in h.stdout and
// h.stderr.
func (h *harness) run(args ...string) int {
	h.stdout.Reset()
	h.stderr.Reset()
	return Execute(args, Options{
		Out:         &h.stdout,
		Err:         &h.stderr,
		Getenv:      h.getenv,
		Now:         func() time.Time { return now },
		OpenBrowser: h.browser,
		Version:     "v1.2.3",
	})
}

// decode parses h.stdout as JSON.
func (h *harness) decode() map[string]any {
	h.t.Helper()
	var v map[string]any
	if err := json.Unmarshal(h.stdout.Bytes(), &v); err != nil {
		h.t.Fatalf("stdout is not JSON: %v\n%s", err, h.stdout.String())
	}
	return v
}

// readOnlyDir returns a directory nothing can be created in, for save errors.
func readOnlyDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	return dir
}
