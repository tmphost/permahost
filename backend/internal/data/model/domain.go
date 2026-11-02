// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package model

import "time"

// Domain is a custom domain a user points at one of their folders with a
// CNAME record. Several users may claim the same hostname while it is
// pending; only the one whose folder the CNAME names gets it verified, and
// a hostname is verified for at most one folder.
type Domain struct {
	ID        int64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt time.Time
	UserID    int64  `gorm:"index;not null"`
	FolderID  int64  `gorm:"index;not null"`
	Hostname  string `gorm:"type:varchar(253);index;not null"`
	// VerifiedAt is when the CNAME was found and the setup was paid; nil
	// while the domain is pending.
	VerifiedAt *time.Time `gorm:"index"`
	// CheckedAt is when the CNAME was last looked up.
	CheckedAt *time.Time
	// LastError is why the last check failed; empty once verified.
	LastError string `gorm:"type:varchar(255);not null;default:''"`
}
