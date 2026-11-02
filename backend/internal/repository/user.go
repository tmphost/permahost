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
		// AdjustCredits adds a.Amount (negative to deduct) to a user's
		// balance and records a in the ledger with the balance after it,
		// in one transaction. A deduction that would take the balance
		// below zero returns ErrInsufficientCredits and changes nothing;
		// an unknown user returns ErrNotFound.
		AdjustCredits(ctx context.Context, a model.CreditAdjustment) (model.CreditAdjustment, error)
		// ListCreditAdjustments returns up to limit of a user's ledger
		// entries, newest first, with IDs below before (0: from the newest).
		ListCreditAdjustments(ctx context.Context, userID, before int64, limit int) ([]model.CreditAdjustment, error)
	}

	userRepository struct {
		db *gorm.DB
	}
)

// NewUserRepository returns a GORM-backed user repository.
func NewUserRepository(db *gorm.DB) UserRepository {
	return &userRepository{db: db}
}

func (r *userRepository) FindUserByID(ctx context.Context, id int64) (model.User, error) {
	return r.first(ctx, "id = ?", id)
}

func (r *userRepository) FindUserByEmail(ctx context.Context, email string) (model.User, error) {
	return r.first(ctx, "email = ?", email)
}

func (r *userRepository) CreateUser(ctx context.Context, u model.User) (model.User, error) {
	if err := r.db.WithContext(ctx).Create(&u).Error; err != nil {
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
	return r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).
		Updates(map[string]any{"credits": credits, "storage_blocks_paid": storageBlocks, "credits_renew_at": renewAt}).Error
}

func (r *userRepository) SpendCredits(ctx context.Context, id int64, n int) error {
	res := r.db.WithContext(ctx).Model(&model.User{}).
		Where("id = ? AND credits >= ?", id, n).
		Update("credits", gorm.Expr("credits - ?", n))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrInsufficientCredits
	}
	return nil
}

func (r *userRepository) RefundCredits(ctx context.Context, id int64, n int) error {
	return r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).
		Update("credits", gorm.Expr("credits + ?", n)).Error
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
		// The update only applies if nobody paid for blocks since the read;
		// otherwise the cost is worked out again.
		res := r.db.WithContext(ctx).Model(&model.User{}).
			Where("id = ? AND storage_blocks_paid = ? AND credits >= ?", id, u.StorageBlocksPaid, cost).
			Updates(map[string]any{"credits": gorm.Expr("credits - ?", cost), "storage_blocks_paid": blocks})
		if res.Error != nil {
			return 0, res.Error
		}
		if res.RowsAffected > 0 {
			return cost, nil
		}
	}
}

func (r *userRepository) RefundStorageBlocks(ctx context.Context, id int64, blocks, n int) error {
	return r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).
		Updates(map[string]any{
			"credits":             gorm.Expr("credits + ?", n),
			"storage_blocks_paid": gorm.Expr("storage_blocks_paid - ?", blocks),
		}).Error
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

func (r *userRepository) AdjustCredits(ctx context.Context, a model.CreditAdjustment) (model.CreditAdjustment, error) {
	for {
		u, err := r.FindUserByID(ctx, a.UserID)
		if err != nil {
			return model.CreditAdjustment{}, err
		}
		a.BalanceAfter = u.Credits + a.Amount
		if a.Amount < 0 && a.BalanceAfter < 0 {
			return model.CreditAdjustment{}, ErrInsufficientCredits
		}
		applied := false
		err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			// The update only applies if no charge or refund changed the
			// balance since the read; otherwise it is worked out again.
			res := tx.Model(&model.User{}).Where("id = ? AND credits = ?", a.UserID, u.Credits).
				Update("credits", a.BalanceAfter)
			if res.Error != nil || res.RowsAffected == 0 {
				return res.Error
			}
			applied = true
			return tx.Create(&a).Error
		})
		if err != nil {
			return model.CreditAdjustment{}, err
		}
		if applied {
			return a, nil
		}
	}
}

func (r *userRepository) ListCreditAdjustments(ctx context.Context, userID, before int64, limit int) ([]model.CreditAdjustment, error) {
	db := r.db.WithContext(ctx).Where("user_id = ?", userID)
	if before > 0 {
		db = db.Where("id < ?", before)
	}
	var as []model.CreditAdjustment
	if err := db.Order("id DESC").Limit(limit).Find(&as).Error; err != nil {
		return nil, err
	}
	return as, nil
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
