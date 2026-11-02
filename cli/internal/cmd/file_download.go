// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package cmd

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

func newFileDownloadCmd(a *app) *cobra.Command {
	var id, dir, out, format string
	var force bool
	c := &cobra.Command{
		Use:   "download --id ID [--path DIR] <fileName> [-o OUT] [--format FORMAT]",
		Short: "Download a file",
		Long: `Download a file from a folder to -o, or to ./<fileName> by default.

An existing file is not overwritten unless --force is given. "-o -" writes
the bytes to stdout; combined with --json the content goes in the JSON
instead, as in the MCP download action: in base64Content by default, or as
UTF-8 text in textContent with --format textContent.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if format != formatBase64 && format != formatText {
				return fmt.Errorf("--format must be %s or %s", formatBase64, formatText)
			}
			p, err := filePath(dir, args[0])
			if err != nil {
				return err
			}
			if out == "" {
				out = args[0]
			}
			return a.download(cmd.Context(), id, p, out, format, force)
		},
	}
	c.Flags().StringVar(&id, "id", "", "folder id")
	c.Flags().StringVar(&dir, "path", "", "directory of the file inside the folder")
	c.Flags().StringVarP(&out, "output", "o", "", `where to write the file ("-" for stdout; default ./NAME)`)
	c.Flags().BoolVar(&force, "force", false, "overwrite an existing output file")
	c.Flags().StringVar(&format, "format", formatBase64, "JSON content field with -o - --json: base64Content or textContent")
	_ = c.MarkFlagRequired("id")
	return c
}

// The content formats of a JSON download, as in the MCP tool.
const (
	formatBase64 = "base64Content"
	formatText   = "textContent"
)

func (a *app) download(ctx context.Context, id, p, out, format string, force bool) error {
	c, err := a.client()
	if err != nil {
		return err
	}
	body, err := c.GetFile(ctx, id, p)
	if err != nil {
		return err
	}
	defer body.Close()
	dir, name := splitPath(p)
	res := fileOutput{ID: id, FileName: name, Path: dir}

	if out == "-" {
		if !a.json {
			_, err := io.Copy(a.opts.Out, body)
			return err
		}
		data, err := io.ReadAll(body)
		if err != nil {
			return err
		}
		res.Size = int64(len(data))
		if format == formatText {
			if !utf8.Valid(data) {
				return fmt.Errorf("%s is not UTF-8 text: use --format %s", p, formatBase64)
			}
			text := string(data)
			res.TextContent = &text
		} else {
			encoded := base64.StdEncoding.EncodeToString(data)
			res.Base64Content = &encoded
		}
		return a.output(res, nil)
	}

	// The file is only created once the server has answered, so a failed
	// request leaves nothing behind.
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if force {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	f, err := os.OpenFile(out, flags, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%s already exists: use --force to overwrite it", out)
	}
	if err != nil {
		return err
	}
	res.Size, err = io.Copy(f, body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		// A truncated download is worse than none.
		_ = os.Remove(out)
		return fmt.Errorf("download %s: %w", p, err)
	}
	return a.output(res, func(w io.Writer) {
		fmt.Fprintf(w, "Downloaded %s (%s) to %s\n", p, humanSize(res.Size), out)
	})
}
