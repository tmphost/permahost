// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package oauth

import (
	"context"
	"fmt"
	"strconv"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

const _githubAPIURL = "https://api.github.com"

type githubProvider struct {
	config *oauth2.Config
	apiURL string
}

// NewGitHub returns the GitHub sign-in provider.
func NewGitHub(c Credentials) Provider {
	return newGitHub(c, github.Endpoint, _githubAPIURL)
}

func newGitHub(c Credentials, endpoint oauth2.Endpoint, apiURL string) *githubProvider {
	return &githubProvider{
		config: newConfig(c, endpoint, "read:user", "user:email"),
		apiURL: apiURL,
	}
}

func (p *githubProvider) Name() string { return ProviderGitHub }

func (p *githubProvider) Label() string { return "GitHub" }

func (p *githubProvider) Icon() string { return "" }

func (p *githubProvider) AuthURL(_ context.Context, state string) (string, error) {
	return p.config.AuthCodeURL(state), nil
}

func (p *githubProvider) Exchange(ctx context.Context, code string) (*Identity, error) {
	tok, err := p.config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("github oauth exchange: %w", err)
	}
	client := p.config.Client(ctx, tok)

	var user struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := getJSON(client, p.apiURL+"/user", &user); err != nil {
		return nil, fmt.Errorf("github user: %w", err)
	}

	// The public profile email is not guaranteed to be verified, so always
	// read the verified primary address from /user/emails.
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := getJSON(client, p.apiURL+"/user/emails", &emails); err != nil {
		return nil, fmt.Errorf("github emails: %w", err)
	}
	email := ""
	for _, e := range emails {
		if e.Verified && (e.Primary || email == "") {
			email = e.Email
		}
	}
	if email == "" {
		return nil, ErrNoVerifiedEmail
	}

	name := user.Name
	if name == "" {
		name = user.Login
	}
	return &Identity{
		Subject: strconv.FormatInt(user.ID, 10),
		Email:   email,
		Name:    name,
		Picture: user.AvatarURL,
	}, nil
}
