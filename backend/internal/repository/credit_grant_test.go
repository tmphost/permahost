// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-Commercial

package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tmphost/permahost/backend/internal/data/model"
	"github.com/tmphost/permahost/backend/internal/service/idgen"
)

var (
	// _renewAt is when the test users' monthly part expires; _soon and
	// _late are grant expiries before and after it.
	_renewAt = time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	_soon    = _renewAt.Add(-24 * time.Hour)
	_late    = _renewAt.Add(24 * time.Hour)
)

// grantUser makes user 1 with monthly credits and the given grants, in
// order, and returns the grants' IDs.
func grantUser(t *testing.T, repo UserRepository, monthly int, grants map[time.Time]int, order ...time.Time) []int64 {
	t.Helper()
	ctx := context.Background()
	_, err := repo.CreateUser(ctx, model.User{ID: 1, Email: "a@x", Credits: monthly, CreditsRenewAt: _renewAt})
	require.NoError(t, err)
	ids := []int64{}
	for _, at := range order {
		_, err := repo.AdjustCredits(ctx, 1, grants[at], at, "root", "n")
		require.NoError(t, err)
		gs := remaining(t, repo)
		ids = append(ids, gs[len(gs)-1].ID)
	}
	return ids
}

// remaining lists user 1's grants, oldest first.
func remaining(t *testing.T, repo UserRepository) []model.CreditGrant {
	t.Helper()
	var gs []model.CreditGrant
	require.NoError(t, repo.(*userRepository).db.Order("id").Find(&gs).Error)
	return gs
}

// left is what is left of user 1's grants, oldest first.
func left(t *testing.T, repo UserRepository) []int {
	t.Helper()
	out := []int{}
	for _, g := range remaining(t, repo) {
		out = append(out, g.Remaining)
	}
	return out
}

// balanceOf is user 1's balance.
func balanceOf(t *testing.T, repo UserRepository) int {
	t.Helper()
	u, err := repo.FindUserByID(context.Background(), 1)
	require.NoError(t, err)
	return u.Credits
}

func TestUserRepository_AdjustCreditsKeepsGrants(t *testing.T) {
	repo := NewUserRepository(newDB(t), idgen.New(1))
	grantUser(t, repo, 100, map[time.Time]int{_late: 300}, _late)

	gs := remaining(t, repo)
	require.Len(t, gs, 1)
	assert.Equal(t, int64(1), gs[0].UserID)
	assert.Equal(t, 300, gs[0].Credits)
	assert.Equal(t, 300, gs[0].Remaining)
	assert.True(t, _late.Equal(gs[0].ExpiresAt))
	assert.Equal(t, 400, balanceOf(t, repo))

	// A deduction takes the monthly part first: it expires sooner.
	_, err := repo.AdjustCredits(context.Background(), 1, -150, time.Time{}, "root", "n")
	require.NoError(t, err)
	assert.Equal(t, []int{250}, left(t, repo))
	assert.Equal(t, 250, balanceOf(t, repo))
	assert.Len(t, remaining(t, repo), 1, "a deduction makes no grant")
}

func TestUserRepository_ChargesTakeWhatExpiresFirst(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepository(newDB(t), idgen.New(1))
	grantUser(t, repo, 1000, map[time.Time]int{_soon: 300, _late: 500, _renewAt: 200}, _late, _soon, _renewAt)
	assert.Equal(t, 2000, balanceOf(t, repo))

	require.NoError(t, repo.SpendCredits(ctx, 1, 400))
	assert.Equal(t, []int{500, 0, 200}, left(t, repo), "the grant expiring before renewal goes first, then the monthly part")

	spent, err := repo.PayStorageBlocks(ctx, 1, 2, 500)
	require.NoError(t, err)
	require.Equal(t, 1000, spent)
	assert.Equal(t, []int{500, 0, 100}, left(t, repo), "the monthly part goes before a grant expiring with it")

	require.NoError(t, repo.RefundCredits(ctx, 1, 50))
	assert.Equal(t, []int{500, 0, 100}, left(t, repo), "a refund goes to the monthly part")

	require.NoError(t, repo.SpendCredits(ctx, 1, 600))
	assert.Equal(t, []int{50, 0, 0}, left(t, repo))
	assert.Equal(t, 50, balanceOf(t, repo))
}

func TestUserRepository_ChargesWithoutGrants(t *testing.T) {
	repo := NewUserRepository(newDB(t), idgen.New(1))
	grantUser(t, repo, 100, nil)
	require.NoError(t, repo.SpendCredits(context.Background(), 1, 60))
	assert.Equal(t, 40, balanceOf(t, repo))
	assert.Empty(t, left(t, repo))
}

func TestUserRepository_RenewalKeepsGrants(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepository(newDB(t), idgen.New(1))
	grantUser(t, repo, 100, map[time.Time]int{_soon: 300, _late: 500}, _late, _soon)
	require.NoError(t, repo.SpendCredits(ctx, 1, 200))
	assert.Equal(t, []int{500, 100}, left(t, repo), "the grant expiring before renewal pays")

	next := _renewAt.Add(30 * 24 * time.Hour)
	balance, err := repo.UpdateUserCredits(ctx, 1, 1500, 1, next)
	require.NoError(t, err)
	assert.Equal(t, 2100, balance, "the monthly part plus what is left of the grants")
	assert.Equal(t, 2100, balanceOf(t, repo))
	assert.Equal(t, []int{500, 100}, left(t, repo), "the monthly grant pays for the period")

	// When the monthly grant can't pay for the period, the grants do.
	balance, err = repo.UpdateUserCredits(ctx, 1, -300, 1, next)
	require.NoError(t, err)
	assert.Equal(t, 300, balance)
	assert.Equal(t, []int{300, 0}, left(t, repo), "the grant expiring first pays first")

	// Even when they can't pay for all of it.
	balance, err = repo.UpdateUserCredits(ctx, 1, -1000, 1, next)
	require.NoError(t, err)
	assert.Equal(t, -700, balance)
	assert.Equal(t, []int{0, 0}, left(t, repo))
	rn := int(model.CreditOpRenewal)
	acts := activities(t, repo, 1)
	assert.Equal(t, [][3]int{{rn, 1400, 2100}, {rn, -1800, 300}, {rn, -1000, -700}}, acts[len(acts)-3:])

	// An unknown user is left alone.
	balance, err = repo.UpdateUserCredits(ctx, 99, 5, 0, next)
	require.NoError(t, err)
	assert.Zero(t, balance)
}

func TestUserRepository_ExpireCreditGrant(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepository(newDB(t), idgen.New(1))
	ids := grantUser(t, repo, 100, map[time.Time]int{_soon: 300, _late: 500}, _soon, _late)
	require.NoError(t, repo.SpendCredits(ctx, 1, 50))
	_, err := repo.CreateUser(ctx, model.User{ID: 2, Email: "b@x"})
	require.NoError(t, err)
	_, err = repo.AdjustCredits(ctx, 2, 10, _soon, "root", "n")
	require.NoError(t, err)

	gs, err := repo.ListExpiredCreditGrants(ctx, _soon.Add(-time.Second), 10)
	require.NoError(t, err)
	assert.Empty(t, gs, "not expired yet")
	gs, err = repo.ListExpiredCreditGrants(ctx, _late, 10)
	require.NoError(t, err)
	require.Len(t, gs, 3)
	assert.Equal(t, ids[0], gs[0].ID, "the first to expire first")
	assert.Equal(t, ids[1], gs[2].ID)
	gs, err = repo.ListExpiredCreditGrants(ctx, _soon, 1)
	require.NoError(t, err)
	require.Len(t, gs, 1)

	// A grant is read again before it expires: a charge may have taken from it.
	stale := gs[0]
	require.NoError(t, repo.SpendCredits(ctx, 1, 100))
	require.NoError(t, repo.ExpireCreditGrant(ctx, stale))
	assert.Equal(t, []int{0, 500, 10}, left(t, repo))
	assert.Equal(t, 600, balanceOf(t, repo))
	acts := activities(t, repo, 1)
	assert.Equal(t, [3]int{int(model.CreditOpExpiry), -150, 600}, acts[len(acts)-1], "what the charges left of it")

	// Expiring it again changes nothing.
	require.NoError(t, repo.ExpireCreditGrant(ctx, stale))
	assert.Equal(t, 600, balanceOf(t, repo))
	assert.Len(t, activities(t, repo, 1), len(acts))

	gs, err = repo.ListExpiredCreditGrants(ctx, _soon, 10)
	require.NoError(t, err)
	require.Len(t, gs, 1, "only grants with credits left")
	assert.Equal(t, int64(2), gs[0].UserID)

	// An unknown grant is an error.
	assert.ErrorIs(t, repo.ExpireCreditGrant(ctx, model.CreditGrant{ID: 99, UserID: 1}), gorm.ErrRecordNotFound)
}

func TestUserRepository_ListUserIDs(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepository(newDB(t), idgen.New(1))
	for _, id := range []int64{3, 1, 2} {
		_, err := repo.CreateUser(ctx, model.User{ID: id, Email: string(rune('a'+id)) + "@x"})
		require.NoError(t, err)
	}
	ids, err := repo.ListUserIDs(ctx, 0, 2)
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 2}, ids)
	ids, err = repo.ListUserIDs(ctx, 2, 2)
	require.NoError(t, err)
	assert.Equal(t, []int64{3}, ids)
	ids, err = repo.ListUserIDs(ctx, 3, 2)
	require.NoError(t, err)
	assert.Empty(t, ids)
}

func TestUserRepository_CreditGrantDBErrors(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	repo := NewUserRepository(db, idgen.New(1))

	_, err = repo.ListUserIDs(ctx, 0, 10)
	assert.Error(t, err)
	_, err = repo.ListExpiredCreditGrants(ctx, time.Now(), 10)
	assert.Error(t, err)
	assert.Error(t, repo.ExpireCreditGrant(ctx, model.CreditGrant{ID: 1, UserID: 1}))
}

// failOn makes db fail the next writes (update) or reads (query) that
// match, after skipping the first skip of them.
func failOn(t *testing.T, db *gorm.DB, query bool, skip int, match func(tx *gorm.DB) bool) {
	t.Helper()
	seen := 0
	fail := func(tx *gorm.DB) {
		if !match(tx) {
			return
		}
		if seen++; seen > skip {
			_ = tx.AddError(errors.New("boom"))
		}
	}
	if query {
		require.NoError(t, db.Callback().Query().Before("gorm:query").Register("fail", fail))
	} else {
		require.NoError(t, db.Callback().Update().Before("gorm:update").Register("fail", fail))
	}
	t.Cleanup(func() {
		_ = db.Callback().Query().Remove("fail")
		_ = db.Callback().Update().Remove("fail")
	})
}

func table(name string) func(tx *gorm.DB) bool {
	return func(tx *gorm.DB) bool { return tx.Statement.Table == name }
}

func TestUserRepository_CreditGrantFailures(t *testing.T) {
	ctx := context.Background()
	setup := func(t *testing.T) (*gorm.DB, UserRepository) {
		t.Helper()
		db := newDB(t)
		repo := NewUserRepository(db, idgen.New(1))
		grantUser(t, repo, 100, map[time.Time]int{_soon: 300}, _soon)
		return db, repo
	}
	unchanged := func(t *testing.T, repo UserRepository) {
		t.Helper()
		assert.Equal(t, 400, balanceOf(t, repo), "rolled back")
		assert.Equal(t, []int{300}, left(t, repo))
	}

	t.Run("reading the grants", func(t *testing.T) {
		db, repo := setup(t)
		failOn(t, db, true, 0, table("credit_grants"))
		assert.EqualError(t, repo.SpendCredits(ctx, 1, 1), "boom")
		_, err := repo.PayStorageBlocks(ctx, 1, 1, 1)
		assert.EqualError(t, err, "boom")
		_, err = repo.AdjustCredits(ctx, 1, -1, time.Time{}, "root", "n")
		assert.EqualError(t, err, "boom")
		assert.EqualError(t, renew(ctx, repo, 1, 5, 0, _renewAt), "boom")
		require.NoError(t, db.Callback().Query().Remove("fail"))
		unchanged(t, repo)
	})

	t.Run("reading the user for a charge", func(t *testing.T) {
		db, repo := setup(t)
		failOn(t, db, true, 0, func(tx *gorm.DB) bool { return tx.Statement.Table == "users" && len(tx.Statement.Selects) == 2 })
		assert.EqualError(t, repo.SpendCredits(ctx, 1, 1), "boom")
		require.NoError(t, db.Callback().Query().Remove("fail"))
		unchanged(t, repo)
	})

	t.Run("saving a grant", func(t *testing.T) {
		db, repo := setup(t)
		failOn(t, db, false, 0, table("credit_grants"))
		assert.EqualError(t, repo.SpendCredits(ctx, 1, 200), "boom")
		assert.EqualError(t, renew(ctx, repo, 1, -5, 0, _renewAt), "boom")
		assert.EqualError(t, repo.ExpireCreditGrant(ctx, remaining(t, repo)[0]), "boom")
		require.NoError(t, db.Callback().Update().Remove("fail"))
		unchanged(t, repo)
	})

	t.Run("creating a grant", func(t *testing.T) {
		db, repo := setup(t)
		require.NoError(t, db.Callback().Create().Before("gorm:create").Register("fail", func(tx *gorm.DB) {
			if tx.Statement.Table == "credit_grants" {
				_ = tx.AddError(errors.New("boom"))
			}
		}))
		_, err := repo.AdjustCredits(ctx, 1, 5, _soon, "root", "n")
		assert.EqualError(t, err, "boom")
		unchanged(t, repo)
	})

	t.Run("setting the renewed balance", func(t *testing.T) {
		db, repo := setup(t)
		// The first users update locks the row; the second sets the balance.
		failOn(t, db, false, 1, table("users"))
		assert.EqualError(t, renew(ctx, repo, 1, 5, 0, _renewAt), "boom")
		require.NoError(t, db.Callback().Update().Remove("fail"))
		unchanged(t, repo)
	})

	t.Run("expiring", func(t *testing.T) {
		db, repo := setup(t)
		g := remaining(t, repo)[0]
		failOn(t, db, false, 0, table("users"))
		assert.EqualError(t, repo.ExpireCreditGrant(ctx, g), "boom", "locking the user")
		require.NoError(t, db.Callback().Update().Remove("fail"))
		// The first users update locks the row; the second takes the credits.
		failOn(t, db, false, 1, table("users"))
		assert.EqualError(t, repo.ExpireCreditGrant(ctx, g), "boom")
		require.NoError(t, db.Callback().Update().Remove("fail"))
		unchanged(t, repo)

		require.NoError(t, db.Migrator().DropTable(&model.CreditActivity{}))
		assert.Error(t, repo.ExpireCreditGrant(ctx, g), "recording it")
		unchanged(t, repo)
	})
}
