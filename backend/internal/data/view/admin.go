// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package view

type (
	// AdminUser is a user as root sees it. CreditsLimit is the monthly
	// grant.
	AdminUser struct {
		User
		// Paused is true while the balance is below zero: the account's
		// sites answer 402 until it is paid for again.
		Paused bool       `json:"paused"`
		Usage  AdminUsage `json:"usage"`
	}

	// AdminUsage is what an account hosts and what it costs a month.
	AdminUsage struct {
		Folders        int   `json:"folders"`
		StorageBytes   int64 `json:"storageBytes"`
		StorageBlocks  int   `json:"storageBlocks"`
		Domains        int   `json:"domains"`
		MonthlyCredits int   `json:"monthlyCredits"`
	}

	// ListAdminUsersResponse is GET /setup/users. Next is the before of the
	// next page, empty on the last.
	ListAdminUsersResponse struct {
		Users []AdminUser `json:"users"`
		Next  string      `json:"next"`
	}

	// AdjustCreditsRequest is the POST /setup/users/:id/credits body: a
	// positive amount grants, a negative one deducts.
	AdjustCreditsRequest struct {
		Amount int    `json:"amount"`
		Note   string `json:"note"`
	}

	// CreditAdjustment is a ledger entry.
	CreditAdjustment struct {
		ID           string `json:"id"`
		UserID       string `json:"userId"`
		Actor        string `json:"actor"`
		Amount       int    `json:"amount"`
		Note         string `json:"note"`
		BalanceAfter int    `json:"balanceAfter"`
		CreatedAt    string `json:"createdAt"`
	}

	// AdjustCreditsResponse is the POST /setup/users/:id/credits result.
	AdjustCreditsResponse struct {
		User       AdminUser        `json:"user"`
		Adjustment CreditAdjustment `json:"adjustment"`
	}

	// ListCreditAdjustmentsResponse is GET /setup/users/:id/credits. Next is
	// the before of the next page, empty on the last.
	ListCreditAdjustmentsResponse struct {
		Adjustments []CreditAdjustment `json:"adjustments"`
		Next        string             `json:"next"`
	}
)
