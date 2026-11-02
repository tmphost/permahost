// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	domainctrl "github.com/tmphost/permahost/backend/internal/controller/domain"
	mapper "github.com/tmphost/permahost/backend/internal/mapper/api"
)

// Domain handles the custom domains of the signed-in user's folders.
type Domain struct {
	controller domainctrl.Controller
}

// Create adds a pending custom domain to a folder.
func (h *Domain) Create(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToCreateDomainRequestEntity(c)
	if rq == nil {
		return fiber.ErrBadRequest
	}
	rs, err := h.controller.CreateDomain(c.Context(), rq)
	if err != nil {
		return sendDomainError(c, err)
	}
	return sendJSON(c, fiber.StatusCreated, mapper.FromCreateDomainResponseEntityToHTTPResponse(rs))
}

// List lists a folder's custom domains.
func (h *Domain) List(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToListDomainsRequestEntity(c)
	if rq == nil {
		return fiber.ErrBadRequest
	}
	rs, err := h.controller.ListDomains(c.Context(), rq)
	if err != nil {
		return sendDomainError(c, err)
	}
	return sendJSON(c, fiber.StatusOK, mapper.FromListDomainsResponseEntityToHTTPResponse(rs))
}

// Verify checks a domain's CNAME record now.
func (h *Domain) Verify(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToVerifyDomainRequestEntity(c)
	if rq == nil {
		return fiber.ErrBadRequest
	}
	rs, err := h.controller.VerifyDomain(c.Context(), rq)
	if err != nil {
		return sendDomainError(c, err)
	}
	return sendJSON(c, fiber.StatusOK, mapper.FromVerifyDomainResponseEntityToHTTPResponse(rs))
}

// Delete removes a custom domain.
func (h *Domain) Delete(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToDeleteDomainRequestEntity(c)
	if rq == nil {
		return fiber.ErrBadRequest
	}
	if err := h.controller.DeleteDomain(c.Context(), rq); err != nil {
		return sendDomainError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func sendDomainError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, domainctrl.ErrNotFound):
		return sendError(c, fiber.StatusNotFound, "not found")
	case errors.Is(err, domainctrl.ErrInvalidHostname):
		return sendError(c, fiber.StatusBadRequest, err.Error())
	case errors.Is(err, domainctrl.ErrDomainTaken):
		return sendError(c, fiber.StatusConflict, err.Error())
	case errors.Is(err, domainctrl.ErrNotVerified):
		return sendError(c, fiber.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, domainctrl.ErrInsufficientCredits):
		return sendError(c, fiber.StatusPaymentRequired, err.Error())
	default:
		return sendError(c, fiber.StatusInternalServerError, "domain update failed")
	}
}
