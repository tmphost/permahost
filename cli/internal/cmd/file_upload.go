// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/tmphost/tmphost/cli/internal/client"
)

func newFileUploadCmd(a *app) *cobra.Command {
	var o uploadOptions
	c := &cobra.Command{
		Use:   "upload <local path>... [--id ID | --ttl 30m] [--path DIR] [--name NAME]",
		Short: "Upload files to a temporary public folder",
		Long: `Upload files to a temporary public folder and print their public URLs.

A file is uploaded under its base name, or under --name when it is the only
argument. A directory is uploaded recursively under its own name ("site"
uploads site/index.html), while a directory with a trailing slash, or ".",
uploads its contents (rsync style: "site/" uploads index.html). Symlinks to
files are followed; other non-regular files are skipped. --path puts
everything under a directory inside the folder.

Without --id a new folder is created that expires after --ttl. With --id the
files are added to that folder and keep its expiry; a file already at the same
path is replaced and keeps its URL, so re-uploading updates it.

Each file costs one credit per started MB of its size per started billing
block of the folder's remaining lifetime; the total is checked against your credits up front.`,
		Example: `  tmphost file upload report.pdf --ttl 2h
  tmphost file upload site/                # index.html, css/app.css, ...
  tmphost file upload site --id rs         # site/index.html, ... in folder rs
  tmphost file upload out.txt --path logs --name today.txt`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			o.ttlSet = cmd.Flags().Changed("ttl")
			return a.upload(cmd.Context(), args, o)
		},
	}
	c.Flags().StringVar(&o.id, "id", "", "upload into this existing folder instead of creating one, replacing files at the same paths")
	c.Flags().DurationVar(&o.ttl, "ttl", 30*time.Minute, "lifetime of a new folder, in whole minutes (e.g. 30m, 2h, 72h)")
	c.Flags().StringVar(&o.dir, "path", "", "directory inside the folder, such as site/css")
	c.Flags().StringVar(&o.name, "name", "", "upload a single local file under this file name")
	c.MarkFlagsMutuallyExclusive("id", "ttl")
	return c
}

type uploadOptions struct {
	id     string
	ttl    time.Duration
	ttlSet bool
	dir    string
	name   string
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
	for _, f := range files {
		if f.size > limits.MaxUploadBytes {
			return fmt.Errorf("%s is %s, over the %s upload limit", f.src, humanSize(f.size), humanSize(limits.MaxUploadBytes))
		}
	}
	user, err := c.Me(ctx)
	if err != nil {
		return err
	}

	var folder *client.Folder
	var minutes int
	if o.id != "" {
		d, err := c.GetFolder(ctx, o.id)
		if err != nil {
			return err
		}
		folder = &d.Folder
		minutes = int(math.Ceil(folder.ExpiresAt.Sub(a.opts.Now()).Minutes()))
	} else if minutes, err = ttlMinutes(o.ttl, o.ttlSet, limits); err != nil {
		return err
	}

	cost := 0
	for _, f := range files {
		cost += billingUnits(f.size, limits.BillingUnitBytes) * billingBlocks(minutes, limits.BillingBlockMinutes)
	}
	a.progress("Uploading %d files (cost: %d credits)\n", len(files), cost)
	if user.Credits < cost {
		return fmt.Errorf("not enough credits: this upload costs %d but you have %d (renews at %s)",
			cost, user.Credits, formatTime(user.CreditsRenewAt))
	}

	if folder == nil {
		if folder, err = c.CreateFolder(ctx, minutes); err != nil {
			return err
		}
	}
	res := fileOutput{ID: folder.ID, URL: folder.URL, ExpiresAt: rfc3339(folder.ExpiresAt)}
	for _, f := range files {
		put, err := putFile(ctx, c, folder.ID, f)
		if err != nil {
			a.progress("Uploaded %d of %d files to folder %s before the error.\n", len(res.Files), len(files), folder.ID)
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
		fmt.Fprintf(w, "\nid:      %s\nurl:     %s\nexpires: %s\n", folder.ID, folder.URL, a.expiry(folder.ExpiresAt))
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

// ttlMinutes validates --ttl against the server limits, falling back to the
// server default when the flag was not given.
func ttlMinutes(ttl time.Duration, set bool, l *client.Limits) (int, error) {
	if !set {
		return l.DefaultTTLMinutes, nil
	}
	if ttl%time.Minute != 0 {
		return 0, fmt.Errorf("--ttl %s is not a whole number of minutes", ttl)
	}
	m := int(ttl / time.Minute)
	if m < 1 || m > l.MaxTTLMinutes {
		return 0, fmt.Errorf("--ttl must be between 1m and %dm", l.MaxTTLMinutes)
	}
	return m, nil
}

// billingBlocks is how many started blocks a lifetime spans, at least one
// because even an about-to-expire folder is charged for the upload.
func billingBlocks(minutes, block int) int {
	block = max(block, 1)
	return max(1, (minutes+block-1)/block)
}

// billingUnits is how many started units a file's size spans, at least
// one because even an empty file is charged for the upload.
func billingUnits(size, unit int64) int {
	unit = max(unit, 1)
	return int(max(1, (size+unit-1)/unit))
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
