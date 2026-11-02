// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/tmphost/permahost/backend/internal/data/model"
	"github.com/tmphost/permahost/backend/internal/service/idgen"
)

func newDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Folder{}, &model.Domain{}, &model.CreditActivity{}, &model.DeviceCode{}))
	return db
}

func TestUserRepository_CRUD(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepository(newDB(t), idgen.New(1))

	_, err := repo.FindUserByEmail(ctx, "a@b.c")
	assert.ErrorIs(t, err, ErrNotFound)

	renew := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	created, err := repo.CreateUser(ctx, model.User{ID: 1, Email: "a@b.c", Name: "A", Credits: 250, CreditsRenewAt: renew})
	require.NoError(t, err)
	assert.Equal(t, int64(1), created.ID)

	_, err = repo.CreateUser(ctx, model.User{ID: 2, Email: "a@b.c"})
	assert.Error(t, err, "email is unique")

	byEmail, err := repo.FindUserByEmail(ctx, "a@b.c")
	require.NoError(t, err)
	assert.Equal(t, "A", byEmail.Name)
	assert.True(t, renew.Equal(byEmail.CreditsRenewAt))

	byEmail.Credits = 10
	updated, err := repo.UpdateUser(ctx, byEmail)
	require.NoError(t, err)
	assert.Equal(t, 10, updated.Credits)

	byID, err := repo.FindUserByID(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 10, byID.Credits)

	_, err = repo.FindUserByID(ctx, 99)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestUserRepository_DBErrors(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	repo := NewUserRepository(db, idgen.New(1))

	_, err = repo.FindUserByID(ctx, 1)
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrNotFound)

	_, err = repo.UpdateUser(ctx, model.User{ID: 1, Email: "x"})
	assert.Error(t, err)

	_, err = repo.ListUsersDueForCreditRenewal(ctx, time.Now(), 10)
	assert.Error(t, err)

	assert.Error(t, repo.UpdateUserCredits(ctx, 1, 250, 0, time.Now()))
	_, err = repo.PayStorageBlocks(ctx, 1, 1, 500)
	assert.Error(t, err)
	assert.Error(t, repo.RefundStorageBlocks(ctx, 1, 1, 500))
	assert.Error(t, repo.SpendCredits(ctx, 1, 1))
	assert.NotErrorIs(t, repo.SpendCredits(ctx, 1, 1), ErrInsufficientCredits)
	assert.Error(t, repo.RefundCredits(ctx, 1, 1))
	_, err = repo.SearchUsers(ctx, "a", 0, 10)
	assert.Error(t, err)
	_, err = repo.AdjustCredits(ctx, 1, 1, "root", "n")
	assert.Error(t, err)
	_, err = repo.ListCreditActivities(ctx, 1, 0, 10)
	assert.Error(t, err)
}

func TestUserRepository_SpendAndRefundCredits(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepository(newDB(t), idgen.New(1))
	_, err := repo.CreateUser(ctx, model.User{ID: 1, Email: "a@x", Credits: 5})
	require.NoError(t, err)

	require.NoError(t, repo.SpendCredits(ctx, 1, 3))
	assert.ErrorIs(t, repo.SpendCredits(ctx, 1, 3), ErrInsufficientCredits, "only 2 left")
	assert.ErrorIs(t, repo.SpendCredits(ctx, 99, 1), ErrInsufficientCredits, "unknown user")
	require.NoError(t, repo.SpendCredits(ctx, 1, 2))
	require.NoError(t, repo.RefundCredits(ctx, 1, 4))

	u, err := repo.FindUserByID(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 4, u.Credits)
}

func TestUserRepository_CreditRenewal(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepository(newDB(t), idgen.New(1))
	now := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)

	for i, renewAt := range []time.Time{now.Add(time.Hour), now, now.Add(-48 * time.Hour), now.Add(-24 * time.Hour)} {
		_, err := repo.CreateUser(ctx, model.User{ID: int64(i + 1), Email: string(rune('a'+i)) + "@x", Name: "keep", Credits: 1, CreditsRenewAt: renewAt})
		require.NoError(t, err)
	}

	due, err := repo.ListUsersDueForCreditRenewal(ctx, now, 10)
	require.NoError(t, err)
	ids := []int64{}
	for _, u := range due {
		ids = append(ids, u.ID)
	}
	assert.Equal(t, []int64{3, 4, 2}, ids, "oldest first, future excluded")

	page, err := repo.ListUsersDueForCreditRenewal(ctx, now, 1)
	require.NoError(t, err)
	assert.Len(t, page, 1)

	next := now.Add(30 * 24 * time.Hour)
	require.NoError(t, repo.UpdateUserCredits(ctx, 3, -250, 2, next))
	u, err := repo.FindUserByID(ctx, 3)
	require.NoError(t, err)
	assert.Equal(t, -250, u.Credits)
	assert.Equal(t, 2, u.StorageBlocksPaid)
	assert.True(t, next.Equal(u.CreditsRenewAt))
	assert.Equal(t, "keep", u.Name, "other columns untouched")
}

func TestUserRepository_PayStorageBlocks(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepository(newDB(t), idgen.New(1))
	_, err := repo.CreateUser(ctx, model.User{ID: 1, Email: "a@x", Credits: 1200, StorageBlocksPaid: 1})
	require.NoError(t, err)

	spent, err := repo.PayStorageBlocks(ctx, 1, 1, 500)
	require.NoError(t, err)
	assert.Zero(t, spent, "already paid for")

	spent, err = repo.PayStorageBlocks(ctx, 1, 3, 500)
	require.NoError(t, err)
	assert.Equal(t, 1000, spent, "two blocks added")

	_, err = repo.PayStorageBlocks(ctx, 1, 4, 500)
	assert.ErrorIs(t, err, ErrInsufficientCredits, "200 left")

	require.NoError(t, repo.RefundStorageBlocks(ctx, 1, 2, 1000))
	u, err := repo.FindUserByID(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 1200, u.Credits)
	assert.Equal(t, 1, u.StorageBlocksPaid)

	_, err = repo.PayStorageBlocks(ctx, 99, 1, 500)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestUserRepository_PayStorageBlocksRace(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	repo := NewUserRepository(db, idgen.New(1))
	_, err := repo.CreateUser(ctx, model.User{ID: 1, Email: "a@x", Credits: 2000})
	require.NoError(t, err)

	// Another upload pays for a block between the read and the update.
	raced := false
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("race", func(tx *gorm.DB) {
		if !raced {
			raced = true
			require.NoError(t, tx.Session(&gorm.Session{NewDB: true, SkipHooks: true}).Exec(
				"UPDATE users SET credits = credits - 500, storage_blocks_paid = 1 WHERE id = 1").Error)
		}
	}))
	spent, err := repo.PayStorageBlocks(ctx, 1, 2, 500)
	require.NoError(t, err)
	assert.Equal(t, 500, spent, "only the block nobody paid for")
	u, err := repo.FindUserByID(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 1000, u.Credits)
	assert.Equal(t, 2, u.StorageBlocksPaid)

	// An update that fails after a good read is an error.
	require.NoError(t, db.Callback().Update().Replace("race", func(tx *gorm.DB) { _ = tx.AddError(errors.New("boom")) }))
	_, err = repo.PayStorageBlocks(ctx, 1, 3, 500)
	assert.EqualError(t, err, "boom")
}

func TestUserRepository_SearchUsers(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepository(newDB(t), idgen.New(1))
	for _, u := range []model.User{
		{ID: 1, Email: "ada@lovelace.dev", Name: "Ada Lovelace"},
		{ID: 2, Email: "grace@navy.mil", Name: "Grace Hopper"},
		{ID: 3, Email: "alan@turing.org", Name: "Alan"},
		{ID: 4, Email: "x_y%z@odd.dev", Name: ""},
	} {
		_, err := repo.CreateUser(ctx, u)
		require.NoError(t, err)
	}
	ids := func(q string, before int64, limit int) []int64 {
		t.Helper()
		us, err := repo.SearchUsers(ctx, q, before, limit)
		require.NoError(t, err)
		out := []int64{}
		for _, u := range us {
			out = append(out, u.ID)
		}
		return out
	}

	assert.Equal(t, []int64{4, 3, 2, 1}, ids("", 0, 10), "newest first")
	assert.Equal(t, []int64{4, 3}, ids("", 0, 2))
	assert.Equal(t, []int64{2, 1}, ids("", 3, 10), "below the cursor")
	assert.Equal(t, []int64{1}, ids("LOVELACE", 0, 10), "email or name, any case")
	assert.Equal(t, []int64{2}, ids("hopper", 0, 10), "name only")
	assert.Equal(t, []int64{2, 1}, ids("a", 3, 10), "search with a cursor")
	assert.Equal(t, []int64{4}, ids("_y%", 0, 10), "wildcards match themselves")
	assert.Empty(t, ids("%", 4, 10))
	assert.Empty(t, ids("nobody", 0, 10))
}

// activities lists a user's credit activities, oldest first, as
// ops/change/current.
func activities(t *testing.T, repo UserRepository, userID int64) [][3]int {
	t.Helper()
	as, err := repo.ListCreditActivities(context.Background(), userID, 0, 100)
	require.NoError(t, err)
	out := [][3]int{}
	for i := len(as) - 1; i >= 0; i-- {
		out = append(out, [3]int{int(as[i].Ops), as[i].Change, as[i].Current})
	}
	return out
}

func TestUserRepository_RecordsEveryBalanceChange(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepository(newDB(t), idgen.New(1))
	_, err := repo.CreateUser(ctx, model.User{ID: 1, Email: "a@x", Credits: 2000})
	require.NoError(t, err)

	require.NoError(t, repo.SpendCredits(ctx, 1, 500))
	require.ErrorIs(t, repo.SpendCredits(ctx, 1, 5000), ErrInsufficientCredits)
	require.NoError(t, repo.RefundCredits(ctx, 1, 500))
	spent, err := repo.PayStorageBlocks(ctx, 1, 2, 500)
	require.NoError(t, err)
	require.Equal(t, 1000, spent)
	_, err = repo.PayStorageBlocks(ctx, 1, 2, 500)
	require.NoError(t, err, "already paid: nothing recorded")
	require.NoError(t, repo.RefundStorageBlocks(ctx, 1, 1, 500))
	require.NoError(t, repo.UpdateUserCredits(ctx, 1, 300, 1, time.Now()))
	_, err = repo.AdjustCredits(ctx, 1, 200, "root", "n")
	require.NoError(t, err)
	_, err = repo.AdjustCredits(ctx, 1, -100, "root", "n")
	require.NoError(t, err)

	g, sp, rf, rn := int(model.CreditOpGrant), int(model.CreditOpSpend), int(model.CreditOpRefund), int(model.CreditOpRenewal)
	ag, ad := int(model.CreditOpAdminGrant), int(model.CreditOpAdminDeduct)
	assert.Equal(t, [][3]int{
		{g, 2000, 2000},
		{sp, -500, 1500},
		{rf, 500, 2000},
		{sp, -1000, 1000},
		{rf, 500, 1500},
		{rn, -1200, 300},
		{ag, 200, 500},
		{ad, -100, 400},
	}, activities(t, repo, 1))

	// Changes to an unknown user change and record nothing.
	require.NoError(t, repo.RefundCredits(ctx, 99, 1))
	require.NoError(t, repo.RefundStorageBlocks(ctx, 99, 1, 1))
	require.NoError(t, repo.UpdateUserCredits(ctx, 99, 1, 0, time.Now()))
	assert.Empty(t, activities(t, repo, 99))
}

func TestUserRepository_RecordFailures(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	repo := NewUserRepository(db, idgen.New(1))
	_, err := repo.CreateUser(ctx, model.User{ID: 1, Email: "a@x", Credits: 2000})
	require.NoError(t, err)

	// Reading the balance back fails: the change is rolled back.
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("fail", func(tx *gorm.DB) {
		if tx.Statement.Table == "users" && len(tx.Statement.Selects) == 1 {
			_ = tx.AddError(errors.New("boom"))
		}
	}))
	assert.EqualError(t, repo.SpendCredits(ctx, 1, 1), "boom")
	assert.EqualError(t, repo.UpdateUserCredits(ctx, 1, 5, 0, time.Now()), "boom", "reading the old balance")
	require.NoError(t, db.Callback().Query().Remove("fail"))

	// So does writing the activity.
	require.NoError(t, db.Migrator().DropTable(&model.CreditActivity{}))
	_, err = repo.CreateUser(ctx, model.User{ID: 2, Email: "b@x"})
	assert.Error(t, err)
	assert.Error(t, repo.SpendCredits(ctx, 1, 1))
	assert.Error(t, repo.RefundCredits(ctx, 1, 1))
	assert.Error(t, repo.UpdateUserCredits(ctx, 1, 5, 0, time.Now()))
	_, err = repo.PayStorageBlocks(ctx, 1, 1, 1)
	assert.Error(t, err)
	u, err := repo.FindUserByID(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 2000, u.Credits, "rolled back")
	assert.Zero(t, u.StorageBlocksPaid)
	_, err = repo.FindUserByID(ctx, 2)
	assert.ErrorIs(t, err, ErrNotFound)

	// A failing update is an error too.
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("fail", func(tx *gorm.DB) { _ = tx.AddError(errors.New("boom")) }))
	assert.EqualError(t, repo.UpdateUserCredits(ctx, 1, 5, 0, time.Now()), "boom")
	assert.EqualError(t, repo.SpendCredits(ctx, 1, 1), "boom")
}

func TestUserRepository_AdjustCredits(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepository(newDB(t), idgen.New(1))
	_, err := repo.CreateUser(ctx, model.User{ID: 1, Email: "a@x", Credits: -300})
	require.NoError(t, err)

	_, err = repo.AdjustCredits(ctx, 1, -1, "root", "x")
	assert.ErrorIs(t, err, ErrInsufficientCredits, "already below zero")

	a, err := repo.AdjustCredits(ctx, 1, 200, "root", "goodwill")
	require.NoError(t, err)
	assert.Equal(t, -100, a.Current, "a grant may leave the balance below zero")
	assert.NotZero(t, a.ID)
	assert.Equal(t, model.CreditOpAdminGrant, a.Ops)

	a, err = repo.AdjustCredits(ctx, 1, 600, "root", "top-up")
	require.NoError(t, err)
	assert.Equal(t, 500, a.Current)

	_, err = repo.AdjustCredits(ctx, 1, -501, "root", "too much")
	assert.ErrorIs(t, err, ErrInsufficientCredits)
	a, err = repo.AdjustCredits(ctx, 1, -500, "root", "to zero")
	require.NoError(t, err)
	assert.Equal(t, 0, a.Current, "a deduction may reach zero")
	assert.Equal(t, model.CreditOpAdminDeduct, a.Ops)

	_, err = repo.AdjustCredits(ctx, 99, 1, "root", "x")
	assert.ErrorIs(t, err, ErrNotFound)

	u, err := repo.FindUserByID(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 0, u.Credits)

	all, err := repo.ListCreditActivities(ctx, 1, 0, 10)
	require.NoError(t, err)
	require.Len(t, all, 4, "the sign-up grant and three adjustments; refused ones are not recorded")
	assert.Equal(t, model.CreditActivity{ID: a.ID, CreatedAt: all[0].CreatedAt, UserID: 1, Ops: model.CreditOpAdminDeduct, Change: -500, Current: 0, Actor: "root", Note: "to zero"}, all[0])
	assert.WithinDuration(t, time.Now(), all[0].CreatedAt, time.Minute)

	page, err := repo.ListCreditActivities(ctx, 1, all[0].ID, 1)
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, all[1].ID, page[0].ID, "below the cursor")

	none, err := repo.ListCreditActivities(ctx, 2, 0, 10)
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestUserRepository_AdjustCreditsRace(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	repo := NewUserRepository(db, idgen.New(1))
	_, err := repo.CreateUser(ctx, model.User{ID: 1, Email: "a@x", Credits: 1000})
	require.NoError(t, err)

	// A charge lands between the read and the update.
	raced := false
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("race", func(tx *gorm.DB) {
		if !raced {
			raced = true
			require.NoError(t, tx.Session(&gorm.Session{NewDB: true, SkipHooks: true}).Exec(
				"UPDATE users SET credits = credits - 500 WHERE id = 1").Error)
		}
	}))
	a, err := repo.AdjustCredits(ctx, 1, -700, "root", "x")
	assert.ErrorIs(t, err, ErrInsufficientCredits, "worked out again on the new balance")
	assert.Zero(t, a)

	a, err = repo.AdjustCredits(ctx, 1, -200, "root", "x")
	require.NoError(t, err)
	assert.Equal(t, 300, a.Current)

	// An update that fails after a good read is an error.
	require.NoError(t, db.Callback().Update().Replace("race", func(tx *gorm.DB) { _ = tx.AddError(errors.New("boom")) }))
	_, err = repo.AdjustCredits(ctx, 1, 1, "root", "x")
	assert.EqualError(t, err, "boom")
	require.NoError(t, db.Callback().Update().Remove("race"))

	// So is a ledger write that fails; the balance is left as it was.
	require.NoError(t, db.Migrator().DropTable(&model.CreditActivity{}))
	_, err = repo.AdjustCredits(ctx, 1, 1, "root", "x")
	assert.Error(t, err)
	u, err := repo.FindUserByID(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 300, u.Credits, "rolled back")
}
