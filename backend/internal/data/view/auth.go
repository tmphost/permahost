// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package view holds the JSON request and response bodies.
package view

type (
	// AuthConfig lists the enabled sign-in providers.
	AuthConfig struct {
		Providers []AuthProvider `json:"providers"`
	}

	// AuthProvider is a sign-in button.
	AuthProvider struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Icon string `json:"icon"`
	}

	// User is the signed-in user as the frontend sees it.
	User struct {
		ID             string `json:"id"`
		Email          string `json:"email"`
		Name           string `json:"name"`
		Picture        string `json:"picture"`
		Provider       string `json:"provider"`
		Credits        int    `json:"credits"`
		CreditsLimit   int    `json:"creditsLimit"`
		CreditsRenewAt string `json:"creditsRenewAt"`
		CreatedAt      string `json:"createdAt"`
	}

	// GetCurrentUserResponse wraps the signed-in user.
	GetCurrentUserResponse struct {
		User User `json:"user"`
	}

	// Error is the error body returned by the API.
	Error struct {
		Error string `json:"error"`
	}
)
