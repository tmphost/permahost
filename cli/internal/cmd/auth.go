// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package cmd

import (
	"context"
	"fmt"
	"io"
	"runtime"

	"github.com/spf13/cobra"
	"github.com/tmphost/permahost/cli/internal/client"
	"github.com/tmphost/permahost/cli/internal/config"
	"github.com/tmphost/permahost/cli/internal/oauth"
)

// goos is swapped in tests to check how each platform picks a login flow.
var goos = runtime.GOOS

func newLoginCmd(a *app) *cobra.Command {
	var device, browser bool
	cmd := &cobra.Command{
		Use:   "login [--server URL] [--device | --browser]",
		Short: "Sign in with the browser",
		Long: `Sign in to a permahost server (default ` + config.DefaultServer + `) in the browser.

The CLI opens the server's sign-in page and waits up to 5 minutes for you to
approve it. The resulting tokens are saved in the config file, readable only
by you, and refreshed automatically.

On a machine without a browser, such as over SSH or in a container, the CLI
shows a link and a code instead: open the link on any device, enter the
code, and approve. That is the default when $SSH_CONNECTION or $SSH_TTY is
set, or on Linux without $DISPLAY or $WAYLAND_DISPLAY; --device and
--browser pick a flow yourself.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !browser && (device || a.headless()) {
				return a.login(cmd.Context(), (*oauth.Client).DeviceLogin)
			}
			return a.login(cmd.Context(), (*oauth.Client).Login)
		},
	}
	cmd.Flags().BoolVar(&device, "device", false, "sign in with a code on another device, for machines without a browser")
	cmd.Flags().BoolVar(&browser, "browser", false, "sign in with this machine's browser")
	cmd.MarkFlagsMutuallyExclusive("device", "browser")
	return cmd
}

// headless tells whether no browser here can reach the CLI's loopback
// port: over SSH, or on Linux without a display.
func (a *app) headless() bool {
	env := a.opts.Getenv
	if env("SSH_CONNECTION") != "" || env("SSH_TTY") != "" {
		return true
	}
	return goos == "linux" && env("DISPLAY") == "" && env("WAYLAND_DISPLAY") == ""
}

// loginFlow signs in with one of the OAuth client's flows.
type loginFlow func(c *oauth.Client, ctx context.Context, o oauth.LoginOptions) (*oauth.Token, error)

func (a *app) login(ctx context.Context, flow loginFlow) error {
	cfg, err := config.Load(a.opts.Getenv)
	if err != nil {
		return err
	}
	server := a.serverURL(cfg)
	// The URL and progress go to stderr so stdout stays clean for --json.
	tok, err := flow(a.oauth(server), ctx, oauth.LoginOptions{Open: a.opts.OpenBrowser, Out: a.opts.Err, Timeout: loginTimeout})
	if err != nil {
		return err
	}
	cfg = config.Config{Server: server, AccessToken: tok.AccessToken, RefreshToken: tok.RefreshToken, Expiry: tok.Expiry}
	if err := config.Save(a.opts.Getenv, cfg); err != nil {
		return err
	}
	user, err := client.New(server, &session{a: a, cfg: cfg}).Me(ctx)
	if err != nil {
		return err
	}
	return a.output(userView{User: user}, func(w io.Writer) {
		fmt.Fprintf(w, "Logged in to %s as %s (%d/%d credits)\n", server, user.Email, user.Credits, user.CreditsLimit)
	})
}

type userView struct {
	User  *client.User  `json:"user"`
	Usage *client.Usage `json:"usage,omitempty"`
}

func newLogoutCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Forget the saved tokens",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			cfg, err := config.Load(a.opts.Getenv)
			if err != nil {
				return err
			}
			if cfg.LoggedIn() {
				// The server is kept so the next login goes to the same place.
				if err := config.Save(a.opts.Getenv, config.Config{Server: cfg.Server}); err != nil {
					return err
				}
			}
			return a.output(map[string]bool{"loggedOut": true}, func(w io.Writer) {
				fmt.Fprintln(w, "Logged out.")
			})
		},
	}
}

func newWhoamiCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the signed-in user, credits, usage and prices",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := a.client()
			if err != nil {
				return err
			}
			user, err := c.Me(cmd.Context())
			if err != nil {
				return err
			}
			usage, err := c.Usage(cmd.Context())
			if err != nil {
				return err
			}
			return a.output(userView{User: user, Usage: usage}, func(w io.Writer) {
				p := usage.Pricing
				table(w, "Email:\t"+user.Email, []string{
					"Name:\t" + user.Name,
					fmt.Sprintf("Credits:\t%d/%d", user.Credits, user.CreditsLimit),
					"Renews at:\t" + formatTime(user.CreditsRenewAt),
					fmt.Sprintf("Hosting:\t%d folders, %s in %d storage blocks, %d domains", usage.Folders, humanSize(usage.StorageBytes), usage.StorageBlocks, usage.Domains),
					fmt.Sprintf("Monthly cost:\t%d credits", usage.MonthlyCredits),
					fmt.Sprintf("Prices:\t%d credits a month per folder, %d per started %s stored, %d per domain (+%d to set it up)",
						p.FolderCredits, p.StorageCredits, humanSize(p.StorageBlockBytes), p.DomainCredits, p.DomainSetupCredits),
				})
			})
		},
	}
}
