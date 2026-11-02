// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/tmphost/tmphost/cli/internal/client"
	"github.com/tmphost/tmphost/cli/internal/config"
	"github.com/tmphost/tmphost/cli/internal/oauth"
)

func newLoginCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "login [--server URL]",
		Short: "Sign in with the browser",
		Long: `Sign in to a tmphost server (default ` + config.DefaultServer + `) in the browser.

The CLI opens the server's sign-in page and waits up to 5 minutes for you to
approve it. The resulting tokens are saved in the config file, readable only
by you, and refreshed automatically.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return a.login(cmd.Context())
		},
	}
}

func (a *app) login(ctx context.Context) error {
	cfg, err := config.Load(a.opts.Getenv)
	if err != nil {
		return err
	}
	server := a.serverURL(cfg)
	// The URL and progress go to stderr so stdout stays clean for --json.
	tok, err := a.oauth(server).Login(ctx, oauth.LoginOptions{Open: a.opts.OpenBrowser, Out: a.opts.Err, Timeout: loginTimeout})
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
	return a.output(userView{user}, func(w io.Writer) {
		fmt.Fprintf(w, "Logged in to %s as %s (%d/%d credits)\n", server, user.Email, user.Credits, user.CreditsLimit)
	})
}

type userView struct {
	User *client.User `json:"user"`
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
		Short: "Show the signed-in user and remaining credits",
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
			return a.output(userView{user}, func(w io.Writer) {
				table(w, "Email:\t"+user.Email, []string{
					"Name:\t" + user.Name,
					fmt.Sprintf("Credits:\t%d/%d", user.Credits, user.CreditsLimit),
					"Renews at:\t" + formatTime(user.CreditsRenewAt),
				})
			})
		},
	}
}
