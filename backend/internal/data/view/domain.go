// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package view

// Domain statuses.
const (
	DomainPending = "pending"
	DomainActive  = "active"
)

type (
	// Domain is a custom domain pointed at a folder.
	Domain struct {
		ID       string `json:"id"`
		FolderID string `json:"folderId"`
		Hostname string `json:"hostname"`
		// Target is what the hostname's CNAME record must name.
		Target string `json:"target"`
		// Status is DomainPending until the CNAME is verified, then
		// DomainActive.
		Status     string `json:"status"`
		CreatedAt  string `json:"createdAt"`
		VerifiedAt string `json:"verifiedAt,omitempty"`
		CheckedAt  string `json:"checkedAt,omitempty"`
		LastError  string `json:"lastError,omitempty"`
	}

	// CreateDomainRequest is the POST /folders/:id/domains body.
	CreateDomainRequest struct {
		Hostname string `json:"hostname"`
	}

	// DomainResponse wraps a domain.
	DomainResponse struct {
		Domain Domain `json:"domain"`
	}

	// ListDomainsResponse lists a folder's domains.
	ListDomainsResponse struct {
		Domains []Domain `json:"domains"`
	}
)
