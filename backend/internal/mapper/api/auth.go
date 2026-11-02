// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package api maps between HTTP requests/responses and domain entities.
package api

import (
	"encoding/json"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/mustafaturan/monoflake"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/view"
)

const (
	// SessionCookie holds the signed session token.
	SessionCookie = "tmphost_session"
	// StateCookie pins the OAuth state to the browser that started login.
	StateCookie = "tmphost_oauth_state"
	// LocalUserID is the fiber.Ctx local the auth middleware stores the
	// authenticated user ID under.
	LocalUserID = "userID"
	// LocalAnonymous is the fiber.Ctx local set to true when LocalUserID
	// is an anonymous visitor's.
	LocalAnonymous = "anonymous"
)

// FromHTTPRequestToStartLoginRequestEntity reads GET /auth/:provider/login.
func FromHTTPRequestToStartLoginRequestEntity(c fiber.Ctx) *entity.StartLoginRequest {
	provider := c.Params("provider")
	if provider == "" {
		return nil
	}
	return &entity.StartLoginRequest{Provider: provider, RedirectURL: c.Query("redirect_url")}
}

// FromHTTPRequestToCompleteLoginRequestEntity reads the provider callback.
// It returns nil when the provider sent no code (e.g. the user cancelled).
func FromHTTPRequestToCompleteLoginRequestEntity(c fiber.Ctx) *entity.CompleteLoginRequest {
	provider, code := c.Params("provider"), c.Query("code")
	if provider == "" || code == "" {
		return nil
	}
	return &entity.CompleteLoginRequest{
		Provider:    provider,
		Code:        code,
		State:       c.Query("state"),
		StateCookie: c.Cookies(StateCookie),
	}
}

// FromHTTPRequestToAuthenticateRequestEntity reads an OAuth access token
// from the Authorization header, or else the session cookie.
func FromHTTPRequestToAuthenticateRequestEntity(c fiber.Ctx) *entity.AuthenticateRequest {
	if scheme, key, ok := strings.Cut(c.Get(fiber.HeaderAuthorization), " "); ok && strings.EqualFold(scheme, "Bearer") && key != "" {
		return &entity.AuthenticateRequest{AccessToken: key}
	}
	tok := c.Cookies(SessionCookie)
	if tok == "" {
		return nil
	}
	return &entity.AuthenticateRequest{SessionToken: tok}
}

// FromHTTPRequestToAuthenticateAnonymousRequestEntity reads what
// identifies a request that has no session: its client IP and where the
// browser sent it from.
func FromHTTPRequestToAuthenticateAnonymousRequestEntity(c fiber.Ctx) *entity.AuthenticateAnonymousRequest {
	return &entity.AuthenticateAnonymousRequest{IP: c.IP(), Origin: c.Get(fiber.HeaderOrigin), Referer: c.Get(fiber.HeaderReferer)}
}

// FromGetAuthConfigResponseEntityToHTTPResponse renders GET /auth/config.
func FromGetAuthConfigResponseEntityToHTTPResponse(rs *entity.GetAuthConfigResponse) []byte {
	providers := make([]view.AuthProvider, len(rs.Providers))
	for i, p := range rs.Providers {
		providers[i] = view.AuthProvider{ID: p.ID, Name: p.Name, Icon: p.Icon}
	}
	return marshal(view.AuthConfig{Providers: providers})
}

// FromErrorToHTTPResponse renders an API error body.
func FromErrorToHTTPResponse(msg string) []byte {
	return marshal(view.Error{Error: msg})
}

// FromIDToBase62 renders an int64 ID the way the API exposes it.
func FromIDToBase62(id int64) string {
	return monoflake.ID(id).String()
}

// marshal encodes views, which are plain structs that always encode.
func marshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
