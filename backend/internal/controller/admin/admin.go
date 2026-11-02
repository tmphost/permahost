// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package admin implements the Users section of the root-only setup page:
// searching users with their balance and usage, and granting or deducting
// one user's credits with a note kept in a ledger.
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
		// ListUsers returns a page of users, newest first, with their usage.
		ListUsers(ctx context.Context, rq *entity.ListAdminUsersRequest) (*entity.ListAdminUsersResponse, error)
		// AdjustCredits grants or deducts a user's credits and records it.
		AdjustCredits(ctx context.Context, rq *entity.AdjustCreditsRequest) (*entity.AdjustCreditsResponse, error)
		// ListCreditAdjustments returns a page of a user's adjustments,
		// newest first.
		ListCreditAdjustments(ctx context.Context, rq *entity.ListCreditAdjustmentsRequest) (*entity.ListCreditAdjustmentsResponse, error)
	}

	// Params are the controller's dependencies.
	Params struct {
		Users   repository.UserRepository
		Credits creditctrl.Controller
		// Grant returns the current monthly grant; it can change at runtime.
		Grant func() int
		NewID func() int64
		Now   func() time.Time
	}

	controller struct {
		users   repository.UserRepository
		credits creditctrl.Controller
		grant   func() int
		newID   func() int64
		now     func() time.Time
	}
)

// New returns the admin controller.
func New(p Params) Controller {
	c := &controller{users: p.Users, credits: p.Credits, grant: creditctrl.GrantOrDefault(p.Grant), newID: p.NewID, now: p.Now}
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
	rs := &entity.ListAdminUsersResponse{Users: make([]entity.AdminUser, 0, len(ms))}
	if len(ms) > limit {
		ms = ms[:limit]
		rs.Next = ms[limit-1].ID
	}
	for _, m := range ms {
		u, err := c.adminUser(ctx, m)
		if err != nil {
			return nil, err
		}
		rs.Users = append(rs.Users, u)
	}
	return rs, nil
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
	a, err := c.users.AdjustCredits(ctx, model.CreditAdjustment{
		ID:        c.newID(),
		CreatedAt: c.now(),
		UserID:    rq.UserID,
		Actor:     ActorRoot,
		Amount:    rq.Amount,
		Note:      note,
	})
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return nil, ErrUserNotFound
	case errors.Is(err, repository.ErrInsufficientCredits):
		return nil, ErrInsufficientCredits
	case err != nil:
		return nil, fmt.Errorf("adjust credits: %w", err)
	}
	m, err := c.findUser(ctx, rq.UserID)
	if err != nil {
		return nil, err
	}
	u, err := c.adminUser(ctx, m)
	if err != nil {
		return nil, err
	}
	return &entity.AdjustCreditsResponse{User: u, Adjustment: mappermodel.FromCreditAdjustmentModelToCreditAdjustmentEntity(a)}, nil
}

func (c *controller) ListCreditAdjustments(ctx context.Context, rq *entity.ListCreditAdjustmentsRequest) (*entity.ListCreditAdjustmentsResponse, error) {
	if _, err := c.findUser(ctx, rq.UserID); err != nil {
		return nil, err
	}
	limit := pageSize(rq.Limit)
	ms, err := c.users.ListCreditAdjustments(ctx, rq.UserID, rq.Before, limit+1)
	if err != nil {
		return nil, fmt.Errorf("list credit adjustments: %w", err)
	}
	rs := &entity.ListCreditAdjustmentsResponse{}
	if len(ms) > limit {
		ms = ms[:limit]
		rs.Next = ms[limit-1].ID
	}
	rs.Adjustments = mappermodel.FromCreditAdjustmentModelsToCreditAdjustmentEntities(ms)
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

func (c *controller) adminUser(ctx context.Context, m model.User) (entity.AdminUser, error) {
	usage, err := c.credits.GetUsage(ctx, &entity.GetUsageRequest{UserID: m.ID})
	if err != nil {
		return entity.AdminUser{}, fmt.Errorf("usage of user %d: %w", m.ID, err)
	}
	return entity.AdminUser{
		User:           mappermodel.FromUserModelToUserEntity(m, c.grant()),
		Usage:          usage.Usage,
		StorageBlocks:  usage.StorageBlocks,
		MonthlyCredits: usage.MonthlyCredits,
	}, nil
}

// pageSize is limit within [1, MaxLimit], DefaultLimit when unset.
func pageSize(limit int) int {
	if limit <= 0 {
		return DefaultLimit
	}
	return min(limit, MaxLimit)
}
