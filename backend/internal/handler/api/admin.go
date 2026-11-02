// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	adminctrl "github.com/tmphost/permahost/backend/internal/controller/admin"
	"github.com/tmphost/permahost/backend/internal/data/entity"
	mapper "github.com/tmphost/permahost/backend/internal/mapper/api"
)

// Admin handles the Users section of the root-only setup page.
type Admin struct {
	controller adminctrl.Controller
}

// GetStats returns the totals over every account and the daily stats.
func (h *Admin) GetStats(c fiber.Ctx) error {
	rs, err := h.controller.GetStats(c.Context(), &entity.GetAdminStatsRequest{})
	if err != nil {
		return sendError(c, fiber.StatusInternalServerError, "failed to load stats")
	}
	return sendJSON(c, fiber.StatusOK, mapper.FromGetAdminStatsResponseEntityToHTTPResponse(rs))
}

// ListUsers returns a page of users matching the search, with their
// balance and usage.
func (h *Admin) ListUsers(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToListAdminUsersRequestEntity(c)
	if rq == nil {
		return fiber.ErrBadRequest
	}
	rs, err := h.controller.ListUsers(c.Context(), rq)
	if err != nil {
		return sendError(c, fiber.StatusInternalServerError, "failed to load users")
	}
	return sendJSON(c, fiber.StatusOK, mapper.FromListAdminUsersResponseEntityToHTTPResponse(rs))
}

// GetUser returns one user with their usage.
func (h *Admin) GetUser(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToGetAdminUserRequestEntity(c)
	if rq == nil {
		return fiber.ErrBadRequest
	}
	rs, err := h.controller.GetUser(c.Context(), rq)
	if errors.Is(err, adminctrl.ErrUserNotFound) {
		return sendError(c, fiber.StatusNotFound, err.Error())
	}
	if err != nil {
		return sendError(c, fiber.StatusInternalServerError, "failed to load user")
	}
	return sendJSON(c, fiber.StatusOK, mapper.FromGetAdminUserResponseEntityToHTTPResponse(rs))
}

// AdjustCredits grants or deducts one user's credits.
func (h *Admin) AdjustCredits(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToAdjustCreditsRequestEntity(c)
	if rq == nil {
		return fiber.ErrBadRequest
	}
	rs, err := h.controller.AdjustCredits(c.Context(), rq)
	switch {
	case errors.Is(err, adminctrl.ErrUserNotFound):
		return sendError(c, fiber.StatusNotFound, err.Error())
	case errors.Is(err, adminctrl.ErrInvalidAdjustment):
		return sendError(c, fiber.StatusBadRequest, err.Error())
	case errors.Is(err, adminctrl.ErrInsufficientCredits):
		return sendError(c, fiber.StatusConflict, err.Error())
	case err != nil:
		return sendError(c, fiber.StatusInternalServerError, "failed to adjust credits")
	}
	return sendJSON(c, fiber.StatusCreated, mapper.FromAdjustCreditsResponseEntityToHTTPResponse(rs))
}

// ListCreditActivities returns a page of one user's credit history.
func (h *Admin) ListCreditActivities(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToListCreditActivitiesRequestEntity(c)
	if rq == nil {
		return fiber.ErrBadRequest
	}
	rs, err := h.controller.ListCreditActivities(c.Context(), rq)
	if errors.Is(err, adminctrl.ErrUserNotFound) {
		return sendError(c, fiber.StatusNotFound, err.Error())
	}
	if err != nil {
		return sendError(c, fiber.StatusInternalServerError, "failed to load credit history")
	}
	return sendJSON(c, fiber.StatusOK, mapper.FromListCreditActivitiesResponseEntityToHTTPResponse(rs))
}
