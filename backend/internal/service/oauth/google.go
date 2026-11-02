// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package oauth

import (
	"context"
	"fmt"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const _googleUserInfoURL = "https://www.googleapis.com/oauth2/v2/userinfo"

type googleProvider struct {
	config      *oauth2.Config
	userInfoURL string
}

// NewGoogle returns the Google sign-in provider.
func NewGoogle(c Credentials) Provider {
	return newGoogle(c, google.Endpoint, _googleUserInfoURL)
}

func newGoogle(c Credentials, endpoint oauth2.Endpoint, userInfoURL string) *googleProvider {
	return &googleProvider{
		config:      newConfig(c, endpoint, "openid", "email", "profile"),
		userInfoURL: userInfoURL,
	}
}

func (p *googleProvider) Name() string { return ProviderGoogle }

func (p *googleProvider) Label() string { return "Google" }

func (p *googleProvider) Icon() string { return "" }

func (p *googleProvider) AuthURL(_ context.Context, state string) (string, error) {
	return p.config.AuthCodeURL(state), nil
}

func (p *googleProvider) Exchange(ctx context.Context, code string) (*Identity, error) {
	tok, err := p.config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("google oauth exchange: %w", err)
	}
	var info struct {
		ID            string `json:"id"`
		Email         string `json:"email"`
		VerifiedEmail bool   `json:"verified_email"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}
	if err := getJSON(p.config.Client(ctx, tok), p.userInfoURL, &info); err != nil {
		return nil, fmt.Errorf("google userinfo: %w", err)
	}
	if info.Email == "" || !info.VerifiedEmail {
		return nil, ErrNoVerifiedEmail
	}
	return &Identity{
		Subject: info.ID,
		Email:   info.Email,
		Name:    info.Name,
		Picture: info.Picture,
	}, nil
}
