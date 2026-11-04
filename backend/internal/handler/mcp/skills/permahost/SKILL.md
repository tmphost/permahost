---
name: permahost
description: Host files and static sites at permanent public URLs with permahost. Use when the user asks to host, publish, share or send a lasting link to a file, a generated report, an image or a static HTML site, to add to or update something they published on permahost, or to fetch a file back from a permahost folder.
---

# permahost

permahost hosts files in folders. Every file gets a public URL that keeps
working for as long as the account has credits; nothing expires. This skill
comes from the permahost MCP server, and its tools are the ones named below.

The user's explicit instructions take precedence over this guidance: if they
name a folder, a file name or a path, use theirs.

## Before uploading

- Anyone with the link can open the file. Do not upload secrets, private keys,
  passwords, tokens or other credentials, even when asked; explain why and
  suggest a private channel instead.
- If it is unclear which file or content to share, ask before uploading.
- If the user is adding to or updating something already on permahost, find
  its folder first (`listFolders`) instead of creating a new one.

## Tools

- `uploadFile`: `fileName`, the content, and optionally `path` (a directory
  inside the folder, such as `site/css`). Without `folderId` it creates a new
  folder; with `folderId` the file goes into that existing folder, and a file
  already at that path is replaced in place (its URL stays the same). Returns
  the `folderId`, the file `url` and `creditsSpent`.
- `downloadFile`: `folderId`, `fileName`, optional `path`. Returns the content.
- `listFiles`: `folderId`. Lists the folder's files with their URLs.
- `getUrl`: the public URL of a folder (`folderId`), a directory (`folderId`,
  `path`) or a file (`folderId`, `fileName`, optional `path`).
- `listFolders`: the user's folders, newest first, with `folderId`, `title`,
  `url`, `sizeBytes` and `createdAt`; an empty `folders` list means there are
  none.

Only use folder ids that a tool call returned or that the user gave you; never
guess one.

## Content

- Text files (HTML, CSS, JS, JSON, Markdown, CSV, SVG): pass
  `format: "textContent"` and the file as is in `textContent`.
- Anything else (images, PDFs, archives): `format: "base64Content"` (the
  default) with the bytes in `base64Content`. Encode local files with a
  command such as `base64 -w0 <file>` rather than by hand.

## Sharing a site or several files

1. Upload the first file with `uploadFile` without `folderId` and note the
   returned `folderId`.
2. Upload the rest with that `folderId`, keeping their directories in `path`
   (`index.html` at the root, `css/site.css` as `fileName: "site.css"`,
   `path: "css"`). Paths are at most 5 directories deep, with no `.` or `..`
   segments.
3. Give the user the folder URL (`getUrl` with just `folderId`); `index.html`
   is served for the folder root.

## Updating files

Upload to the same `folderId`, `path` and `fileName` to replace a file: its
URL stays the same, so links the user already shared keep working. Upload only
the files that changed. Use `listFiles` to see what a folder holds before
changing it, and `listFolders` to find the folder of something published
earlier.

## Credits

Hosting is paid monthly from the account's credits:

- A new folder costs 500 credits a month. Only `uploadFile` without
  `folderId` creates one, so put related files in one folder and reuse its
  `folderId`.
- Uploading with `folderId` into an existing folder never costs folder
  credits again, however many files it adds or replaces.
- Storage costs 500 credits a month per started 25 MB the whole account
  stores. An upload pays only when it starts a new block, so most uploads cost
  0; replacing a file counts only the size difference.
- `listFolders`, `listFiles`, `getUrl` and `downloadFile` are free.

If a call fails for lack of credits, tell the user rather than retrying. When
an account runs out of credits at its monthly renewal, its sites are paused
until credits renew; nothing is deleted.

## Custom domains

A folder can also be served on the user's own domain (such as
`www.example.com`), set up in the permahost web app or with the
`permahost domain` CLI command; the MCP tools don't manage domains.

## Reply

Give the user the link and mention the credits spent. Say the link is
permanent while their account has credits.
