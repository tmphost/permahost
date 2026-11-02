---
name: share-site
description: Publish a static HTML site to one permahost folder and reply with its public URL.
---

# Share a static site

1. Find the site's directory (a build output such as `dist/` or the folder the
   user names) and list its files. It needs an `index.html` at its root.
2. If the site was published before, find its folder `id` with the `file`
   tool's `list` action and skip to step 4: uploading into it keeps the URL and
   costs no new folder.
3. Upload `index.html` with action `upload` without `id` and note the returned
   `id`.
4. Upload every other file with that `id`, its directory in `path`
   (`css/site.css` is `fileName: "site.css"`, `path: "css"`). Text files go as
   `textContent`, images and fonts as `base64Content`.
5. Call `file` with action `url` and just the `id`, and reply with that URL
   and the credits spent.
