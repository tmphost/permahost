// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package model

import "time"

// Device code statuses.
const (
	DeviceCodePending  = "pending"
	DeviceCodeApproved = "approved"
	DeviceCodeDenied   = "denied"
)

// DeviceCode is a pending device authorization (RFC 8628): a device, such
// as the CLI over SSH, polls with its device code while the user enters the
// user code in a browser elsewhere. Only the device code's digest is kept,
// so a leaked table cannot be redeemed.
type DeviceCode struct {
	DeviceCodeHash string `gorm:"primaryKey;size:64"`
	// UserCode is what the user types, without the dash (e.g. WDJBMJHT).
	UserCode   string `gorm:"uniqueIndex;size:16;not null"`
	ClientID   string `gorm:"size:64;not null"`
	ClientName string `gorm:"size:100;not null;default:''"`
	// UserID is who answered; 0 while pending.
	UserID    int64     `gorm:"not null;default:0"`
	Status    string    `gorm:"size:16;not null"`
	ExpiresAt time.Time `gorm:"index;not null"`
	// PolledAt is when the device last asked for its tokens, to tell it to
	// slow down.
	PolledAt  time.Time
	CreatedAt time.Time
}
