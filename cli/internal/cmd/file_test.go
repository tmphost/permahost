// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package cmd

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// folderJSON uses the subdomain URL style; the CLI must use it as given.
const folderJSON = `{"folder":{"id":"rs","expiresAt":"2026-10-18T12:30:00Z","url":"https://rs-u1.tmphost.test/"},` +
	`"files":[{"path":"site/css/app.css","size":2048,"url":"https://rs-u1.tmphost.test/site/css/app.css"},{"path":"a.txt","size":3,"url":"https://rs-u1.tmphost.test/a.txt"}]}`

func TestValidPath(t *testing.T) {
	for _, p := range []string{"a.txt", "site/index.html", "a/b/c/d/e/f.txt", "ünï cödé.txt", strings.Repeat("x", 255)} {
		assert.NoError(t, validPath(p), p)
	}
	for p, want := range map[string]string{
		"":                              "path is empty",
		strings.Repeat("a/", 257):       "longer than 512 bytes",
		"bad\xff":                       "not valid UTF-8",
		`a\b`:                           "backslash or control character",
		"a\tb":                          "backslash or control character",
		"a\x7fb":                        "backslash or control character",
		"a/b/c/d/e/f/g.txt":             "more than 5 directories deep",
		"a//b":                          `empty, "." or ".." segment`,
		"./a":                           `empty, "." or ".." segment`,
		"a/../b":                        `empty, "." or ".." segment`,
		strings.Repeat("x", 256):        "segment longer than 255 bytes",
		"site/.upload-123/a":            `starting with ".upload-"`,
		"/abs":                          `empty, "." or ".." segment`,
		"trailing/":                     `empty, "." or ".." segment`,
		"a/" + strings.Repeat("x", 256): "segment longer than 255 bytes",
	} {
		assert.ErrorContains(t, validPath(p), want, p)
	}
}

func TestSplitAndJoinPath(t *testing.T) {
	dir, name := splitPath("site/css/app.css")
	assert.Equal(t, []string{"site/css", "app.css"}, []string{dir, name})
	dir, name = splitPath("a.txt")
	assert.Equal(t, []string{"", "a.txt"}, []string{dir, name})
	assert.Equal(t, "a.txt", joinPath("/", "a.txt"))
	assert.Equal(t, "site/a.txt", joinPath("/site/", "a.txt"))
}

func TestFileGet(t *testing.T) {
	h := newHarness(t)
	h.reply("GET /api/v1/folders/rs", 200, folderJSON)
	h.reply("GET /api/v1/folders/ab", 200, `{"folder":{"id":"ab","expiresAt":"2026-10-18T13:00:00Z","url":"https://h.test/files/u1/ab/"},"files":[]}`)
	h.reply("GET /api/v1/folders/zz", 404, `{"error":"folder not found"}`)

	assert.Equal(t, 0, h.run("file", "get", "--id", "rs"))
	assert.Equal(t, "FILE NAME  PATH      SIZE     URL\n"+
		"app.css    site/css  2.0 KiB  https://rs-u1.tmphost.test/site/css/app.css\n"+
		"a.txt                3 B      https://rs-u1.tmphost.test/a.txt\n\n"+
		"id:      rs\nurl:     https://rs-u1.tmphost.test/\nexpires: "+formatTime(now.Add(30*60e9))+" (in 30m)\n", h.stdout.String())

	assert.Equal(t, 0, h.run("file", "get", "--id", "rs", "--json"))
	assert.Equal(t, map[string]any{
		"id": "rs", "url": "https://rs-u1.tmphost.test/", "expiresAt": "2026-10-18T12:30:00Z",
		"files": []any{
			map[string]any{"fileName": "app.css", "path": "site/css", "size": float64(2048), "url": "https://rs-u1.tmphost.test/site/css/app.css"},
			map[string]any{"fileName": "a.txt", "path": "", "size": float64(3), "url": "https://rs-u1.tmphost.test/a.txt"},
		},
	}, h.decode())

	assert.Equal(t, 0, h.run("file", "get", "--id", "ab"))
	assert.Contains(t, h.stdout.String(), "No files.\n\nid:      ab\nurl:     https://h.test/files/u1/ab/\n")

	assert.Equal(t, 1, h.run("file", "get", "--id", "zz"))
	assert.Equal(t, "error: folder not found\n", h.stderr.String())

	assert.Equal(t, 1, h.run("file", "get"))
	assert.Contains(t, h.stderr.String(), `required flag(s) "id" not set`)
}

func TestFileURL(t *testing.T) {
	h := newHarness(t)
	h.reply("GET /api/v1/folders/rs", 200, folderJSON)
	h.reply("GET /api/v1/folders/ab", 200, `{"folder":{"id":"ab","expiresAt":"2026-10-18T13:00:00Z","url":"https://h.test/files/u1/ab/"},"files":[]}`)
	h.reply("GET /api/v1/folders/zz", 404, `{"error":"folder not found"}`)

	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--id", "rs"}, "https://rs-u1.tmphost.test/"},
		{[]string{"--id", "rs", "a.txt"}, "https://rs-u1.tmphost.test/a.txt"},
		{[]string{"--id", "rs", "--path", "/site/css/"}, "https://rs-u1.tmphost.test/site/css/"},
		{[]string{"--id", "rs", "--path", "my site", "a#b?.html"}, "https://rs-u1.tmphost.test/my%20site/a%23b%3F.html"},
		{[]string{"--id", "ab", "--path", "x", "y.txt"}, "https://h.test/files/u1/ab/x/y.txt"},
	} {
		assert.Equal(t, 0, h.run(append([]string{"file", "url"}, tc.args...)...), tc.args)
		assert.Equal(t, tc.want+"\n", h.stdout.String(), tc.args)
	}

	assert.Equal(t, 0, h.run("file", "url", "--id", "rs", "--path", "site", "a.txt", "--json"))
	assert.Equal(t, map[string]any{
		"id": "rs", "url": "https://rs-u1.tmphost.test/site/a.txt", "expiresAt": "2026-10-18T12:30:00Z",
		"fileName": "a.txt", "path": "site",
	}, h.decode())

	for args, want := range map[string]string{
		"--id rs a/b.txt":       `file name "a/b.txt" must not contain /`,
		"--id rs --path a/../b": `empty, "." or ".." segment`,
		"--id zz":               "folder not found",
		"--id rs a b":           "accepts at most 1 arg",
	} {
		assert.Equal(t, 1, h.run(append([]string{"file", "url"}, strings.Fields(args)...)...), args)
		assert.Contains(t, h.stderr.String(), want, args)
	}
}

func TestFileLs(t *testing.T) {
	h := newHarness(t)
	h.reply("GET /api/v1/folders", 200, `{"folders":[{"id":"rs","expiresAt":"2026-10-18T14:00:00Z","url":"https://rs-u1.tmphost.test/"},{"id":"a1","expiresAt":"2026-10-18T12:10:00Z","url":"https://h.test/files/u1/a1/"}]}`)
	assert.Equal(t, 0, h.run("file", "ls"))
	assert.Equal(t, "ID  EXPIRES IN  URL\nrs  2h0m        https://rs-u1.tmphost.test/\na1  10m         https://h.test/files/u1/a1/\n", h.stdout.String())

	assert.Equal(t, 0, h.run("file", "ls", "--json"))
	assert.Len(t, h.decode()["folders"], 2)

	h = newHarness(t)
	h.reply("GET /api/v1/folders", 200, `{"folders":[]}`)
	assert.Equal(t, 0, h.run("file", "ls"))
	assert.Equal(t, "No folders.\n", h.stdout.String())

	h = newHarness(t)
	h.reply("GET /api/v1/folders", 500, `{"error":"db down"}`)
	assert.Equal(t, 1, h.run("file", "ls"))
	assert.Equal(t, "error: db down\n", h.stderr.String())
}

func TestFileRm(t *testing.T) {
	h := newHarness(t)
	h.reply("DELETE /api/v1/folders/rs", 204, ``)
	h.reply("DELETE /api/v1/folders/rs/files/site/index.html", 204, ``)
	h.reply("DELETE /api/v1/folders/zz", 404, `{"error":"folder not found"}`)
	h.reply("DELETE /api/v1/folders/rs/files/nope", 404, `{"error":"file not found"}`)

	assert.Equal(t, 0, h.run("file", "rm", "--id", "rs"))
	assert.Equal(t, "Deleted folder rs.\n", h.stdout.String())

	assert.Equal(t, 0, h.run("file", "rm", "--id", "rs", "--json"))
	assert.Equal(t, map[string]any{"deleted": true, "id": "rs"}, h.decode())

	assert.Equal(t, 0, h.run("file", "rm", "--id", "rs", "/site/index.html"))
	assert.Equal(t, "Deleted site/index.html from folder rs.\n", h.stdout.String())

	assert.Equal(t, 0, h.run("file", "rm", "--id", "rs", "site/index.html", "--json"))
	assert.Equal(t, map[string]any{"deleted": true, "id": "rs", "fileName": "index.html", "path": "site"}, h.decode())

	assert.Equal(t, 1, h.run("file", "rm", "--id", "zz"))
	assert.Equal(t, "error: folder not found\n", h.stderr.String())

	assert.Equal(t, 1, h.run("file", "rm", "--id", "rs", "nope"))
	assert.Equal(t, "error: file not found\n", h.stderr.String())

	assert.Equal(t, 1, h.run("file", "rm", "--id", "rs", "a/../b"))
	assert.Contains(t, h.stderr.String(), `".." segment`)
}
