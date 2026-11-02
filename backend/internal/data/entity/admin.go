// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package entity

import "time"

type (
	// AdminUser is an account as root sees it on the setup page: the user,
	// with what it hosts and what that costs a month.
	AdminUser struct {
		User User
		// Usage is what the account hosts.
		Usage Usage
		// StorageBlocks is how many storage blocks Usage takes.
		StorageBlocks int
		// MonthlyCredits is what the next renewal will charge for Usage.
		MonthlyCredits int
	}

	// CreditAdjustment is a ledger entry: credits root granted to (a
	// positive Amount) or deducted from (a negative one) a user.
	CreditAdjustment struct {
		ID           int64
		UserID       int64
		Actor        string
		Amount       int
		Note         string
		BalanceAfter int
		CreatedAt    time.Time
	}

	// ListAdminUsersRequest asks for a page of users, newest first.
	ListAdminUsersRequest struct {
		// Query matches an email or name; empty matches every user.
		Query string
		// Before is the Next of the previous page; 0 starts at the newest.
		Before int64
		Limit  int
	}

	// ListAdminUsersResponse is a page of users.
	ListAdminUsersResponse struct {
		Users []AdminUser
		// Next is the Before of the next page; 0 when this is the last.
		Next int64
	}

	// AdjustCreditsRequest grants (a positive Amount) or deducts (a
	// negative one) a user's credits.
	AdjustCreditsRequest struct {
		UserID int64
		Amount int
		Note   string
	}

	// AdjustCreditsResponse carries the adjusted user and the ledger entry.
	AdjustCreditsResponse struct {
		User       AdminUser
		Adjustment CreditAdjustment
	}

	// ListCreditAdjustmentsRequest asks for a page of a user's ledger,
	// newest first.
	ListCreditAdjustmentsRequest struct {
		UserID int64
		// Before is the Next of the previous page; 0 starts at the newest.
		Before int64
		Limit  int
	}

	// ListCreditAdjustmentsResponse is a page of a user's ledger.
	ListCreditAdjustmentsResponse struct {
		Adjustments []CreditAdjustment
		// Next is the Before of the next page; 0 when this is the last.
		Next int64
	}
)
