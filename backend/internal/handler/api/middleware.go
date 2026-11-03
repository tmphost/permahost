// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v3"

	authctrl "github.com/tmphost/permahost/backend/internal/controller/auth"
	mapper "github.com/tmphost/permahost/backend/internal/mapper/api"
)

// Authenticate rejects requests without a valid session cookie or access
// token and stores the user ID for the handlers behind it.
func Authenticate(controller authctrl.Controller) fiber.Handler {
	return func(c fiber.Ctx) error {
		rq := mapper.FromHTTPRequestToAuthenticateRequestEntity(c)
		if rq == nil {
			return sendError(c, fiber.StatusUnauthorized, "unauthorized")
		}
		rs, err := controller.Authenticate(c.Context(), rq)
		if err != nil {
			return sendError(c, fiber.StatusUnauthorized, "unauthorized")
		}
		c.Locals(mapper.LocalUserID, rs.UserID)
		return c.Next()
	}
}

// OptionalAuthenticate stores the user ID of a valid session, if any, and
// always continues.
func OptionalAuthenticate(controller authctrl.Controller) fiber.Handler {
	return func(c fiber.Ctx) error {
		if rq := mapper.FromHTTPRequestToAuthenticateRequestEntity(c); rq != nil {
			if rs, err := controller.Authenticate(c.Context(), rq); err == nil {
				c.Locals(mapper.LocalUserID, rs.UserID)
			}
		}
		return c.Next()
	}
}

// AuthenticateOrAnonymous is Authenticate, except that a request without
// an access token may continue as the anonymous visitor of its IP address
// when it comes from the app's web origin. That covers a browser whose
// session expired; an invalid access token is still rejected.
func AuthenticateOrAnonymous(controller authctrl.Controller) fiber.Handler {
	return func(c fiber.Ctx) error {
		rq := mapper.FromHTTPRequestToAuthenticateRequestEntity(c)
		if rq != nil {
			rs, err := controller.Authenticate(c.Context(), rq)
			if err == nil {
				c.Locals(mapper.LocalUserID, rs.UserID)
				return c.Next()
			}
			if rq.AccessToken != "" {
				return sendError(c, fiber.StatusUnauthorized, "unauthorized")
			}
		}
		rs, err := controller.AuthenticateAnonymous(c.Context(), mapper.FromHTTPRequestToAuthenticateAnonymousRequestEntity(c))
		if err != nil {
			return sendError(c, fiber.StatusUnauthorized, "unauthorized")
		}
		c.Locals(mapper.LocalUserID, rs.UserID)
		c.Locals(mapper.LocalAnonymous, rs.Anonymous)
		return c.Next()
	}
}

// SameOrigin refuses a state-changing request that carries cookies unless
// the browser sent it from the app's own origin. Folder hosts are on
// sibling subdomains, so they are the same site as the app and SameSite
// cookies alone do not stop their pages from posting to it. Requests
// without cookies (CLI, MCP and other access token clients) pass.
func SameOrigin(c fiber.Ctx) error {
	switch c.Method() {
	case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions:
		return c.Next()
	}
	if c.Get(fiber.HeaderCookie) == "" {
		return c.Next()
	}
	switch c.Get(fiber.HeaderSecFetchSite) {
	case "same-origin", "none":
		return c.Next()
	case "":
		// Browsers without Fetch Metadata still send Origin.
	default:
		return sendError(c, fiber.StatusForbidden, "cross-origin request refused")
	}
	origin := c.Get(fiber.HeaderOrigin)
	if origin == "" {
		return c.Next()
	}
	if u, err := url.Parse(origin); err == nil && u.Host != "" && strings.EqualFold(u.Host, c.Host()) {
		return c.Next()
	}
	return sendError(c, fiber.StatusForbidden, "cross-origin request refused")
}
