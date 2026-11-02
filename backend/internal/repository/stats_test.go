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
	"gorm.io/gorm"

	"github.com/tmphost/permahost/backend/internal/data/model"
)

var _statsNow = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

// seedStats stores three users, their credit activities and folders.
func seedStats(t *testing.T, db *gorm.DB) {
	t.Helper()
	day := 24 * time.Hour
	east := time.FixedZone("east", 3*3600)
	require.NoError(t, db.Create(&[]model.User{
		{ID: 1, Email: "ann@example.com", Name: "Ann", Credits: 100, CreatedAt: _statsNow.Add(-40 * day)},
		{ID: 2, Email: "bob@example.com", Name: "Bob", Credits: -50, CreatedAt: _statsNow.Add(-2 * day)},
		// 2026-10-21 01:00 east is 2026-10-20 22:00 UTC.
		{ID: 3, Email: "cy@test.org", Name: "Cy", Credits: 7, CreatedAt: time.Date(2026, 10, 21, 1, 0, 0, 0, east)},
	}).Error)
	require.NoError(t, db.Create(&[]model.CreditActivity{
		{ID: 10, UserID: 1, Ops: model.CreditOpGrant, Change: 1000, CreatedAt: _statsNow.Add(-2 * day)},
		{ID: 11, UserID: 1, Ops: model.CreditOpSpend, Change: -500, CreatedAt: _statsNow.Add(-2 * day)},
		{ID: 12, UserID: 1, Ops: model.CreditOpRefund, Change: 200, CreatedAt: _statsNow.Add(-2 * day)},
		{ID: 13, UserID: 2, Ops: model.CreditOpSpend, Change: -400, CreatedAt: _statsNow},
		{ID: 14, UserID: 1, Ops: model.CreditOpSpend, Change: -900, CreatedAt: _statsNow.Add(-40 * day)},
		// Root's changes and renewals are not usage.
		{ID: 15, UserID: 2, Ops: model.CreditOpAdminDeduct, Change: -50, CreatedAt: _statsNow},
		{ID: 16, UserID: 2, Ops: model.CreditOpRenewal, Change: -70, CreatedAt: _statsNow},
		// Fully refunded: nets to zero.
		{ID: 17, UserID: 3, Ops: model.CreditOpSpend, Change: -1, CreatedAt: _statsNow},
		{ID: 18, UserID: 3, Ops: model.CreditOpRefund, Change: 1, CreatedAt: _statsNow},
	}).Error)
	soon, gone := _statsNow.Add(time.Hour), _statsNow.Add(-time.Hour)
	require.NoError(t, db.Create(&[]model.Folder{
		{ID: 20, UserID: 1},
		{ID: 21, UserID: 1, ExpiresAt: &soon},
		{ID: 22, UserID: 0, ExpiresAt: &gone},
	}).Error)
}

func TestStatsRepository(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	seedStats(t, db)
	repo := NewStatsRepository(db)
	since := _statsNow.Add(-30 * 24 * time.Hour)

	totals, err := repo.Totals(ctx, _statsNow)
	require.NoError(t, err)
	assert.Equal(t, Totals{Users: 3, Credits: 57, Paused: 1, Folders: 2}, totals)

	usage, err := repo.DailyCreditUsage(ctx, since)
	require.NoError(t, err)
	assert.Equal(t, []DayCount{{Day: "2026-10-19", Count: 300}, {Day: "2026-10-21", Count: 400}}, usage)

	signups, err := repo.DailySignups(ctx, since)
	require.NoError(t, err)
	assert.Equal(t, []DayCount{{Day: "2026-10-19", Count: 1}, {Day: "2026-10-20", Count: 1}}, signups)

	byUser, err := repo.CreditUsageByUser(ctx, []int64{1, 2, 3}, since)
	require.NoError(t, err)
	assert.Equal(t, map[int64]int{1: 300, 2: 400}, byUser)
}

func TestStatsRepository_Empty(t *testing.T) {
	totals, err := NewStatsRepository(newDB(t)).Totals(context.Background(), _statsNow)
	require.NoError(t, err)
	assert.Equal(t, Totals{}, totals)
}

func TestStatsRepository_Errors(t *testing.T) {
	ctx := context.Background()
	db := newDB(t)
	repo := NewStatsRepository(db)
	require.NoError(t, db.Migrator().DropTable(&model.Folder{}))

	_, err := repo.Totals(ctx, _statsNow)
	assert.Error(t, err, "folders")

	require.NoError(t, db.Migrator().DropTable(&model.User{}, &model.CreditActivity{}))
	_, err = repo.Totals(ctx, _statsNow)
	assert.Error(t, err, "users")
	_, err = repo.DailyCreditUsage(ctx, _statsNow)
	assert.Error(t, err)
	_, err = repo.DailySignups(ctx, _statsNow)
	assert.Error(t, err)
	_, err = repo.CreditUsageByUser(ctx, []int64{1}, _statsNow)
	assert.Error(t, err)
}

func TestDayExpr(t *testing.T) {
	assert.Equal(t, "date(created_at)", dayExpr("sqlite", "created_at"))
	assert.Equal(t, "to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD')", dayExpr("postgres", "created_at"))
}
