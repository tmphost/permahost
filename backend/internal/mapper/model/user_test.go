// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/model"
)

func TestFromUserModelToUserEntity(t *testing.T) {
	now := time.Now()
	got := FromUserModelToUserEntity(model.User{
		ID: 7, Email: "a@b.c", Name: "A", Picture: "p", Provider: "google",
		Credits: 12, CreditsRenewAt: now, CreatedAt: now,
	}, 250)
	assert.Equal(t, entity.User{
		ID: 7, Email: "a@b.c", Name: "A", Picture: "p", Provider: "google",
		Credits: 12, CreditsLimit: 250, CreditsRenewAt: now, CreatedAt: now,
	}, got)
}
