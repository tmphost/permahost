// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v3"

	domainctrl "github.com/tmphost/permahost/backend/internal/controller/domain"
	folderctrl "github.com/tmphost/permahost/backend/internal/controller/folder"
	"github.com/tmphost/permahost/backend/internal/data/entity"
	mapper "github.com/tmphost/permahost/backend/internal/mapper/api"
)

// _sandbox keeps uploaded pages working (scripts, forms, popups) but in an
// opaque origin, so they can't read or set cookies of any permahost origin,
// or call the API as the visitor, even when served on the app's origin.
const _sandbox = "sandbox allow-scripts allow-forms allow-popups allow-modals allow-downloads"

// Public serves folders to anyone: on folder hosts, <folderID>.file.<domain>,
// with subdomains on, else below /files/<userID>/<folderID>/, and on their
// verified custom domains.
type Public struct {
	controller folderctrl.Controller
	domains    domainctrl.Controller
	// settings returns the current file settings; the mode can change at
	// runtime.
	settings func() entity.FileSettings
}

// Serve answers every request to a folder host and passes any other
// request on. It runs before every route, so no app route is ever
// reachable on a host that serves user content.
func (h *Public) Serve(c fiber.Ctx) error {
	s := h.settings()
	if !s.Subdomains {
		return c.Next()
	}
	rq := mapper.FromHTTPRequestToOpenPublicFileRequestEntity(c, folderctrl.FolderDomain(s.PublicDomain))
	if rq == nil {
		return c.Next()
	}
	return h.send(c, rq)
}

// ServePath answers /files/<userID>/<folderID>/<path> when subdomains are
// off; otherwise the path is left to the web app.
func (h *Public) ServePath(c fiber.Ctx) error {
	if h.settings().Subdomains {
		return c.Next()
	}
	rq := mapper.FromHTTPRequestToOpenPublicFileByPathRequestEntity(c)
	if rq == nil {
		return notFound(c)
	}
	// Relative links in the folder's pages need the trailing slash.
	if rq.Path == "" && !strings.HasSuffix(c.Path(), "/") {
		return c.Redirect().Status(fiber.StatusMovedPermanently).To(c.Params("folder") + "/")
	}
	return h.send(c, rq)
}

// NewCustomDomainHandler returns the handler of hosts that are neither the
// app's nor folder hosts: a verified custom domain is served its folder,
// every other host is refused.
func NewCustomDomainHandler(folders folderctrl.Controller, domains domainctrl.Controller) fiber.Handler {
	h := &Public{controller: folders, domains: domains}
	return h.ServeDomain
}

// ServeDomain answers a request to a custom domain; the whole path is the
// file path.
func (h *Public) ServeDomain(c fiber.Ctx) error {
	folderID, err := h.domains.GetDomainFolder(c.Context(), string(c.Request().Host()))
	if errors.Is(err, domainctrl.ErrNotFound) {
		return sendError(c, fiber.StatusNotFound, "host not allowed")
	}
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Something went wrong.")
	}
	rq := mapper.FromHTTPRequestToOpenDomainFileRequestEntity(c, folderID)
	if rq == nil {
		return notFound(c)
	}
	return h.send(c, rq)
}

func (h *Public) send(c fiber.Ctx, rq *entity.OpenPublicFileRequest) error {
	if c.Method() != fiber.MethodGet && c.Method() != fiber.MethodHead {
		return c.SendStatus(fiber.StatusMethodNotAllowed)
	}
	rs, err := h.controller.OpenPublicFile(c.Context(), rq)
	// Visitors get plain text, not the API's JSON errors.
	if errors.Is(err, folderctrl.ErrNotFound) {
		return notFound(c)
	}
	if errors.Is(err, folderctrl.ErrSuspended) {
		return c.Status(fiber.StatusPaymentRequired).SendString("This site is paused: its owner's account is out of credits.")
	}
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).SendString("Something went wrong.")
	}
	c.Set(fiber.HeaderContentType, rs.ContentType)
	c.Set(fiber.HeaderContentSecurityPolicy, _sandbox)
	c.Set(fiber.HeaderXContentTypeOptions, "nosniff")
	c.Set(fiber.HeaderCacheControl, "public, max-age=60")
	c.Set(fiber.HeaderLastModified, rs.ModTime.UTC().Format(http.TimeFormat))
	return c.SendStream(rs.Body, int(rs.Size))
}

func notFound(c fiber.Ctx) error {
	return c.Status(fiber.StatusNotFound).SendString("Not found.")
}
