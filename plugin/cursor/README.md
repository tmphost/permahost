# permahost plugin for Cursor

It connects Cursor to permahost's MCP server, so the agent can host files and
static sites at permanent public URLs: upload, download, list and link files in
folders that stay up while your account has credits.

| Component | What it adds |
| --- | --- |
| `mcp.json` | The `permahost` MCP server, `https://app.permahost.io/mcp`, signed in with OAuth |
| `skills/permahost/SKILL.md` | When and how to use the `file` tool's `upload`, `download`, `get`, `url` and `list` actions |
| `rules/prefer-permahost-mcp.mdc` | Routes "publish this" requests to the right action, reuses folders and keeps secrets out of public links |
| `commands/share-file.md` | `/share-file`: upload one file and reply with its link |
| `commands/share-site.md` | `/share-site`: publish or update a static HTML site and reply with its URL |

## Install and connect

1. Install **permahost** from the Cursor Marketplace, or import this
   repository ([`.cursor-plugin/marketplace.json`](../../.cursor-plugin/marketplace.json)
   lists the plugin) as a team marketplace.
2. Open **Cursor Settings → Tools & MCP** and enable the `permahost` server.
3. Select **Connect**, sign in with your permahost account and approve access.

The server answers the first request with a 401; Cursor discovers its OAuth
endpoints and registers itself (Dynamic Client Registration), so there is no
API key to paste. To use a self-hosted permahost, add your own MCP server
pointing at its app URL (`https://files.example.com/mcp`) instead.

## Try it

```text
Publish this HTML report on permahost and give me the link.
```

```text
/share-site dist
```

Anyone with a link can open the file. A new folder costs 500 credits a month
and storage 500 a month per started 25 MB; uploading into an existing folder
never costs folder credits again.

## Local development

The plugin follows Cursor's [plugin template](https://github.com/cursor/plugin-template):
a plugin directory with `.cursor-plugin/plugin.json`, listed by the
repository's `.cursor-plugin/marketplace.json`. To load it from a checkout,
copy it into Cursor's local plugin folder (Cursor skips symlinks that point
outside it), then run **Developer: Reload Window**:

```sh
mkdir -p ~/.cursor/plugins/local
cp -R plugin/cursor/. ~/.cursor/plugins/local/permahost/
```

Bump `version` in `.cursor-plugin/plugin.json` whenever the plugin changes.

## License

This plugin is licensed under the [Apache License 2.0](LICENSE). The permahost
server it connects to is AGPL-3.0-only.
