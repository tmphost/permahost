// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Commercial

package model

import "time"

// CreditGrant is a block of credits root gave a user. It is part of the
// user's balance until it expires; then whatever is left of it is taken
// away. The monthly renewal keeps live grants, and uses them for what the
// monthly grant can't pay for. Its ID is a monoflake ID.
type CreditGrant struct {
	ID        int64     `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt time.Time `gorm:"not null"`
	UserID    int64     `gorm:"index;not null"`
	// ExpiresAt is when what is left of the grant is taken away. Always
	// 00:00 UTC, so it lines up with a run of the daily job.
	ExpiresAt time.Time `gorm:"index;not null"`
	// Credits is the amount granted.
	Credits int `gorm:"not null"`
	// Remaining is the part of Credits not spent yet.
	Remaining int `gorm:"not null"`
}
