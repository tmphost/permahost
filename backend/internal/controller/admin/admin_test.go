// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package admin

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mockcredit "github.com/tmphost/permahost/backend/internal/controller/credit/mocks"
	"github.com/tmphost/permahost/backend/internal/data/entity"
	"github.com/tmphost/permahost/backend/internal/data/model"
	"github.com/tmphost/permahost/backend/internal/repository"
	mockrepo "github.com/tmphost/permahost/backend/internal/repository/mocks"
)

var (
	_now = time.Date(2026, 11, 2, 7, 0, 0, 0, time.UTC)
	// _since is the first stats day: 29 days before today.
	_since  = time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
)

type env struct {
	c       Controller
	users   *mockrepo.MockUserRepository
	stats   *mockrepo.MockStatsRepository
	credits *mockcredit.MockController
}

func newEnv(t *testing.T) env {
	t.Helper()
	ctrl := gomock.NewController(t)
	e := env{users: mockrepo.NewMockUserRepository(ctrl), stats: mockrepo.NewMockStatsRepository(ctrl), credits: mockcredit.NewMockController(ctrl)}
	e.c = New(Params{
		Users:   e.users,
		Stats:   e.stats,
		Credits: e.credits,
		Grant:   func() int { return 2000 },
		Now:     func() time.Time { return _now },
	})
	return e
}

func usage(folders int) *entity.GetUsageResponse {
	return &entity.GetUsageResponse{
		Usage:          entity.Usage{Folders: folders, StorageBytes: 10, Domains: 1},
		StorageBlocks:  1,
		MonthlyCredits: folders*500 + 1000,
	}
}

func TestNewDefaultsNow(t *testing.T) {
	c := New(Params{}).(*controller)
	assert.WithinDuration(t, time.Now(), c.now(), time.Minute)
	assert.Equal(t, 2000, c.grant(), "the default grant")
	assert.Equal(t, 30, StatsDays)
}

func TestGetStats(t *testing.T) {
	t.Run("totals and one entry per day", func(t *testing.T) {
		e := newEnv(t)
		e.stats.EXPECT().Totals(gomock.Any(), _now).Return(repository.Totals{Users: 3, Credits: 900, Paused: 1, Folders: 4}, nil)
		e.stats.EXPECT().DailyCreditUsage(gomock.Any(), _since).Return([]repository.DayCount{
			{Day: "2026-10-03", Count: 99}, {Day: "2026-10-04", Count: 500}, {Day: "2026-11-02", Count: 1000},
		}, nil)
		e.stats.EXPECT().DailySignups(gomock.Any(), _since).Return([]repository.DayCount{{Day: "2026-10-20", Count: 2}}, nil)

		rs, err := e.c.GetStats(context.Background(), &entity.GetAdminStatsRequest{})
		require.NoError(t, err)
		assert.Equal(t, int64(3), rs.Users)
		assert.Equal(t, int64(900), rs.Credits)
		assert.Equal(t, int64(1), rs.Paused)
		assert.Equal(t, int64(4), rs.Folders)
		assert.Equal(t, int64(1500), rs.Used, "days before the window are left out")
		assert.Equal(t, int64(2), rs.Signups)
		require.Len(t, rs.Days, StatsDays)
		assert.Equal(t, entity.AdminStatsDay{Date: "2026-10-04", Used: 500}, rs.Days[0])
		assert.Equal(t, entity.AdminStatsDay{Date: "2026-11-02", Used: 1000}, rs.Days[StatsDays-1])
		assert.Equal(t, entity.AdminStatsDay{Date: "2026-10-20", Signups: 2}, rs.Days[16])
	})

	for name, fail := range map[string]func(e env){
		"totals": func(e env) {
			e.stats.EXPECT().Totals(gomock.Any(), gomock.Any()).Return(repository.Totals{}, errBoom)
		},
		"usage": func(e env) {
			e.stats.EXPECT().Totals(gomock.Any(), gomock.Any()).Return(repository.Totals{}, nil)
			e.stats.EXPECT().DailyCreditUsage(gomock.Any(), gomock.Any()).Return(nil, errBoom)
		},
		"signups": func(e env) {
			e.stats.EXPECT().Totals(gomock.Any(), gomock.Any()).Return(repository.Totals{}, nil)
			e.stats.EXPECT().DailyCreditUsage(gomock.Any(), gomock.Any()).Return(nil, nil)
			e.stats.EXPECT().DailySignups(gomock.Any(), gomock.Any()).Return(nil, errBoom)
		},
	} {
		t.Run(name+" fails", func(t *testing.T) {
			e := newEnv(t)
			fail(e)
			_, err := e.c.GetStats(context.Background(), &entity.GetAdminStatsRequest{})
			assert.ErrorIs(t, err, errBoom)
		})
	}
}

func TestListUsers(t *testing.T) {
	t.Run("a page with a next cursor", func(t *testing.T) {
		e := newEnv(t)
		e.users.EXPECT().SearchUsers(gomock.Any(), "ada", int64(9), 3).Return([]model.User{
			{ID: 5, Email: "a@x", Credits: 700}, {ID: 4, Email: "b@x", Credits: -100}, {ID: 3, Email: "c@x"},
		}, nil)
		e.stats.EXPECT().CreditUsageByUser(gomock.Any(), []int64{5, 4}, _since).Return(map[int64]int{5: 1500}, nil)
		e.credits.EXPECT().GetUsage(gomock.Any(), &entity.GetUsageRequest{UserID: 5}).Return(usage(1), nil)
		e.credits.EXPECT().GetUsage(gomock.Any(), &entity.GetUsageRequest{UserID: 4}).Return(usage(2), nil)

		rs, err := e.c.ListUsers(context.Background(), &entity.ListAdminUsersRequest{Query: "  ada ", Before: 9, Limit: 2})
		require.NoError(t, err)
		assert.Equal(t, int64(4), rs.Next)
		require.Len(t, rs.Users, 2)
		assert.Equal(t, entity.AdminUser{
			User:           entity.User{ID: 5, Email: "a@x", Credits: 700, CreditsLimit: 2000},
			Usage:          entity.Usage{Folders: 1, StorageBytes: 10, Domains: 1},
			StorageBlocks:  1,
			MonthlyCredits: 1500,
			Used:           1500,
		}, rs.Users[0])
		assert.Equal(t, -100, rs.Users[1].User.Credits)
		assert.Zero(t, rs.Users[1].Used)
	})

	t.Run("the last page, default and capped limits", func(t *testing.T) {
		e := newEnv(t)
		e.users.EXPECT().SearchUsers(gomock.Any(), "", int64(0), DefaultLimit+1).Return(nil, nil)
		rs, err := e.c.ListUsers(context.Background(), &entity.ListAdminUsersRequest{})
		require.NoError(t, err)
		assert.Zero(t, rs.Next)
		assert.NotNil(t, rs.Users)
		assert.Empty(t, rs.Users)

		e.users.EXPECT().SearchUsers(gomock.Any(), "", int64(0), MaxLimit+1).Return(nil, nil)
		_, err = e.c.ListUsers(context.Background(), &entity.ListAdminUsersRequest{Limit: 5000})
		require.NoError(t, err)
	})

	t.Run("search fails", func(t *testing.T) {
		e := newEnv(t)
		e.users.EXPECT().SearchUsers(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errBoom)
		_, err := e.c.ListUsers(context.Background(), &entity.ListAdminUsersRequest{})
		assert.ErrorIs(t, err, errBoom)
	})

	t.Run("credit usage fails", func(t *testing.T) {
		e := newEnv(t)
		e.users.EXPECT().SearchUsers(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]model.User{{ID: 5}}, nil)
		e.stats.EXPECT().CreditUsageByUser(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errBoom)
		_, err := e.c.ListUsers(context.Background(), &entity.ListAdminUsersRequest{})
		assert.ErrorIs(t, err, errBoom)
	})

	t.Run("usage fails", func(t *testing.T) {
		e := newEnv(t)
		e.users.EXPECT().SearchUsers(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]model.User{{ID: 5}}, nil)
		e.stats.EXPECT().CreditUsageByUser(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
		e.credits.EXPECT().GetUsage(gomock.Any(), gomock.Any()).Return(nil, errBoom)
		_, err := e.c.ListUsers(context.Background(), &entity.ListAdminUsersRequest{})
		assert.ErrorIs(t, err, errBoom)
	})
}

// expectUser sets up GetUser for user 5.
func (e env) expectUser(credits int) {
	e.users.EXPECT().FindUserByID(gomock.Any(), int64(5)).Return(model.User{ID: 5, Email: "a@x", Credits: credits}, nil)
	e.stats.EXPECT().CreditUsageByUser(gomock.Any(), []int64{5}, _since).Return(map[int64]int{5: 300}, nil)
	e.credits.EXPECT().GetUsage(gomock.Any(), &entity.GetUsageRequest{UserID: 5}).Return(usage(1), nil)
}

func TestGetUser(t *testing.T) {
	e := newEnv(t)
	e.expectUser(100)
	rs, err := e.c.GetUser(context.Background(), &entity.GetAdminUserRequest{UserID: 5})
	require.NoError(t, err)
	assert.Equal(t, 100, rs.User.User.Credits)
	assert.Equal(t, 300, rs.User.Used)
	assert.Equal(t, 2000, rs.User.User.CreditsLimit)

	e.users.EXPECT().FindUserByID(gomock.Any(), int64(6)).Return(model.User{}, repository.ErrNotFound)
	_, err = e.c.GetUser(context.Background(), &entity.GetAdminUserRequest{UserID: 6})
	assert.ErrorIs(t, err, ErrUserNotFound)

	e.users.EXPECT().FindUserByID(gomock.Any(), int64(6)).Return(model.User{}, errBoom)
	_, err = e.c.GetUser(context.Background(), &entity.GetAdminUserRequest{UserID: 6})
	assert.ErrorIs(t, err, errBoom)

	e.users.EXPECT().FindUserByID(gomock.Any(), int64(6)).Return(model.User{ID: 6}, nil)
	e.stats.EXPECT().CreditUsageByUser(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errBoom)
	_, err = e.c.GetUser(context.Background(), &entity.GetAdminUserRequest{UserID: 6})
	assert.ErrorIs(t, err, errBoom)
}

func TestAdjustCredits(t *testing.T) {
	t.Run("a grant is recorded as root with a trimmed note", func(t *testing.T) {
		e := newEnv(t)
		e.users.EXPECT().AdjustCredits(gomock.Any(), int64(5), 600, ActorRoot, "support refund").
			Return(model.CreditActivity{ID: 77, CreatedAt: _now, UserID: 5, Ops: model.CreditOpAdminGrant, Change: 600, Current: 100, Actor: ActorRoot, Note: "support refund"}, nil)
		e.expectUser(100)

		rs, err := e.c.AdjustCredits(context.Background(), &entity.AdjustCreditsRequest{UserID: 5, Amount: 600, Note: "  support refund\n"})
		require.NoError(t, err)
		assert.Equal(t, entity.CreditActivity{ID: 77, UserID: 5, Ops: "admin_grant", Change: 600, Current: 100, Actor: ActorRoot, Note: "support refund", CreatedAt: _now}, rs.Activity)
		assert.Equal(t, 100, rs.User.User.Credits)
	})

	t.Run("invalid", func(t *testing.T) {
		e := newEnv(t)
		for name, rq := range map[string]entity.AdjustCreditsRequest{
			"zero":      {UserID: 5, Amount: 0, Note: "x"},
			"too big":   {UserID: 5, Amount: MaxAdjustment + 1, Note: "x"},
			"too small": {UserID: 5, Amount: -MaxAdjustment - 1, Note: "x"},
			"no note":   {UserID: 5, Amount: 1, Note: "   "},
			"long note": {UserID: 5, Amount: 1, Note: strings.Repeat("é", MaxNoteLength+1)},
		} {
			_, err := e.c.AdjustCredits(context.Background(), &rq)
			assert.ErrorIs(t, err, ErrInvalidAdjustment, name)
		}
	})

	t.Run("a note of the longest length is fine", func(t *testing.T) {
		e := newEnv(t)
		e.users.EXPECT().AdjustCredits(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(model.CreditActivity{}, errBoom)
		_, err := e.c.AdjustCredits(context.Background(), &entity.AdjustCreditsRequest{UserID: 5, Amount: -MaxAdjustment, Note: strings.Repeat("é", MaxNoteLength)})
		assert.ErrorIs(t, err, errBoom)
		assert.NotErrorIs(t, err, ErrInvalidAdjustment)
	})

	for name, tc := range map[string]struct{ repo, want error }{
		"unknown user":       {repository.ErrNotFound, ErrUserNotFound},
		"below zero refused": {repository.ErrInsufficientCredits, ErrInsufficientCredits},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.users.EXPECT().AdjustCredits(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(model.CreditActivity{}, tc.repo)
			_, err := e.c.AdjustCredits(context.Background(), &entity.AdjustCreditsRequest{UserID: 5, Amount: -1, Note: "x"})
			assert.ErrorIs(t, err, tc.want)
		})
	}

	t.Run("reading the user back fails", func(t *testing.T) {
		e := newEnv(t)
		e.users.EXPECT().AdjustCredits(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(model.CreditActivity{}, nil)
		e.users.EXPECT().FindUserByID(gomock.Any(), int64(5)).Return(model.User{}, errBoom)
		_, err := e.c.AdjustCredits(context.Background(), &entity.AdjustCreditsRequest{UserID: 5, Amount: 1, Note: "x"})
		assert.ErrorIs(t, err, errBoom)
	})
}

func TestListCreditActivities(t *testing.T) {
	t.Run("a page with a next cursor", func(t *testing.T) {
		e := newEnv(t)
		e.users.EXPECT().FindUserByID(gomock.Any(), int64(5)).Return(model.User{ID: 5}, nil)
		e.users.EXPECT().ListCreditActivities(gomock.Any(), int64(5), int64(30), 3).Return([]model.CreditActivity{
			{ID: 20, UserID: 5, Ops: model.CreditOpSpend, Change: -500, Current: 5, CreatedAt: _now}, {ID: 19}, {ID: 18},
		}, nil)
		rs, err := e.c.ListCreditActivities(context.Background(), &entity.ListCreditActivitiesRequest{UserID: 5, Before: 30, Limit: 2})
		require.NoError(t, err)
		assert.Equal(t, int64(19), rs.Next)
		require.Len(t, rs.Activities, 2)
		assert.Equal(t, entity.CreditActivity{ID: 20, UserID: 5, Ops: "spend", Change: -500, Current: 5, CreatedAt: _now}, rs.Activities[0])
	})

	t.Run("the last page", func(t *testing.T) {
		e := newEnv(t)
		e.users.EXPECT().FindUserByID(gomock.Any(), int64(5)).Return(model.User{ID: 5}, nil)
		e.users.EXPECT().ListCreditActivities(gomock.Any(), int64(5), int64(0), DefaultLimit+1).Return(nil, nil)
		rs, err := e.c.ListCreditActivities(context.Background(), &entity.ListCreditActivitiesRequest{UserID: 5})
		require.NoError(t, err)
		assert.Zero(t, rs.Next)
		assert.NotNil(t, rs.Activities)
	})

	t.Run("unknown user", func(t *testing.T) {
		e := newEnv(t)
		e.users.EXPECT().FindUserByID(gomock.Any(), int64(5)).Return(model.User{}, repository.ErrNotFound)
		_, err := e.c.ListCreditActivities(context.Background(), &entity.ListCreditActivitiesRequest{UserID: 5})
		assert.ErrorIs(t, err, ErrUserNotFound)
	})

	t.Run("list fails", func(t *testing.T) {
		e := newEnv(t)
		e.users.EXPECT().FindUserByID(gomock.Any(), int64(5)).Return(model.User{ID: 5}, nil)
		e.users.EXPECT().ListCreditActivities(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errBoom)
		_, err := e.c.ListCreditActivities(context.Background(), &entity.ListCreditActivitiesRequest{UserID: 5})
		assert.ErrorIs(t, err, errBoom)
	})
}
