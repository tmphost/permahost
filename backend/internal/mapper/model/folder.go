// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package model

import (
	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/model"
)

// FromFolderModelToFolderEntity maps a stored folder to the domain. The
// public URL and the default title are not stored, so it is left for the caller.
func FromFolderModelToFolderEntity(m model.Folder) entity.Folder {
	return entity.Folder{ID: m.ID, UserID: m.UserID, CreatedAt: m.CreatedAt, ExpiresAt: m.ExpiresAt, Title: m.Title}
}
