// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package repository is the data access layer.
package repository

//go:generate mockgen -source=user.go -destination=mocks/user.go -package=mocks

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/tmphost/tmphost/backend/internal/data/model"
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
		// UpdateUserCredits sets a user's balance and next renewal time.
		UpdateUserCredits(ctx context.Context, id int64, credits int, renewAt time.Time) error
		// SpendCredits takes n credits from a user in one atomic update, or
		// returns ErrInsufficientCredits and takes none.
		SpendCredits(ctx context.Context, id int64, n int) error
		// RefundCredits gives n spent credits back.
		RefundCredits(ctx context.Context, id int64, n int) error
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

func (r *userRepository) UpdateUserCredits(ctx context.Context, id int64, credits int, renewAt time.Time) error {
	return r.db.WithContext(ctx).Model(&model.User{}).Where("id = ?", id).
		Updates(map[string]any{"credits": credits, "credits_renew_at": renewAt}).Error
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
