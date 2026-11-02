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

func TestDeviceCodeRepository(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	repo := NewDeviceCodeRepository(db)
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

	pending := func(hash, userCode string, expiresAt time.Time) model.DeviceCode {
		return model.DeviceCode{DeviceCodeHash: hash, UserCode: userCode, ClientID: "cli", ClientName: "permahost CLI", Status: model.DeviceCodePending, ExpiresAt: expiresAt}
	}
	require.NoError(t, repo.CreateDeviceCode(ctx, pending("h1", "BCDFGHJK", now.Add(time.Minute))))
	require.NoError(t, repo.CreateDeviceCode(ctx, pending("h2", "LMNPQRST", now)))
	assert.Error(t, repo.CreateDeviceCode(ctx, pending("h3", "BCDFGHJK", now.Add(time.Minute))), "user codes are unique")
	assert.Error(t, repo.CreateDeviceCode(ctx, pending("h1", "VWXZBCDF", now.Add(time.Minute))), "device codes are unique")

	got, err := repo.FindPendingDeviceCode(ctx, "BCDFGHJK", now)
	require.NoError(t, err)
	assert.Equal(t, "h1", got.DeviceCodeHash)
	_, err = repo.FindPendingDeviceCode(ctx, "LMNPQRST", now)
	assert.ErrorIs(t, err, ErrNotFound, "expired")
	_, err = repo.FindPendingDeviceCode(ctx, "VWXZBCDF", now)
	assert.ErrorIs(t, err, ErrNotFound)

	// Polls are recorded.
	require.NoError(t, repo.PollDeviceCode(ctx, "h1", now))
	got, err = repo.FindDeviceCodeByHash(ctx, "h1")
	require.NoError(t, err)
	assert.True(t, now.Equal(got.PolledAt))

	// An answer is given once, and only while live.
	require.NoError(t, repo.AnswerDeviceCode(ctx, "h1", 7, model.DeviceCodeApproved, now))
	assert.ErrorIs(t, repo.AnswerDeviceCode(ctx, "h1", 8, model.DeviceCodeDenied, now), ErrNotFound)
	assert.ErrorIs(t, repo.AnswerDeviceCode(ctx, "h2", 7, model.DeviceCodeApproved, now), ErrNotFound)
	got, err = repo.FindDeviceCodeByHash(ctx, "h1")
	require.NoError(t, err)
	assert.Equal(t, int64(7), got.UserID)
	assert.Equal(t, model.DeviceCodeApproved, got.Status)
	_, err = repo.FindPendingDeviceCode(ctx, "BCDFGHJK", now)
	assert.ErrorIs(t, err, ErrNotFound, "no longer pending")

	// Deleting redeems once.
	require.NoError(t, repo.DeleteDeviceCode(ctx, "h1"))
	assert.ErrorIs(t, repo.DeleteDeviceCode(ctx, "h1"), ErrNotFound)
	_, err = repo.FindDeviceCodeByHash(ctx, "h1")
	assert.ErrorIs(t, err, ErrNotFound)

	// The sweep deletes what expired.
	require.NoError(t, repo.CreateDeviceCode(ctx, pending("h4", "VWXZBCDF", now.Add(time.Second))))
	require.NoError(t, repo.DeleteExpiredDeviceCodes(ctx, now))
	_, err = repo.FindDeviceCodeByHash(ctx, "h2")
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = repo.FindDeviceCodeByHash(ctx, "h4")
	assert.NoError(t, err)

	// Database failures come back as they are.
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	_, err = repo.FindDeviceCodeByHash(ctx, "h4")
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrNotFound)
	assert.Error(t, repo.DeleteDeviceCode(ctx, "h4"))
	assert.NotErrorIs(t, repo.DeleteDeviceCode(ctx, "h4"), ErrNotFound)
}
