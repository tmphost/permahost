// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const _index = `<!doctype html><html><head><link rel="icon" href="./favicon.svg"><script type="module" src="./assets/index-abc.js"></script></head><body></body></html>`

func spaApp(web fstest.MapFS, basePath string) *fiber.App {
	f := fiber.New()
	f.Get("/*", spaHandler(web, basePath))
	return f
}

func fetch(t *testing.T, f *fiber.App, target string) (*http.Response, string) {
	t.Helper()
	res, err := f.Test(httptest.NewRequest(http.MethodGet, target, nil))
	require.NoError(t, err)
	b, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return res, string(b)
}

func TestSPAHandler(t *testing.T) {
	web := fstest.MapFS{
		"index.html":          {Data: []byte(_index)},
		"favicon.svg":         {Data: []byte("<svg/>")},
		"assets/index-abc.js": {Data: []byte("console.log(1)")},
	}
	f := spaApp(web, "/base")

	t.Run("index for deep links", func(t *testing.T) {
		for _, target := range []string{"/", "/index.html", "/login", "/settings/profile"} {
			res, body := fetch(t, f, target)
			assert.Equal(t, http.StatusOK, res.StatusCode, target)
			assert.Equal(t, "no-cache", res.Header.Get(fiber.HeaderCacheControl))
			assert.Contains(t, res.Header.Get(fiber.HeaderContentType), "text/html")
			assert.Contains(t, body, `<head><script>window.__TMPHOST_BASE_PATH__="/base";</script>`)
			assert.Contains(t, body, `src="/base/assets/index-abc.js"`)
			assert.Contains(t, body, `href="/base/favicon.svg"`)
		}
	})

	t.Run("static file", func(t *testing.T) {
		res, body := fetch(t, f, "/favicon.svg")
		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.Equal(t, "<svg/>", body)
		assert.Contains(t, res.Header.Get(fiber.HeaderContentType), "image/svg+xml")
		assert.Empty(t, res.Header.Get(fiber.HeaderCacheControl))
	})

	t.Run("fingerprinted asset", func(t *testing.T) {
		res, _ := fetch(t, f, "/assets/index-abc.js")
		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.Contains(t, res.Header.Get(fiber.HeaderCacheControl), "immutable")
	})

	t.Run("missing file", func(t *testing.T) {
		res, _ := fetch(t, f, "/assets/missing.js")
		assert.Equal(t, http.StatusNotFound, res.StatusCode)
	})

	t.Run("private files are never served", func(t *testing.T) {
		private := fstest.MapFS{
			"index.html":         {Data: []byte(_index)},
			"settings.json":      {Data: []byte(`{"secret":"x"}`)},
			".env":               {Data: []byte("SECRET=x")},
			"config.yaml":        {Data: []byte("secret: x")},
			".hidden/app.js":     {Data: []byte("secret")},
			"assets/.map/app.js": {Data: []byte("secret")},
			".hidden/notes":      {Data: []byte("secret")},
		}
		pf := spaApp(private, "")
		for _, target := range []string{"/settings.json", "/.env", "/config.yaml", "/.hidden/app.js", "/assets/.map/app.js", "/SETTINGS.JSON"} {
			res, body := fetch(t, pf, target)
			assert.Equal(t, http.StatusNotFound, res.StatusCode, target)
			assert.NotContains(t, body, "secret", target)
		}
		// An extensionless path is a client-side route, so it gets index.html.
		res, body := fetch(t, pf, "/.hidden/notes")
		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.NotContains(t, body, "secret")
	})

	t.Run("unknown api route", func(t *testing.T) {
		res, _ := fetch(t, f, "/api/v1/nope")
		assert.Equal(t, http.StatusNotFound, res.StatusCode)
	})
}

func TestSPAHandler_NoBuild(t *testing.T) {
	res, _ := fetch(t, spaApp(fstest.MapFS{}, ""), "/")
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
}

func TestInjectBasePath_EscapesValue(t *testing.T) {
	got := string(injectBasePath([]byte("<head></head>"), "</script>"))
	assert.NotContains(t, got, `="</script>"`)
	assert.Contains(t, got, `</script>`)
}
