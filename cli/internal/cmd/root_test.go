// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/tmphost/permahost/cli/internal/config"
)

func TestExecute_otherErrorsHaveNoHint(t *testing.T) {
	h := newHarness(t)
	h.reply("GET /api/v1/users/me", 500, `{"error":"boom"}`)
	assert.Equal(t, 1, h.run("whoami"))
	assert.Equal(t, "error: boom\n", h.stderr.String())
}

func TestExecute_helpAndVersion(t *testing.T) {
	h := newHarness(t)
	assert.Equal(t, 0, h.run("--help"))
	assert.Contains(t, h.stdout.String(), "permahost login")
	assert.Equal(t, 0, h.run("--version"))
	assert.Equal(t, "permahost version v1.2.3\n", h.stdout.String())
}

func TestHumanSize(t *testing.T) {
	for n, want := range map[int64]string{
		0:                "0 B",
		1023:             "1023 B",
		1536:             "1.5 KiB",
		16 * 1024 * 1024: "16.0 MiB",
		3 << 30:          "3.0 GiB",
	} {
		assert.Equal(t, want, humanSize(n), n)
	}
}

func TestServerURL(t *testing.T) {
	a := &app{opts: Options{Getenv: func(string) string { return "" }}}
	assert.Equal(t, "https://permahost.io", a.serverURL(config.Config{}))
	assert.Equal(t, "https://saved.example", a.serverURL(config.Config{Server: "https://saved.example"}))
	a.opts.Getenv = func(string) string { return "https://env.example/" }
	assert.Equal(t, "https://env.example", a.serverURL(config.Config{Server: "https://saved.example"}))
	a.server = "https://flag.example"
	assert.Equal(t, "https://flag.example", a.serverURL(config.Config{Server: "https://saved.example"}))
}
