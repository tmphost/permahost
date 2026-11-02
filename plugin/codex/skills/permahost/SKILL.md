---
name: permahost
description: Host files and static sites at permanent public URLs with permahost. Use when the user asks to host, publish, share or send a lasting link to a file, a generated report, an image or a static HTML site, to add to or update something they published on permahost, or to fetch a file back from a permahost folder.
---

# permahost

permahost hosts files in folders. Every file gets a public URL that keeps
working for as long as the account has credits; nothing expires. Use the
`file` tool of the `permahost` MCP server; the first call asks the user to
sign in to permahost (OAuth).

The user's explicit instructions take precedence over this guidance: if they
name a folder, a file name or a path, use theirs.

## Before uploading

- Anyone with the link can open the file. Do not upload secrets, private keys,
  passwords, tokens or other credentials, even when asked; explain why and
  suggest a private channel instead.
- If it is unclear which file or content to share, ask before uploading.
- If the user is adding to or updating something already on permahost, find
  its folder first (`list`) instead of creating a new one.

## The `file` tool

One tool with an `action` argument:

- `upload`: `fileName`, the content, and optionally `path` (a directory inside
  the folder, such as `site/css`). Without `id` it creates a new folder; with
  `id` the file goes into that existing folder, and a file already at that
  path is replaced in place (its URL stays the same). Returns the folder `id`,
  the file `url` and `creditsSpent`.
- `download`: `id`, `fileName`, optional `path`. Returns the content.
- `get`: `id`. Lists the folder's files with their URLs.
- `url`: the public URL of a folder (`id`), a directory (`id`, `path`) or a
  file (`id`, `fileName`, optional `path`).
- `list`: the user's folders, newest first, with `id`, `title`, `url`,
  `sizeBytes` and `createdAt`. No `folders` field means there are none.

Only use folder ids that a tool call returned or that the user gave you; never
guess one.

## Content

- Text files (HTML, CSS, JS, JSON, Markdown, CSV, SVG): pass
  `format: "textContent"` and the file as is in `textContent`.
- Anything else (images, PDFs, archives): `format: "base64Content"` (the
  default) with the bytes in `base64Content`. Encode local files with a
  command such as `base64 -w0 <file>` rather than by hand.

## Sharing a site or several files

1. Upload the first file with `upload` without `id` and note the returned
   `id`.
2. Upload the rest with that `id`, keeping their directories in `path`
   (`index.html` at the root, `css/site.css` as `fileName: "site.css"`,
   `path: "css"`). Paths are at most 5 directories deep, with no `.` or `..`
   segments.
3. Give the user the folder URL (`url` with just `id`); `index.html` is served
   for the folder root.

To update a site later, upload the changed files with the same `id`: the links
stay the same.

## Credits

Hosting is paid monthly from the account's credits:

- A new folder costs 500 credits a month. Only `upload` without `id` creates
  one, so put related files in one folder and reuse its `id`.
- Uploading with `id` into an existing folder never costs folder credits
  again, however many files it adds or replaces.
- Storage costs 500 credits a month per started 25 MB the whole account
  stores. An upload pays only when it starts a new block, so most uploads cost
  0; replacing a file counts only the size difference.
- `list`, `get`, `url` and `download` are free.

If a call fails for lack of credits, tell the user rather than retrying. When
an account runs out of credits at its monthly renewal, its sites are paused
until credits renew; nothing is deleted.

## Custom domains

A folder can also be served on the user's own domain (such as
`www.example.com`), set up in the permahost web app or with the
`permahost domain` CLI command; the MCP tool doesn't manage domains.

## Reply

Give the user the link and mention the credits spent. Say the link is
permanent while their account has credits.
