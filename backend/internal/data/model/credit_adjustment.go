// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package model

import "time"

// CreditAdjustment is a ledger entry: credits root granted to (a positive
// Amount) or deducted from (a negative one) a user's balance.
type CreditAdjustment struct {
	ID        int64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt time.Time
	UserID    int64 `gorm:"index;not null"`
	// Actor is who made the adjustment; only root can.
	Actor  string `gorm:"type:varchar(32);not null"`
	Amount int    `gorm:"not null"`
	Note   string `gorm:"type:varchar(500);not null"`
	// BalanceAfter is the user's balance right after the adjustment.
	BalanceAfter int `gorm:"not null"`
}
