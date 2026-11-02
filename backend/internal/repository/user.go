// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package repository is the data access layer.
package repository

//go:generate mockgen -source=user.go -destination=mocks/user.go -package=mocks

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/tmphost/permahost/backend/internal/data/model"
	"github.com/tmphost/permahost/backend/internal/service/idgen"
)

var (
	// ErrNotFound is returned when a record does not exist.
	ErrNotFound = errors.New("not found")
	// ErrInsufficientCredits is returned when a user cannot cover a spend.
	ErrInsufficientCredits = errors.New("insufficient credits")
)

type (
	// UserRepository persists users.
	UserRepository interface {
		FindUserByID(ctx context.Context, id int64) (model.User, error)
		FindUserByEmail(ctx context.Context, email string) (model.User, error)
		CreateUser(ctx context.Context, u model.User) (model.User, error)
		UpdateUser(ctx context.Context, u model.User) (model.User, error)
		// ListUsersDueForCreditRenewal returns up to limit users whose
		// credit renewal time is at or before now, oldest first.
		ListUsersDueForCreditRenewal(ctx context.Context, now time.Time, limit int) ([]model.User, error)
		// UpdateUserCredits sets a user's balance, the storage blocks paid
		// for and the next renewal time.
		UpdateUserCredits(ctx context.Context, id int64, credits, storageBlocks int, renewAt time.Time) error
		// SpendCredits takes n credits from a user in one atomic update, or
		// returns ErrInsufficientCredits and takes none.
		SpendCredits(ctx context.Context, id int64, n int) error
		// RefundCredits gives n spent credits back.
		RefundCredits(ctx context.Context, id int64, n int) error
		// PayStorageBlocks raises the storage blocks a user has paid for
		// this period to blocks, at price credits per block added, and
		// returns what it took: 0 when they are already paid for, else all
		// of it or ErrInsufficientCredits and nothing.
		PayStorageBlocks(ctx context.Context, id int64, blocks, price int) (int, error)
		// RefundStorageBlocks gives back n credits paid for blocks storage
		// blocks.
		RefundStorageBlocks(ctx context.Context, id int64, blocks, n int) error
		// SearchUsers returns up to limit users, newest first, with IDs
		// below before (0: from the newest) and, unless q is empty, an
		// email or name containing q, ignoring case.
		SearchUsers(ctx context.Context, q string, before int64, limit int) ([]model.User, error)
		// AdjustCredits adds amount (negative to deduct) to a user's
		// balance and records it as an admin grant or deduction by actor,
		// with note, in one transaction. A deduction that would take the
		// balance below zero returns ErrInsufficientCredits and changes
		// nothing; an unknown user returns ErrNotFound.
		AdjustCredits(ctx context.Context, id int64, amount int, actor, note string) (model.CreditActivity, error)
		// ListCreditActivities returns up to limit of a user's credit
		// activities, newest first, with IDs below before (0: from the
		// newest).
		ListCreditActivities(ctx context.Context, userID, before int64, limit int) ([]model.CreditActivity, error)
	}

	userRepository struct {
		db *gorm.DB
		// ids issues credit activity IDs.
		ids idgen.Service
	}
)

// NewUserRepository returns a GORM-backed user repository. Every change to
// a balance is recorded as a credit activity in the same transaction;
// activities take their IDs from ids, so they are ordered by time.
func NewUserRepository(db *gorm.DB, ids idgen.Service) UserRepository {
	return &userRepository{db: db, ids: ids}
}

func (r *userRepository) FindUserByID(ctx context.Context, id int64) (model.User, error) {
	return r.first(ctx, "id = ?", id)
}

func (r *userRepository) FindUserByEmail(ctx context.Context, email string) (model.User, error) {
	return r.first(ctx, "email = ?", email)
}

// CreateUser stores a new user and the grant it starts with.
func (r *userRepository) CreateUser(ctx context.Context, u model.User) (model.User, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&u).Error; err != nil {
			return err
		}
		return r.record(tx, u.ID, model.CreditOpGrant, u.Credits)
	})
	if err != nil {
		return model.User{}, err
	}
	return u, nil
}

func (r *userRepository) UpdateUser(ctx context.Context, u model.User) (model.User, error) {
	if err := r.db.WithContext(ctx).Save(&u).Error; err != nil {
		return model.User{}, err
	}
	return u, nil
}

func (r *userRepository) ListUsersDueForCreditRenewal(ctx context.Context, now time.Time, limit int) ([]model.User, error) {
	var users []model.User
	err := r.db.WithContext(ctx).
		Where("credits_renew_at <= ?", now).
		Order("credits_renew_at, id").
		Limit(limit).
		Find(&users).Error
	if err != nil {
		return nil, err
	}
	return users, nil
}

func (r *userRepository) UpdateUserCredits(ctx context.Context, id int64, credits, storageBlocks int, renewAt time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var old model.User
		if err := tx.Select("credits").Where("id = ?", id).Take(&old).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		err := tx.Model(&model.User{}).Where("id = ?", id).
			Updates(map[string]any{"credits": credits, "storage_blocks_paid": storageBlocks, "credits_renew_at": renewAt}).Error
		if err != nil {
			return err
		}
		return r.record(tx, id, model.CreditOpRenewal, credits-old.Credits)
	})
}

func (r *userRepository) SpendCredits(ctx context.Context, id int64, n int) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.User{}).
			Where("id = ? AND credits >= ?", id, n).
			Update("credits", gorm.Expr("credits - ?", n))
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrInsufficientCredits
		}
		return r.record(tx, id, model.CreditOpSpend, -n)
	})
}

func (r *userRepository) RefundCredits(ctx context.Context, id int64, n int) error {
	return r.refund(ctx, id, n, map[string]any{"credits": gorm.Expr("credits + ?", n)})
}

func (r *userRepository) PayStorageBlocks(ctx context.Context, id int64, blocks, price int) (int, error) {
	for {
		u, err := r.FindUserByID(ctx, id)
		if err != nil {
			return 0, err
		}
		if u.StorageBlocksPaid >= blocks {
			return 0, nil
		}
		cost := (blocks - u.StorageBlocksPaid) * price
		if u.Credits < cost {
			return 0, ErrInsufficientCredits
		}
		paid := false
		err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// The update only applies if nobody paid for blocks since the
			// read; otherwise the cost is worked out again.
			res := tx.Model(&model.User{}).
				Where("id = ? AND storage_blocks_paid = ? AND credits >= ?", id, u.StorageBlocksPaid, cost).
				Updates(map[string]any{"credits": gorm.Expr("credits - ?", cost), "storage_blocks_paid": blocks})
			if res.Error != nil || res.RowsAffected == 0 {
				return res.Error
			}
			paid = true
			return r.record(tx, id, model.CreditOpSpend, -cost)
		})
		if err != nil {
			return 0, err
		}
		if paid {
			return cost, nil
		}
	}
}

func (r *userRepository) RefundStorageBlocks(ctx context.Context, id int64, blocks, n int) error {
	return r.refund(ctx, id, n, map[string]any{
		"credits":             gorm.Expr("credits + ?", n),
		"storage_blocks_paid": gorm.Expr("storage_blocks_paid - ?", blocks),
	})
}

// refund applies updates, which give n credits back, and records it. An
// unknown user is left alone.
func (r *userRepository) refund(ctx context.Context, id int64, n int, updates map[string]any) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.User{}).Where("id = ?", id).Updates(updates)
		if res.Error != nil || res.RowsAffected == 0 {
			return res.Error
		}
		return r.record(tx, id, model.CreditOpRefund, n)
	})
}

func (r *userRepository) SearchUsers(ctx context.Context, q string, before int64, limit int) ([]model.User, error) {
	db := r.db.WithContext(ctx)
	if before > 0 {
		db = db.Where("id < ?", before)
	}
	if q != "" {
		like := "%" + _likeEscaper.Replace(strings.ToLower(q)) + "%"
		db = db.Where(`(LOWER(email) LIKE ? ESCAPE '\' OR LOWER(name) LIKE ? ESCAPE '\')`, like, like)
	}
	var users []model.User
	if err := db.Order("id DESC").Limit(limit).Find(&users).Error; err != nil {
		return nil, err
	}
	return users, nil
}

// _likeEscaper makes LIKE wildcards in a search match themselves.
var _likeEscaper = strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)

func (r *userRepository) AdjustCredits(ctx context.Context, id int64, amount int, actor, note string) (model.CreditActivity, error) {
	a := model.CreditActivity{UserID: id, Ops: model.CreditOpAdminGrant, Change: amount, Actor: actor, Note: note}
	if amount < 0 {
		a.Ops = model.CreditOpAdminDeduct
	}
	for {
		u, err := r.FindUserByID(ctx, id)
		if err != nil {
			return model.CreditActivity{}, err
		}
		a.Current = u.Credits + amount
		if amount < 0 && a.Current < 0 {
			return model.CreditActivity{}, ErrInsufficientCredits
		}
		applied := false
		err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// The update only applies if no charge or refund changed the
			// balance since the read; otherwise it is worked out again.
			res := tx.Model(&model.User{}).Where("id = ? AND credits = ?", id, u.Credits).
				Update("credits", a.Current)
			if res.Error != nil || res.RowsAffected == 0 {
				return res.Error
			}
			applied = true
			a.ID = r.ids.NextID()
			return tx.Create(&a).Error
		})
		if err != nil {
			return model.CreditActivity{}, err
		}
		if applied {
			return a, nil
		}
	}
}

func (r *userRepository) ListCreditActivities(ctx context.Context, userID, before int64, limit int) ([]model.CreditActivity, error) {
	db := r.db.WithContext(ctx).Where("user_id = ?", userID)
	if before > 0 {
		db = db.Where("id < ?", before)
	}
	var as []model.CreditActivity
	if err := db.Order("id DESC").Limit(limit).Find(&as).Error; err != nil {
		return nil, err
	}
	return as, nil
}

// record writes a credit activity for a change made in tx, with the
// balance after it.
func (r *userRepository) record(tx *gorm.DB, id int64, ops uint8, change int) error {
	var u model.User
	if err := tx.Select("credits").Where("id = ?", id).Take(&u).Error; err != nil {
		return err
	}
	return tx.Create(&model.CreditActivity{ID: r.ids.NextID(), UserID: id, Ops: ops, Change: change, Current: u.Credits}).Error
}

func (r *userRepository) first(ctx context.Context, query string, arg any) (model.User, error) {
	var u model.User
	err := r.db.WithContext(ctx).Where(query, arg).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.User{}, ErrNotFound
	}
	if err != nil {
		return model.User{}, err
	}
	return u, nil
}
