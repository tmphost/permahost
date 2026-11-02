// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package repository

//go:generate mockgen -source=stats.go -destination=mocks/stats.go -package=mocks

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/tmphost/permahost/backend/internal/data/model"
)

// _usageOps are the ops that count as credit usage. A []uint8 would be
// bound as one blob, so they are ints.
var _usageOps = []int{int(model.CreditOpSpend), int(model.CreditOpRefund)}

type (
	// StatsRepository answers the setup page's questions about every
	// account. It only reads.
	StatsRepository interface {
		// Totals counts the users, the credits they hold, the paused
		// accounts and the folders live at now.
		Totals(ctx context.Context, now time.Time) (Totals, error)
		// DailyCreditUsage returns, per UTC day since since, the credits
		// spends took net of refunds. Days without any are left out.
		DailyCreditUsage(ctx context.Context, since time.Time) ([]DayCount, error)
		// DailySignups returns, per UTC day since since, how many users
		// registered. Days without any are left out.
		DailySignups(ctx context.Context, since time.Time) ([]DayCount, error)
		// CreditUsageByUser returns the credits each of the users spent
		// since since, net of refunds. Users who spent none are left out.
		CreditUsageByUser(ctx context.Context, userIDs []int64, since time.Time) (map[int64]int, error)
	}

	// Totals are counts over every account.
	Totals struct {
		Users   int64
		Credits int64
		// Paused is how many accounts have a balance below zero.
		Paused  int64
		Folders int64
	}

	// DayCount is a count for one UTC day, YYYY-MM-DD.
	DayCount struct {
		Day   string
		Count int64
	}

	statsRepository struct {
		db *gorm.DB
	}

	userCount struct {
		UserID int64
		Count  int
	}
)

// NewStatsRepository returns a GORM-backed stats repository.
func NewStatsRepository(db *gorm.DB) StatsRepository {
	return &statsRepository{db: db}
}

// dayExpr is the SQL expression for the UTC day (YYYY-MM-DD text) of a
// time column. SQLite stores times as text with their offset, which its
// date() turns into UTC; Postgres needs the zone named.
func dayExpr(dialect, column string) string {
	if dialect == "postgres" {
		return "to_char(" + column + " AT TIME ZONE 'UTC', 'YYYY-MM-DD')"
	}
	return "date(" + column + ")"
}

func (r *statsRepository) Totals(ctx context.Context, now time.Time) (Totals, error) {
	var t Totals
	db := r.db.WithContext(ctx)
	err := db.Model(&model.User{}).
		Select("COUNT(*) AS users, COALESCE(SUM(credits), 0) AS credits, COALESCE(SUM(CASE WHEN credits < 0 THEN 1 ELSE 0 END), 0) AS paused").
		Scan(&t).Error
	if err != nil {
		return Totals{}, err
	}
	// Account folders never expire; anonymous ones count until they do.
	if err := db.Model(&model.Folder{}).Where("expires_at IS NULL OR expires_at > ?", now).Count(&t.Folders).Error; err != nil {
		return Totals{}, err
	}
	return t, nil
}

func (r *statsRepository) DailyCreditUsage(ctx context.Context, since time.Time) ([]DayCount, error) {
	day := dayExpr(r.db.Name(), "created_at")
	var ds []DayCount
	err := r.db.WithContext(ctx).Model(&model.CreditActivity{}).
		Select(day+" AS day, -SUM(change) AS count").
		Where("created_at >= ? AND ops IN ?", since, _usageOps).
		Group(day).Order("day").
		Scan(&ds).Error
	if err != nil {
		return nil, err
	}
	return ds, nil
}

func (r *statsRepository) DailySignups(ctx context.Context, since time.Time) ([]DayCount, error) {
	day := dayExpr(r.db.Name(), "created_at")
	var ds []DayCount
	err := r.db.WithContext(ctx).Model(&model.User{}).
		Select(day+" AS day, COUNT(*) AS count").
		Where("created_at >= ?", since).
		Group(day).Order("day").
		Scan(&ds).Error
	if err != nil {
		return nil, err
	}
	return ds, nil
}

func (r *statsRepository) CreditUsageByUser(ctx context.Context, userIDs []int64, since time.Time) (map[int64]int, error) {
	var rows []userCount
	err := r.db.WithContext(ctx).Model(&model.CreditActivity{}).
		Select("user_id, -SUM(change) AS count").
		Where("user_id IN ? AND created_at >= ? AND ops IN ?", userIDs, since, _usageOps).
		Group("user_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	counts := make(map[int64]int, len(rows))
	for _, row := range rows {
		if row.Count != 0 {
			counts[row.UserID] = row.Count
		}
	}
	return counts, nil
}
