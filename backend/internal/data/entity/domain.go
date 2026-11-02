// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package entity

import "time"

type (
	// Domain is a custom domain pointed at a folder.
	Domain struct {
		ID       int64
		UserID   int64
		FolderID int64
		Hostname string
		// Target is what the hostname's CNAME record must name.
		Target    string
		CreatedAt time.Time
		// VerifiedAt is zero while the domain is pending.
		VerifiedAt time.Time
		// CheckedAt is zero until the CNAME is first looked up.
		CheckedAt time.Time
		LastError string
	}

	// CreateDomainRequest adds a custom domain to one of a user's folders.
	CreateDomainRequest struct {
		UserID   int64
		FolderID int64
		Hostname string
	}

	// CreateDomainResponse carries the new, pending domain.
	CreateDomainResponse struct {
		Domain Domain
	}

	// ListDomainsRequest lists a folder's custom domains.
	ListDomainsRequest struct {
		UserID   int64
		FolderID int64
	}

	// ListDomainsResponse carries a folder's domains, oldest first.
	ListDomainsResponse struct {
		Domains []Domain
	}

	// VerifyDomainRequest checks a domain's CNAME now.
	VerifyDomainRequest struct {
		UserID   int64
		FolderID int64
		DomainID int64
	}

	// VerifyDomainResponse carries the domain after the check.
	VerifyDomainResponse struct {
		Domain Domain
	}

	// DeleteDomainRequest removes a custom domain.
	DeleteDomainRequest struct {
		UserID   int64
		FolderID int64
		DomainID int64
	}

	// VerifyPendingDomainsResponse reports a background check.
	VerifyPendingDomainsResponse struct {
		Checked  int
		Verified int
	}
)
