// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package entity

type (
	// RenewDueCreditsResponse reports how many accounts were renewed, and
	// how many of them the grant could not pay for.
	RenewDueCreditsResponse struct {
		Renewed   int
		Suspended int
	}

	// Pricing is what hosting costs, in credits.
	Pricing struct {
		// StorageBlockBytes of stored files cost StorageCredits a month.
		StorageBlockBytes int64
		StorageCredits    int
		// FolderCredits is the monthly price of a folder.
		FolderCredits int
		// DomainCredits is the monthly price of a custom domain, and
		// DomainSetupCredits is charged once when it is verified.
		DomainCredits      int
		DomainSetupCredits int
	}

	// Usage is what an account hosts.
	Usage struct {
		Folders      int
		StorageBytes int64
		Domains      int
	}

	// GetUsageRequest asks for an account's usage.
	GetUsageRequest struct {
		UserID int64
	}

	// GetUsageResponse carries an account's usage and what it costs.
	GetUsageResponse struct {
		Usage Usage
		// StorageBlocks is how many storage blocks Usage takes.
		StorageBlocks int
		// MonthlyCredits is what the next renewal will charge for Usage.
		MonthlyCredits int
		Pricing        Pricing
	}
)
