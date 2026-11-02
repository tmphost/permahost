// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/mustafaturan/monoflake"

	"github.com/tmphost/permahost/backend/internal/data/entity"
	"github.com/tmphost/permahost/backend/internal/data/view"
	"github.com/tmphost/permahost/backend/internal/service/idgen"
)

// FromHTTPRequestToCreateDomainRequestEntity reads POST
// /folders/:id/domains, whose body names the hostname.
func FromHTTPRequestToCreateDomainRequestEntity(c fiber.Ctx) *entity.CreateDomainRequest {
	rq := FromHTTPRequestToListDomainsRequestEntity(c)
	var body view.CreateDomainRequest
	if rq == nil || json.Unmarshal(c.Body(), &body) != nil || body.Hostname == "" {
		return nil
	}
	return &entity.CreateDomainRequest{UserID: rq.UserID, FolderID: rq.FolderID, Hostname: body.Hostname}
}

// FromHTTPRequestToListDomainsRequestEntity reads GET /folders/:id/domains.
func FromHTTPRequestToListDomainsRequestEntity(c fiber.Ctx) *entity.ListDomainsRequest {
	userID, folderID := localUserID(c), idgen.ParseFolderID(c.Params("id"))
	if userID == 0 || folderID == 0 {
		return nil
	}
	return &entity.ListDomainsRequest{UserID: userID, FolderID: folderID}
}

// FromHTTPRequestToVerifyDomainRequestEntity reads POST
// /folders/:id/domains/:domain/verify.
func FromHTTPRequestToVerifyDomainRequestEntity(c fiber.Ctx) *entity.VerifyDomainRequest {
	rq := FromHTTPRequestToListDomainsRequestEntity(c)
	domainID := monoflake.IDFromBase62(c.Params("domain")).Int64()
	if rq == nil || domainID <= 0 {
		return nil
	}
	return &entity.VerifyDomainRequest{UserID: rq.UserID, FolderID: rq.FolderID, DomainID: domainID}
}

// FromHTTPRequestToDeleteDomainRequestEntity reads DELETE
// /folders/:id/domains/:domain.
func FromHTTPRequestToDeleteDomainRequestEntity(c fiber.Ctx) *entity.DeleteDomainRequest {
	rq := FromHTTPRequestToVerifyDomainRequestEntity(c)
	if rq == nil {
		return nil
	}
	return &entity.DeleteDomainRequest{UserID: rq.UserID, FolderID: rq.FolderID, DomainID: rq.DomainID}
}

// FromCreateDomainResponseEntityToHTTPResponse renders POST
// /folders/:id/domains.
func FromCreateDomainResponseEntityToHTTPResponse(rs *entity.CreateDomainResponse) []byte {
	return marshal(view.DomainResponse{Domain: FromDomainEntityToDomainView(rs.Domain)})
}

// FromListDomainsResponseEntityToHTTPResponse renders GET
// /folders/:id/domains.
func FromListDomainsResponseEntityToHTTPResponse(rs *entity.ListDomainsResponse) []byte {
	domains := make([]view.Domain, len(rs.Domains))
	for i, d := range rs.Domains {
		domains[i] = FromDomainEntityToDomainView(d)
	}
	return marshal(view.ListDomainsResponse{Domains: domains})
}

// FromVerifyDomainResponseEntityToHTTPResponse renders POST
// /folders/:id/domains/:domain/verify.
func FromVerifyDomainResponseEntityToHTTPResponse(rs *entity.VerifyDomainResponse) []byte {
	return marshal(view.DomainResponse{Domain: FromDomainEntityToDomainView(rs.Domain)})
}

// FromDomainEntityToDomainView maps a domain to its JSON view.
func FromDomainEntityToDomainView(e entity.Domain) view.Domain {
	status := view.DomainPending
	if !e.VerifiedAt.IsZero() {
		status = view.DomainActive
	}
	return view.Domain{
		ID:         FromIDToBase62(e.ID),
		FolderID:   idgen.FormatFolderID(e.FolderID),
		Hostname:   e.Hostname,
		Target:     e.Target,
		Status:     status,
		CreatedAt:  e.CreatedAt.UTC().Format(time.RFC3339),
		VerifiedAt: formatOptionalTime(e.VerifiedAt),
		CheckedAt:  formatOptionalTime(e.CheckedAt),
		LastError:  e.LastError,
	}
}
