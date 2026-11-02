// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	authctrl "github.com/tmphost/tmphost/backend/internal/controller/auth"
	mapper "github.com/tmphost/tmphost/backend/internal/mapper/api"
)

// User handles the signed-in user's account.
type User struct {
	controller authctrl.Controller
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
