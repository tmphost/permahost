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
	_now    = time.Date(2026, 11, 2, 7, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
)

type env struct {
	c       Controller
	users   *mockrepo.MockUserRepository
	credits *mockcredit.MockController
}

func newEnv(t *testing.T) env {
	t.Helper()
	ctrl := gomock.NewController(t)
	e := env{users: mockrepo.NewMockUserRepository(ctrl), credits: mockcredit.NewMockController(ctrl)}
	e.c = New(Params{
		Users:   e.users,
		Credits: e.credits,
		Grant:   func() int { return 2000 },
		NewID:   func() int64 { return 77 },
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
}

func TestListUsers(t *testing.T) {
	t.Run("a page with a next cursor", func(t *testing.T) {
		e := newEnv(t)
		e.users.EXPECT().SearchUsers(gomock.Any(), "ada", int64(9), 3).Return([]model.User{
			{ID: 5, Email: "a@x", Credits: 700}, {ID: 4, Email: "b@x", Credits: -100}, {ID: 3, Email: "c@x"},
		}, nil)
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
		}, rs.Users[0])
		assert.Equal(t, -100, rs.Users[1].User.Credits)
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

	t.Run("usage fails", func(t *testing.T) {
		e := newEnv(t)
		e.users.EXPECT().SearchUsers(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return([]model.User{{ID: 5}}, nil)
		e.credits.EXPECT().GetUsage(gomock.Any(), gomock.Any()).Return(nil, errBoom)
		_, err := e.c.ListUsers(context.Background(), &entity.ListAdminUsersRequest{})
		assert.ErrorIs(t, err, errBoom)
	})
}

func TestAdjustCredits(t *testing.T) {
	t.Run("a grant is recorded with root and a trimmed note", func(t *testing.T) {
		e := newEnv(t)
		e.users.EXPECT().AdjustCredits(gomock.Any(), model.CreditAdjustment{ID: 77, CreatedAt: _now, UserID: 5, Actor: ActorRoot, Amount: 600, Note: "support refund"}).
			DoAndReturn(func(_ context.Context, a model.CreditAdjustment) (model.CreditAdjustment, error) {
				a.BalanceAfter = 100
				return a, nil
			})
		e.users.EXPECT().FindUserByID(gomock.Any(), int64(5)).Return(model.User{ID: 5, Email: "a@x", Credits: 100}, nil)
		e.credits.EXPECT().GetUsage(gomock.Any(), &entity.GetUsageRequest{UserID: 5}).Return(usage(1), nil)

		rs, err := e.c.AdjustCredits(context.Background(), &entity.AdjustCreditsRequest{UserID: 5, Amount: 600, Note: "  support refund\n"})
		require.NoError(t, err)
		assert.Equal(t, entity.CreditAdjustment{ID: 77, UserID: 5, Actor: ActorRoot, Amount: 600, Note: "support refund", BalanceAfter: 100, CreatedAt: _now}, rs.Adjustment)
		assert.Equal(t, 100, rs.User.User.Credits)
		assert.Equal(t, 2000, rs.User.User.CreditsLimit)
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
		e.users.EXPECT().AdjustCredits(gomock.Any(), gomock.Any()).Return(model.CreditAdjustment{}, errBoom)
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
			e.users.EXPECT().AdjustCredits(gomock.Any(), gomock.Any()).Return(model.CreditAdjustment{}, tc.repo)
			_, err := e.c.AdjustCredits(context.Background(), &entity.AdjustCreditsRequest{UserID: 5, Amount: -1, Note: "x"})
			assert.ErrorIs(t, err, tc.want)
		})
	}

	t.Run("reading the user back fails", func(t *testing.T) {
		e := newEnv(t)
		e.users.EXPECT().AdjustCredits(gomock.Any(), gomock.Any()).Return(model.CreditAdjustment{}, nil)
		e.users.EXPECT().FindUserByID(gomock.Any(), int64(5)).Return(model.User{}, errBoom)
		_, err := e.c.AdjustCredits(context.Background(), &entity.AdjustCreditsRequest{UserID: 5, Amount: 1, Note: "x"})
		assert.ErrorIs(t, err, errBoom)
	})

	t.Run("usage fails", func(t *testing.T) {
		e := newEnv(t)
		e.users.EXPECT().AdjustCredits(gomock.Any(), gomock.Any()).Return(model.CreditAdjustment{}, nil)
		e.users.EXPECT().FindUserByID(gomock.Any(), int64(5)).Return(model.User{ID: 5}, nil)
		e.credits.EXPECT().GetUsage(gomock.Any(), gomock.Any()).Return(nil, errBoom)
		_, err := e.c.AdjustCredits(context.Background(), &entity.AdjustCreditsRequest{UserID: 5, Amount: 1, Note: "x"})
		assert.ErrorIs(t, err, errBoom)
	})
}

func TestListCreditAdjustments(t *testing.T) {
	t.Run("a page with a next cursor", func(t *testing.T) {
		e := newEnv(t)
		e.users.EXPECT().FindUserByID(gomock.Any(), int64(5)).Return(model.User{ID: 5}, nil)
		e.users.EXPECT().ListCreditAdjustments(gomock.Any(), int64(5), int64(30), 3).Return([]model.CreditAdjustment{
			{ID: 20, UserID: 5, Actor: "root", Amount: 5, Note: "a", BalanceAfter: 5, CreatedAt: _now}, {ID: 19}, {ID: 18},
		}, nil)
		rs, err := e.c.ListCreditAdjustments(context.Background(), &entity.ListCreditAdjustmentsRequest{UserID: 5, Before: 30, Limit: 2})
		require.NoError(t, err)
		assert.Equal(t, int64(19), rs.Next)
		require.Len(t, rs.Adjustments, 2)
		assert.Equal(t, entity.CreditAdjustment{ID: 20, UserID: 5, Actor: "root", Amount: 5, Note: "a", BalanceAfter: 5, CreatedAt: _now}, rs.Adjustments[0])
	})

	t.Run("the last page", func(t *testing.T) {
		e := newEnv(t)
		e.users.EXPECT().FindUserByID(gomock.Any(), int64(5)).Return(model.User{ID: 5}, nil)
		e.users.EXPECT().ListCreditAdjustments(gomock.Any(), int64(5), int64(0), DefaultLimit+1).Return(nil, nil)
		rs, err := e.c.ListCreditAdjustments(context.Background(), &entity.ListCreditAdjustmentsRequest{UserID: 5})
		require.NoError(t, err)
		assert.Zero(t, rs.Next)
		assert.NotNil(t, rs.Adjustments)
	})

	t.Run("unknown user", func(t *testing.T) {
		e := newEnv(t)
		e.users.EXPECT().FindUserByID(gomock.Any(), int64(5)).Return(model.User{}, repository.ErrNotFound)
		_, err := e.c.ListCreditAdjustments(context.Background(), &entity.ListCreditAdjustmentsRequest{UserID: 5})
		assert.ErrorIs(t, err, ErrUserNotFound)
	})

	t.Run("list fails", func(t *testing.T) {
		e := newEnv(t)
		e.users.EXPECT().FindUserByID(gomock.Any(), int64(5)).Return(model.User{ID: 5}, nil)
		e.users.EXPECT().ListCreditAdjustments(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errBoom)
		_, err := e.c.ListCreditAdjustments(context.Background(), &entity.ListCreditAdjustmentsRequest{UserID: 5})
		assert.ErrorIs(t, err, errBoom)
	})
}
