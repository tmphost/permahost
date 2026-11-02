# tmphost.io

Go (Fiber v3, GORM) backend and Vue 3 (Vite, Tailwind) frontend.

## Sign-in and credits

- Sign in with Google or GitHub (OAuth). Accounts are keyed by verified email,
  so both providers reach the same account.
- Every account gets **250 operations** per period. A cron job running daily at
  00:00 UTC resets due accounts to 250 (unused credits do not carry over) and
  moves their renewal 30 days forward.

## File hosting

- Files live in folders that expire after a TTL (default 30 minutes, at most
  `files.maxTtlMinutes`, 7200 hours by default). Each file written costs 1
  credit per started MB (1 MiB) of its size per started 30 minutes of the
  folder's remaining lifetime: a 1.01 MB file kept for an hour costs 2 x 2 = 4.
- Files can be added to an existing folder (web app, MCP `upload` with `id`,
  CLI `--id`). Uploading to a path that is already there replaces the file
  in place, an update: its URL stays the same and the folder keeps its
  expiry. The write is charged like any other.
- Without signing in, the web app's Upload page makes folders that live at
  most `files.anonymousMaxTtlMinutes` (`TMPHOSTIO_FILES_ANONYMOUS_MAX_TTL_MINUTES`,
  10 minutes by default; root can change it on /setup).
- Storage is local disk (`TMPHOSTIO_STORAGE_LOCAL_PATH`) or an S3 bucket, under
  `u<userId>/<folderId>/<path>`. Paths are at most 5 directories deep and are
  validated server-side, so a file can never leave its folder. The database
  stores only each folder's ID, owner and expiry; a job deletes expired
  folders every minute. Folder IDs are 11 random lowercase base36
  characters, so public URLs can't be guessed.
- Public URLs: `<TMPHOSTIO_BASE_URL>/files/<userId>/<folderId>/<path>` by
  default, or `f<folderId>.<publicDomain>/<path>` with subdomains on
  (`TMPHOSTIO_FILES_SUBDOMAINS`, `TMPHOSTIO_FILES_PUBLIC_DOMAIN`, which defaults
  to the sites domain; needs wildcard DNS and a certificate, which the
  built-in TLS obtains when it is the sites domain). Both are served with `Content-Security-Policy:
  sandbox`, so hosted pages never run with the app's origin.

## MCP, CLI and OAuth

- `/mcp` is a stateless MCP server with one tool, `file` (actions `upload`,
  `download`, `get`, `url`). The `tmphost` CLI in [cli/](cli/) mirrors it.
  File content travels as `base64Content` (the default) or, when the
  `format` argument asks for it, as UTF-8 `textContent`.
- Both sign in with tmphost's own OAuth 2.1 server: dynamic client
  registration (`/oauth2/register`), PKCE S256, a consent page
  (`/oauth2/authorize`) and refresh tokens (`/oauth2/token`), discovered via
  `/.well-known/oauth-authorization-server` and
  `/.well-known/oauth-protected-resource/mcp`. The CLI uses the built-in
  public client ID `00000000001` with a loopback redirect. Access tokens also
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

Database: SQLite by default (`TMPHOSTIO_SQLITE_DSN`); set
`TMPHOSTIO_POSTGRES_ENABLED=true` and `TMPHOSTIO_POSTGRES_DSN`
(`postgres://user:password@host:5432/tmphost?sslmode=disable`) for PostgreSQL.

## Build and test

```sh
make build   # bin/tmphost with the frontend embedded
make test    # 100% coverage is enforced on both sides
```

## Setup page

`/setup` opens only with the root access token (`TMPHOSTIO_AUTH_ROOT_ACCESS_TOKEN`;
when it is empty, a random one is generated and printed at startup). There you
can change the monthly credit grant, the maximum TTL and the public URL mode
(these apply immediately), and these, which apply after a restart:

- **Domains**: the app URL (default `http://localhost:2027`; e.g.
  `https://app.tmphost.io`, any domain or subdomain) and the sites domain
  (e.g. `tmphost.io`) whose dynamic subdomains `f<11 characters>.<sites domain>`
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
- **Files**: the maximum TTL and upload size, and whether folders are served
  at paths or on `f<folder ID>.<public domain>` subdomains.
- **Sign-in providers**: Google, GitHub, and any number of custom OpenID
  Connect providers (name, SVG icon, client ID, client secret, auth URL = the
  issuer; endpoints come from its `/.well-known/openid-configuration`).

Settings are saved to `TMPHOSTIO_SETTINGS_FILE` (default
`./_storage/settings.json`, mode 0600) and override the config file and
environment. Secrets (including the PostgreSQL DSN) are write-only on the
setup page.

- **Encrypted at rest**: the file is AES-256-GCM encrypted with a key derived
  (HKDF-SHA256) from `TMPHOSTIO_MASTER_KEY`. The master key is read from the
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
docker run -p 2027:2027 -v tmphost-data:/data \
  -e TMPHOSTIO_MASTER_KEY=... -e TMPHOSTIO_AUTH_JWT_SECRET=... -e TMPHOSTIO_AUTH_ROOT_ACCESS_TOKEN=... \
  -e TMPHOSTIO_BASE_URL=https://app.tmphost.io tmphost
```

Settings and the SQLite database live in the `/data` volume.
