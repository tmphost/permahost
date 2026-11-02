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
)

func newDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Folder{}, &model.Domain{}, &model.CreditAdjustment{}))
	return db
}

func TestUserRepository_CRUD(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepository(newDB(t))

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
	repo := NewUserRepository(db)

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
	_, err = repo.AdjustCredits(ctx, model.CreditAdjustment{ID: 1, UserID: 1, Amount: 1})
	assert.Error(t, err)
	_, err = repo.ListCreditAdjustments(ctx, 1, 0, 10)
	assert.Error(t, err)
}

func TestUserRepository_SpendAndRefundCredits(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepository(newDB(t))
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
	repo := NewUserRepository(newDB(t))
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
	repo := NewUserRepository(newDB(t))
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
	repo := NewUserRepository(db)
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
	repo := NewUserRepository(newDB(t))
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

func TestUserRepository_AdjustCredits(t *testing.T) {
	ctx := context.Background()
	repo := NewUserRepository(newDB(t))
	_, err := repo.CreateUser(ctx, model.User{ID: 1, Email: "a@x", Credits: -300})
	require.NoError(t, err)
	at := time.Date(2026, 11, 2, 7, 0, 0, 0, time.UTC)

	_, err = repo.AdjustCredits(ctx, model.CreditAdjustment{ID: 10, UserID: 1, Actor: "root", Amount: -1, Note: "x"})
	assert.ErrorIs(t, err, ErrInsufficientCredits, "already below zero")

	a, err := repo.AdjustCredits(ctx, model.CreditAdjustment{ID: 11, CreatedAt: at, UserID: 1, Actor: "root", Amount: 200, Note: "goodwill"})
	require.NoError(t, err)
	assert.Equal(t, -100, a.BalanceAfter, "a grant may leave the balance below zero")

	a, err = repo.AdjustCredits(ctx, model.CreditAdjustment{ID: 12, CreatedAt: at.Add(time.Minute), UserID: 1, Actor: "root", Amount: 600, Note: "top-up"})
	require.NoError(t, err)
	assert.Equal(t, 500, a.BalanceAfter)

	_, err = repo.AdjustCredits(ctx, model.CreditAdjustment{ID: 13, UserID: 1, Actor: "root", Amount: -501, Note: "too much"})
	assert.ErrorIs(t, err, ErrInsufficientCredits)
	a, err = repo.AdjustCredits(ctx, model.CreditAdjustment{ID: 14, CreatedAt: at.Add(2 * time.Minute), UserID: 1, Actor: "root", Amount: -500, Note: "to zero"})
	require.NoError(t, err)
	assert.Equal(t, 0, a.BalanceAfter, "a deduction may reach zero")

	_, err = repo.AdjustCredits(ctx, model.CreditAdjustment{ID: 15, UserID: 99, Actor: "root", Amount: 1, Note: "x"})
	assert.ErrorIs(t, err, ErrNotFound)

	u, err := repo.FindUserByID(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 0, u.Credits)

	all, err := repo.ListCreditAdjustments(ctx, 1, 0, 10)
	require.NoError(t, err)
	require.Len(t, all, 3, "refused adjustments are not recorded")
	assert.Equal(t, model.CreditAdjustment{ID: 14, CreatedAt: all[0].CreatedAt, UserID: 1, Actor: "root", Amount: -500, Note: "to zero", BalanceAfter: 0}, all[0])
	assert.True(t, at.Add(2*time.Minute).Equal(all[0].CreatedAt))
	assert.Equal(t, []int64{12, 11}, []int64{all[1].ID, all[2].ID}, "newest first")

	page, err := repo.ListCreditAdjustments(ctx, 1, 14, 1)
	require.NoError(t, err)
	require.Len(t, page, 1)
	assert.Equal(t, int64(12), page[0].ID, "below the cursor")

	none, err := repo.ListCreditAdjustments(ctx, 2, 0, 10)
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestUserRepository_AdjustCreditsRace(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	repo := NewUserRepository(db)
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
	a, err := repo.AdjustCredits(ctx, model.CreditAdjustment{ID: 1, UserID: 1, Actor: "root", Amount: -700, Note: "x"})
	assert.ErrorIs(t, err, ErrInsufficientCredits, "worked out again on the new balance")
	assert.Zero(t, a)

	a, err = repo.AdjustCredits(ctx, model.CreditAdjustment{ID: 2, UserID: 1, Actor: "root", Amount: -200, Note: "x"})
	require.NoError(t, err)
	assert.Equal(t, 300, a.BalanceAfter)

	// An update that fails after a good read is an error.
	require.NoError(t, db.Callback().Update().Replace("race", func(tx *gorm.DB) { _ = tx.AddError(errors.New("boom")) }))
	_, err = repo.AdjustCredits(ctx, model.CreditAdjustment{ID: 3, UserID: 1, Actor: "root", Amount: 1, Note: "x"})
	assert.EqualError(t, err, "boom")
	require.NoError(t, db.Callback().Update().Remove("race"))

	// So is a ledger write that fails; the balance is left as it was.
	require.NoError(t, db.Migrator().DropTable(&model.CreditAdjustment{}))
	_, err = repo.AdjustCredits(ctx, model.CreditAdjustment{ID: 4, UserID: 1, Actor: "root", Amount: 1, Note: "x"})
	assert.Error(t, err)
	u, err := repo.FindUserByID(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 300, u.Credits, "rolled back")
}
