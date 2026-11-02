// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v3"

	oauthctrl "github.com/tmphost/tmphost/backend/internal/controller/oauthserver"
	mapper "github.com/tmphost/tmphost/backend/internal/mapper/api"
)

// The well-known metadata paths (RFC 8414, RFC 9728).
const (
	ServerMetadataPath   = "/.well-known/oauth-authorization-server"
	ResourceMetadataPath = "/.well-known/oauth-protected-resource"
)

// _pageCSP locks the consent page down: no scripts but the theme one, no
// frames around it, inline styles only.
var _pageCSP = "default-src 'none'; script-src " + _themeScriptHash + "; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; frame-ancestors 'none'"

// OAuth is tmphost's own OAuth server, for MCP clients and the CLI.
type OAuth struct {
	controller oauthctrl.Controller
	basePath   string
}

// ServerMetadata returns the authorization server metadata.
func (h *OAuth) ServerMetadata(c fiber.Ctx) error {
	return sendJSON(c, fiber.StatusOK, mapper.FromGetOAuthServerMetadataResponseEntityToHTTPResponse(h.controller.GetServerMetadata(c.Context())))
}

// ResourceMetadata returns the MCP endpoint's protected resource metadata.
func (h *OAuth) ResourceMetadata(c fiber.Ctx) error {
	return sendJSON(c, fiber.StatusOK, mapper.FromGetProtectedResourceMetadataResponseEntityToHTTPResponse(h.controller.GetProtectedResourceMetadata(c.Context())))
}

// Register registers a client (RFC 7591).
func (h *OAuth) Register(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToRegisterClientRequestEntity(c)
	if rq == nil {
		return sendOAuthError(c, fiber.StatusBadRequest, oauthctrl.ErrInvalidClientMetadata, errors.New("body must be JSON client metadata"))
	}
	rs, err := h.controller.RegisterClient(c.Context(), rq)
	switch {
	case err == nil:
	case errors.Is(err, oauthctrl.ErrInvalidRedirectURI):
		return sendOAuthError(c, fiber.StatusBadRequest, oauthctrl.ErrInvalidRedirectURI, err)
	case errors.Is(err, oauthctrl.ErrInvalidClientMetadata):
		return sendOAuthError(c, fiber.StatusBadRequest, oauthctrl.ErrInvalidClientMetadata, err)
	default:
		return sendOAuthError(c, fiber.StatusInternalServerError, errServerError, nil)
	}
	return sendJSON(c, fiber.StatusCreated, mapper.FromRegisterClientResponseEntityToHTTPResponse(rs))
}

// Authorize shows the consent page, after sign-in if needed.
func (h *OAuth) Authorize(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToAuthorizeRequestEntity(c)
	if rq == nil {
		return h.errorPage(c, fiber.StatusBadRequest, "The app didn't say who it is (no client_id). Try connecting again from the app.")
	}
	rs, err := h.controller.Authorize(c.Context(), rq)
	switch {
	case err == nil:
	case errors.Is(err, oauthctrl.ErrLoginRequired):
		return h.login(c)
	case errors.Is(err, oauthctrl.ErrInvalidClient):
		return h.errorPage(c, fiber.StatusBadRequest, "This app is not registered with tmphost.io. Try connecting again from the app.")
	case errors.Is(err, oauthctrl.ErrInvalidRedirectURI):
		return h.errorPage(c, fiber.StatusBadRequest, "This app asked to send you somewhere it did not register, so tmphost.io stopped here.")
	default:
		return h.errorPage(c, fiber.StatusInternalServerError, "Something went wrong. Try again in a moment.")
	}
	if rs.Consent == nil {
		return c.Redirect().Status(fiber.StatusFound).To(rs.RedirectURL)
	}
	return h.page(c, fiber.StatusOK, consentPage(rs.Consent))
}

// Consent takes the user's answer and sends the browser back to the app.
func (h *OAuth) Consent(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToConsentRequestEntity(c)
	if rq == nil {
		return h.errorPage(c, fiber.StatusBadRequest, "The answer was incomplete. Try connecting again from the app.")
	}
	if rq.UserID == 0 {
		return h.login(c)
	}
	rs, err := h.controller.Consent(c.Context(), rq)
	if errors.Is(err, oauthctrl.ErrInvalidConsent) {
		return h.errorPage(c, fiber.StatusBadRequest, "This request expired or was made for another account. Try connecting again from the app.")
	}
	if err != nil {
		return h.errorPage(c, fiber.StatusInternalServerError, "Something went wrong. Try again in a moment.")
	}
	return c.Redirect().Status(fiber.StatusSeeOther).To(rs.RedirectURL)
}

// Token issues tokens for a code or a refresh token.
func (h *OAuth) Token(c fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	c.Set(fiber.HeaderPragma, "no-cache")
	rs, err := h.controller.Token(c.Context(), mapper.FromHTTPRequestToTokenRequestEntity(c))
	switch {
	case err == nil:
	case errors.Is(err, oauthctrl.ErrInvalidClient):
		return sendOAuthError(c, fiber.StatusUnauthorized, oauthctrl.ErrInvalidClient, err)
	case errors.Is(err, oauthctrl.ErrInvalidRequest):
		return sendOAuthError(c, fiber.StatusBadRequest, oauthctrl.ErrInvalidRequest, err)
	case errors.Is(err, oauthctrl.ErrInvalidGrant):
		return sendOAuthError(c, fiber.StatusBadRequest, oauthctrl.ErrInvalidGrant, err)
	case errors.Is(err, oauthctrl.ErrUnsupportedGrantType):
		return sendOAuthError(c, fiber.StatusBadRequest, oauthctrl.ErrUnsupportedGrantType, err)
	default:
		return sendOAuthError(c, fiber.StatusInternalServerError, errServerError, nil)
	}
	return sendJSON(c, fiber.StatusOK, mapper.FromTokenResponseEntityToHTTPResponse(rs))
}

// login sends the browser to sign in, then back here.
func (h *OAuth) login(c fiber.Ctx) error {
	return c.Redirect().Status(fiber.StatusFound).To(h.basePath + "/login?redirect=" + url.QueryEscape(c.OriginalURL()))
}

func (h *OAuth) errorPage(c fiber.Ctx, status int, msg string) error {
	return h.page(c, status, errorPage(msg))
}

func (h *OAuth) page(c fiber.Ctx, status int, html []byte) error {
	c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
	c.Set(fiber.HeaderContentSecurityPolicy, _pageCSP)
	c.Set(fiber.HeaderXFrameOptions, "DENY")
	c.Set(fiber.HeaderCacheControl, "no-store")
	c.Set(fiber.HeaderReferrerPolicy, "no-referrer")
	c.Set(fiber.HeaderXContentTypeOptions, "nosniff")
	return c.Status(status).Send(html)
}

var errServerError = errors.New("server_error")

// sendOAuthError writes an OAuth error; the code is code's text and the
// description err's, without the repeated code.
func sendOAuthError(c fiber.Ctx, status int, code, err error) error {
	desc := ""
	if err != nil && err != code {
		desc = strings.TrimPrefix(err.Error(), code.Error()+": ")
	}
	return sendJSON(c, status, mapper.FromOAuthErrorToHTTPResponse(code.Error(), desc))
}
