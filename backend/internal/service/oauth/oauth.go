// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package oauth talks to third-party identity providers (Google, GitHub and
// custom OpenID Connect providers) and turns an authorization code into a
// verified identity.
package oauth

//go:generate mockgen -source=oauth.go -destination=mocks/oauth.go -package=mocks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"
)

const (
	// ProviderGoogle is the provider name for Google sign-in.
	ProviderGoogle = "google"
	// ProviderGitHub is the provider name for GitHub sign-in.
	ProviderGitHub = "github"
)

// ErrNoVerifiedEmail is returned when the provider has no verified email
// for the user; accounts are keyed by verified email.
var ErrNoVerifiedEmail = errors.New("oauth: no verified email")

type (
	// Provider is a single OAuth identity provider.
	Provider interface {
		// Name returns the provider name used in routes and state tokens.
		Name() string
		// Label returns the name shown on the sign-in button.
		Label() string
		// Icon returns the SVG icon for the sign-in button; built-in
		// providers return "" since the frontend ships theirs.
		Icon() string
		// AuthURL returns the consent screen URL carrying state.
		AuthURL(ctx context.Context, state string) (string, error)
		// Exchange trades an authorization code for the user's identity.
		Exchange(ctx context.Context, code string) (*Identity, error)
	}

	// Identity is what a provider tells us about the user.
	Identity struct {
		Subject string
		Email   string
		Name    string
		Picture string
	}

	// Credentials are an OAuth client's settings.
	Credentials struct {
		ClientID     string
		ClientSecret string
		RedirectURL  string
	}
)

func newConfig(c Credentials, endpoint oauth2.Endpoint, scopes ...string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		RedirectURL:  c.RedirectURL,
		Endpoint:     endpoint,
		Scopes:       scopes,
	}
}

// getJSON fetches url with an authenticated client and decodes the body.
func getJSON(client *http.Client, url string, out any) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
