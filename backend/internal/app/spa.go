// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"path"
	"strings"

	"github.com/gofiber/fiber/v3"

	"github.com/tmphost/tmphost/backend/internal/handler/api"
)

// _servableExts are the only file types served from the built frontend.
// Anything else (settings.json, .env, YAML, ...) that ends up in public/
// by mistake stays private.
var _servableExts = map[string]bool{
	".js": true, ".css": true, ".svg": true, ".png": true, ".jpg": true, ".webp": true,
	".ico": true, ".woff": true, ".woff2": true, ".txt": true, ".webmanifest": true,
}

// servable reports whether a built-frontend file may be sent to browsers:
// a known web asset type, with no hidden path segment.
func servable(name string) bool {
	for _, seg := range strings.Split(name, "/") {
		if strings.HasPrefix(seg, ".") {
			return false
		}
	}
	return _servableExts[strings.ToLower(path.Ext(name))]
}

// spaHandler serves the built frontend: allowed files as-is, and index.html
// for every other path so the client-side router can take over.
func spaHandler(web fs.FS, basePath string) fiber.Handler {
	index, err := fs.ReadFile(web, "index.html")
	if err == nil {
		index = injectBasePath(index, basePath)
	}
	return func(c fiber.Ctx) error {
		if strings.HasPrefix(c.Path(), api.RoutePrefix+"/") {
			return fiber.ErrNotFound
		}
		name := strings.TrimPrefix(path.Clean(c.Path()), "/")
		if name != "" && name != "index.html" {
			if !servable(name) {
				// A file we won't serve is a 404; an extensionless path is
				// a client-side route.
				if path.Ext(name) != "" {
					return fiber.ErrNotFound
				}
			} else if b, err := fs.ReadFile(web, name); err == nil {
				if strings.HasPrefix(name, "assets/") {
					// Vite fingerprints everything under assets/.
					c.Set(fiber.HeaderCacheControl, "public, max-age=31536000, immutable")
				}
				c.Type(strings.TrimPrefix(path.Ext(name), "."))
				return c.Send(b)
			}
			if path.Ext(name) != "" {
				return fiber.ErrNotFound
			}
		}
		if index == nil {
			return fiber.ErrNotFound
		}
		c.Set(fiber.HeaderCacheControl, "no-cache")
		c.Type("html")
		return c.Send(index)
	}
}

// injectBasePath makes the relative asset URLs Vite emits absolute under
// basePath (so deep links resolve), and tells the frontend its base path.
func injectBasePath(index []byte, basePath string) []byte {
	prefix := []byte(basePath + "/")
	index = bytes.ReplaceAll(index, []byte(`src="./`), append([]byte(`src="`), prefix...))
	index = bytes.ReplaceAll(index, []byte(`href="./`), append([]byte(`href="`), prefix...))
	// json.Marshal escapes <, > and &, so the value cannot close the tag.
	js, _ := json.Marshal(basePath)
	script := []byte("<head><script>window.__TMPHOST_BASE_PATH__=" + string(js) + ";</script>")
	return bytes.Replace(index, []byte("<head>"), script, 1)
}
