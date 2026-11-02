// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package model

import (
	"github.com/tmphost/permahost/backend/internal/data/entity"
	"github.com/tmphost/permahost/backend/internal/data/model"
)

// FromCreditAdjustmentModelToCreditAdjustmentEntity maps a ledger entry.
func FromCreditAdjustmentModelToCreditAdjustmentEntity(m model.CreditAdjustment) entity.CreditAdjustment {
	return entity.CreditAdjustment{
		ID:           m.ID,
		UserID:       m.UserID,
		Actor:        m.Actor,
		Amount:       m.Amount,
		Note:         m.Note,
		BalanceAfter: m.BalanceAfter,
		CreatedAt:    m.CreatedAt,
	}
}

// FromCreditAdjustmentModelsToCreditAdjustmentEntities maps ledger entries;
// none maps to an empty, non-nil slice.
func FromCreditAdjustmentModelsToCreditAdjustmentEntities(ms []model.CreditAdjustment) []entity.CreditAdjustment {
	es := make([]entity.CreditAdjustment, len(ms))
	for i, m := range ms {
		es[i] = FromCreditAdjustmentModelToCreditAdjustmentEntity(m)
	}
	return es
}
