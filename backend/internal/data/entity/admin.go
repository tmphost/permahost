// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package entity

import "time"

type (
	// GetAdminStatsRequest asks for the stats over every account.
	GetAdminStatsRequest struct{}

	// GetAdminStatsResponse is the stats over every account.
	GetAdminStatsResponse struct {
		// Users is how many accounts there are.
		Users int64
		// Credits is the credits every account holds together.
		Credits int64
		// Paused is how many accounts have a balance below zero.
		Paused int64
		// Used is the credits spent over Days, net of refunds.
		Used int64
		// Signups is how many accounts registered over Days.
		Signups int64
		// Folders is how many folders are live.
		Folders int64
		// Days has one entry per UTC day, oldest first, ending today.
		Days []AdminStatsDay
	}

	// AdminStatsDay is one UTC day of the admin stats.
	AdminStatsDay struct {
		// Date is the day, YYYY-MM-DD.
		Date string
		// Used is the credits spent that day, net of refunds.
		Used int64
		// Signups is how many accounts registered that day.
		Signups int64
	}

	// AdminUser is an account as root sees it on the setup page: the user,
	// with what it hosts, what that costs a month and what it spent.
	AdminUser struct {
		User User
		// Usage is what the account hosts.
		Usage Usage
		// StorageBlocks is how many storage blocks Usage takes.
		StorageBlocks int
		// MonthlyCredits is what the next renewal will charge for Usage.
		MonthlyCredits int
		// Used is the credits the user spent over the stats days, net of
		// refunds.
		Used int
	}

	// CreditActivity is one change to a user's balance.
	CreditActivity struct {
		ID     int64
		UserID int64
		// Ops is what changed the balance: grant, renewal, spend, refund,
		// admin_grant or admin_deduct.
		Ops string
		// Change is the signed amount.
		Change int
		// Current is the balance right after the change.
		Current int
		// Actor is who made an admin change; empty for the system's.
		Actor string
		// Note is why root made an admin change.
		Note      string
		CreatedAt time.Time
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

	// GetAdminUserRequest asks for one account.
	GetAdminUserRequest struct {
		UserID int64
	}

	// GetAdminUserResponse carries one account.
	GetAdminUserResponse struct {
		User AdminUser
	}

	// AdjustCreditsRequest grants (a positive Amount) or deducts (a
	// negative one) a user's credits.
	AdjustCreditsRequest struct {
		UserID int64
		Amount int
		Note   string
	}

	// AdjustCreditsResponse carries the adjusted user and the activity
	// that records it.
	AdjustCreditsResponse struct {
		User     AdminUser
		Activity CreditActivity
	}

	// ListCreditActivitiesRequest asks for a page of a user's credit
	// history, newest first.
	ListCreditActivitiesRequest struct {
		UserID int64
		// Before is the Next of the previous page; 0 starts at the newest.
		Before int64
		Limit  int
	}

	// ListCreditActivitiesResponse is a page of a user's credit history.
	ListCreditActivitiesResponse struct {
		Activities []CreditActivity
		// Next is the Before of the next page; 0 when this is the last.
		Next int64
	}
)
