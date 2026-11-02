// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"

	"github.com/gofiber/fiber/v3"

	folderctrl "github.com/tmphost/tmphost/backend/internal/controller/folder"
	mapper "github.com/tmphost/tmphost/backend/internal/mapper/api"
)

// Folder handles the signed-in user's folders and files.
type Folder struct {
	controller folderctrl.Controller
}

// Limits returns the upload limits and pricing.
func (h *Folder) Limits(c fiber.Ctx) error {
	rs := h.controller.GetLimits(c.Context(), mapper.FromHTTPRequestToGetLimitsRequestEntity(c))
	return sendJSON(c, fiber.StatusOK, mapper.FromGetLimitsResponseEntityToHTTPResponse(rs))
}

// Create creates an empty folder.
func (h *Folder) Create(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToCreateFolderRequestEntity(c)
	if rq == nil {
		return fiber.ErrBadRequest
	}
	rs, err := h.controller.CreateFolder(c.Context(), rq)
	if err != nil {
		return sendFolderError(c, err)
	}
	return sendJSON(c, fiber.StatusCreated, mapper.FromCreateFolderResponseEntityToHTTPResponse(rs))
}

// List lists the live folders.
func (h *Folder) List(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToListFoldersRequestEntity(c)
	if rq == nil {
		return fiber.ErrBadRequest
	}
	rs, err := h.controller.ListFolders(c.Context(), rq)
	if err != nil {
		return sendFolderError(c, err)
	}
	return sendJSON(c, fiber.StatusOK, mapper.FromListFoldersResponseEntityToHTTPResponse(rs))
}

// Get returns a folder and its files.
func (h *Folder) Get(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToGetFolderRequestEntity(c)
	if rq == nil {
		return fiber.ErrBadRequest
	}
	rs, err := h.controller.GetFolder(c.Context(), rq)
	if err != nil {
		return sendFolderError(c, err)
	}
	return sendJSON(c, fiber.StatusOK, mapper.FromGetFolderResponseEntityToHTTPResponse(rs))
}

// Update renames a folder.
func (h *Folder) Update(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToUpdateFolderRequestEntity(c)
	if rq == nil {
		return fiber.ErrBadRequest
	}
	rs, err := h.controller.UpdateFolder(c.Context(), rq)
	if err != nil {
		return sendFolderError(c, err)
	}
	return sendJSON(c, fiber.StatusOK, mapper.FromUpdateFolderResponseEntityToHTTPResponse(rs))
}

// Delete deletes a folder and its files.
func (h *Folder) Delete(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToDeleteFolderRequestEntity(c)
	if rq == nil {
		return fiber.ErrBadRequest
	}
	if err := h.controller.DeleteFolder(c.Context(), rq); err != nil {
		return sendFolderError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// PutFile writes the request body as a file.
func (h *Folder) PutFile(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToPutFilesRequestEntity(c)
	if rq == nil {
		return fiber.ErrBadRequest
	}
	rs, err := h.controller.PutFiles(c.Context(), rq)
	if err != nil {
		return sendFolderError(c, err)
	}
	return sendJSON(c, fiber.StatusOK, mapper.FromPutFilesResponseEntityToHTTPResponse(rs))
}

// GetFile downloads a file. It is always sent as an attachment: this is
// the app's origin, where user content must never render.
func (h *Folder) GetFile(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToOpenFileRequestEntity(c)
	if rq == nil {
		return fiber.ErrBadRequest
	}
	rs, err := h.controller.OpenFile(c.Context(), rq)
	if err != nil {
		return sendFolderError(c, err)
	}
	// Attachment guesses a type from the name, so it goes first.
	c.Attachment(rq.Path)
	c.Set(fiber.HeaderContentType, fiber.MIMEOctetStream)
	c.Set(fiber.HeaderXContentTypeOptions, "nosniff")
	return c.SendStream(rs.Body, int(rs.Size))
}

// DeleteFile deletes one file.
func (h *Folder) DeleteFile(c fiber.Ctx) error {
	rq := mapper.FromHTTPRequestToDeleteFileRequestEntity(c)
	if rq == nil {
		return fiber.ErrBadRequest
	}
	if err := h.controller.DeleteFile(c.Context(), rq); err != nil {
		return sendFolderError(c, err)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func sendFolderError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, folderctrl.ErrNotFound):
		return sendError(c, fiber.StatusNotFound, "not found")
	case errors.Is(err, folderctrl.ErrInvalidTTL), errors.Is(err, folderctrl.ErrInvalidPath), errors.Is(err, folderctrl.ErrInvalidTitle),
		errors.Is(err, folderctrl.ErrNoFiles), errors.Is(err, folderctrl.ErrDuplicatePath):
		return sendError(c, fiber.StatusBadRequest, err.Error())
	case errors.Is(err, folderctrl.ErrTooLarge):
		return sendError(c, fiber.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, folderctrl.ErrInsufficientCredits):
		return sendError(c, fiber.StatusPaymentRequired, err.Error())
	case errors.Is(err, folderctrl.ErrQuotaExceeded):
		return sendError(c, fiber.StatusTooManyRequests, err.Error())
	default:
		return sendError(c, fiber.StatusInternalServerError, "storage failed")
	}
}
