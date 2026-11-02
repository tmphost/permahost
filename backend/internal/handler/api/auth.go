// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
	"net/url"
	"time"

	"github.com/gofiber/fiber/v3"

	authctrl "github.com/tmphost/tmphost/backend/internal/controller/auth"
	mapper "github.com/tmphost/tmphost/backend/internal/mapper/api"
	"github.com/tmphost/tmphost/backend/internal/service/token"
)

// Login error codes the frontend shows on /login?error=<code>.
const (
	LoginErrorCancelled        = "cancelled"
	LoginErrorUnknownProvider  = "unknown_provider"
	LoginErrorInvalidState     = "invalid_state"
	LoginErrorEmailNotVerified = "email_not_verified"
	LoginErrorProvider         = "provider_error"
	LoginErrorServer           = "server_error"
)

// Auth handles sign-in and sign-out.
type Auth struct {
	controller   authctrl.Controller
	basePath     string
	cookieSecure bool
}

// Config returns the enabled sign-in providers.
func (h *Auth) Config(c fiber.Ctx) error {
	rs := h.controller.GetAuthConfig(c.Context())
	return sendJSON(c, fiber.StatusOK, mapper.FromGetAuthConfigResponseEntityToHTTPResponse(rs))
}

// Login redirects the browser to the provider's consent screen and pins
// the OAuth state in a short-lived cookie.
func (h *Auth) Login(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToStartLoginRequestEntity(c)
	if rq == nil {
		return fiber.ErrBadRequest
	}
	rs, err := h.controller.StartLogin(c.Context(), rq)
	if errors.Is(err, authctrl.ErrUnknownProvider) {
		return sendError(c, fiber.StatusNotFound, "unknown provider")
	}
	if errors.Is(err, authctrl.ErrProviderUnavailable) {
		return h.loginError(c, LoginErrorProvider)
	}
	if err != nil {
		return sendError(c, fiber.StatusInternalServerError, "failed to start login")
	}
	c.Cookie(h.cookie(mapper.StateCookie, rs.State, token.StateTTL, h.stateCookiePath()))
	return c.Redirect().Status(fiber.StatusFound).To(rs.AuthURL)
}

// Callback finishes the provider round trip, issues the session cookie and
// sends the browser back into the app. The browser navigated here, so
// failures redirect to the login page instead of returning JSON.
func (h *Auth) Callback(c fiber.Ctx) error {
	// The state is single use, whatever the outcome.
	c.Cookie(h.cookie(mapper.StateCookie, "", -1, h.stateCookiePath()))

	rq := mapper.FromHTTPRequestToCompleteLoginRequestEntity(c)
	if rq == nil {
		return h.loginError(c, LoginErrorCancelled)
	}
	rs, err := h.controller.CompleteLogin(c.Context(), rq)
	switch {
	case err == nil:
	case errors.Is(err, authctrl.ErrUnknownProvider):
		return h.loginError(c, LoginErrorUnknownProvider)
	case errors.Is(err, authctrl.ErrInvalidState):
		return h.loginError(c, LoginErrorInvalidState)
	case errors.Is(err, authctrl.ErrEmailNotVerified):
		return h.loginError(c, LoginErrorEmailNotVerified)
	case errors.Is(err, authctrl.ErrProviderExchange):
		return h.loginError(c, LoginErrorProvider)
	default:
		return h.loginError(c, LoginErrorServer)
	}
	c.Cookie(h.cookie(mapper.SessionCookie, rs.SessionToken, token.SessionTTL, h.sessionCookiePath()))
	return c.Redirect().Status(fiber.StatusFound).To(rs.RedirectURL)
}

// Logout clears the session cookie.
func (h *Auth) Logout(c fiber.Ctx) error {
	c.Cookie(h.cookie(mapper.SessionCookie, "", -1, h.sessionCookiePath()))
	return c.SendStatus(fiber.StatusNoContent)
}

func (h *Auth) loginError(c fiber.Ctx, code string) error {
	return c.Redirect().Status(fiber.StatusFound).To(h.basePath + "/login?error=" + url.QueryEscape(code))
}

func (h *Auth) sessionCookiePath() string { return h.basePath + "/" }

func (h *Auth) stateCookiePath() string { return h.basePath + AuthPrefix }

// cookie builds an HttpOnly, SameSite=Lax cookie. Lax still sends the
// cookie on the provider's top-level redirect back to the callback. A
// negative ttl expires the cookie.
func (h *Auth) cookie(name, value string, ttl time.Duration, path string) *fiber.Cookie {
	ck := &fiber.Cookie{
		Name:     name,
		Value:    value,
		Path:     path,
		HTTPOnly: true,
		Secure:   h.cookieSecure,
		SameSite: fiber.CookieSameSiteLaxMode,
	}
	if ttl < 0 {
		ck.Expires = time.Unix(0, 0)
		ck.MaxAge = -1
		return ck
	}
	ck.Expires = time.Now().Add(ttl)
	ck.MaxAge = int(ttl / time.Second)
	return ck
}
