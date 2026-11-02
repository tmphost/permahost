// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/permahost/backend/internal/data/model"
)

func TestDomainRepository(t *testing.T) {
	ctx := context.Background()
	repo := NewDomainRepository(newDB(t))
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { t := now.Add(d); return &t }

	for _, d := range []model.Domain{
		{ID: 1, CreatedAt: now.Add(-time.Hour), UserID: 7, FolderID: 70, Hostname: "a.example.com", VerifiedAt: at(-time.Minute), CheckedAt: at(-time.Minute)},
		{ID: 2, CreatedAt: now.Add(-time.Hour), UserID: 7, FolderID: 70, Hostname: "b.example.com", CheckedAt: at(-time.Minute)},
		{ID: 3, CreatedAt: now.Add(-2 * time.Hour), UserID: 8, FolderID: 80, Hostname: "a.example.com"},
		{ID: 4, CreatedAt: now.Add(-10 * 24 * time.Hour), UserID: 8, FolderID: 80, Hostname: "old.example.com"},
		{ID: 5, CreatedAt: now.Add(-time.Hour), UserID: 7, FolderID: 71, Hostname: "c.example.com", VerifiedAt: at(0)},
	} {
		_, err := repo.CreateDomain(ctx, d)
		require.NoError(t, err)
	}
	_, err := repo.CreateDomain(ctx, model.Domain{ID: 1, Hostname: "x.io"})
	assert.Error(t, err, "ids are unique")

	got, err := repo.FindDomainByID(ctx, 2)
	require.NoError(t, err)
	assert.Equal(t, "b.example.com", got.Hostname)
	_, err = repo.FindDomainByID(ctx, 99)
	assert.ErrorIs(t, err, ErrNotFound)

	got, err = repo.FindVerifiedDomainByHostname(ctx, "a.example.com")
	require.NoError(t, err)
	assert.Equal(t, int64(1), got.ID, "the verified claim, not the pending one")
	_, err = repo.FindVerifiedDomainByHostname(ctx, "b.example.com")
	assert.ErrorIs(t, err, ErrNotFound, "pending")

	byFolder, err := repo.ListDomainsByFolder(ctx, 70)
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 2}, domainIDs(byFolder))

	pending, err := repo.ListPendingDomains(ctx, now.Add(-7*24*time.Hour), 10)
	require.NoError(t, err)
	assert.Equal(t, []int64{3, 2}, domainIDs(pending), "never checked first, too old left out")
	pending, err = repo.ListPendingDomains(ctx, now.Add(-7*24*time.Hour), 1)
	require.NoError(t, err)
	assert.Len(t, pending, 1)

	n, err := repo.CountVerifiedDomainsByUser(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	require.NoError(t, repo.UpdateDomainCheck(ctx, 3, now, nil, "no CNAME"))
	got, err = repo.FindDomainByID(ctx, 3)
	require.NoError(t, err)
	assert.True(t, now.Equal(*got.CheckedAt))
	assert.Nil(t, got.VerifiedAt)
	assert.Equal(t, "no CNAME", got.LastError)
	require.NoError(t, repo.UpdateDomainCheck(ctx, 2, now, &now, ""))
	got, err = repo.FindDomainByID(ctx, 2)
	require.NoError(t, err)
	assert.True(t, now.Equal(*got.VerifiedAt))

	require.NoError(t, repo.DeleteDomain(ctx, 3))
	_, err = repo.FindDomainByID(ctx, 3)
	assert.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, repo.DeleteDomainsByFolder(ctx, 70))
	byFolder, err = repo.ListDomainsByFolder(ctx, 70)
	require.NoError(t, err)
	assert.Empty(t, byFolder)
}

func TestDomainRepository_DBErrors(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	repo := NewDomainRepository(db)

	_, err = repo.CreateDomain(ctx, model.Domain{ID: 1})
	assert.Error(t, err)
	_, err = repo.FindDomainByID(ctx, 1)
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrNotFound)
	_, err = repo.ListDomainsByFolder(ctx, 1)
	assert.Error(t, err)
	_, err = repo.ListPendingDomains(ctx, time.Now(), 1)
	assert.Error(t, err)
	_, err = repo.CountVerifiedDomainsByUser(ctx, 1)
	assert.Error(t, err)
	assert.Error(t, repo.UpdateDomainCheck(ctx, 1, time.Now(), nil, ""))
	assert.Error(t, repo.DeleteDomain(ctx, 1))
	assert.Error(t, repo.DeleteDomainsByFolder(ctx, 1))
}

func domainIDs(ds []model.Domain) []int64 {
	ids := []int64{}
	for _, d := range ds {
		ids = append(ids, d.ID)
	}
	return ids
}
