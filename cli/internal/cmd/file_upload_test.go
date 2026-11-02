// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package cmd

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmphost/permahost/cli/internal/client"
)

// writeTree creates files (slash paths relative to root) with their contents.
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, body := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
	}
}

func dsts(files []localFile) []string {
	out := make([]string, len(files))
	for i, f := range files {
		out[i] = f.dst
	}
	sort.Strings(out)
	return out
}

func TestCollect(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"site/index.html":  "<h1>",
		"site/css/app.css": "body{}",
		"notes.txt":        "hi",
	})
	site := filepath.Join(root, "site")
	require.NoError(t, os.Symlink(filepath.Join(root, "notes.txt"), filepath.Join(site, "link.txt")))
	require.NoError(t, os.Symlink(filepath.Join(root, "missing"), filepath.Join(site, "dangling")))

	files, err := collect([]string{site, filepath.Join(root, "notes.txt")})
	require.NoError(t, err)
	assert.Equal(t, []string{"notes.txt", "site/css/app.css", "site/index.html", "site/link.txt"}, dsts(files))
	for _, f := range files {
		if f.dst == "site/css/app.css" {
			assert.Equal(t, filepath.Join(site, "css", "app.css"), f.src)
			assert.Equal(t, int64(6), f.size)
		}
	}

	files, err = collect([]string{site + "/"})
	require.NoError(t, err)
	assert.Equal(t, []string{"css/app.css", "index.html", "link.txt"}, dsts(files))

	t.Chdir(site)
	files, err = collect([]string{"."})
	require.NoError(t, err)
	assert.Equal(t, []string{"css/app.css", "index.html", "link.txt"}, dsts(files))

	t.Chdir(filepath.Join(site, "css"))
	files, err = collect([]string{".."})
	require.NoError(t, err)
	assert.Equal(t, []string{"css/app.css", "index.html", "link.txt"}, dsts(files))
}

func TestCollect_errors(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"a/x.txt": "1", "b/x.txt": "2", "x.txt": "3"})
	require.NoError(t, os.Mkdir(filepath.Join(root, "empty"), 0o755))

	_, err := collect([]string{filepath.Join(root, "a") + "/", filepath.Join(root, "b") + "/"})
	assert.EqualError(t, err, `two files would upload to "x.txt"`)

	_, err = collect([]string{filepath.Join(root, "x.txt"), filepath.Join(root, "a") + "/"})
	assert.EqualError(t, err, `two files would upload to "x.txt"`)

	_, err = collect([]string{filepath.Join(root, "nope")})
	assert.Error(t, err)

	_, err = collect([]string{os.DevNull})
	assert.EqualError(t, err, os.DevNull+" is not a regular file or directory")

	_, err = collect([]string{filepath.Join(root, "empty")})
	assert.EqualError(t, err, "no files to upload")

	locked := filepath.Join(root, "locked")
	writeTree(t, root, map[string]string{"locked/sub/y.txt": "y"})
	require.NoError(t, os.Chmod(filepath.Join(locked, "sub"), 0o000))
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(locked, "sub"), 0o755) })
	_, err = collect([]string{locked})
	assert.ErrorIs(t, err, os.ErrPermission)
}

func TestMaxCost(t *testing.T) {
	u := &client.Usage{StorageBytes: 6, StorageBlocks: 2, Pricing: client.Pricing{StorageBlockBytes: 4, StorageCredits: 5, FolderCredits: 10}}
	assert.Equal(t, 10, maxCost(true, 2, u), "fits in the started block")
	assert.Equal(t, 0, maxCost(false, 2, u))
	assert.Equal(t, 5, maxCost(false, 3, u), "starts a third block")
	assert.Equal(t, 20, maxCost(true, 7, u))

	u.StorageBlocks = 4
	assert.Equal(t, 0, maxCost(false, 1, u), "usage reports more blocks than stored")
}

func TestStorageBlocks(t *testing.T) {
	const mb = 1 << 20
	assert.Equal(t, 0, storageBlocks(0, 25*mb))
	assert.Equal(t, 1, storageBlocks(1, 25*mb))
	assert.Equal(t, 1, storageBlocks(25*mb, 25*mb))
	assert.Equal(t, 2, storageBlocks(25*mb+1, 25*mb))
	assert.Equal(t, 5, storageBlocks(5, 0), "a zero block size counts bytes")
}

// fakeUploads serves the endpoints an upload touches and records the files.
type fakeUploads struct {
	usage    string
	created  int
	puts     map[string]string
	failPath string
}

func newUploadHarness(t *testing.T) (*harness, *fakeUploads) {
	h := newHarness(t)
	f := &fakeUploads{usage: usageJSON, puts: map[string]string{}}
	h.reply("GET /api/v1/limits", 200, limitsJSON)
	h.reply("GET /api/v1/users/me", 200, meJSON)
	h.mux.HandleFunc("GET /api/v1/users/me/usage", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, f.usage)
	})
	h.mux.HandleFunc("POST /api/v1/folders", func(w http.ResponseWriter, _ *http.Request) {
		f.created++
		w.WriteHeader(201)
		_, _ = io.WriteString(w, `{"folder":{"id":"rs","url":"https://rs-u1.permahost.test/"},"creditsSpent":10}`)
	})
	h.reply("GET /api/v1/folders/ab", 200, `{"folder":{"id":"ab","url":"https://ab-u1.permahost.test/"},"files":[]}`)
	h.reply("GET /api/v1/folders/nope", 404, `{"error":"folder not found"}`)
	h.mux.HandleFunc("PUT /api/v1/folders/{id}/files/{path...}", func(w http.ResponseWriter, r *http.Request) {
		p := r.PathValue("path")
		if p == f.failPath {
			w.WriteHeader(402)
			_, _ = w.Write([]byte(`{"error":"insufficient credits"}`))
			return
		}
		b, _ := io.ReadAll(r.Body)
		f.puts[r.PathValue("id")+"/"+p] = string(b)
		fmt.Fprintf(w, `{"file":{"path":%q,"size":%d,"url":"https://%s-u1.permahost.test/%s"},"creditsSpent":5}`, p, len(b), r.PathValue("id"), p)
	})
	return h, f
}

func TestUpload_newFolder(t *testing.T) {
	h, f := newUploadHarness(t)
	root := t.TempDir()
	writeTree(t, root, map[string]string{"site/index.html": "<h1>", "site/my page.html": "x"})

	// 6 stored + 5 new bytes start a third 4 byte block: 10 + 5 credits.
	assert.Equal(t, 0, h.run("file", "upload", filepath.Join(root, "site")), h.stderr.String())
	assert.Equal(t, 1, f.created)
	assert.Equal(t, map[string]string{"rs/site/index.html": "<h1>", "rs/site/my page.html": "x"}, f.puts)
	assert.Equal(t, "Uploading 2 files (5 B, at most 15 credits; you have 40)\n"+
		"https://rs-u1.permahost.test/site/index.html\nhttps://rs-u1.permahost.test/site/my page.html\n\n"+
		"id:      rs\nurl:     https://rs-u1.permahost.test/\ncredits: 20 spent\n", h.stdout.String())

	assert.Equal(t, 0, h.run("file", "upload", filepath.Join(root, "site")+"/", "--path", "/www/", "--json"), h.stderr.String())
	assert.Equal(t, map[string]any{
		"id": "rs", "url": "https://rs-u1.permahost.test/", "creditsSpent": float64(20),
		"files": []any{
			map[string]any{"fileName": "index.html", "path": "www", "size": float64(4), "url": "https://rs-u1.permahost.test/www/index.html"},
			map[string]any{"fileName": "my page.html", "path": "www", "size": float64(1), "url": "https://rs-u1.permahost.test/www/my page.html"},
		},
	}, h.decode())
}

func TestUpload_singleFileJSON(t *testing.T) {
	h, f := newUploadHarness(t)
	root := t.TempDir()
	writeTree(t, root, map[string]string{"a.txt": "abc"})

	assert.Equal(t, 0, h.run("file", "upload", filepath.Join(root, "a.txt"), "--path", "logs/x", "--name", "today.txt", "--json"), h.stderr.String())
	assert.Equal(t, map[string]string{"rs/logs/x/today.txt": "abc"}, f.puts)
	assert.Equal(t, map[string]any{
		"id": "rs", "url": "https://rs-u1.permahost.test/logs/x/today.txt", "fileName": "today.txt", "path": "logs/x", "size": float64(3),
		"creditsSpent": float64(15),
		"files":        []any{map[string]any{"fileName": "today.txt", "path": "logs/x", "size": float64(3), "url": "https://rs-u1.permahost.test/logs/x/today.txt"}},
	}, h.decode())
}

func TestUpload_existingFolder(t *testing.T) {
	h, f := newUploadHarness(t)
	root := t.TempDir()
	writeTree(t, root, map[string]string{"a.txt": "a"})

	// One byte fits in the started block and no folder is created.
	assert.Equal(t, 0, h.run("file", "upload", filepath.Join(root, "a.txt"), "--id", "ab"), h.stderr.String())
	assert.Equal(t, "Uploading 1 files (1 B, at most 0 credits; you have 40)\nhttps://ab-u1.permahost.test/a.txt\n\n"+
		"id:      ab\nurl:     https://ab-u1.permahost.test/\ncredits: 5 spent\n", h.stdout.String())
	assert.Equal(t, map[string]string{"ab/a.txt": "a"}, f.puts)
	assert.Zero(t, f.created)

	assert.Equal(t, 1, h.run("file", "upload", filepath.Join(root, "a.txt"), "--id", "nope"))
	assert.Equal(t, "error: folder not found\n", h.stderr.String())
}

func TestUpload_validation(t *testing.T) {
	h, _ := newUploadHarness(t)
	root := t.TempDir()
	writeTree(t, root, map[string]string{"a.txt": "a", "b.txt": "b", "dir/c.txt": "c", "big.bin": strings.Repeat("x", 17)})
	a := filepath.Join(root, "a.txt")

	for _, tc := range []struct {
		args []string
		err  string
	}{
		{[]string{a, "--ttl", "1h"}, "unknown flag: --ttl"},
		{[]string{filepath.Join(root, "nope")}, "no such file"},
		{[]string{filepath.Join(root, "big.bin")}, "is 17 B, over the 16 B upload limit"},
		{[]string{a, "--name", "x/y.txt"}, `--name "x/y.txt" must not contain /`},
		{[]string{a, filepath.Join(root, "b.txt"), "--name", "y.txt"}, "--name renames a single file"},
		{[]string{filepath.Join(root, "dir"), "--name", "y.txt"}, "--name renames a single file"},
		{[]string{}, "requires at least 1 arg"},
		{[]string{a, "--path", "a/b/c/d/e/f"}, "more than 5 directories deep"},
		{[]string{a, "--path", "../x"}, `has an empty, "." or ".." segment`},
	} {
		assert.Equal(t, 1, h.run(append([]string{"file", "upload"}, tc.args...)...), tc.args)
		assert.Contains(t, h.stderr.String(), tc.err, tc.args)
	}
}

func TestUpload_lowCredits(t *testing.T) {
	h, f := newUploadHarness(t)
	f.usage = `{"usage":{"pricing":{"storageBlockBytes":1,"storageCredits":20,"folderCredits":10}}}`
	f.failPath = "b.txt"
	root := t.TempDir()
	writeTree(t, root, map[string]string{"a.txt": "a", "b.txt": "b"})

	// The estimate is only a bound, so the server decides where to stop.
	assert.Equal(t, 1, h.run("file", "upload", filepath.Join(root, "a.txt"), filepath.Join(root, "b.txt")))
	assert.Equal(t, "Uploading 2 files (2 B, at most 50 credits; you have 40)\n"+
		"Your credits may not cover this upload: it stops at the first file they cannot pay for.\n"+
		"https://rs-u1.permahost.test/a.txt\n"+
		"Uploaded 1 of 2 files to folder rs for 15 credits before the error.\n", h.stdout.String())
	assert.Equal(t, "error: upload b.txt: insufficient credits\n", h.stderr.String())
}

func TestUpload_apiErrors(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"a.txt": "a"})
	a := filepath.Join(root, "a.txt")

	h := newHarness(t)
	h.reply("GET /api/v1/limits", 500, `{"error":"limits down"}`)
	assert.Equal(t, 1, h.run("file", "upload", a))
	assert.Equal(t, "error: limits down\n", h.stderr.String())

	h = newHarness(t)
	h.reply("GET /api/v1/limits", 200, limitsJSON)
	h.reply("GET /api/v1/users/me", 401, `{"error":"invalid token"}`)
	assert.Equal(t, 1, h.run("file", "upload", a))
	assert.Contains(t, h.stderr.String(), "permahost login")

	h = newHarness(t)
	h.reply("GET /api/v1/limits", 200, limitsJSON)
	h.reply("GET /api/v1/users/me", 200, meJSON)
	h.reply("GET /api/v1/users/me/usage", 500, `{"error":"usage down"}`)
	assert.Equal(t, 1, h.run("file", "upload", a))
	assert.Equal(t, "error: usage down\n", h.stderr.String())

	h = newHarness(t)
	h.reply("GET /api/v1/limits", 200, limitsJSON)
	h.reply("GET /api/v1/users/me", 200, meJSON)
	h.reply("GET /api/v1/users/me/usage", 200, usageJSON)
	h.reply("POST /api/v1/folders", 402, `{"error":"insufficient credits: 10 needed"}`)
	assert.Equal(t, 1, h.run("file", "upload", a))
	assert.Equal(t, "error: insufficient credits: 10 needed\n", h.stderr.String())
}

func TestUpload_partialFailure(t *testing.T) {
	h, f := newUploadHarness(t)
	f.failPath = "site/b.txt"
	root := t.TempDir()
	writeTree(t, root, map[string]string{"site/a.txt": "a", "site/b.txt": "b", "site/c.txt": "c"})

	assert.Equal(t, 1, h.run("file", "upload", filepath.Join(root, "site")))
	assert.Equal(t, "Uploading 3 files (3 B, at most 15 credits; you have 40)\nhttps://rs-u1.permahost.test/site/a.txt\n"+
		"Uploaded 1 of 3 files to folder rs for 15 credits before the error.\n", h.stdout.String())
	assert.Equal(t, "error: upload site/b.txt: insufficient credits\n", h.stderr.String())
	assert.Equal(t, map[string]string{"rs/site/a.txt": "a"}, f.puts)

	assert.Equal(t, 1, h.run("file", "upload", filepath.Join(root, "site"), "--json"))
	v := h.decode()
	assert.Len(t, v["files"], 1)
	assert.Equal(t, "rs", v["id"])
	assert.Equal(t, float64(15), v["creditsSpent"])
}

func TestUpload_unreadableFile(t *testing.T) {
	h, f := newUploadHarness(t)
	root := t.TempDir()
	writeTree(t, root, map[string]string{"a.txt": "a"})
	require.NoError(t, os.Chmod(filepath.Join(root, "a.txt"), 0o000))

	assert.Equal(t, 1, h.run("file", "upload", filepath.Join(root, "a.txt")))
	assert.Contains(t, h.stderr.String(), "error: upload a.txt: open ")
	assert.Empty(t, f.puts)
}
