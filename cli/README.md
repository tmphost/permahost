# permahost CLI

Upload files to temporary public folders on a permahost server from the
terminal. The `permahost file` commands mirror the `file` tool of the server's
MCP endpoint.

## Install

macOS and Linux (amd64, arm64):

```sh
curl -fsSL https://raw.githubusercontent.com/tmphost/permahost/main/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/tmphost/permahost/main/install.ps1 | iex
```

Both download the latest release, verify it against `checksums.txt` and
install `permahost` to `/usr/local/bin` when writable or `~/.local/bin`
(Windows: `%LOCALAPPDATA%\permahost\bin`, added to your user PATH). Set
`PERMAHOSTIO_VERSION=v1.2.3` for a specific release or `PERMAHOSTIO_INSTALL_DIR` for
another directory.

From source: `cd cli && go build -o permahost .` (or `make cli`, which writes
`bin/permahost-cli`).

## Sign in

```sh
permahost login                              # https://permahost.io
permahost login --server https://files.example.com
```

`login` opens the browser to approve the CLI (OAuth 2.1 with PKCE; the URL is
also printed in case the browser does not open) and waits up to 5 minutes.
The tokens are saved to `$PERMAHOSTIO_CONFIG`, or `permahost/config.json` under your
user config directory, readable only by you, and refreshed automatically.
`permahost logout` forgets them. `--server` or `PERMAHOSTIO_SERVER` picks the server;
tokens are only ever sent to the server that issued them.

## Usage

```sh
permahost file upload report.pdf                 # new folder, 30 minute TTL
permahost file upload site/ --ttl 2h             # a directory's contents, recursively
permahost file upload site --id rs               # site/index.html, ... into folder rs
permahost file upload notes.txt --id rs --path docs --name readme.txt

permahost file get --id rs                       # files: fileName, path, size, url
permahost file url --id rs --path docs readme.txt
permahost file download --id rs --path docs readme.txt -o readme.txt
permahost file download --id rs a.txt -o -       # to stdout

permahost file ls                                # your live folders
permahost file rm --id rs docs/readme.txt        # one file
permahost file rm --id rs                        # the whole folder
permahost whoami
```

| Command | Description |
| --- | --- |
| `permahost login [--server URL]` | Sign in with the browser |
| `permahost logout` | Forget the saved tokens |
| `permahost whoami` | Show the user, credits and renewal time |
| `permahost file upload <local path>... [--id ID \| --ttl 30m] [--path DIR] [--name NAME]` | Upload files and print their URLs and the folder id |
| `permahost file download --id ID [--path DIR] <fileName> [-o OUT] [--force] [--format FORMAT]` | Download a file (`-o -` for stdout) |
| `permahost file get --id ID` | List a folder's files |
| `permahost file url --id ID [--path DIR] [fileName]` | Print a folder, directory or file URL |
| `permahost file ls` | List your live folders |
| `permahost file rm --id ID [path]` | Delete a folder, or one file |

Global flags: `--json` prints JSON with the same fields as the MCP tool's
output (`id`, `url`, `expiresAt`, `fileName`, `path`, `size`, `creditsSpent`,
`files`), `--server` picks the server, `--version` prints the version.
`download -o - --json` puts the file in `base64Content`, or as UTF-8 text in
`textContent` with `--format textContent`.

Uploading a directory keeps its name as a prefix (`site` uploads
`site/index.html`), while `site/` or `.` uploads only its contents, like
rsync. Paths follow the server's rules (at most 5 directories deep, no `.` or
`..` segments, no backslashes) and are checked before anything is sent. Each
file costs one credit per started MB of its size per started billing block of
the folder's lifetime; the cost is checked against your credits before anything is uploaded.

Folder URLs come from the server and may look like
`https://f<folder>.<domain>/` or `<server>/files/<user>/<folder>/`.

## MCP

The server offers the same `file` tool (actions `upload`, `download`, `get`
and `url`) at `<server>/mcp`, for AI agents:

```json
{
  "mcpServers": {
    "permahost": {
      "type": "http",
      "url": "https://permahost.io/mcp"
    }
  }
}
```

MCP clients that support OAuth sign in through the browser the first time
they connect.

File content travels in one of two fields, picked by the tool's `format`
argument: `base64Content` (the default, for any file) or `textContent` (UTF-8
text sent as is, handy for HTML, CSS or JSON). `upload` reads the field that
`format` names and `download` fills it; a download as `textContent` fails for
a file that isn't UTF-8 text.

## Development

```sh
make test-cli   # go test -race with coverage; CI requires 100%
```

Pushing a `v*` tag runs `.github/workflows/release.yml`, which cross-builds
the CLI and attaches `permahost_<os>_<arch>.tar.gz` / `.zip` and
`checksums.txt` to the GitHub release.
