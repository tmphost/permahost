// Copyright 2026 Contextual, Inc. https://tmphost.io
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

	"github.com/tmphost/tmphost/backend/internal/data/model"
	mockrepo "github.com/tmphost/tmphost/backend/internal/repository/mocks"
)

var (
	_now    = time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
)

func newController(t *testing.T, batch int) (Controller, *mockrepo.MockUserRepository) {
	t.Helper()
	users := mockrepo.NewMockUserRepository(gomock.NewController(t))
	return New(Params{Users: users, Now: func() time.Time { return _now }, BatchSize: batch}), users
}

func TestNew_Defaults(t *testing.T) {
	c := New(Params{}).(*controller)
	assert.Equal(t, _batchSize, c.batchSize)
	assert.Equal(t, DefaultMonthlyGrant, c.grant())
	assert.WithinDuration(t, time.Now(), c.now(), time.Second)
}

func TestRenewDueCredits_UsesCurrentGrant(t *testing.T) {
	ctx := context.Background()
	users := mockrepo.NewMockUserRepository(gomock.NewController(t))
	c := New(Params{Users: users, Grant: func() int { return 400 }, Now: func() time.Time { return _now }})
	users.EXPECT().ListUsersDueForCreditRenewal(ctx, _now, _batchSize).Return([]model.User{{ID: 1, CreditsRenewAt: _now}}, nil)
	users.EXPECT().UpdateUserCredits(ctx, int64(1), 400, _now.Add(Period)).Return(nil)
	_, err := c.RenewDueCredits(ctx)
	require.NoError(t, err)
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
	c, users := newController(t, 2)
	gomock.InOrder(
		users.EXPECT().ListUsersDueForCreditRenewal(ctx, _now, 2).Return([]model.User{
			{ID: 1, CreditsRenewAt: _now}, {ID: 2, CreditsRenewAt: _now.Add(-24 * time.Hour)},
		}, nil),
		users.EXPECT().UpdateUserCredits(ctx, int64(1), DefaultMonthlyGrant, _now.Add(Period)).Return(nil),
		users.EXPECT().UpdateUserCredits(ctx, int64(2), DefaultMonthlyGrant, _now.Add(-24*time.Hour+Period)).Return(nil),
		users.EXPECT().ListUsersDueForCreditRenewal(ctx, _now, 2).Return([]model.User{{ID: 3, CreditsRenewAt: _now}}, nil),
		users.EXPECT().UpdateUserCredits(ctx, int64(3), DefaultMonthlyGrant, _now.Add(Period)).Return(nil),
	)
	rs, err := c.RenewDueCredits(ctx)
	require.NoError(t, err)
	assert.Equal(t, 3, rs.Renewed)
}

func TestRenewDueCredits_NothingDue(t *testing.T) {
	ctx := context.Background()
	c, users := newController(t, 2)
	users.EXPECT().ListUsersDueForCreditRenewal(ctx, _now, 2).Return(nil, nil)
	rs, err := c.RenewDueCredits(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, rs.Renewed)
}

func TestRenewDueCredits_Errors(t *testing.T) {
	ctx := context.Background()

	t.Run("list", func(t *testing.T) {
		c, users := newController(t, 2)
		users.EXPECT().ListUsersDueForCreditRenewal(ctx, _now, 2).Return(nil, errBoom)
		rs, err := c.RenewDueCredits(ctx)
		assert.ErrorIs(t, err, errBoom)
		assert.Equal(t, 0, rs.Renewed)
	})

	t.Run("update", func(t *testing.T) {
		c, users := newController(t, 2)
		users.EXPECT().ListUsersDueForCreditRenewal(ctx, _now, 2).Return([]model.User{{ID: 1, CreditsRenewAt: _now}, {ID: 2, CreditsRenewAt: _now}}, nil)
		users.EXPECT().UpdateUserCredits(ctx, int64(1), DefaultMonthlyGrant, gomock.Any()).Return(nil)
		users.EXPECT().UpdateUserCredits(ctx, int64(2), DefaultMonthlyGrant, gomock.Any()).Return(errBoom)
		rs, err := c.RenewDueCredits(ctx)
		assert.ErrorIs(t, err, errBoom)
		assert.Equal(t, 1, rs.Renewed)
	})
}
