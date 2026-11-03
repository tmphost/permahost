# permahost plugin for Claude Code

It connects Claude Code to permahost's MCP server, so Claude can host files
and static sites at permanent public URLs: upload, download, list and link
files in folders that stay up while your account has credits. It also adds a
`permahost` skill on when and how to publish files with it.

```sh
/plugin marketplace add tmphost/permahost
/plugin install permahost@permahost
```

It connects to `https://mcp.permahost.io/mcp` and signs in through the browser
on the first tool call. To use a self-hosted permahost, add your own MCP server
pointing at its app URL instead:
`claude mcp add --transport http permahost https://files.example.com/mcp`.

The server's `file` tool has the actions `upload`, `download`, `get`, `url`
and `list`. A new folder costs 500 credits a month; uploading into an existing
folder never costs folder credits again.

## License

This plugin is licensed under the [Apache License 2.0](LICENSE). The permahost
server it connects to is AGPL-3.0-only.
