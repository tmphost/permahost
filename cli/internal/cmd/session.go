// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/tmphost/tmphost/cli/internal/config"
	"github.com/tmphost/tmphost/cli/internal/oauth"
)

// refreshMargin refreshes tokens this long before they expire, so a request
// never starts with a token that dies in flight.
const refreshMargin = time.Minute

var errSessionExpired = errors.New(`session expired: run "tmphost login"`)

// session is the signed-in user's tokens. It implements client.TokenSource,
// refreshing the access token when needed and saving the rotated tokens.
type session struct {
	a   *app
	cfg config.Config
}

// Token returns the access token, refreshing it when it expires within
// refreshMargin.
func (s *session) Token(ctx context.Context) (string, error) {
	if s.cfg.AccessToken != "" && s.a.opts.Now().Add(refreshMargin).Before(s.cfg.Expiry) {
		return s.cfg.AccessToken, nil
	}
	return s.Refresh(ctx)
}

// Refresh trades the refresh token for new tokens and saves them.
func (s *session) Refresh(ctx context.Context) (string, error) {
	if s.cfg.RefreshToken == "" {
		return "", errSessionExpired
	}
	tok, err := s.a.oauth(s.cfg.Server).Refresh(ctx, s.cfg.RefreshToken)
	var oauthErr *oauth.Error
	if errors.As(err, &oauthErr) {
		// The server rejected the refresh token: only a new login helps.
		return "", fmt.Errorf("%w (%v)", errSessionExpired, err)
	}
	if err != nil {
		return "", fmt.Errorf("refresh session: %w", err)
	}
	s.cfg.AccessToken, s.cfg.Expiry = tok.AccessToken, tok.Expiry
	if tok.RefreshToken != "" {
		s.cfg.RefreshToken = tok.RefreshToken
	}
	if err := config.Save(s.a.opts.Getenv, s.cfg); err != nil {
		// The new token still works for this run; the next one will ask
		// for a login if the old refresh token was rotated away.
		fmt.Fprintf(s.a.opts.Err, "warning: could not save the refreshed session: %v\n", err)
	}
	return s.cfg.AccessToken, nil
}

// oauth returns an OAuth client for server.
func (a *app) oauth(server string) *oauth.Client {
	return &oauth.Client{Server: server, HTTP: &http.Client{Timeout: 30 * time.Second}, Now: a.opts.Now}
}
