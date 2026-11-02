// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package cmd

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/tmphost/tmphost/cli/internal/client"
)

// fileOutput mirrors the MCP file tool's FileOutput, so scripts read the
// same fields whether they call the CLI with --json or the tool.
type fileOutput struct {
	ID           string `json:"id,omitempty"`
	URL          string `json:"url,omitempty"`
	ExpiresAt    string `json:"expiresAt,omitempty"`
	FileName     string `json:"fileName,omitempty"`
	Path         string `json:"path,omitempty"`
	Size         int64  `json:"size,omitempty"`
	CreditsSpent int    `json:"creditsSpent,omitempty"`
	// Base64Content and TextContent are pointers so an empty file still
	// carries its (empty) content field.
	Base64Content *string    `json:"base64Content,omitempty"`
	TextContent   *string    `json:"textContent,omitempty"`
	Files         []fileInfo `json:"files,omitempty"`
}

// fileInfo mirrors the MCP FileInfo: a file split into directory and name.
type fileInfo struct {
	FileName string `json:"fileName"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	URL      string `json:"url"`
}

func toFileInfo(f client.File) fileInfo {
	dir, name := splitPath(f.Path)
	return fileInfo{FileName: name, Path: dir, Size: f.Size, URL: f.URL}
}

// splitPath splits a folder-relative path into its directory and base name.
func splitPath(p string) (dir, name string) {
	i := strings.LastIndex(p, "/")
	if i < 0 {
		return "", p
	}
	return p[:i], p[i+1:]
}

// joinPath puts a file name under a directory given by --path, which may
// carry stray slashes.
func joinPath(dir, name string) string {
	if dir = strings.Trim(dir, "/"); dir == "" {
		return name
	}
	return dir + "/" + name
}

// filePath joins --path and a file name into a folder-relative path and
// validates it.
func filePath(dir, name string) (string, error) {
	if strings.Contains(name, "/") {
		return "", fmt.Errorf("file name %q must not contain /: put directories in --path", name)
	}
	p := joinPath(dir, name)
	return p, validPath(p)
}

// The server's path rules, checked up front so a bad path fails before
// anything is uploaded or charged.
const (
	maxDirDepth     = 5
	maxPathBytes    = 512
	maxSegmentBytes = 255
	// reservedPrefix marks the server's in-progress uploads.
	reservedPrefix = ".upload-"
)

// validPath reports why p is not a clean folder-relative file path: slash
// separated, at most maxDirDepth directories deep, no empty, "." or ".."
// segments, no backslashes or control characters, within the length limits.
func validPath(p string) error {
	switch {
	case p == "":
		return errors.New("path is empty")
	case len(p) > maxPathBytes:
		return fmt.Errorf("path %q is longer than %d bytes", p, maxPathBytes)
	case !utf8.ValidString(p):
		return fmt.Errorf("path %q is not valid UTF-8", p)
	case strings.ContainsFunc(p, func(r rune) bool { return r < 0x20 || r == 0x7f || r == '\\' }):
		return fmt.Errorf("path %q contains a backslash or control character", p)
	}
	segs := strings.Split(p, "/")
	if len(segs) > maxDirDepth+1 {
		return fmt.Errorf("path %q is more than %d directories deep", p, maxDirDepth)
	}
	for _, seg := range segs {
		switch {
		case seg == "" || seg == "." || seg == "..":
			return fmt.Errorf("path %q has an empty, \".\" or \"..\" segment", p)
		case len(seg) > maxSegmentBytes:
			return fmt.Errorf("path %q has a segment longer than %d bytes", p, maxSegmentBytes)
		case strings.HasPrefix(seg, reservedPrefix):
			return fmt.Errorf("path %q has a segment starting with %q, which is reserved", p, reservedPrefix)
		}
	}
	return nil
}

// rfc3339 matches how the MCP tool renders times.
func rfc3339(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// expiry renders a folder's expiry for humans.
func (a *app) expiry(t time.Time) string {
	return fmt.Sprintf("%s (in %s)", formatTime(t), humanDuration(t.Sub(a.opts.Now())))
}

func newFileCmd(a *app) *cobra.Command {
	c := &cobra.Command{
		Use:   "file",
		Short: "Upload, download and share files (mirrors the MCP file tool)",
		Long: `Upload, download and share files in temporary folders.

The upload, download, get and url subcommands mirror the actions of the
server's MCP "file" tool: --id, --ttl and --path are its id, ttl and path
arguments, and the file name is its fileName. ls and rm are extras for
managing folders.`,
	}
	c.AddCommand(newFileUploadCmd(a), newFileDownloadCmd(a), newFileGetCmd(a), newFileURLCmd(a), newFileLsCmd(a), newFileRmCmd(a))
	return c
}

func newFileGetCmd(a *app) *cobra.Command {
	var id string
	c := &cobra.Command{
		Use:   "get --id ID",
		Short: "List a folder's files",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			d, err := c.GetFolder(cmd.Context(), id)
			if err != nil {
				return err
			}
			out := fileOutput{ID: d.Folder.ID, URL: d.Folder.URL, ExpiresAt: rfc3339(d.Folder.ExpiresAt), Files: make([]fileInfo, len(d.Files))}
			for i, f := range d.Files {
				out.Files[i] = toFileInfo(f)
			}
			return a.output(out, func(w io.Writer) {
				if len(out.Files) == 0 {
					fmt.Fprintln(w, "No files.")
				} else {
					rows := make([]string, len(out.Files))
					for i, f := range out.Files {
						rows[i] = f.FileName + "\t" + f.Path + "\t" + humanSize(f.Size) + "\t" + f.URL
					}
					table(w, "FILE NAME\tPATH\tSIZE\tURL", rows)
				}
				fmt.Fprintf(w, "\nid:      %s\nurl:     %s\nexpires: %s\n", out.ID, out.URL, a.expiry(d.Folder.ExpiresAt))
			})
		},
	}
	c.Flags().StringVar(&id, "id", "", "folder id")
	_ = c.MarkFlagRequired("id")
	return c
}

func newFileURLCmd(a *app) *cobra.Command {
	var id, dir string
	c := &cobra.Command{
		Use:   "url --id ID [--path DIR] [fileName]",
		Short: "Print the public URL of a folder, a directory or a file",
		Long: `Print the public URL of a folder (--id), a directory in it (--path) or a
file (fileName, optionally under --path). A directory URL ends with "/".
The file does not have to exist yet.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) == 1 {
				name = args[0]
			}
			dir = strings.Trim(dir, "/")
			var err error
			if name != "" {
				_, err = filePath(dir, name)
			} else if dir != "" {
				err = validPath(dir)
			}
			if err != nil {
				return err
			}
			c, err := a.client()
			if err != nil {
				return err
			}
			d, err := c.GetFolder(cmd.Context(), id)
			if err != nil {
				return err
			}
			var segs []string
			if dir != "" {
				segs = strings.Split(dir, "/")
			}
			// An empty name leaves a trailing slash on a directory URL; the
			// folder URL already ends with one.
			segs = append(segs, name)
			for i, s := range segs {
				segs[i] = url.PathEscape(s)
			}
			out := fileOutput{ID: d.Folder.ID, URL: d.Folder.URL + strings.Join(segs, "/"), ExpiresAt: rfc3339(d.Folder.ExpiresAt), FileName: name, Path: dir}
			return a.output(out, func(w io.Writer) {
				fmt.Fprintln(w, out.URL)
			})
		},
	}
	c.Flags().StringVar(&id, "id", "", "folder id")
	c.Flags().StringVar(&dir, "path", "", "directory inside the folder, such as site/css")
	_ = c.MarkFlagRequired("id")
	return c
}

func newFileLsCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List your live folders",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			folders, err := c.ListFolders(cmd.Context())
			if err != nil {
				return err
			}
			return a.output(map[string]any{"folders": folders}, func(w io.Writer) {
				if len(folders) == 0 {
					fmt.Fprintln(w, "No folders.")
					return
				}
				rows := make([]string, len(folders))
				for i, f := range folders {
					rows[i] = f.ID + "\t" + humanDuration(f.ExpiresAt.Sub(a.opts.Now())) + "\t" + f.URL
				}
				table(w, "ID\tEXPIRES IN\tURL", rows)
			})
		},
	}
}

func newFileRmCmd(a *app) *cobra.Command {
	var id string
	c := &cobra.Command{
		Use:   "rm --id ID [path]",
		Short: "Delete a folder, or one file in it",
		Long:  `Delete a whole folder, or only the file at path (such as site/index.html).`,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var p string
			if len(args) == 1 {
				p = strings.Trim(args[0], "/")
				if err := validPath(p); err != nil {
					return err
				}
			}
			c, err := a.client()
			if err != nil {
				return err
			}
			if p == "" {
				if err := c.DeleteFolder(cmd.Context(), id); err != nil {
					return err
				}
				return a.output(map[string]any{"deleted": true, "id": id}, func(w io.Writer) {
					fmt.Fprintf(w, "Deleted folder %s.\n", id)
				})
			}
			if err := c.DeleteFile(cmd.Context(), id, p); err != nil {
				return err
			}
			dir, name := splitPath(p)
			return a.output(map[string]any{"deleted": true, "id": id, "fileName": name, "path": dir}, func(w io.Writer) {
				fmt.Fprintf(w, "Deleted %s from folder %s.\n", p, id)
			})
		},
	}
	c.Flags().StringVar(&id, "id", "", "folder id")
	_ = c.MarkFlagRequired("id")
	return c
}
