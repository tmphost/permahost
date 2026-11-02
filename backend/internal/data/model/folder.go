// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package model

import "time"

// Folder is a group of uploaded files hosted together. The files
// themselves live in storage under u<UserID>/<ID>/; only the ownership,
// the size and, for anonymous folders, the expiry are kept here.
type Folder struct {
	ID        int64 `gorm:"primaryKey;autoIncrement:false"`
	CreatedAt time.Time
	UserID    int64 `gorm:"index;not null"`
	// ExpiresAt is set only for anonymous folders; an account's folders
	// stay up as long as the account pays for them.
	ExpiresAt *time.Time `gorm:"index"`
	// SizeBytes is the total size of the folder's files.
	SizeBytes int64 `gorm:"not null;default:0"`
	// Title is the name the owner gave the folder; empty until renamed.
	Title string `gorm:"size:400;not null;default:''"`
}
