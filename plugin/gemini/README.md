# permahost extension for Gemini CLI

It connects [Gemini CLI](https://github.com/google-gemini/gemini-cli) to
permahost's MCP server, so Gemini can host files and static sites at permanent
public URLs: upload, download, list and link files in folders that stay up
while your account has credits.

```bash
gemini extensions install https://github.com/tmphost/permahost
```

The server is `https://app.permahost.io/mcp`. It answers an unauthenticated
request with a 401, Gemini CLI discovers its OAuth endpoints from that, and you
sign in with your permahost account; `/mcp auth permahost` starts the sign-in
by hand. To use a self-hosted permahost, define a server named `permahost` in
`~/.gemini/settings.json` pointing at its app URL; a server defined there takes
precedence over the extension's:

```json
{
  "mcpServers": {
    "permahost": { "type": "http", "url": "https://files.example.com/mcp" }
  }
}
```

## Where the files are

Gemini CLI installs an extension from a git URL and reads its manifest from
the repository root. So the manifest is
[`gemini-extension.json`](../../gemini-extension.json) at the root of this
repository, and its `contextFileName` points back here, at
[`GEMINI.md`](GEMINI.md), the instructions loaded into every session.

Gemini CLI also loads `skills/`, `agents/`, `commands/`, `hooks/` and
`policies/` from an extension's root, which is this repository's root, so
don't add a folder with one of those names there.

## Permissions

Gemini CLI asks before each tool call. To allow the permahost tool without
asking, add a policy file such as `~/.gemini/policies/permahost.toml`:

```toml
[[rule]]
mcpName = "permahost"
decision = "allow"
priority = 100
```

Uploads spend credits, so allow them only if you are happy for Gemini to spend
them unprompted.

## Actions

The server has one tool, `file`; its `action` argument picks what it does:

| Action | What it does |
| --- | --- |
| `upload` | Uploads a file into a new folder, or with `id` into an existing one (same path replaces the file, keeping its URL); text as `textContent`, anything else as `base64Content` |
| `download` | Reads a file back from a folder |
| `get` | Lists a folder's files and their URLs |
| `url` | Builds the public URL of a folder, directory or file |
| `list` | Lists your folders, newest first, to find one to add to |

A new folder costs 500 credits a month; uploading into an existing folder never
costs folder credits again. Storage costs 500 credits a month per started 25 MB
the account stores.

This table is checked against the server in CI (see
`backend/internal/handler/mcp/plugin_docs_test.go`): an action added to the
server without being documented here fails the build.

## License

This plugin is licensed under the [Apache License 2.0](LICENSE). The permahost
server it connects to is AGPL-3.0-only.
