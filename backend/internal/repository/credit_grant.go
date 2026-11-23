// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Commercial

package repository

import (
	"context"
	"sort"
	"time"

	"gorm.io/gorm"

	"github.com/tmphost/permahost/backend/internal/data/model"
)

// A user's balance is the monthly part plus what is left of their live
// admin grants (model.CreditGrant). Charges take the credits that expire
// first, refunds go back to the monthly part, the renewal resets the
// monthly part, and the daily job takes away what is left of a grant once
// it expires. Every write to a grant happens in a transaction that has
// already written the user's row, so changes to one user's grants never
// overlap.

func (r *userRepository) ListUserIDs(ctx context.Context, after int64, limit int) ([]int64, error) {
	var ids []int64
	err := r.db.WithContext(ctx).Model(&model.User{}).
		Where("id > ?", after).
		Order("id").
		Limit(limit).
		Pluck("id", &ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func (r *userRepository) ListExpiredCreditGrants(ctx context.Context, now time.Time, limit int) ([]model.CreditGrant, error) {
	var gs []model.CreditGrant
	err := r.db.WithContext(ctx).
		Where("expires_at <= ? AND remaining > 0", now).
		Order("expires_at, id").
		Limit(limit).
		Find(&gs).Error
	if err != nil {
		return nil, err
	}
	return gs, nil
}

func (r *userRepository) ExpireCreditGrant(ctx context.Context, g model.CreditGrant) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lock the user first, so no charge takes from the grant while it
		// expires; then read what is left of it.
		if err := tx.Model(&model.User{}).Where("id = ?", g.UserID).Update("credits", gorm.Expr("credits")).Error; err != nil {
			return err
		}
		if err := tx.Where("id = ?", g.ID).Take(&g).Error; err != nil {
			return err
		}
		if g.Remaining == 0 {
			return nil
		}
		if err := tx.Model(&model.CreditGrant{}).Where("id = ?", g.ID).Update("remaining", 0).Error; err != nil {
			return err
		}
		err := tx.Model(&model.User{}).Where("id = ?", g.UserID).
			Update("credits", gorm.Expr("credits - ?", g.Remaining)).Error
		if err != nil {
			return err
		}
		return r.record(tx, g.UserID, model.CreditOpExpiry, -g.Remaining)
	})
}

// adjustGrants keeps a root adjustment of amount, already applied to the
// user's balance in tx, in their grants: a grant becomes a new grant that
// expires at expiresAt, a deduction takes from the grants like a charge.
func (r *userRepository) adjustGrants(tx *gorm.DB, id int64, amount int, expiresAt time.Time) error {
	if amount < 0 {
		return drawGrants(tx, id, -amount)
	}
	g := model.CreditGrant{ID: r.ids.NextID(), UserID: id, ExpiresAt: expiresAt, Credits: amount, Remaining: amount}
	return tx.Create(&g).Error
}

// drawGrants takes from a user's grants their share of a charge of n
// credits, already taken from the user's balance in tx.
func drawGrants(tx *gorm.DB, id int64, n int) error {
	grants, err := liveGrants(tx, id)
	if err != nil || len(grants) == 0 {
		return err
	}
	var u model.User
	if err := tx.Select("credits", "credits_renew_at").Where("id = ?", id).Take(&u).Error; err != nil {
		return err
	}
	return takeFromGrants(tx, grants, u.Credits+n-granted(grants), u.CreditsRenewAt, n)
}

// liveGrants returns a user's admin grants that still have credits left,
// inside tx, the ones that expire first first.
func liveGrants(tx *gorm.DB, id int64) ([]model.CreditGrant, error) {
	var gs []model.CreditGrant
	if err := tx.Where("user_id = ? AND remaining > 0", id).Order("expires_at, id").Find(&gs).Error; err != nil {
		return nil, err
	}
	return gs, nil
}

// granted sums what is left of grants.
func granted(gs []model.CreditGrant) int {
	n := 0
	for _, g := range gs {
		n += g.Remaining
	}
	return n
}

// takeFromGrants takes n credits, the ones that expire first first, from
// the monthly part (monthly credits, expiring at renewAt) and gs, and
// saves what is left of each grant it took from.
func takeFromGrants(tx *gorm.DB, gs []model.CreditGrant, monthly int, renewAt time.Time, n int) error {
	for _, g := range spendFromGrants(gs, monthly, renewAt, n) {
		if err := tx.Model(&model.CreditGrant{}).Where("id = ?", g.ID).Update("remaining", g.Remaining).Error; err != nil {
			return err
		}
	}
	return nil
}

// spendFromGrants works out takeFromGrants: it returns the grants it took
// from, with their new Remaining.
func spendFromGrants(gs []model.CreditGrant, monthly int, renewAt time.Time, n int) []model.CreditGrant {
	type bucket struct {
		grant     int // index into gs; -1 is the monthly part
		expiresAt time.Time
		left      int
	}
	bs := []bucket{{grant: -1, expiresAt: renewAt, left: max(monthly, 0)}}
	for i, g := range gs {
		bs = append(bs, bucket{grant: i, expiresAt: g.ExpiresAt, left: g.Remaining})
	}
	// Stable, so the monthly part goes before a grant expiring with it.
	sort.SliceStable(bs, func(i, j int) bool { return bs[i].expiresAt.Before(bs[j].expiresAt) })
	var changed []model.CreditGrant
	for _, b := range bs {
		take := min(n, b.left)
		if take == 0 {
			continue
		}
		n -= take
		if b.grant >= 0 {
			g := gs[b.grant]
			g.Remaining -= take
			changed = append(changed, g)
		}
	}
	return changed
}
