// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package repository

//go:generate mockgen -source=device_code.go -destination=mocks/device_code.go -package=mocks

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/tmphost/permahost/backend/internal/data/model"
)

type (
	// DeviceCodeRepository persists device authorizations.
	DeviceCodeRepository interface {
		CreateDeviceCode(ctx context.Context, d model.DeviceCode) error
		// FindDeviceCodeByHash returns the authorization of a device code's
		// digest, expired or not, or ErrNotFound.
		FindDeviceCodeByHash(ctx context.Context, hash string) (model.DeviceCode, error)
		// FindPendingDeviceCode returns the pending authorization of a user
		// code that is still live at now, or ErrNotFound.
		FindPendingDeviceCode(ctx context.Context, userCode string, now time.Time) (model.DeviceCode, error)
		// AnswerDeviceCode records the user's answer to a pending
		// authorization that is still live at now; else ErrNotFound.
		AnswerDeviceCode(ctx context.Context, hash string, userID int64, status string, now time.Time) error
		// PollDeviceCode records that the device polled at now.
		PollDeviceCode(ctx context.Context, hash string, now time.Time) error
		// DeleteDeviceCode deletes an authorization; ErrNotFound when it is
		// already gone, so only one caller can redeem it.
		DeleteDeviceCode(ctx context.Context, hash string) error
		// DeleteExpiredDeviceCodes deletes the authorizations that expired
		// at or before now.
		DeleteExpiredDeviceCodes(ctx context.Context, now time.Time) error
	}

	deviceCodeRepository struct {
		db *gorm.DB
	}
)

// NewDeviceCodeRepository returns a GORM-backed device code repository.
func NewDeviceCodeRepository(db *gorm.DB) DeviceCodeRepository {
	return &deviceCodeRepository{db: db}
}

func (r *deviceCodeRepository) CreateDeviceCode(ctx context.Context, d model.DeviceCode) error {
	return r.db.WithContext(ctx).Create(&d).Error
}

func (r *deviceCodeRepository) FindDeviceCodeByHash(ctx context.Context, hash string) (model.DeviceCode, error) {
	return r.first(r.db.WithContext(ctx).Where("device_code_hash = ?", hash))
}

func (r *deviceCodeRepository) FindPendingDeviceCode(ctx context.Context, userCode string, now time.Time) (model.DeviceCode, error) {
	return r.first(r.db.WithContext(ctx).Where("user_code = ? AND status = ? AND expires_at > ?", userCode, model.DeviceCodePending, now))
}

func (r *deviceCodeRepository) first(q *gorm.DB) (model.DeviceCode, error) {
	var d model.DeviceCode
	err := q.First(&d).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.DeviceCode{}, ErrNotFound
	}
	if err != nil {
		return model.DeviceCode{}, err
	}
	return d, nil
}

func (r *deviceCodeRepository) AnswerDeviceCode(ctx context.Context, hash string, userID int64, status string, now time.Time) error {
	res := r.db.WithContext(ctx).Model(&model.DeviceCode{}).
		Where("device_code_hash = ? AND status = ? AND expires_at > ?", hash, model.DeviceCodePending, now).
		Updates(map[string]any{"user_id": userID, "status": status})
	return affected(res)
}

func (r *deviceCodeRepository) PollDeviceCode(ctx context.Context, hash string, now time.Time) error {
	return r.db.WithContext(ctx).Model(&model.DeviceCode{}).Where("device_code_hash = ?", hash).Update("polled_at", now).Error
}

func (r *deviceCodeRepository) DeleteDeviceCode(ctx context.Context, hash string) error {
	return affected(r.db.WithContext(ctx).Where("device_code_hash = ?", hash).Delete(&model.DeviceCode{}))
}

func (r *deviceCodeRepository) DeleteExpiredDeviceCodes(ctx context.Context, now time.Time) error {
	return r.db.WithContext(ctx).Where("expires_at <= ?", now).Delete(&model.DeviceCode{}).Error
}

// affected turns a write that matched no row into ErrNotFound.
func affected(res *gorm.DB) error {
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}
