// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

const (
	_discoveryPath    = "/.well-known/openid-configuration"
	_discoveryTimeout = 10 * time.Second
)

type (
	// OIDCParams configure a custom OpenID Connect provider.
	OIDCParams struct {
		// Name is the route name; Label and Icon are shown on the button.
		Name  string
		Label string
		Icon  string
		// Issuer is the OpenID Connect issuer URL. The endpoints are
		// discovered from its /.well-known/openid-configuration.
		Issuer      string
		Credentials Credentials
	}

	oidcProvider struct {
		params OIDCParams
		client *http.Client

		mu sync.Mutex
		// config and userInfoURL are set by the first successful discovery.
		config      *oauth2.Config
		userInfoURL string
	}

	oidcDiscovery struct {
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
		UserInfoEndpoint      string `json:"userinfo_endpoint"`
	}
)

// NewOIDC returns a custom OpenID Connect sign-in provider. Discovery runs on
// first use, so a provider that is down at startup works once it is back.
func NewOIDC(p OIDCParams) Provider {
	return &oidcProvider{params: p, client: &http.Client{Timeout: _discoveryTimeout}}
}

func (p *oidcProvider) Name() string { return p.params.Name }

func (p *oidcProvider) Label() string { return p.params.Label }

func (p *oidcProvider) Icon() string { return p.params.Icon }

func (p *oidcProvider) AuthURL(ctx context.Context, state string) (string, error) {
	config, _, err := p.discover(ctx)
	if err != nil {
		return "", err
	}
	return config.AuthCodeURL(state), nil
}

func (p *oidcProvider) Exchange(ctx context.Context, code string) (*Identity, error) {
	config, userInfoURL, err := p.discover(ctx)
	if err != nil {
		return nil, err
	}
	tok, err := config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("%s oauth exchange: %w", p.params.Name, err)
	}
	var info struct {
		Subject string `json:"sub"`
		Email   string `json:"email"`
		// Some providers send email_verified as a string.
		EmailVerified any    `json:"email_verified"`
		Name          string `json:"name"`
		Picture       string `json:"picture"`
	}
	if err := getJSON(config.Client(ctx, tok), userInfoURL, &info); err != nil {
		return nil, fmt.Errorf("%s userinfo: %w", p.params.Name, err)
	}
	if info.Subject == "" {
		return nil, fmt.Errorf("%s userinfo: no subject", p.params.Name)
	}
	if info.Email == "" || (info.EmailVerified != true && info.EmailVerified != "true") {
		return nil, ErrNoVerifiedEmail
	}
	return &Identity{Subject: info.Subject, Email: info.Email, Name: info.Name, Picture: info.Picture}, nil
}

// discover fetches the issuer's endpoints once; a failure is retried on the
// next call.
func (p *oidcProvider) discover(ctx context.Context) (*oauth2.Config, string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.config != nil {
		return p.config, p.userInfoURL, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.params.Issuer+_discoveryPath, nil)
	if err != nil {
		return nil, "", fmt.Errorf("%s discovery: %w", p.params.Name, err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("%s discovery: %w", p.params.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%s discovery: status %d", p.params.Name, resp.StatusCode)
	}
	var d oidcDiscovery
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
		return nil, "", fmt.Errorf("%s discovery: %w", p.params.Name, err)
	}
	if d.AuthorizationEndpoint == "" || d.TokenEndpoint == "" || d.UserInfoEndpoint == "" {
		return nil, "", errors.New(p.params.Name + " discovery: authorization, token and userinfo endpoints are required")
	}
	p.config = newConfig(p.params.Credentials, oauth2.Endpoint{
		AuthURL:  d.AuthorizationEndpoint,
		TokenURL: d.TokenEndpoint,
	}, "openid", "email", "profile")
	p.userInfoURL = d.UserInfoEndpoint
	return p.config, p.userInfoURL, nil
}
