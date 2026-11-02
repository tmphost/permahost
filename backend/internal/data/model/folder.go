// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package model

import "time"

// Folder is a group of uploaded files that expire together. The files
// themselves live in storage under u<UserID>/<ID>/; only the ownership and
// the expiry are kept here.
type Folder struct {
	ID        int64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt time.Time
	UserID    int64     `gorm:"index;not null"`
	ExpiresAt time.Time `gorm:"index;not null"`
	// Title is the name the owner gave the folder; empty until renamed.
	Title string `gorm:"size:400;not null;default:''"`
}
