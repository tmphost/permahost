// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package cmd

import (
	"encoding/base64"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newDownloadHarness(t *testing.T) *harness {
	h := newHarness(t)
	h.mux.HandleFunc("GET /api/v1/folders/rs/files/site/a.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("hello"))
	})
	// Promises 100 bytes but sends 2, so the copy fails midway.
	h.mux.HandleFunc("GET /api/v1/folders/rs/files/short.bin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte("hi"))
	})
	h.mux.HandleFunc("GET /api/v1/folders/rs/files/bin.dat", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte{0xff, 0xfe})
	})
	h.mux.HandleFunc("GET /api/v1/folders/rs/files/empty.txt", func(w http.ResponseWriter, r *http.Request) {})
	h.reply("GET /api/v1/folders/rs/files/nope.txt", 404, `{"error":"file not found"}`)
	t.Chdir(t.TempDir())
	return h
}

func TestFileDownload_toFile(t *testing.T) {
	h := newDownloadHarness(t)

	assert.Equal(t, 0, h.run("file", "download", "--id", "rs", "--path", "site", "a.txt"), h.stderr.String())
	assert.Equal(t, "Downloaded site/a.txt (5 B) to a.txt\n", h.stdout.String())
	b, err := os.ReadFile("a.txt")
	require.NoError(t, err)
	assert.Equal(t, "hello", string(b))

	assert.Equal(t, 1, h.run("file", "download", "--id", "rs", "--path", "site", "a.txt"))
	assert.Equal(t, "error: a.txt already exists: use --force to overwrite it\n", h.stderr.String())

	require.NoError(t, os.WriteFile("a.txt", []byte("old content, longer"), 0o644))
	assert.Equal(t, 0, h.run("file", "download", "--id", "rs", "--path", "site", "a.txt", "--force"))
	b, err = os.ReadFile("a.txt")
	require.NoError(t, err)
	assert.Equal(t, "hello", string(b), "overwritten and truncated")

	assert.Equal(t, 0, h.run("file", "download", "--id", "rs", "--path", "site", "a.txt", "-o", "out.txt", "--json"))
	assert.Equal(t, map[string]any{"id": "rs", "fileName": "a.txt", "path": "site", "size": float64(5)}, h.decode())
	_, err = os.Stat("out.txt")
	assert.NoError(t, err)
}

func TestFileDownload_stdout(t *testing.T) {
	h := newDownloadHarness(t)

	assert.Equal(t, 0, h.run("file", "download", "--id", "rs", "--path", "site", "a.txt", "-o", "-"))
	assert.Equal(t, "hello", h.stdout.String())

	assert.Equal(t, 0, h.run("file", "download", "--id", "rs", "--path", "site", "a.txt", "-o", "-", "--json"))
	assert.Equal(t, map[string]any{
		"id": "rs", "fileName": "a.txt", "path": "site", "size": float64(5),
		"base64Content": base64.StdEncoding.EncodeToString([]byte("hello")),
	}, h.decode())

	assert.Equal(t, 0, h.run("file", "download", "--id", "rs", "--path", "site", "a.txt", "-o", "-", "--json", "--format", "textContent"))
	assert.Equal(t, map[string]any{"id": "rs", "fileName": "a.txt", "path": "site", "size": float64(5), "textContent": "hello"}, h.decode())

	assert.Equal(t, 0, h.run("file", "download", "--id", "rs", "empty.txt", "-o", "-", "--json", "--format", "textContent"))
	assert.Equal(t, map[string]any{"id": "rs", "fileName": "empty.txt", "textContent": ""}, h.decode(), "an empty file still carries its content field")

	assert.Equal(t, 1, h.run("file", "download", "--id", "rs", "bin.dat", "-o", "-", "--json", "--format", "textContent"))
	assert.Equal(t, "error: bin.dat is not UTF-8 text: use --format base64Content\n", h.stderr.String())

	assert.Equal(t, 1, h.run("file", "download", "--id", "rs", "short.bin", "-o", "-", "--json"))
	assert.Contains(t, h.stderr.String(), "unexpected EOF")
}

func TestFileDownload_errors(t *testing.T) {
	h := newDownloadHarness(t)

	assert.Equal(t, 1, h.run("file", "download", "--id", "rs", "nope.txt"))
	assert.Equal(t, "error: file not found\n", h.stderr.String())
	_, err := os.Stat("nope.txt")
	assert.True(t, os.IsNotExist(err), "nothing is created for a failed request")

	assert.Equal(t, 1, h.run("file", "download", "--id", "rs", "short.bin"))
	assert.Contains(t, h.stderr.String(), "error: download short.bin: unexpected EOF")
	_, err = os.Stat("short.bin")
	assert.True(t, os.IsNotExist(err), "a truncated download is removed")

	assert.Equal(t, 1, h.run("file", "download", "--id", "rs", "--path", "site", "a.txt", "-o", filepath.Join("missing", "dir", "a.txt")))
	assert.Contains(t, h.stderr.String(), "no such file or directory")

	assert.Equal(t, 1, h.run("file", "download", "--id", "rs", "a/b.txt"))
	assert.Contains(t, h.stderr.String(), "must not contain /")

	assert.Equal(t, 1, h.run("file", "download", "--id", "rs", ".upload-x"))
	assert.Contains(t, h.stderr.String(), "reserved")

	assert.Equal(t, 1, h.run("file", "download", "--id", "rs"))
	assert.Contains(t, h.stderr.String(), "accepts 1 arg")

	assert.Equal(t, 1, h.run("file", "download", "--id", "rs", "a.txt", "--format", "hex"))
	assert.Equal(t, "error: --format must be base64Content or textContent\n", h.stderr.String())
}
