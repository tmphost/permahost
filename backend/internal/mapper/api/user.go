// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/view"
)

// FromHTTPRequestToGetCurrentUserRequestEntity reads the user ID the auth
// middleware stored on the request.
func FromHTTPRequestToGetCurrentUserRequestEntity(c fiber.Ctx) *entity.GetCurrentUserRequest {
	id := localUserID(c)
	if id == 0 {
		return nil
	}
	return &entity.GetCurrentUserRequest{UserID: id}
}

// FromGetCurrentUserResponseEntityToHTTPResponse renders GET /users/me.
func FromGetCurrentUserResponseEntityToHTTPResponse(rs *entity.GetCurrentUserResponse) []byte {
	return marshal(view.GetCurrentUserResponse{User: FromUserEntityToUserView(rs.User)})
}

// FromUserEntityToUserView maps a domain user to its JSON view.
func FromUserEntityToUserView(e entity.User) view.User {
	return view.User{
		ID:             FromIDToBase62(e.ID),
		Email:          e.Email,
		Name:           e.Name,
		Picture:        e.Picture,
		Provider:       e.Provider,
		Credits:        e.Credits,
		CreditsLimit:   e.CreditsLimit,
		CreditsRenewAt: e.CreditsRenewAt.UTC().Format(time.RFC3339),
		CreatedAt:      e.CreatedAt.UTC().Format(time.RFC3339),
	}
}
