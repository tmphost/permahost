// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tmphost/permahost/cli/internal/client"
)

func newFileUploadCmd(a *app) *cobra.Command {
	var o uploadOptions
	c := &cobra.Command{
		Use:   "upload <local path>... [--id ID] [--path DIR] [--name NAME]",
		Short: "Upload files to a public folder",
		Long: `Upload files to a permanent public folder and print their public URLs.

A file is uploaded under its base name, or under --name when it is the only
argument. A directory is uploaded recursively under its own name ("site"
uploads site/index.html), while a directory with a trailing slash, or ".",
uploads its contents (rsync style: "site/" uploads index.html). Symlinks to
files are followed; other non-regular files are skipped. --path puts
everything under a directory inside the folder.

Without --id a new folder is created. With --id the files are added to that
folder; a file already at the same path is replaced and keeps its URL, so
re-uploading updates it. Folders stay up until you delete them.

Hosting is billed monthly in credits: a new folder is charged when it is
created, and storage per started block (25 MB) your account stores, when an
upload starts a block not yet paid for this month. The most the upload can
cost is shown before it starts, and the credits actually spent after it.`,
		Example: `  permahost file upload report.pdf
  permahost file upload site/                # index.html, css/app.css, ...
  permahost file upload site --id rs         # site/index.html, ... in folder rs
  permahost file upload out.txt --path logs --name today.txt`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.upload(cmd.Context(), args, o)
		},
	}
	c.Flags().StringVar(&o.id, "id", "", "upload into this existing folder instead of creating one, replacing files at the same paths")
	c.Flags().StringVar(&o.dir, "path", "", "directory inside the folder, such as site/css")
	c.Flags().StringVar(&o.name, "name", "", "upload a single local file under this file name")
	return c
}

type uploadOptions struct {
	id   string
	dir  string
	name string
}

// localFile is a file on disk and the slash-separated path it uploads to.
type localFile struct {
	src  string
	dst  string
	size int64
}

// remotePaths applies --path and --name to the collected files and checks
// the results against the server's path rules.
func remotePaths(args []string, files []localFile, o uploadOptions) error {
	if o.name != "" {
		if len(args) != 1 || len(files) != 1 || files[0].src != args[0] {
			return errors.New("--name renames a single file: pass exactly one file, not a directory")
		}
		if strings.Contains(o.name, "/") {
			return fmt.Errorf("--name %q must not contain /: put directories in --path", o.name)
		}
		files[0].dst = o.name
	}
	for i := range files {
		files[i].dst = joinPath(o.dir, files[i].dst)
		if err := validPath(files[i].dst); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) upload(ctx context.Context, args []string, o uploadOptions) error {
	files, err := collect(args)
	if err != nil {
		return err
	}
	if err := remotePaths(args, files, o); err != nil {
		return err
	}
	c, err := a.client()
	if err != nil {
		return err
	}
	limits, err := c.Limits(ctx)
	if err != nil {
		return err
	}
	var size int64
	for _, f := range files {
		if f.size > limits.MaxUploadBytes {
			return fmt.Errorf("%s is %s, over the %s upload limit", f.src, humanSize(f.size), humanSize(limits.MaxUploadBytes))
		}
		size += f.size
	}
	user, err := c.Me(ctx)
	if err != nil {
		return err
	}
	usage, err := c.Usage(ctx)
	if err != nil {
		return err
	}

	var folder *client.Folder
	if o.id != "" {
		d, err := c.GetFolder(ctx, o.id)
		if err != nil {
			return err
		}
		folder = &d.Folder
	}
	cost := maxCost(folder == nil, size, usage)
	a.progress("Uploading %d files (%s, at most %d credits; you have %d)\n", len(files), humanSize(size), cost, user.Credits)
	if user.Credits < cost {
		a.progress("Your credits may not cover this upload: it stops at the first file they cannot pay for.\n")
	}

	var res fileOutput
	if folder == nil {
		created, err := c.CreateFolder(ctx)
		if err != nil {
			return err
		}
		folder, res.CreditsSpent = &created.Folder, created.CreditsSpent
	}
	res.ID, res.URL = folder.ID, folder.URL
	for _, f := range files {
		put, err := putFile(ctx, c, folder.ID, f)
		if err != nil {
			a.progress("Uploaded %d of %d files to folder %s for %d credits before the error.\n", len(res.Files), len(files), folder.ID, res.CreditsSpent)
			if a.json {
				_ = a.output(res, nil)
			}
			return fmt.Errorf("upload %s: %w", f.dst, err)
		}
		res.Files = append(res.Files, toFileInfo(put.File))
		res.CreditsSpent += put.CreditsSpent
		a.progress("%s\n", put.File.URL)
	}
	if len(res.Files) == 1 {
		// A single upload reads like the MCP upload action: url is the file's.
		f := res.Files[0]
		res.URL, res.FileName, res.Path, res.Size = f.URL, f.FileName, f.Path, f.Size
	}
	return a.output(res, func(w io.Writer) {
		fmt.Fprintf(w, "\nid:      %s\nurl:     %s\ncredits: %d spent\n", folder.ID, folder.URL, res.CreditsSpent)
	})
}

func putFile(ctx context.Context, c *client.Client, folderID string, f localFile) (*client.PutFileResult, error) {
	fh, err := os.Open(f.src)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	return c.PutFile(ctx, folderID, f.dst, fh, f.size)
}

// maxCost is the most an upload of size bytes can cost: a new folder, plus
// every storage block the bytes start beyond what the account stores. The
// server may charge less, since replaced files free their space and blocks
// already paid for this month are not charged again.
func maxCost(newFolder bool, size int64, u *client.Usage) int {
	p := u.Pricing
	cost := max(0, storageBlocks(u.StorageBytes+size, p.StorageBlockBytes)-u.StorageBlocks) * p.StorageCredits
	if newFolder {
		cost += p.FolderCredits
	}
	return cost
}

// storageBlocks is how many started blocks size bytes take.
func storageBlocks(size, block int64) int {
	block = max(block, 1)
	return int((size + block - 1) / block)
}

// collect expands the arguments into the files to upload, rejecting two
// files that would land on the same path.
func collect(args []string) ([]localFile, error) {
	var files []localFile
	seen := map[string]bool{}
	add := func(f localFile) error {
		if seen[f.dst] {
			return fmt.Errorf("two files would upload to %q", f.dst)
		}
		seen[f.dst] = true
		files = append(files, f)
		return nil
	}
	for _, arg := range args {
		info, err := os.Stat(arg)
		if err != nil {
			return nil, err
		}
		switch {
		case info.Mode().IsRegular():
			err = add(localFile{src: arg, dst: filepath.Base(arg), size: info.Size()})
		case info.IsDir():
			err = collectDir(arg, add)
		default:
			err = fmt.Errorf("%s is not a regular file or directory", arg)
		}
		if err != nil {
			return nil, err
		}
	}
	if len(files) == 0 {
		return nil, errors.New("no files to upload")
	}
	return files, nil
}

func collectDir(dir string, add func(localFile) error) error {
	prefix := dirPrefix(dir)
	fsys := os.DirFS(dir)
	return fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		// Stat follows symlinks; dangling links, sockets and devices are skipped.
		info, err := fs.Stat(fsys, p)
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		return add(localFile{src: filepath.Join(dir, filepath.FromSlash(p)), dst: path.Join(prefix, p), size: info.Size()})
	})
}

// dirPrefix is the remote prefix for a directory argument: its own name, or
// none for "dir/" and "." which upload the contents (rsync style).
func dirPrefix(dir string) string {
	if strings.HasSuffix(dir, "/") || strings.HasSuffix(dir, string(filepath.Separator)) {
		return ""
	}
	name := filepath.Base(dir)
	if name == "." || name == ".." {
		return ""
	}
	return name
}
