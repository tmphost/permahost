// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package credit implements hosting billing. Every account gets a monthly
// credit grant (DefaultMonthlyGrant unless root changed it), renewed every
// Period by a daily job that runs at 00:00 UTC. Hosting is paid for by
// the month:
//
//   - FolderCredits per folder, charged when it is created,
//   - StorageCredits per started StorageBlockBytes stored, charged when an
//     upload starts a block the period has not paid for yet,
//   - DomainCredits per custom domain, charged with DomainSetupCredits
//     when it is verified.
//
// Each renewal charges the next period for everything the account hosts
// then. When the grant cannot pay for it, the balance goes below zero and
// the account's sites are paused until it is paid for again; nothing is
// deleted.
//
// Root can also grant credits to one user or to everyone. Those expire
// GrantExpiry later; renewals keep them, and use them for what the monthly
// grant can't pay for. The daily job takes what is left of an expired
// grant away before it renews accounts.
package credit

//go:generate mockgen -source=credit.go -destination=mocks/credit.go -package=mocks

import (
	"context"
	"fmt"
	"time"

	"github.com/tmphost/permahost/backend/internal/data/entity"
	"github.com/tmphost/permahost/backend/internal/repository"
)

const (
	// DefaultMonthlyGrant is the credit balance each period starts with,
	// unless configured otherwise: one folder of up to one storage block
	// with one custom domain, setup included.
	DefaultMonthlyGrant = 2000
	// StorageBlockBytes is the storage StorageCredits pays for a month.
	StorageBlockBytes = 25 << 20
	// StorageCredits is the monthly price of a started storage block.
	StorageCredits = 500
	// FolderCredits is the monthly price of a folder.
	FolderCredits = 500
	// DomainCredits is the monthly price of a custom domain.
	DomainCredits = 500
	// DomainSetupCredits is charged once, when a custom domain is verified.
	DomainSetupCredits = 500
	// Period is how often the grant renews.
	Period = 30 * 24 * time.Hour
	// RenewalSchedule is the cron spec (UTC) of the renewal job.
	RenewalSchedule = "0 0 * * *"

	_batchSize = 500
)

type (
	// Controller renews credits and reports usage.
	Controller interface {
		// RenewDueCredits expires every admin grant whose time has passed,
		// then renews every account whose renewal time has passed and
		// charges its hosting for the next period.
		RenewDueCredits(ctx context.Context) (*entity.RenewDueCreditsResponse, error)
		// GetUsage reports what an account hosts and what it costs.
		GetUsage(ctx context.Context, rq *entity.GetUsageRequest) (*entity.GetUsageResponse, error)
	}

	// Params are the controller's dependencies.
	Params struct {
		Users   repository.UserRepository
		Folders repository.FolderRepository
		Domains repository.DomainRepository
		// Grant returns the current monthly grant; it can change at runtime.
		Grant     func() int
		Now       func() time.Time
		BatchSize int
	}

	controller struct {
		users     repository.UserRepository
		folders   repository.FolderRepository
		domains   repository.DomainRepository
		grant     func() int
		now       func() time.Time
		batchSize int
	}
)

// New returns the credit controller.
func New(p Params) Controller {
	c := &controller{users: p.Users, folders: p.Folders, domains: p.Domains, grant: GrantOrDefault(p.Grant), now: p.Now, batchSize: p.BatchSize}
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

// GrantExpiry is when an admin grant made at grantedAt expires: Period
// later, at the next 00:00 UTC, so it lines up with a run of the daily job.
func GrantExpiry(grantedAt time.Time) time.Time {
	at := grantedAt.UTC().Add(Period)
	day := at.Truncate(24 * time.Hour)
	if day.Equal(at) {
		return day
	}
	return day.Add(24 * time.Hour)
}

// CurrentPricing is what hosting costs.
func CurrentPricing() entity.Pricing {
	return entity.Pricing{
		StorageBlockBytes:  StorageBlockBytes,
		StorageCredits:     StorageCredits,
		FolderCredits:      FolderCredits,
		DomainCredits:      DomainCredits,
		DomainSetupCredits: DomainSetupCredits,
	}
}

// StorageBlocks is how many storage blocks size bytes take.
func StorageBlocks(size int64) int {
	return int((size + StorageBlockBytes - 1) / StorageBlockBytes)
}

// MonthlyCost is what hosting u costs for a period.
func MonthlyCost(u entity.Usage) int {
	return u.Folders*FolderCredits + StorageBlocks(u.StorageBytes)*StorageCredits + u.Domains*DomainCredits
}

func (c *controller) RenewDueCredits(ctx context.Context) (*entity.RenewDueCreditsResponse, error) {
	now, grant := c.now(), c.grant()
	rs := &entity.RenewDueCreditsResponse{}
	if err := c.expireGrants(ctx, now, rs); err != nil {
		return rs, err
	}
	for {
		// Each renewed user's renewal time moves past now, so the next page
		// only returns users not yet handled.
		users, err := c.users.ListUsersDueForCreditRenewal(ctx, now, c.batchSize)
		if err != nil {
			return rs, fmt.Errorf("list due users: %w", err)
		}
		for _, u := range users {
			usage, err := c.usage(ctx, u.ID)
			if err != nil {
				return rs, fmt.Errorf("usage of user %d: %w", u.ID, err)
			}
			balance, err := c.users.UpdateUserCredits(ctx, u.ID, grant-MonthlyCost(usage), StorageBlocks(usage.StorageBytes), NextRenewal(u.CreditsRenewAt, now))
			if err != nil {
				return rs, fmt.Errorf("renew user %d: %w", u.ID, err)
			}
			rs.Renewed++
			if balance < 0 {
				rs.Suspended++
			}
		}
		if len(users) < c.batchSize {
			return rs, nil
		}
	}
}

// expireGrants takes what is left of every admin grant expired by now
// away, counting them in rs.Expired.
func (c *controller) expireGrants(ctx context.Context, now time.Time, rs *entity.RenewDueCreditsResponse) error {
	for {
		// Each expired grant has nothing left, so the next page only
		// returns grants not yet handled.
		grants, err := c.users.ListExpiredCreditGrants(ctx, now, c.batchSize)
		if err != nil {
			return fmt.Errorf("list expired grants: %w", err)
		}
		for _, g := range grants {
			if err := c.users.ExpireCreditGrant(ctx, g); err != nil {
				return fmt.Errorf("expire grant %d: %w", g.ID, err)
			}
			rs.Expired++
		}
		if len(grants) < c.batchSize {
			return nil
		}
	}
}

func (c *controller) GetUsage(ctx context.Context, rq *entity.GetUsageRequest) (*entity.GetUsageResponse, error) {
	usage, err := c.usage(ctx, rq.UserID)
	if err != nil {
		return nil, err
	}
	return &entity.GetUsageResponse{
		Usage:          usage,
		StorageBlocks:  StorageBlocks(usage.StorageBytes),
		MonthlyCredits: MonthlyCost(usage),
		Pricing:        CurrentPricing(),
	}, nil
}

func (c *controller) usage(ctx context.Context, userID int64) (entity.Usage, error) {
	folders, size, err := c.folders.GetUserFolderUsage(ctx, userID)
	if err != nil {
		return entity.Usage{}, fmt.Errorf("folder usage: %w", err)
	}
	domains, err := c.domains.CountVerifiedDomainsByUser(ctx, userID)
	if err != nil {
		return entity.Usage{}, fmt.Errorf("domain usage: %w", err)
	}
	return entity.Usage{Folders: folders, StorageBytes: size, Domains: domains}, nil
}
