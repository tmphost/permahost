// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package model

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/tmphost/permahost/backend/internal/data/entity"
	"github.com/tmphost/permahost/backend/internal/data/model"
)

func TestFromCreditAdjustmentModelsToCreditAdjustmentEntities(t *testing.T) {
	now := time.Now()
	got := FromCreditAdjustmentModelsToCreditAdjustmentEntities([]model.CreditAdjustment{
		{ID: 3, CreatedAt: now, UserID: 7, Actor: "root", Amount: -50, Note: "n", BalanceAfter: 150},
	})
	assert.Equal(t, []entity.CreditAdjustment{
		{ID: 3, CreatedAt: now, UserID: 7, Actor: "root", Amount: -50, Note: "n", BalanceAfter: 150},
	}, got)

	none := FromCreditAdjustmentModelsToCreditAdjustmentEntities(nil)
	assert.NotNil(t, none)
	assert.Empty(t, none)
}
