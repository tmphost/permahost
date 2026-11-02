// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"html/template"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
	mapper "github.com/tmphost/tmphost/backend/internal/mapper/api"
)

// _themeScript applies the theme picked in the web app (same origin, same
// localStorage key) before the page paints. The CSP allows it by hash only.
const _themeScript = `(function () {
  var theme = null
  try { theme = localStorage.getItem('theme') } catch (e) {}
  if (theme === 'light' || theme === 'dark') document.documentElement.classList.add(theme)
})()`

// _themeScriptHash is the CSP source that allows _themeScript and nothing else.
var _themeScriptHash = func() string {
	sum := sha256.Sum256([]byte(_themeScript))
	return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
}()

// _pageTemplate is the consent page and its error variant, styled like the
// web app's sign-in card. Without _themeScript's choice, the theme follows
// the system's.
var _pageTemplate = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>{{if .Consent}}Allow access to tmphost.io?{{else}}tmphost.io{{end}}</title>
<script>` + _themeScript + `</script>
<style>
:root {
  --page: #fafafa; --card: #ffffff; --border: #e5e7eb; --row: #fafafa; --text: #111827; --muted: #6b7280; --label: #9ca3af;
  --tile: #000000; --tile-ink: #ffffff; --primary: #111827; --primary-text: #ffffff; --secondary: #ffffff; --secondary-text: #374151;
  --warn-bg: #fffbeb; --warn-border: #fde68a; --warn-text: #92400e; --error-bg: #fff1f2; --error-border: #ffe4e6; --error-text: #e11d48;
  --ok-bg: #ecfdf5; --ok-border: #a7f3d0; --ok-text: #047857;
}
@media (prefers-color-scheme: dark) {
  :root:not(.light) {
    --page: #09090b; --card: #18181b; --border: #27272a; --row: #111113; --text: #fafafa; --muted: #a1a1aa; --label: #71717a;
    --tile: #ffffff; --tile-ink: #000000; --primary: #fafafa; --primary-text: #09090b; --secondary: #27272a; --secondary-text: #e4e4e7;
    --warn-bg: #2a1d08; --warn-border: #4a3410; --warn-text: #fcd34d; --error-bg: #2a0e14; --error-border: #4c1520; --error-text: #fda4af;
    --ok-bg: #062016; --ok-border: #0b3b28; --ok-text: #6ee7b7;
  }
}
:root.dark {
  --page: #09090b; --card: #18181b; --border: #27272a; --row: #111113; --text: #fafafa; --muted: #a1a1aa; --label: #71717a;
  --tile: #ffffff; --tile-ink: #000000; --primary: #fafafa; --primary-text: #09090b; --secondary: #27272a; --secondary-text: #e4e4e7;
  --warn-bg: #2a1d08; --warn-border: #4a3410; --warn-text: #fcd34d; --error-bg: #2a0e14; --error-border: #4c1520; --error-text: #fda4af;
  --ok-bg: #062016; --ok-border: #0b3b28; --ok-text: #6ee7b7;
}
:root { color-scheme: light; }
:root.dark { color-scheme: dark; }
@media (prefers-color-scheme: dark) { :root:not(.light) { color-scheme: dark; } }
* { box-sizing: border-box; }
html, body { margin: 0; }
body {
  min-height: 100vh; display: flex; align-items: center; justify-content: center; padding: 40px 16px;
  background: var(--page); color: var(--text);
  font-family: ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; -webkit-font-smoothing: antialiased;
}
.card { width: 100%; max-width: 26rem; padding: 32px; background: var(--card); border: 1px solid var(--border); border-radius: 12px; box-shadow: 0 1px 2px rgba(0,0,0,.05); }
.tile { width: 48px; height: 48px; margin: 0 auto 20px; border-radius: 8px; background: var(--tile); color: var(--tile-ink); display: flex; align-items: center; justify-content: center; }
.tile svg { width: 36px; height: 36px; }
h1 { margin: 0 0 6px; font-size: 22px; line-height: 1.3; font-weight: 900; letter-spacing: -0.02em; text-align: center; overflow-wrap: anywhere; }
.lead { margin: 0 0 20px; font-size: 14px; line-height: 1.5; color: var(--muted); text-align: center; }
.note { margin: 0 0 20px; padding: 10px 12px; border-radius: 8px; border: 1px solid; font-size: 12px; line-height: 1.5; font-weight: 500; overflow-wrap: anywhere; }
.warn { background: var(--warn-bg); border-color: var(--warn-border); color: var(--warn-text); }
.ok { background: var(--ok-bg); border-color: var(--ok-border); color: var(--ok-text); }
.error { background: var(--error-bg); border-color: var(--error-border); color: var(--error-text); }
dl { margin: 0 0 20px; border: 1px solid var(--border); border-radius: 8px; background: var(--row); }
dl div { padding: 10px 14px; }
dl div + div { border-top: 1px solid var(--border); }
dt { margin: 0 0 2px; font-size: 10px; font-weight: 900; letter-spacing: 0.1em; text-transform: uppercase; color: var(--label); }
dd { margin: 0; font-size: 14px; line-height: 1.5; overflow-wrap: anywhere; }
.hint { margin: 0 0 20px; font-size: 12px; line-height: 1.5; color: var(--muted); text-align: center; }
form { display: flex; gap: 12px; }
button { flex: 1; padding: 12px 16px; border-radius: 8px; font: inherit; font-size: 11px; font-weight: 900; letter-spacing: 0.1em; text-transform: uppercase; cursor: pointer; }
button:focus-visible { outline: 2px solid var(--muted); outline-offset: 2px; }
.deny { background: var(--secondary); color: var(--secondary-text); border: 1px solid var(--border); }
.allow { background: var(--primary); color: var(--primary-text); border: 1px solid var(--primary); }
</style>
</head>
<body>
<main class="card">
  <div class="tile" aria-hidden="true">
    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
      <path d="M12 3a9 9 0 0 0 0 18" /><path d="M12 3a9 9 0 0 1 9 9" stroke-dasharray="2.5 3.5" />
      <path d="M21 12a9 9 0 0 1-9 9" stroke-dasharray="1 4" opacity="0.5" />
      <path d="m8 9.5-2.5 2.5 2.5 2.5" /><path d="m16 9.5 2.5 2.5-2.5 2.5" /><path d="m13.5 7.5-3 9" />
    </svg>
  </div>
{{with .Consent}}
  <h1 data-test="consent-client">{{if .ClientName}}{{.ClientName}}{{else}}An unnamed app{{end}}</h1>
  <p class="lead">wants to use your tmphost.io account.</p>
  {{if .Verified}}
  <p class="note ok" data-test="consent-verified">This is the official tmphost.io command line tool.</p>
  {{else}}
  <p class="note warn" data-test="consent-unverified">Apps name themselves, so this name is not verified. Check where it sends you back.</p>
  {{end}}
  <dl>
    <div><dt>It will be able to</dt><dd>Upload, list and download your files, and spend your credits</dd></div>
    <div><dt>Sends you back to</dt><dd data-test="consent-destination">{{.Destination}}</dd></div>
    <div><dt>Signed in as</dt><dd data-test="consent-account">{{.Email}}</dd></div>
  </dl>
  <p class="hint">Only allow an app you just asked to connect. If you didn't, deny.</p>
  <form method="post">
    <input type="hidden" name="consent" value="{{.Token}}">
    <button type="submit" name="decision" value="deny" class="deny" data-test="consent-deny">Deny</button>
    <button type="submit" name="decision" value="{{$.Allow}}" class="allow" data-test="consent-allow">Allow</button>
  </form>
{{else}}
  <h1>Can't connect</h1>
  <p class="note error" role="alert" data-test="consent-error">{{.Error}}</p>
{{end}}
</main>
</body>
</html>
`))

type pageData struct {
	Consent *entity.Consent
	Error   string
	Allow   string
}

func consentPage(consent *entity.Consent) []byte {
	return renderPage(pageData{Consent: consent, Allow: mapper.ConsentDecisionAllow})
}

func errorPage(msg string) []byte {
	return renderPage(pageData{Error: msg})
}

func renderPage(data pageData) []byte {
	var buf bytes.Buffer
	// The template is fixed and the data plain strings, so it cannot fail.
	_ = _pageTemplate.Execute(&buf, data)
	return buf.Bytes()
}
