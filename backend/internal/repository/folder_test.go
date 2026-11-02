// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/tmphost/backend/internal/data/model"
)

func TestFolderRepository(t *testing.T) {
	ctx := context.Background()
	repo := NewFolderRepository(newDB(t))
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

	for _, f := range []model.Folder{
		{ID: 1, UserID: 7, ExpiresAt: now.Add(-time.Minute)},
		{ID: 2, UserID: 7, ExpiresAt: now.Add(time.Hour)},
		{ID: 3, UserID: 7, ExpiresAt: now.Add(2 * time.Hour)},
		{ID: 4, UserID: 8, ExpiresAt: now.Add(-2 * time.Minute)},
		{ID: 5, UserID: 8, ExpiresAt: now},
	} {
		_, err := repo.CreateFolder(ctx, f)
		require.NoError(t, err)
	}
	_, err := repo.CreateFolder(ctx, model.Folder{ID: 1, UserID: 9})
	assert.Error(t, err, "ids are unique")

	got, err := repo.FindFolderByID(ctx, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(7), got.UserID)
	assert.True(t, now.Add(time.Hour).Equal(got.ExpiresAt))
	_, err = repo.FindFolderByID(ctx, 99)
	assert.ErrorIs(t, err, ErrNotFound)

	live, err := repo.ListFoldersByUser(ctx, 7, now)
	require.NoError(t, err)
	assert.Equal(t, []int64{3, 2}, folderIDs(live), "live only, newest first")

	expired, err := repo.ListExpiredFolders(ctx, now, 10)
	require.NoError(t, err)
	assert.Equal(t, []int64{4, 1, 5}, folderIDs(expired), "oldest expiry first")
	expired, err = repo.ListExpiredFolders(ctx, now, 1)
	require.NoError(t, err)
	assert.Len(t, expired, 1)

	require.NoError(t, repo.UpdateFolderTitle(ctx, 2, "Holiday photos"))
	got, err = repo.FindFolderByID(ctx, 2)
	require.NoError(t, err)
	assert.Equal(t, "Holiday photos", got.Title)
	got, err = repo.FindFolderByID(ctx, 3)
	require.NoError(t, err)
	assert.Empty(t, got.Title, "untitled by default")

	require.NoError(t, repo.DeleteFolder(ctx, 4))
	_, err = repo.FindFolderByID(ctx, 4)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestFolderRepository_DBErrors(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	repo := NewFolderRepository(db)

	_, err = repo.FindFolderByID(ctx, 1)
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrNotFound)
	_, err = repo.ListFoldersByUser(ctx, 1, time.Now())
	assert.Error(t, err)
	_, err = repo.ListExpiredFolders(ctx, time.Now(), 1)
	assert.Error(t, err)
	assert.Error(t, repo.UpdateFolderTitle(ctx, 1, "x"))
	assert.Error(t, repo.DeleteFolder(ctx, 1))
}

func folderIDs(fs []model.Folder) []int64 {
	ids := []int64{}
	for _, f := range fs {
		ids = append(ids, f.ID)
	}
	return ids
}
