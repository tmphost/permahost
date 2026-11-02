# permahost.io

Go (Fiber v3, GORM) backend and Vue 3 (Vite, Tailwind) frontend.

## Sign-in and credits

- Sign in with Google or GitHub (OAuth). Accounts are keyed by verified email,
  so both providers reach the same account.
- Every account gets a monthly grant of credits (2000 by default; root can
  change it on /setup). A cron job running daily at 00:00 UTC renews due
  accounts: the balance is reset to the grant minus the month's hosting cost
  (unused credits do not carry over) and the renewal moves 30 days forward.
- Hosting is permanent while the account has credits. Monthly prices:

  | What | Credits |
  | --- | --- |
  | Folder | 500 a month, charged when it is created |
  | Storage | 500 a month per started 25 MB of the account's files, charged when an upload starts a new block |
  | Custom domain | 500 setup + 500 a month, charged when its CNAME is verified |

- A balance below zero after renewal pauses every site of the account (HTTP
  402) until a later renewal brings it back to zero or above. Nothing is
  deleted. `GET /api/v1/users/me/usage` shows what an account hosts and its
  monthly cost.

## File hosting

- Files live in folders that never expire for a signed-in account. A folder
  records its size, so the storage charge follows what is actually stored.
- Files can be added to an existing folder (web app, MCP `upload` with `id`,
  CLI `--id`). Uploading to a path that is already there replaces the file
  in place, an update: its URL stays the same and only the size difference
  counts toward storage.
- Without signing in, the web app's Upload page makes folders that live at
  most `files.anonymousMaxTtlMinutes` (`PERMAHOSTIO_FILES_ANONYMOUS_MAX_TTL_MINUTES`,
  10 minutes by default; root can change it on /setup). Root can turn uploads
  without an account off on /setup, or with
  `PERMAHOSTIO_FILES_ANONYMOUS_UPLOADS_DISABLED=true`; every upload then needs
  an account.
- Storage is local disk (`PERMAHOSTIO_STORAGE_LOCAL_PATH`) or an S3 bucket, under
  `u<userId>/<folderId>/<path>`. Paths are at most 5 directories deep and are
  validated server-side, so a file can never leave its folder. The database
  stores each folder's ID, owner, size and, for an anonymous folder, its
  expiry; a job deletes expired anonymous folders every minute. Folder IDs are 11 random lowercase base36
  characters, so public URLs can't be guessed.
- Public URLs: `<PERMAHOSTIO_BASE_URL>/files/<userId>/<folderId>/<path>` by
  default, or `f<folderId>.<publicDomain>/<path>` with subdomains on
  (`PERMAHOSTIO_FILES_SUBDOMAINS`, `PERMAHOSTIO_FILES_PUBLIC_DOMAIN`, which defaults
  to the sites domain; needs wildcard DNS and a certificate, which the
  built-in TLS obtains when it is the sites domain). Both are served with `Content-Security-Policy:
  sandbox`, so hosted pages never run with the app's origin.

## Custom domains

- A folder can be served on any number of the owner's hostnames (web app:
  the globe button on a folder; CLI: `permahost domain add --id <folder>
  www.example.com`; API: `/api/v1/folders/:id/domains`).
- Each hostname needs a CNAME record pointing at the folder's own host,
  `f<folderId>.<publicDomain>` with subdomains on (otherwise the app host).
  The server looks the record up directly at public resolvers (`dns.resolvers`
  / `PERMAHOSTIO_DNS_RESOLVERS`, default `1.1.1.1:53,8.8.8.8:53`). It checks
  pending domains every 5 minutes for 7 days, and also on demand ("Verify now").
- Once the CNAME matches, the setup and first month are charged, the domain
  turns active, and with TLS on a Let's Encrypt certificate is obtained for it
  in the background (TLS-ALPN-01, so even with the wildcard DNS-01 mode).
- A pending claim reserves nothing: only a verified domain is unique, so a
  hostname can't be squatted by adding it first. Apex domains can't hold a
  CNAME, so use a subdomain such as `www`.

## MCP, CLI and OAuth

- `/mcp` is a stateless MCP server with one tool, `file` (actions `upload`,
  `download`, `get`, `url`, `list`). The `permahost` CLI in [cli/](cli/) mirrors it.
  The tool description spells out the credits: only an upload without `id`
  creates (and pays for) a folder; `list` finds existing folders to add to.
  File content travels as `base64Content` (the default) or, when the
  `format` argument asks for it, as UTF-8 `textContent`.
- Both sign in with permahost's own OAuth 2.1 server: dynamic client
  registration (`/oauth2/register`), PKCE S256, a consent page
  (`/oauth2/authorize`) and refresh tokens (`/oauth2/token`), discovered via
  `/.well-known/oauth-authorization-server` and
  `/.well-known/oauth-protected-resource/mcp`. The CLI uses the built-in
  public client ID `00000000001` with a loopback redirect, or, where no
  browser can reach it (SSH, containers), the device flow (RFC 8628):
  `/oauth2/device/code` hands out a code the user enters at `/auth/device`
  while the CLI polls `/oauth2/token`. Access tokens also
  work as Bearer tokens on `/api/v1`.

## Development

```sh
cp backend/cmd/server/.env.example backend/cmd/server/.env   # fill in OAuth apps, JWT secret, master key
set -a; . backend/cmd/server/.env; set +a
make dev            # both at once (Ctrl-C stops both), or separately:
make dev-backend    # API on :2027
make dev-frontend   # app on http://localhost:5173 (proxies /api)
```

OAuth callback URLs to register: `<app URL>/auth/<provider>/callback`
(`google`, `github`, or a custom provider's ID; the setup page shows each one).

Database: SQLite by default (`PERMAHOSTIO_SQLITE_DSN`); set
`PERMAHOSTIO_POSTGRES_ENABLED=true` and `PERMAHOSTIO_POSTGRES_DSN`
(`postgres://user:password@host:5432/permahost?sslmode=disable`) for PostgreSQL.

## Build and test

```sh
make build   # bin/permahost with the frontend embedded
make test    # 100% coverage is enforced on both sides
```

## Agent plugins

[plugin/](plugin/) packages the MCP server for AI clients, each with
instructions on when and how to use the `file` tool (Apache-2.0, unlike the
AGPL server). Each package connects to `https://app.permahost.io/mcp`:

| Client | Install |
| --- | --- |
| Claude Code | `/plugin marketplace add tmphost/permahost`, then `/plugin install permahost@permahost` |
| Codex | `codex plugin marketplace add tmphost/permahost`, then `codex plugin add permahost@permahost` |
| Cursor | import this repository as a marketplace (`.cursor-plugin/marketplace.json`) |
| Gemini CLI | `gemini extensions install https://github.com/tmphost/permahost` |

`backend/internal/handler/mcp/plugin_docs_test.go` fails the build when a
plugin's docs miss an action, quote other prices than the server charges, or
point at another MCP URL.

## Setup page

`/setup` opens only with the root access token (`PERMAHOSTIO_AUTH_ROOT_ACCESS_TOKEN`;
when it is empty, a random one is generated and printed at startup). There you
can change the monthly credit grant, the anonymous maximum TTL and the public URL mode
(these apply immediately), and these, which apply after a restart:

- **Domains**: the app URL (default `http://localhost:2027`; e.g.
  `https://app.permahost.io`, any domain or subdomain) and the sites domain
  (e.g. `permahost.io`) whose dynamic subdomains `f<11 characters>.<sites domain>`
  the server answers. Other hosts get a 404; localhost always reaches the app.
- **TLS**: serve HTTPS on its own port (default 443) and redirect the app port
  to it. Certificates come from Let's Encrypt automatically (lego, TLS-ALPN-01
  challenge, so Let's Encrypt must reach the server on port 443), are renewed
  when under 30 days and cached encrypted next to the settings file.
  - App host: always gets its own certificate.
  - Dynamic subdomains (switchable): each gets its own certificate on its first
    visit (Let's Encrypt allows about 50 new ones per week per registered
    domain), or, with an optional Cloudflare API token, one `*.<sites domain>`
    wildcard over DNS-01 with no such limit.
- **Database** (SQLite or PostgreSQL) and **file storage** (local disk or S3).
- **Files**: the upload size, the anonymous maximum TTL, and whether folders are served
  at paths or on `f<folder ID>.<public domain>` subdomains.
- **Sign-in providers**: Google, GitHub, and any number of custom OpenID
  Connect providers (name, SVG icon, client ID, client secret, auth URL = the
  issuer; endpoints come from its `/.well-known/openid-configuration`).

Settings are saved to `PERMAHOSTIO_SETTINGS_FILE` (default
`./_storage/settings.json`, mode 0600) and override the config file and
environment. Secrets (including the PostgreSQL DSN) are write-only on the
setup page.

- **Encrypted at rest**: the file is AES-256-GCM encrypted with a key derived
  (HKDF-SHA256) from `PERMAHOSTIO_MASTER_KEY`. The master key is read from the
  environment only, never from YAML or the settings file, and must be at least
  32 characters (`openssl rand -hex 32`); the server will not start without it.
  A wrong key or a tampered file stops startup with an error; if the key is
  lost, delete the file and re-enter the settings.
- **Never served**: the server only serves the embedded frontend, and only web
  asset types (`.js`, `.css`, `.svg`, ...) from it.

## Docker

One image, one binary: the frontend and base config are embedded, and the
image (`FROM scratch`, runs as `nobody`) holds just the binary and CA certs.

```sh
make docker
docker run -p 2027:2027 -v permahost-data:/data \
  -e PERMAHOSTIO_MASTER_KEY=... -e PERMAHOSTIO_AUTH_JWT_SECRET=... -e PERMAHOSTIO_AUTH_ROOT_ACCESS_TOKEN=... \
  -e PERMAHOSTIO_BASE_URL=https://app.permahost.io permahost
```

Settings and the SQLite database live in the `/data` volume.
