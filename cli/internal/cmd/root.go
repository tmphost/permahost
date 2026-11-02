// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package cmd implements the tmphost commands. Command names and arguments
// mirror the server's MCP tools, so they must stay in sync with them.
package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/tmphost/tmphost/cli/internal/client"
	"github.com/tmphost/tmphost/cli/internal/config"
)

// Options are the process boundaries a command touches, injected so tests
// can run commands without a real terminal, environment or clock.
type Options struct {
	Out    io.Writer
	Err    io.Writer
	Getenv func(string) string
	Now    func() time.Time
	// OpenBrowser shows the login page to the user.
	OpenBrowser func(url string) error
	// Version is printed by --version.
	Version string
}

// loginTimeout bounds how long login waits for the user in the browser.
const loginTimeout = 5 * time.Minute

// app carries the global flags to every command.
type app struct {
	opts   Options
	json   bool
	server string
}

// Execute runs the CLI with args (without the program name) and returns the
// process exit code.
func Execute(args []string, opts Options) int {
	root := newRoot(&app{opts: opts})
	root.SetArgs(args)
	// Ctrl-C cancels the context, so a login waiting for the browser or a
	// long upload stops cleanly instead of dying mid-write.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	err := root.ExecuteContext(ctx)
	if err == nil {
		return 0
	}
	fmt.Fprintf(opts.Err, "error: %s\n", err)
	var apiErr *client.APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized {
		fmt.Fprintln(opts.Err, `Run "tmphost login" to sign in again.`)
	}
	return 1
}

func newRoot(a *app) *cobra.Command {
	root := &cobra.Command{
		Use:   "tmphost",
		Short: "Share files from temporary public folders",
		Long: `tmphost uploads files to temporary public folders that expire on their own.

Sign in with "tmphost login", which opens the browser. The "file" commands
mirror the "file" tool of the server's MCP endpoint.`,
		Version:       a.opts.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(a.opts.Out)
	root.SetErr(a.opts.Err)
	root.PersistentFlags().BoolVar(&a.json, "json", false, "print machine-readable JSON")
	root.PersistentFlags().StringVar(&a.server, "server", "", "server URL (default: $"+config.EnvServer+", the logged-in server or "+config.DefaultServer+")")
	root.AddCommand(newLoginCmd(a), newLogoutCmd(a), newWhoamiCmd(a), newFileCmd(a))
	return root
}

// serverURL picks the server: --server, then $TMPHOSTIO_SERVER, then the one
// logged in to, then the default.
func (a *app) serverURL(cfg config.Config) string {
	for _, s := range []string{a.server, a.opts.Getenv(config.EnvServer), cfg.Server} {
		if s != "" {
			return strings.TrimRight(s, "/")
		}
	}
	return config.DefaultServer
}

// client returns an API client for commands that need to be logged in.
func (a *app) client() (*client.Client, error) {
	cfg, err := config.Load(a.opts.Getenv)
	if err != nil {
		return nil, err
	}
	server := a.serverURL(cfg)
	// Tokens are only ever sent to the server that issued them.
	if !cfg.LoggedIn() || cfg.Server != server {
		return nil, fmt.Errorf(`not logged in to %s: run "tmphost login"`, server)
	}
	return client.New(server, &session{a: a, cfg: cfg}), nil
}

// output prints v as JSON with --json, otherwise lets human write text.
func (a *app) output(v any, human func(w io.Writer)) error {
	if a.json {
		enc := json.NewEncoder(a.opts.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	human(a.opts.Out)
	return nil
}

// progress prints human-only status lines, which would corrupt --json output.
func (a *app) progress(format string, args ...any) {
	if !a.json {
		fmt.Fprintf(a.opts.Out, format, args...)
	}
}

// table writes tab-separated rows as aligned columns.
func table(w io.Writer, header string, rows []string) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, header)
	for _, r := range rows {
		fmt.Fprintln(tw, r)
	}
	tw.Flush()
}

// formatTime renders a timestamp in the user's zone, to the minute.
func formatTime(t time.Time) string {
	return t.Local().Format("2006-01-02 15:04 MST")
}

// humanDuration renders a remaining lifetime with its two largest units,
// rounding up so a folder never looks shorter-lived than it is.
func humanDuration(d time.Duration) string {
	if d <= 0 {
		return "expired"
	}
	m := int(math.Ceil(d.Minutes()))
	days, hours, mins := m/(24*60), m%(24*60)/60, m%60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd%dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh%dm", hours, mins)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}

// humanSize renders a byte count with a binary unit.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
