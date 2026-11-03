---
name: share-file
description: Upload a file to permahost and reply with its permanent public link.
---

# Share a file

1. Find the file the user means: a path they gave, the open file, or content
   you just generated. If it is unclear, ask.
2. Refuse secrets, credentials, `.env` files and private keys: anyone with the
   link can open the file.
3. If the user names a permahost folder or wants it added to an earlier one,
   find its `folderId` with the `listFolders` tool. Otherwise a new folder is
   created.
4. Call `uploadFile` (with that `folderId`, or without one for a new folder). Text files (HTML, CSS, JS, JSON, Markdown, CSV, SVG) go as
   `format: "textContent"`; anything else as `base64Content`, encoded with
   `base64 -w0 <file>`.
5. Reply with the `url` and the credits spent.
