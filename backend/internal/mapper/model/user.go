// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package model maps between database models and domain entities.
package model

import (
	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/model"
)

// FromUserModelToUserEntity maps a stored user to the domain user.
// creditsLimit is the monthly grant, which is policy rather than data.
func FromUserModelToUserEntity(m model.User, creditsLimit int) entity.User {
	return entity.User{
		ID:             m.ID,
		Email:          m.Email,
		Name:           m.Name,
		Picture:        m.Picture,
		Provider:       m.Provider,
		Credits:        m.Credits,
		CreditsLimit:   creditsLimit,
		CreditsRenewAt: m.CreditsRenewAt,
		CreatedAt:      m.CreatedAt,
	}
}
