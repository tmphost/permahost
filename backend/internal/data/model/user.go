// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package model holds the GORM database models.
package model

import "time"

// User is an account. Accounts are keyed by verified email, so signing in
// with Google and GitHub using the same address reaches the same account
// (and the same credit balance).
type User struct {
	ID        int64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt time.Time
	UpdatedAt time.Time
	Email     string `gorm:"type:varchar(255);uniqueIndex;not null"`
	Name      string `gorm:"type:varchar(255)"`
	Picture   string `gorm:"type:text"`
	// Provider is the identity provider the account was registered with.
	Provider string `gorm:"type:varchar(32)"`
	// Credits is the remaining operation balance for the current period.
	Credits int `gorm:"not null;default:0"`
	// CreditsRenewAt is when the daily renewal job next resets the balance
	// to the monthly grant. Always 00:00 UTC.
	CreditsRenewAt time.Time `gorm:"index"`
}
