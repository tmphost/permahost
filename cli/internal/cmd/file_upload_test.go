// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tmphost/tmphost/cli/internal/client"
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

func TestTTLMinutes(t *testing.T) {
	l := &client.Limits{DefaultTTLMinutes: 30, MaxTTLMinutes: 120}
	m, err := ttlMinutes(time.Hour, false, l)
	require.NoError(t, err)
	assert.Equal(t, 30, m)

	m, err = ttlMinutes(2*time.Hour, true, l)
	require.NoError(t, err)
	assert.Equal(t, 120, m)

	_, err = ttlMinutes(90*time.Second, true, l)
	assert.EqualError(t, err, "--ttl 1m30s is not a whole number of minutes")
	_, err = ttlMinutes(0, true, l)
	assert.EqualError(t, err, "--ttl must be between 1m and 120m")
	_, err = ttlMinutes(3*time.Hour, true, l)
	assert.EqualError(t, err, "--ttl must be between 1m and 120m")
}

func TestBillingBlocks(t *testing.T) {
	assert.Equal(t, 1, billingBlocks(30, 30))
	assert.Equal(t, 2, billingBlocks(31, 30))
	assert.Equal(t, 1, billingBlocks(-5, 30), "expired folders still cost one block")
	assert.Equal(t, 7, billingBlocks(7, 0), "a zero block size counts minutes")
}

func TestBillingUnits(t *testing.T) {
	const mb = 1 << 20
	assert.Equal(t, 1, billingUnits(mb, mb))
	assert.Equal(t, 2, billingUnits(mb+mb/100, mb), "1.01 MB")
	assert.Equal(t, 3, billingUnits(2*mb+mb/100, mb), "2.01 MB")
	assert.Equal(t, 1, billingUnits(0, mb), "empty files still cost one unit")
	assert.Equal(t, 5, billingUnits(5, 0), "a zero unit size counts bytes")
}

// fakeUploads serves the endpoints an upload touches and records the files.
type fakeUploads struct {
	ttl      int
	puts     map[string]string
	failPath string
}

func newUploadHarness(t *testing.T) (*harness, *fakeUploads) {
	h := newHarness(t)
	f := &fakeUploads{puts: map[string]string{}}
	h.reply("GET /api/v1/limits", 200, limitsJSON)
	h.reply("GET /api/v1/users/me", 200, meJSON)
	h.mux.HandleFunc("POST /api/v1/folders", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			TTLMinutes int `json:"ttlMinutes"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		f.ttl = in.TTLMinutes
		exp := now.Add(time.Duration(in.TTLMinutes) * time.Minute).Format(time.RFC3339)
		w.WriteHeader(201)
		fmt.Fprintf(w, `{"folder":{"id":"rs","expiresAt":%q,"url":"https://rs-u1.tmphost.test/"}}`, exp)
	})
	h.reply("GET /api/v1/folders/ab", 200, `{"folder":{"id":"ab","expiresAt":"2026-10-18T12:45:00Z","url":"https://ab-u1.tmphost.test/"},"files":[]}`)
	h.reply("GET /api/v1/folders/dd", 200, `{"folder":{"id":"dd","expiresAt":"2026-10-18T11:00:00Z","url":"https://dd-u1.tmphost.test/"},"files":[]}`)
	h.reply("GET /api/v1/folders/nope", 404, `{"error":"folder not found"}`)
	h.mux.HandleFunc("PUT /api/v1/folders/{id}/files/{path...}", func(w http.ResponseWriter, r *http.Request) {
		p := r.PathValue("path")
		if p == f.failPath {
			w.WriteHeader(402)
			_, _ = w.Write([]byte(`{"error":"not enough credits"}`))
			return
		}
		b, _ := io.ReadAll(r.Body)
		f.puts[r.PathValue("id")+"/"+p] = string(b)
		fmt.Fprintf(w, `{"file":{"path":%q,"size":%d,"url":"https://%s-u1.tmphost.test/%s"},"creditsSpent":1}`, p, len(b), r.PathValue("id"), p)
	})
	return h, f
}

func TestUpload_newFolder(t *testing.T) {
	h, f := newUploadHarness(t)
	root := t.TempDir()
	writeTree(t, root, map[string]string{"site/index.html": "<h1>", "site/my page.html": "x"})

	assert.Equal(t, 0, h.run("file", "upload", filepath.Join(root, "site")), h.stderr.String())
	assert.Equal(t, 30, f.ttl, "server default TTL")
	assert.Equal(t, map[string]string{"rs/site/index.html": "<h1>", "rs/site/my page.html": "x"}, f.puts)
	exp := now.Add(30 * time.Minute)
	assert.Equal(t, "Uploading 2 files (cost: 2 credits)\n"+
		"https://rs-u1.tmphost.test/site/index.html\nhttps://rs-u1.tmphost.test/site/my page.html\n\n"+
		"id:      rs\nurl:     https://rs-u1.tmphost.test/\nexpires: "+formatTime(exp)+" (in 30m)\n", h.stdout.String())

	assert.Equal(t, 0, h.run("file", "upload", filepath.Join(root, "site")+"/", "--path", "/www/", "--json"), h.stderr.String())
	assert.Equal(t, map[string]any{
		"id": "rs", "url": "https://rs-u1.tmphost.test/", "expiresAt": "2026-10-18T12:30:00Z", "creditsSpent": float64(2),
		"files": []any{
			map[string]any{"fileName": "index.html", "path": "www", "size": float64(4), "url": "https://rs-u1.tmphost.test/www/index.html"},
			map[string]any{"fileName": "my page.html", "path": "www", "size": float64(1), "url": "https://rs-u1.tmphost.test/www/my page.html"},
		},
	}, h.decode())
}

func TestUpload_singleFileJSON(t *testing.T) {
	h, f := newUploadHarness(t)
	root := t.TempDir()
	writeTree(t, root, map[string]string{"a.txt": "abc"})

	assert.Equal(t, 0, h.run("file", "upload", filepath.Join(root, "a.txt"), "--ttl", "2h", "--path", "logs/x", "--name", "today.txt", "--json"), h.stderr.String())
	assert.Equal(t, 120, f.ttl)
	assert.Equal(t, map[string]string{"rs/logs/x/today.txt": "abc"}, f.puts)
	v := h.decode()
	assert.Equal(t, "rs", v["id"])
	assert.Equal(t, "https://rs-u1.tmphost.test/logs/x/today.txt", v["url"])
	assert.Equal(t, "today.txt", v["fileName"])
	assert.Equal(t, "logs/x", v["path"])
	assert.Equal(t, float64(3), v["size"])
	assert.Equal(t, float64(1), v["creditsSpent"])
	assert.Equal(t, "2026-10-18T14:00:00Z", v["expiresAt"])
	assert.Len(t, v["files"], 1)
}

func TestUpload_existingFolder(t *testing.T) {
	h, f := newUploadHarness(t)
	root := t.TempDir()
	writeTree(t, root, map[string]string{"a.txt": "a"})

	// 45 minutes left spans two 30 minute blocks.
	assert.Equal(t, 0, h.run("file", "upload", filepath.Join(root, "a.txt"), "--id", "ab"), h.stderr.String())
	assert.Contains(t, h.stdout.String(), "Uploading 1 files (cost: 2 credits)\n")
	assert.Contains(t, h.stdout.String(), "id:      ab\n")
	assert.Equal(t, map[string]string{"ab/a.txt": "a"}, f.puts)

	assert.Equal(t, 0, h.run("file", "upload", filepath.Join(root, "a.txt"), "--id", "dd"), h.stderr.String())
	assert.Contains(t, h.stdout.String(), "(cost: 1 credits)")
	assert.Contains(t, h.stdout.String(), "(in expired)")

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
		{[]string{a, "--ttl", "1h", "--id", "ab"}, "[id ttl] were all set"},
		{[]string{a, "--ttl", "90s"}, "not a whole number of minutes"},
		{[]string{a, "--ttl", "8000h"}, "--ttl must be between 1m and 432000m"},
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

	// 30 days is 1440 blocks of 30 minutes, far beyond 40 credits.
	assert.Equal(t, 1, h.run("file", "upload", a, "--ttl", "720h"))
	assert.Equal(t, "Uploading 1 files (cost: 1440 credits)\n", h.stdout.String())
	renew := formatTime(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	assert.Equal(t, "error: not enough credits: this upload costs 1440 but you have 40 (renews at "+renew+")\n", h.stderr.String())
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
	assert.Contains(t, h.stderr.String(), "tmphost login")

	h = newHarness(t)
	h.reply("GET /api/v1/limits", 200, limitsJSON)
	h.reply("GET /api/v1/users/me", 200, meJSON)
	h.reply("POST /api/v1/folders", 400, `{"error":"bad ttl"}`)
	assert.Equal(t, 1, h.run("file", "upload", a))
	assert.Equal(t, "error: bad ttl\n", h.stderr.String())
}

func TestUpload_partialFailure(t *testing.T) {
	h, f := newUploadHarness(t)
	f.failPath = "site/b.txt"
	root := t.TempDir()
	writeTree(t, root, map[string]string{"site/a.txt": "a", "site/b.txt": "b", "site/c.txt": "c"})

	assert.Equal(t, 1, h.run("file", "upload", filepath.Join(root, "site")))
	assert.Equal(t, "Uploading 3 files (cost: 3 credits)\nhttps://rs-u1.tmphost.test/site/a.txt\n"+
		"Uploaded 1 of 3 files to folder rs before the error.\n", h.stdout.String())
	assert.Equal(t, "error: upload site/b.txt: not enough credits\n", h.stderr.String())
	assert.Equal(t, map[string]string{"rs/site/a.txt": "a"}, f.puts)

	assert.Equal(t, 1, h.run("file", "upload", filepath.Join(root, "site"), "--json"))
	v := h.decode()
	assert.Len(t, v["files"], 1)
	assert.Equal(t, "rs", v["id"])
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
