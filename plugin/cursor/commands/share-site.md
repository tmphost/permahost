---
name: share-site
description: Publish a static HTML site to one permahost folder and reply with its public URL.
---

# Share a static site

1. Find the site's directory (a build output such as `dist/` or the folder the
   user names) and list its files. It needs an `index.html` at its root.
2. If the site was published before, find its `folderId` with the
   `listFolders` tool and skip to step 4: uploading into it keeps the URL and
   costs no new folder.
3. Upload `index.html` with `uploadFile` without `folderId` and note the
   returned `folderId`.
4. Upload every other file with that `folderId`, its directory in `path`
   (`css/site.css` is `fileName: "site.css"`, `path: "css"`). Text files go as
   `textContent`, images and fonts as `base64Content`.
5. Call `getUrl` with just the `folderId`, and reply with that URL and the
   credits spent.
