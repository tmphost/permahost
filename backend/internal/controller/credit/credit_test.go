// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package credit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/permahost/backend/internal/data/entity"
	"github.com/tmphost/permahost/backend/internal/data/model"
	mockrepo "github.com/tmphost/permahost/backend/internal/repository/mocks"
)

var (
	_now    = time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
)

type env struct {
	c       Controller
	users   *mockrepo.MockUserRepository
	folders *mockrepo.MockFolderRepository
	domains *mockrepo.MockDomainRepository
}

func newEnv(t *testing.T, batch int, grant func() int) env {
	t.Helper()
	ctrl := gomock.NewController(t)
	e := env{
		users:   mockrepo.NewMockUserRepository(ctrl),
		folders: mockrepo.NewMockFolderRepository(ctrl),
		domains: mockrepo.NewMockDomainRepository(ctrl),
	}
	e.c = New(Params{Users: e.users, Folders: e.folders, Domains: e.domains, Grant: grant, Now: func() time.Time { return _now }, BatchSize: batch})
	return e
}

// expectUsage makes the user host folders folders of size bytes in total
// and domains verified domains.
func (e env) expectUsage(ctx context.Context, userID int64, folders int, size int64, domains int) {
	e.folders.EXPECT().GetUserFolderUsage(ctx, userID).Return(folders, size, nil)
	e.domains.EXPECT().CountVerifiedDomainsByUser(ctx, userID).Return(domains, nil)
}

func TestNew_Defaults(t *testing.T) {
	c := New(Params{}).(*controller)
	assert.Equal(t, _batchSize, c.batchSize)
	assert.Equal(t, DefaultMonthlyGrant, c.grant())
	assert.WithinDuration(t, time.Now(), c.now(), time.Second)
}

func TestPricing(t *testing.T) {
	assert.Equal(t, entity.Pricing{StorageBlockBytes: 25 << 20, StorageCredits: 500, FolderCredits: 500, DomainCredits: 500, DomainSetupCredits: 500}, CurrentPricing())
	assert.Equal(t, 0, StorageBlocks(0))
	assert.Equal(t, 1, StorageBlocks(1))
	assert.Equal(t, 1, StorageBlocks(25<<20))
	assert.Equal(t, 2, StorageBlocks(25<<20+1))
	assert.Equal(t, 0, MonthlyCost(entity.Usage{}))
	assert.Equal(t, 2*500+2*500+500, MonthlyCost(entity.Usage{Folders: 2, StorageBytes: 30 << 20, Domains: 1}))
}

func TestRenewDueCredits_ChargesHosting(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, 0, func() int { return 2000 })
	e.users.EXPECT().ListUsersDueForCreditRenewal(ctx, _now, _batchSize).Return([]model.User{{ID: 1, CreditsRenewAt: _now}, {ID: 2, CreditsRenewAt: _now}}, nil)
	// 1 folder, 30 MB (2 blocks), 1 domain: 2000 a month.
	e.expectUsage(ctx, 1, 1, 30<<20, 1)
	e.users.EXPECT().UpdateUserCredits(ctx, int64(1), 0, 2, _now.Add(Period)).Return(nil)
	// 3 folders, 1 byte, 2 domains: 3000 a month, more than the grant.
	e.expectUsage(ctx, 2, 3, 1, 2)
	e.users.EXPECT().UpdateUserCredits(ctx, int64(2), -1000, 1, _now.Add(Period)).Return(nil)
	rs, err := e.c.RenewDueCredits(ctx)
	require.NoError(t, err)
	assert.Equal(t, &entity.RenewDueCreditsResponse{Renewed: 2, Suspended: 1}, rs)
}

func TestFirstRenewal(t *testing.T) {
	registered := time.Date(2026, 10, 18, 23, 59, 0, 0, time.FixedZone("x", -3*3600)) // 2026-10-19 02:59 UTC
	assert.Equal(t, time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC), FirstRenewal(registered))
}

func TestNextRenewal(t *testing.T) {
	assert.Equal(t, _now.Add(time.Hour), NextRenewal(_now.Add(time.Hour), _now), "not due yet")
	assert.Equal(t, _now.Add(Period), NextRenewal(_now, _now), "due exactly now")
	assert.Equal(t, _now.Add(-24*time.Hour+Period), NextRenewal(_now.Add(-24*time.Hour), _now), "missed a day")
	assert.Equal(t, _now.Add(-time.Hour+Period), NextRenewal(_now.Add(-time.Hour-2*Period), _now), "several periods overdue")
}

func TestRenewDueCredits_Batches(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, 2, nil)
	gomock.InOrder(
		e.users.EXPECT().ListUsersDueForCreditRenewal(ctx, _now, 2).Return([]model.User{
			{ID: 1, CreditsRenewAt: _now}, {ID: 2, CreditsRenewAt: _now.Add(-24 * time.Hour)},
		}, nil),
		e.users.EXPECT().UpdateUserCredits(ctx, int64(1), DefaultMonthlyGrant, 0, _now.Add(Period)).Return(nil),
		e.users.EXPECT().UpdateUserCredits(ctx, int64(2), DefaultMonthlyGrant, 0, _now.Add(-24*time.Hour+Period)).Return(nil),
		e.users.EXPECT().ListUsersDueForCreditRenewal(ctx, _now, 2).Return([]model.User{{ID: 3, CreditsRenewAt: _now}}, nil),
		e.users.EXPECT().UpdateUserCredits(ctx, int64(3), DefaultMonthlyGrant, 0, _now.Add(Period)).Return(nil),
	)
	for _, id := range []int64{1, 2, 3} {
		e.expectUsage(ctx, id, 0, 0, 0)
	}
	rs, err := e.c.RenewDueCredits(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, rs.Renewed)
}

func TestRenewDueCredits_NothingDue(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, 2, nil)
	e.users.EXPECT().ListUsersDueForCreditRenewal(ctx, _now, 2).Return(nil, nil)
	rs, err := e.c.RenewDueCredits(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, rs.Renewed)
}

func TestRenewDueCredits_Errors(t *testing.T) {
	ctx := context.Background()

	t.Run("list", func(t *testing.T) {
		e := newEnv(t, 2, nil)
		e.users.EXPECT().ListUsersDueForCreditRenewal(ctx, _now, 2).Return(nil, errBoom)
		rs, err := e.c.RenewDueCredits(ctx)
		assert.ErrorIs(t, err, errBoom)
		assert.Equal(t, 0, rs.Renewed)
	})

	t.Run("usage", func(t *testing.T) {
		e := newEnv(t, 2, nil)
		e.users.EXPECT().ListUsersDueForCreditRenewal(ctx, _now, 2).Return([]model.User{{ID: 1, CreditsRenewAt: _now}}, nil)
		e.folders.EXPECT().GetUserFolderUsage(ctx, int64(1)).Return(0, int64(0), errBoom)
		rs, err := e.c.RenewDueCredits(ctx)
		assert.ErrorIs(t, err, errBoom)
		assert.Equal(t, 0, rs.Renewed)
	})

	t.Run("update", func(t *testing.T) {
		e := newEnv(t, 2, nil)
		e.users.EXPECT().ListUsersDueForCreditRenewal(ctx, _now, 2).Return([]model.User{{ID: 1, CreditsRenewAt: _now}, {ID: 2, CreditsRenewAt: _now}}, nil)
		e.expectUsage(ctx, 1, 0, 0, 0)
		e.expectUsage(ctx, 2, 0, 0, 0)
		e.users.EXPECT().UpdateUserCredits(ctx, int64(1), DefaultMonthlyGrant, 0, gomock.Any()).Return(nil)
		e.users.EXPECT().UpdateUserCredits(ctx, int64(2), DefaultMonthlyGrant, 0, gomock.Any()).Return(errBoom)
		rs, err := e.c.RenewDueCredits(ctx)
		assert.ErrorIs(t, err, errBoom)
		assert.Equal(t, 1, rs.Renewed)
	})
}

func TestGetUsage(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t, 0, nil)
	e.expectUsage(ctx, 1, 2, 26<<20, 1)
	rs, err := e.c.GetUsage(ctx, &entity.GetUsageRequest{UserID: 1})
	require.NoError(t, err)
	assert.Equal(t, &entity.GetUsageResponse{
		Usage:          entity.Usage{Folders: 2, StorageBytes: 26 << 20, Domains: 1},
		StorageBlocks:  2,
		MonthlyCredits: 2500,
		Pricing:        CurrentPricing(),
	}, rs)
}

func TestGetUsage_Errors(t *testing.T) {
	ctx := context.Background()

	e := newEnv(t, 0, nil)
	e.folders.EXPECT().GetUserFolderUsage(ctx, int64(1)).Return(0, int64(0), errBoom)
	_, err := e.c.GetUsage(ctx, &entity.GetUsageRequest{UserID: 1})
	assert.ErrorIs(t, err, errBoom)

	e = newEnv(t, 0, nil)
	e.folders.EXPECT().GetUserFolderUsage(ctx, int64(1)).Return(1, int64(1), nil)
	e.domains.EXPECT().CountVerifiedDomainsByUser(ctx, int64(1)).Return(0, errBoom)
	_, err = e.c.GetUsage(ctx, &entity.GetUsageRequest{UserID: 1})
	assert.ErrorIs(t, err, errBoom)
}
