// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package model

import (
	"github.com/tmphost/permahost/backend/internal/data/entity"
	"github.com/tmphost/permahost/backend/internal/data/model"
)

// FromDomainModelToDomainEntity maps a stored domain to the domain layer.
// The CNAME target is not stored, so it is left for the caller.
func FromDomainModelToDomainEntity(m model.Domain) entity.Domain {
	e := entity.Domain{
		ID:        m.ID,
		UserID:    m.UserID,
		FolderID:  m.FolderID,
		Hostname:  m.Hostname,
		CreatedAt: m.CreatedAt,
		LastError: m.LastError,
	}
	if m.VerifiedAt != nil {
		e.VerifiedAt = *m.VerifiedAt
	}
	if m.CheckedAt != nil {
		e.CheckedAt = *m.CheckedAt
	}
	return e
}
