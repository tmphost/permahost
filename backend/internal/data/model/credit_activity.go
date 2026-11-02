// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package model

import "time"

// Credit activity operations, as stored. Zero is never stored.
const (
	// CreditOpGrant is the grant a new account starts with.
	CreditOpGrant uint8 = iota + 1
	// CreditOpRenewal resets the balance to the grant minus the monthly
	// cost.
	CreditOpRenewal
	// CreditOpSpend pays for hosting: a folder, a storage block or a
	// custom domain.
	CreditOpSpend
	// CreditOpRefund gives back what a failed operation was paid with.
	CreditOpRefund
	// CreditOpAdminGrant adds credits root granted.
	CreditOpAdminGrant
	// CreditOpAdminDeduct takes away credits root deducted.
	CreditOpAdminDeduct
)

// CreditActivity is one change to a user's credit balance. It is written
// in the same transaction as the change, so a user's history always ends
// at the current balance. Its ID is a monoflake ID, so IDs follow the
// order activities happened in.
type CreditActivity struct {
	ID        int64     `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt time.Time `gorm:"index;not null"`
	UserID    int64     `gorm:"index;not null"`
	// Ops is what changed the balance: one of the CreditOp* constants.
	Ops uint8 `gorm:"not null"`
	// Change is the signed amount: +X added, -Y taken.
	Change int `gorm:"not null"`
	// Current is the balance right after the change.
	Current int `gorm:"not null"`
	// Actor is who made an admin change ("root"); empty for changes the
	// system makes.
	Actor string `gorm:"type:varchar(32);not null;default:''"`
	// Note is why root made an admin change; empty otherwise.
	Note string `gorm:"type:varchar(500);not null;default:''"`
}
