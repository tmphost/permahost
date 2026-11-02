// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	authctrl "github.com/tmphost/permahost/backend/internal/controller/auth"
	"github.com/tmphost/permahost/backend/internal/controller/credit"
	mapper "github.com/tmphost/permahost/backend/internal/mapper/api"
)

// User handles the signed-in user's account.
type User struct {
	controller authctrl.Controller
	credits    credit.Controller
}

// Get returns the signed-in user, including the operation credit balance.
func (h *User) Get(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToGetCurrentUserRequestEntity(c)
	if rq == nil {
		return fiber.ErrBadRequest
	}
	rs, err := h.controller.GetCurrentUser(c.Context(), rq)
	if errors.Is(err, authctrl.ErrUnauthorized) {
		return sendError(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if err != nil {
		return sendError(c, fiber.StatusInternalServerError, "failed to load user")
	}
	return sendJSON(c, fiber.StatusOK, mapper.FromGetCurrentUserResponseEntityToHTTPResponse(rs))
}

// Usage returns what the signed-in account hosts and what it costs a
// month.
func (h *User) Usage(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToGetUsageRequestEntity(c)
	if rq == nil {
		return fiber.ErrBadRequest
	}
	rs, err := h.credits.GetUsage(c.Context(), rq)
	if err != nil {
		return sendError(c, fiber.StatusInternalServerError, "failed to load usage")
	}
	return sendJSON(c, fiber.StatusOK, mapper.FromGetUsageResponseEntityToHTTPResponse(rs))
}
