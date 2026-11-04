// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"bytes"
	"encoding/json"
	"html/template"
	"net/http"
	"strings"

	"github.com/tmphost/permahost/backend/internal/brand"
)

// ManifestPaths are where the server's manifest is published: the
// well-known path MCP discovery probes, and the site root.
var ManifestPaths = []string{"/.well-known/mcp.json", "/mcp.json"}

// _setupPageCSP locks the setup page down: no scripts, frames or requests,
// inline styles and the data URI icon only.
const _setupPageCSP = "default-src 'none'; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

type (
	// manifest is the MCP Registry server.json, as at the repository root,
	// with the remote this server answers on.
	manifest struct {
		Schema      string           `json:"$schema"`
		Name        string           `json:"name"`
		Title       string           `json:"title"`
		Description string           `json:"description"`
		WebsiteURL  string           `json:"websiteUrl"`
		Repository  manifestRepo     `json:"repository"`
		Icons       []manifestIcon   `json:"icons"`
		Version     string           `json:"version"`
		Remotes     []manifestRemote `json:"remotes"`
	}

	manifestRepo struct {
		URL    string `json:"url"`
		Source string `json:"source"`
	}

	manifestIcon struct {
		Src      string   `json:"src"`
		MIMEType string   `json:"mimeType"`
		Sizes    []string `json:"sizes"`
	}

	manifestRemote struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	}

	// toolSummary is a tool as the setup page lists it.
	toolSummary struct {
		Name    string
		Summary string
	}

	setupPageData struct {
		Endpoint string
		Icon     template.URL
		Tools    []toolSummary
	}
)

// _toolSummaries are the tools the setup page lists, in registration order.
var _toolSummaries = []toolSummary{
	{ToolUploadFile, "Store a file in a new or existing folder and get its public URL."},
	{ToolDownloadFile, "Read a file back from a folder."},
	{ToolListFiles, "List a folder's files and URLs."},
	{ToolGetURL, "Build the public URL of a folder, directory or file."},
	{ToolListFolders, "List your folders, newest first."},
}

// Manifest is the server's MCP Registry manifest (server.json) naming
// endpoint as its streamable HTTP remote.
func Manifest(endpoint string) []byte {
	b, _ := json.MarshalIndent(manifest{
		Schema:      "https://static.modelcontextprotocol.io/schemas/2025-12-11/server.schema.json",
		Name:        "io.github.tmphost/permahost",
		Title:       "permahost",
		Description: "Host files at permanent public URLs: upload, list and link files in folders that stay online.",
		WebsiteURL:  "https://permahost.io",
		Repository:  manifestRepo{URL: "https://github.com/tmphost/permahost", Source: "github"},
		Icons:       []manifestIcon{{Src: "https://permahost.io/favicon.svg", MIMEType: brand.IconSVGType, Sizes: []string{"any"}}},
		Version:     brand.Version,
		Remotes:     []manifestRemote{{Type: "streamable-http", URL: endpoint}},
	}, "", "  ")
	// Plain strings only, so it cannot fail.
	return append(b, '\n')
}

// wantsSetupPage reports whether r is a person or a plain HTTP client
// opening the endpoint, rather than an MCP client: a GET without a token
// that asks for neither JSON nor an event stream. MCP clients always send
// both in Accept, so they still get the 401 that starts their sign-in.
func wantsSetupPage(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead || r.Header.Get("Authorization") != "" {
		return false
	}
	accept := strings.ToLower(strings.Join(r.Header.Values("Accept"), ","))
	return !strings.Contains(accept, "text/event-stream") && !strings.Contains(accept, "application/json")
}

// setupPage answers with page, or hands the request to next when an MCP
// client sent it.
func setupPage(page []byte, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !wantsSetupPage(r) {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Set("Content-Type", "text/html; charset=utf-8")
		h.Set("Content-Security-Policy", _setupPageCSP)
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-cache")
		h.Set("Vary", "Accept, Authorization")
		_, _ = w.Write(page)
	})
}

// renderSetupPage is the page that tells a person how to add the server at
// endpoint to their MCP client.
func renderSetupPage(endpoint string) []byte {
	var buf bytes.Buffer
	// The template is fixed and the data plain strings, so it cannot fail.
	_ = _setupTemplate.Execute(&buf, setupPageData{
		Endpoint: endpoint,
		Icon:     template.URL(brand.DataURI(brand.IconSVGType, brand.IconSVG)),
		Tools:    _toolSummaries,
	})
	return buf.Bytes()
}

var _setupTemplate = template.Must(template.New("setup").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>permahost.io MCP server</title>
<meta name="description" content="Connect an MCP client to permahost.io to host files at permanent public URLs.">
<link rel="icon" href="{{.Icon}}" type="image/svg+xml">
<style>
:root {
  --page: #fafafa; --card: #ffffff; --border: #e5e7eb; --row: #fafafa; --text: #111827; --muted: #6b7280; --label: #9ca3af;
  color-scheme: light;
}
@media (prefers-color-scheme: dark) {
  :root { --page: #09090b; --card: #18181b; --border: #27272a; --row: #111113; --text: #fafafa; --muted: #a1a1aa; --label: #71717a; color-scheme: dark; }
}
* { box-sizing: border-box; }
html, body { margin: 0; }
body {
  background: var(--page); color: var(--text); padding: 40px 16px;
  font-family: ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; -webkit-font-smoothing: antialiased;
}
main { max-width: 42rem; margin: 0 auto; padding: 32px; background: var(--card); border: 1px solid var(--border); border-radius: 12px; box-shadow: 0 1px 2px rgba(0,0,0,.05); }
header { display: flex; align-items: center; gap: 14px; margin: 0 0 20px; }
header img { width: 44px; height: 44px; border-radius: 8px; }
h1 { margin: 0; font-size: 22px; line-height: 1.3; font-weight: 900; letter-spacing: -0.02em; }
h2 { margin: 28px 0 8px; font-size: 10px; font-weight: 900; letter-spacing: 0.1em; text-transform: uppercase; color: var(--label); }
p, li { font-size: 14px; line-height: 1.6; }
p { margin: 0 0 12px; }
.lead { color: var(--muted); }
pre, .url { margin: 0 0 12px; padding: 10px 14px; border: 1px solid var(--border); border-radius: 8px; background: var(--row); white-space: pre-wrap; overflow-wrap: anywhere; }
code, pre { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 13px; }
.url { font-size: 15px; font-weight: 700; overflow-wrap: anywhere; }
ul { margin: 0 0 12px; padding-left: 20px; }
a { color: inherit; }
@media (max-width: 480px) { body { padding: 16px; } main { padding: 20px; } }
</style>
</head>
<body>
<main>
  <header>
    <img src="{{.Icon}}" alt="">
    <h1>permahost.io MCP server</h1>
  </header>
  <p class="lead">Host files at permanent public URLs from your AI agent: upload a file, a report or a whole static site and share a link that stays online for as long as your account has credits.</p>

  <h2>MCP server URL</h2>
  <p class="url"><code>{{.Endpoint}}</code></p>
  <p>It speaks streamable HTTP. This page is for people: MCP clients connect to the same URL with POST requests.</p>

  <h2>Sign in</h2>
  <p>The server uses OAuth 2.1 with dynamic client registration, so there is no API key to copy. Your client opens a browser to sign in to permahost.io the first time it connects; uploads spend the credits of the account you sign in with.</p>

  <h2>Claude Code</h2>
  <pre>claude mcp add --transport http permahost {{.Endpoint}}</pre>

  <h2>Claude.ai and Claude Desktop</h2>
  <p>Settings &rarr; Connectors &rarr; Add custom connector, then paste the URL above.</p>

  <h2>Codex</h2>
  <pre>codex mcp add permahost --url {{.Endpoint}}</pre>

  <h2>Gemini CLI</h2>
  <pre>gemini mcp add --transport http permahost {{.Endpoint}}</pre>

  <h2>VS Code and other clients</h2>
  <p>Add an HTTP server, for example in <code>.vscode/mcp.json</code>:</p>
  <pre>{
  "servers": {
    "permahost": { "type": "http", "url": "{{.Endpoint}}" }
  }
}</pre>

  <h2>Tools</h2>
  <ul>
  {{- range .Tools}}
    <li><code>{{.Name}}</code>: {{.Summary}}</li>
  {{- end}}
  </ul>

  <h2>More</h2>
  <p><a href="https://github.com/tmphost/permahost">Source and plugins</a></p>
</main>
</body>
</html>
`))
