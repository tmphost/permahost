// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"

	setupctrl "github.com/tmphost/tmphost/backend/internal/controller/setup"
	mapper "github.com/tmphost/tmphost/backend/internal/mapper/api"
	"github.com/tmphost/tmphost/backend/internal/service/token"
)

// Setup handles the root-only setup page.
type Setup struct {
	controller   setupctrl.Controller
	basePath     string
	cookieSecure bool
}

// Login exchanges the root access token for a root session cookie.
func (h *Setup) Login(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToRootLoginRequestEntity(c)
	if rq == nil {
		return fiber.ErrBadRequest
	}
	rs, err := h.controller.Login(c.Context(), rq)
	if errors.Is(err, setupctrl.ErrInvalidRootToken) {
		return sendError(c, fiber.StatusUnauthorized, "invalid root access token")
	}
	if err != nil {
		return sendError(c, fiber.StatusInternalServerError, "failed to sign in")
	}
	c.Cookie(h.cookie(rs.SessionToken, token.RootTTL))
	return c.SendStatus(fiber.StatusNoContent)
}

// Logout clears the root session cookie.
func (h *Setup) Logout(c fiber.Ctx) error {
	c.Cookie(h.cookie("", -1))
	return c.SendStatus(fiber.StatusNoContent)
}

// Get returns the settings, without secrets.
func (h *Setup) Get(c fiber.Ctx) error {
	rs := h.controller.GetSettings(c.Context())
	return sendJSON(c, fiber.StatusOK, mapper.FromGetSettingsResponseEntityToHTTPResponse(rs))
}

// Update validates and saves the settings.
func (h *Setup) Update(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToUpdateSettingsRequestEntity(c)
	if rq == nil {
		return fiber.ErrBadRequest
	}
	rs, err := h.controller.UpdateSettings(c.Context(), rq)
	if errors.Is(err, setupctrl.ErrInvalidSettings) {
		return sendError(c, fiber.StatusBadRequest, err.Error())
	}
	if err != nil {
		return sendError(c, fiber.StatusInternalServerError, "failed to save settings")
	}
	return sendJSON(c, fiber.StatusOK, mapper.FromUpdateSettingsResponseEntityToHTTPResponse(rs))
}

// cookie scopes the root session to the setup API only.
func (h *Setup) cookie(value string, ttl time.Duration) *fiber.Cookie {
	ck := &fiber.Cookie{
		Name:     mapper.RootCookie,
		Value:    value,
		Path:     h.basePath + RoutePrefix + "/setup",
		HTTPOnly: true,
		Secure:   h.cookieSecure,
		SameSite: fiber.CookieSameSiteStrictMode,
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

// AuthenticateRoot rejects requests without a valid root session.
func AuthenticateRoot(controller setupctrl.Controller) fiber.Handler {
	return func(c fiber.Ctx) error {
		rq := mapper.FromHTTPRequestToAuthenticateRootRequestEntity(c)
		if rq == nil || controller.Authenticate(c.Context(), rq) != nil {
			return sendError(c, fiber.StatusUnauthorized, "unauthorized")
		}
		return c.Next()
	}
}
