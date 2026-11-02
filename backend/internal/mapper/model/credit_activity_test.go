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

func TestFromCreditActivityModelsToCreditActivityEntities(t *testing.T) {
	now := time.Now()
	got := FromCreditActivityModelsToCreditActivityEntities([]model.CreditActivity{
		{ID: 3, CreatedAt: now, UserID: 7, Ops: model.CreditOpAdminDeduct, Change: -50, Current: 150, Actor: "root", Note: "n"},
	})
	assert.Equal(t, []entity.CreditActivity{
		{ID: 3, CreatedAt: now, UserID: 7, Ops: "admin_deduct", Change: -50, Current: 150, Actor: "root", Note: "n"},
	}, got)

	none := FromCreditActivityModelsToCreditActivityEntities(nil)
	assert.NotNil(t, none)
	assert.Empty(t, none)
}

func TestFromCreditOpToCreditOpName(t *testing.T) {
	for op, name := range map[uint8]string{
		model.CreditOpGrant: "grant", model.CreditOpRenewal: "renewal", model.CreditOpSpend: "spend",
		model.CreditOpRefund: "refund", model.CreditOpAdminGrant: "admin_grant", model.CreditOpAdminDeduct: "admin_deduct", 42: "42",
	} {
		assert.Equal(t, name, FromCreditOpToCreditOpName(op))
	}
}
