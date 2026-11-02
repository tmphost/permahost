// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package credit implements the monthly operation credit grant: every
// account gets the grant (DefaultMonthlyGrant unless root changed it),
// renewed every Period by a daily job that runs at 00:00 UTC.
package credit

//go:generate mockgen -source=credit.go -destination=mocks/credit.go -package=mocks

import (
	"context"
	"fmt"
	"time"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/repository"
)

const (
	// DefaultMonthlyGrant is the operation credit balance each period starts
	// with, unless configured otherwise.
	DefaultMonthlyGrant = 250
	// Period is how often the grant renews.
	Period = 30 * 24 * time.Hour
	// RenewalSchedule is the cron spec (UTC) of the renewal job.
	RenewalSchedule = "0 0 * * *"

	_batchSize = 500
)

type (
	// Controller renews credits.
	Controller interface {
		// RenewDueCredits resets every account whose renewal time has passed.
		RenewDueCredits(ctx context.Context) (*entity.RenewDueCreditsResponse, error)
	}

	// Params are the controller's dependencies.
	Params struct {
		Users repository.UserRepository
		// Grant returns the current monthly grant; it can change at runtime.
		Grant     func() int
		Now       func() time.Time
		BatchSize int
	}

	controller struct {
		users     repository.UserRepository
		grant     func() int
		now       func() time.Time
		batchSize int
	}
)

// New returns the credit controller.
func New(p Params) Controller {
	c := &controller{users: p.Users, grant: GrantOrDefault(p.Grant), now: p.Now, batchSize: p.BatchSize}
	if c.now == nil {
		c.now = time.Now
	}
	if c.batchSize <= 0 {
		c.batchSize = _batchSize
	}
	return c
}

// GrantOrDefault returns grant, or a source of DefaultMonthlyGrant when
// grant is nil.
func GrantOrDefault(grant func() int) func() int {
	if grant != nil {
		return grant
	}
	return func() int { return DefaultMonthlyGrant }
}

// FirstRenewal is when a newly registered account's credits first renew:
// Period after the start (00:00 UTC) of the registration day, so it lines
// up with a run of the daily job.
func FirstRenewal(registeredAt time.Time) time.Time {
	return registeredAt.UTC().Truncate(24 * time.Hour).Add(Period)
}

// NextRenewal moves a passed renewal time forward by whole periods until it
// is after now, keeping the cycle anchored to the registration day.
func NextRenewal(renewAt, now time.Time) time.Time {
	if now.Before(renewAt) {
		return renewAt
	}
	periods := now.Sub(renewAt)/Period + 1
	return renewAt.Add(periods * Period)
}

func (c *controller) RenewDueCredits(ctx context.Context) (*entity.RenewDueCreditsResponse, error) {
	now, grant := c.now(), c.grant()
	renewed := 0
	for {
		// Each renewed user's renewal time moves past now, so the next page
		// only returns users not yet handled.
		users, err := c.users.ListUsersDueForCreditRenewal(ctx, now, c.batchSize)
		if err != nil {
			return &entity.RenewDueCreditsResponse{Renewed: renewed}, fmt.Errorf("list due users: %w", err)
		}
		for _, u := range users {
			if err := c.users.UpdateUserCredits(ctx, u.ID, grant, NextRenewal(u.CreditsRenewAt, now)); err != nil {
				return &entity.RenewDueCreditsResponse{Renewed: renewed}, fmt.Errorf("renew user %d: %w", u.ID, err)
			}
			renewed++
		}
		if len(users) < c.batchSize {
			return &entity.RenewDueCreditsResponse{Renewed: renewed}, nil
		}
	}
}
