// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package view

type (
	// AdminStats is GET /setup/stats: totals over every account and one
	// entry per day of the last 30, oldest first.
	AdminStats struct {
		Users   int64           `json:"users"`
		Credits int64           `json:"credits"`
		Paused  int64           `json:"paused"`
		Used    int64           `json:"used"`
		Signups int64           `json:"signups"`
		Folders int64           `json:"folders"`
		Days    []AdminStatsDay `json:"days"`
	}

	// AdminStatsDay is one UTC day of the admin stats.
	AdminStatsDay struct {
		Date    string `json:"date"`
		Used    int64  `json:"used"`
		Signups int64  `json:"signups"`
	}

	// AdminUser is a user as root sees it. CreditsLimit is the monthly
	// grant.
	AdminUser struct {
		User
		// Paused is true while the balance is below zero: the account's
		// sites answer 402 until it is paid for again.
		Paused bool `json:"paused"`
		// Used is the credits spent over the last 30 days, net of refunds.
		Used  int        `json:"used"`
		Usage AdminUsage `json:"usage"`
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

	// GetAdminUserResponse is GET /setup/users/:id.
	GetAdminUserResponse struct {
		User AdminUser `json:"user"`
	}

	// AdjustCreditsRequest is the POST /setup/users/:id/credits body: a
	// positive amount grants, a negative one deducts.
	AdjustCreditsRequest struct {
		Amount int    `json:"amount"`
		Note   string `json:"note"`
	}

	// CreditActivity is one change to a user's balance.
	CreditActivity struct {
		ID      string `json:"id"`
		UserID  string `json:"userId"`
		Ops     string `json:"ops"`
		Change  int    `json:"change"`
		Current int    `json:"current"`
		// Actor and Note are set on root's grants and deductions.
		Actor     string `json:"actor"`
		Note      string `json:"note"`
		CreatedAt string `json:"createdAt"`
	}

	// AdjustCreditsResponse is the POST /setup/users/:id/credits result.
	AdjustCreditsResponse struct {
		User     AdminUser      `json:"user"`
		Activity CreditActivity `json:"activity"`
	}

	// ListCreditActivitiesResponse is GET /setup/users/:id/credits. Next is
	// the before of the next page, empty on the last.
	ListCreditActivitiesResponse struct {
		Activities []CreditActivity `json:"activities"`
		Next       string           `json:"next"`
	}
)
