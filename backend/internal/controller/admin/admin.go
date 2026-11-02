// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package admin implements the root-only setup page's view of every
// account: totals and daily stats, users with their balance and usage, each
// user's credit history, and granting or deducting one user's credits with
// a note.
//
// An adjustment changes the same balance the hosting charges draw from, so
// it lasts until the next renewal resets the balance to the monthly grant
// minus the monthly cost. Sites are paused while the balance is below zero,
// so a grant that lifts it to zero or more resumes them at once.
package admin

//go:generate mockgen -source=admin.go -destination=mocks/admin.go -package=mocks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	creditctrl "github.com/tmphost/permahost/backend/internal/controller/credit"
	"github.com/tmphost/permahost/backend/internal/data/entity"
	"github.com/tmphost/permahost/backend/internal/data/model"
	mappermodel "github.com/tmphost/permahost/backend/internal/mapper/model"
	"github.com/tmphost/permahost/backend/internal/repository"
)

const (
	// StatsDays is how many UTC days, ending today, the stats and each
	// user's usage cover: one renewal period.
	StatsDays = int(creditctrl.Period / _day)
	// ActorRoot names root as who made an adjustment.
	ActorRoot = "root"
	// DefaultLimit is the page size unless asked otherwise.
	DefaultLimit = 20
	// MaxLimit is the largest page.
	MaxLimit = 100
	// MaxAdjustment is the most credits one adjustment grants or deducts.
	MaxAdjustment = 1_000_000
	// MaxNoteLength is the longest note, in characters.
	MaxNoteLength = 500

	_day = 24 * time.Hour
)

var (
	// ErrUserNotFound is returned for an unknown user.
	ErrUserNotFound = errors.New("user not found")
	// ErrInvalidAdjustment wraps why an adjustment was refused.
	ErrInvalidAdjustment = errors.New("invalid adjustment")
	// ErrInsufficientCredits is returned for a deduction that would take
	// the balance below zero.
	ErrInsufficientCredits = errors.New("the deduction would take the balance below zero")
)

type (
	// Controller is the setup page's user administration.
	Controller interface {
		// GetStats returns the totals over every account and one entry per
		// day of the last StatsDays days.
		GetStats(ctx context.Context, rq *entity.GetAdminStatsRequest) (*entity.GetAdminStatsResponse, error)
		// ListUsers returns a page of users, newest first, with their usage.
		ListUsers(ctx context.Context, rq *entity.ListAdminUsersRequest) (*entity.ListAdminUsersResponse, error)
		// GetUser returns one user with their usage.
		GetUser(ctx context.Context, rq *entity.GetAdminUserRequest) (*entity.GetAdminUserResponse, error)
		// AdjustCredits grants or deducts a user's credits and records it.
		AdjustCredits(ctx context.Context, rq *entity.AdjustCreditsRequest) (*entity.AdjustCreditsResponse, error)
		// ListCreditActivities returns a page of a user's credit history,
		// newest first.
		ListCreditActivities(ctx context.Context, rq *entity.ListCreditActivitiesRequest) (*entity.ListCreditActivitiesResponse, error)
	}

	// Params are the controller's dependencies.
	Params struct {
		Users   repository.UserRepository
		Stats   repository.StatsRepository
		Credits creditctrl.Controller
		// Grant returns the current monthly grant; it can change at runtime.
		Grant func() int
		Now   func() time.Time
	}

	controller struct {
		users   repository.UserRepository
		stats   repository.StatsRepository
		credits creditctrl.Controller
		grant   func() int
		now     func() time.Time
	}
)

// New returns the admin controller.
func New(p Params) Controller {
	c := &controller{users: p.Users, stats: p.Stats, credits: p.Credits, grant: creditctrl.GrantOrDefault(p.Grant), now: p.Now}
	if c.now == nil {
		c.now = time.Now
	}
	return c
}

func (c *controller) ListUsers(ctx context.Context, rq *entity.ListAdminUsersRequest) (*entity.ListAdminUsersResponse, error) {
	limit := pageSize(rq.Limit)
	// One extra row tells whether another page follows.
	ms, err := c.users.SearchUsers(ctx, strings.TrimSpace(rq.Query), rq.Before, limit+1)
	if err != nil {
		return nil, fmt.Errorf("search users: %w", err)
	}
	rs := &entity.ListAdminUsersResponse{}
	if len(ms) > limit {
		ms = ms[:limit]
		rs.Next = ms[limit-1].ID
	}
	if rs.Users, err = c.adminUsers(ctx, ms); err != nil {
		return nil, err
	}
	return rs, nil
}

// since is the start (00:00 UTC) of the first stats day.
func (c *controller) since() time.Time {
	return c.now().UTC().Truncate(_day).Add(-time.Duration(StatsDays-1) * _day)
}

func (c *controller) GetStats(ctx context.Context, _ *entity.GetAdminStatsRequest) (*entity.GetAdminStatsResponse, error) {
	now, since := c.now(), c.since()
	totals, err := c.stats.Totals(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("totals: %w", err)
	}
	used, err := c.stats.DailyCreditUsage(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("daily credit usage: %w", err)
	}
	signups, err := c.stats.DailySignups(ctx, since)
	if err != nil {
		return nil, fmt.Errorf("daily signups: %w", err)
	}
	rs := &entity.GetAdminStatsResponse{
		Users: totals.Users, Credits: totals.Credits, Paused: totals.Paused, Folders: totals.Folders,
		Days: make([]entity.AdminStatsDay, StatsDays),
	}
	index := make(map[string]int, StatsDays)
	for i := range rs.Days {
		rs.Days[i].Date = since.Add(time.Duration(i) * _day).Format(time.DateOnly)
		index[rs.Days[i].Date] = i
	}
	for _, d := range used {
		if i, ok := index[d.Day]; ok {
			rs.Days[i].Used = d.Count
			rs.Used += d.Count
		}
	}
	for _, d := range signups {
		if i, ok := index[d.Day]; ok {
			rs.Days[i].Signups = d.Count
			rs.Signups += d.Count
		}
	}
	return rs, nil
}

func (c *controller) GetUser(ctx context.Context, rq *entity.GetAdminUserRequest) (*entity.GetAdminUserResponse, error) {
	m, err := c.findUser(ctx, rq.UserID)
	if err != nil {
		return nil, err
	}
	us, err := c.adminUsers(ctx, []model.User{m})
	if err != nil {
		return nil, err
	}
	return &entity.GetAdminUserResponse{User: us[0]}, nil
}

func (c *controller) AdjustCredits(ctx context.Context, rq *entity.AdjustCreditsRequest) (*entity.AdjustCreditsResponse, error) {
	note := strings.TrimSpace(rq.Note)
	switch {
	case rq.Amount == 0:
		return nil, fmt.Errorf("%w: the amount must not be zero", ErrInvalidAdjustment)
	case rq.Amount > MaxAdjustment || rq.Amount < -MaxAdjustment:
		return nil, fmt.Errorf("%w: the amount must be between -%d and %d", ErrInvalidAdjustment, MaxAdjustment, MaxAdjustment)
	case note == "":
		return nil, fmt.Errorf("%w: a note is required", ErrInvalidAdjustment)
	case utf8.RuneCountInString(note) > MaxNoteLength:
		return nil, fmt.Errorf("%w: the note must be at most %d characters", ErrInvalidAdjustment, MaxNoteLength)
	}
	a, err := c.users.AdjustCredits(ctx, rq.UserID, rq.Amount, ActorRoot, note)
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return nil, ErrUserNotFound
	case errors.Is(err, repository.ErrInsufficientCredits):
		return nil, ErrInsufficientCredits
	case err != nil:
		return nil, fmt.Errorf("adjust credits: %w", err)
	}
	u, err := c.GetUser(ctx, &entity.GetAdminUserRequest{UserID: rq.UserID})
	if err != nil {
		return nil, err
	}
	return &entity.AdjustCreditsResponse{User: u.User, Activity: mappermodel.FromCreditActivityModelToCreditActivityEntity(a)}, nil
}

func (c *controller) ListCreditActivities(ctx context.Context, rq *entity.ListCreditActivitiesRequest) (*entity.ListCreditActivitiesResponse, error) {
	if _, err := c.findUser(ctx, rq.UserID); err != nil {
		return nil, err
	}
	limit := pageSize(rq.Limit)
	ms, err := c.users.ListCreditActivities(ctx, rq.UserID, rq.Before, limit+1)
	if err != nil {
		return nil, fmt.Errorf("list credit activities: %w", err)
	}
	rs := &entity.ListCreditActivitiesResponse{}
	if len(ms) > limit {
		ms = ms[:limit]
		rs.Next = ms[limit-1].ID
	}
	rs.Activities = mappermodel.FromCreditActivityModelsToCreditActivityEntities(ms)
	return rs, nil
}

func (c *controller) findUser(ctx context.Context, id int64) (model.User, error) {
	m, err := c.users.FindUserByID(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return model.User{}, ErrUserNotFound
	}
	if err != nil {
		return model.User{}, fmt.Errorf("find user: %w", err)
	}
	return m, nil
}

// adminUsers maps users to admin users, with what each hosts and what each
// spent over the stats days.
func (c *controller) adminUsers(ctx context.Context, ms []model.User) ([]entity.AdminUser, error) {
	us := make([]entity.AdminUser, len(ms))
	if len(ms) == 0 {
		return us, nil
	}
	ids := make([]int64, len(ms))
	for i, m := range ms {
		ids[i] = m.ID
	}
	used, err := c.stats.CreditUsageByUser(ctx, ids, c.since())
	if err != nil {
		return nil, fmt.Errorf("credit usage: %w", err)
	}
	grant := c.grant()
	for i, m := range ms {
		usage, err := c.credits.GetUsage(ctx, &entity.GetUsageRequest{UserID: m.ID})
		if err != nil {
			return nil, fmt.Errorf("usage of user %d: %w", m.ID, err)
		}
		us[i] = entity.AdminUser{
			User:           mappermodel.FromUserModelToUserEntity(m, grant),
			Usage:          usage.Usage,
			StorageBlocks:  usage.StorageBlocks,
			MonthlyCredits: usage.MonthlyCredits,
			Used:           used[m.ID],
		}
	}
	return us, nil
}

// pageSize is limit within [1, MaxLimit], DefaultLimit when unset.
func pageSize(limit int) int {
	if limit <= 0 {
		return DefaultLimit
	}
	return min(limit, MaxLimit)
}
