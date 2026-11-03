# permahost plugin for Codex and ChatGPT

It connects Codex and ChatGPT to permahost's MCP server, so they can host
files and static sites at permanent public URLs: upload, download, list and
link files in folders that stay up while your account has credits. It also adds
a `permahost` skill on when and how to publish files with it.

It is a portable [Agent Plugins](https://agent-plugins.org) package, laid out
as [OpenAI's plugin guide](https://developers.openai.com/plugins/build/plugins)
describes:

| File | What it holds |
| --- | --- |
| `plugin.json` | The manifest; the listing, review cases and release notes live under `extensions["com.openai"]` |
| `mcp.json` | The `permahost` MCP server, `https://mcp.permahost.io/mcp` |
| `skills/permahost/SKILL.md` | When and how to use the `uploadFile`, `downloadFile`, `listFiles`, `getUrl` and `listFolders` tools |
| `assets/icon.png` | The composer icon and listing logo |

## Install in Codex

The repository's [`.agents/plugins/marketplace.json`](../../.agents/plugins/marketplace.json)
lists this plugin:

```sh
codex plugin marketplace add tmphost/permahost
codex plugin add permahost@permahost
```

The first tool call asks you to sign in with your permahost account: the
server answers with a 401 and Codex discovers its OAuth endpoints and
registers itself (Dynamic Client Registration). To use a self-hosted
permahost, add your own MCP server pointing at its app URL (`<app URL>/mcp`)
instead.

## Submitting to the plugin directory

OpenAI's directory also needs `privacyPolicyURL` and `termsOfServiceURL` in
`interface`, a demo recording URL and reviewer credentials (which never go in
the package). Add the two URLs once permahost.io publishes those pages, and
bump `version` and `publication.release_notes` in `plugin.json` for every new
upload.

## License

This plugin is licensed under the [Apache License 2.0](LICENSE). The permahost
server it connects to is AGPL-3.0-only.
