// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package model

import (
	"strconv"

	"github.com/tmphost/permahost/backend/internal/data/entity"
	"github.com/tmphost/permahost/backend/internal/data/model"
)

// _creditOps name the stored credit activity ops.
var _creditOps = map[uint8]string{
	model.CreditOpGrant:       "grant",
	model.CreditOpRenewal:     "renewal",
	model.CreditOpSpend:       "spend",
	model.CreditOpRefund:      "refund",
	model.CreditOpAdminGrant:  "admin_grant",
	model.CreditOpAdminDeduct: "admin_deduct",
}

// FromCreditOpToCreditOpName names a stored op; an unknown one is its
// number.
func FromCreditOpToCreditOpName(op uint8) string {
	if name, ok := _creditOps[op]; ok {
		return name
	}
	return strconv.Itoa(int(op))
}

// FromCreditActivityModelToCreditActivityEntity maps a credit activity.
func FromCreditActivityModelToCreditActivityEntity(m model.CreditActivity) entity.CreditActivity {
	return entity.CreditActivity{
		ID:        m.ID,
		UserID:    m.UserID,
		Ops:       FromCreditOpToCreditOpName(m.Ops),
		Change:    m.Change,
		Current:   m.Current,
		Actor:     m.Actor,
		Note:      m.Note,
		CreatedAt: m.CreatedAt,
	}
}

// FromCreditActivityModelsToCreditActivityEntities maps credit activities;
// none maps to an empty, non-nil slice.
func FromCreditActivityModelsToCreditActivityEntities(ms []model.CreditActivity) []entity.CreditActivity {
	es := make([]entity.CreditActivity, len(ms))
	for i, m := range ms {
		es[i] = FromCreditActivityModelToCreditActivityEntity(m)
	}
	return es
}
