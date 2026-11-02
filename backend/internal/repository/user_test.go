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
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Folder{}, &model.Domain{}))
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
