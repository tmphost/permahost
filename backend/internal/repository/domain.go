// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package repository

//go:generate mockgen -source=domain.go -destination=mocks/domain.go -package=mocks

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/tmphost/permahost/backend/internal/data/model"
)

type (
	// DomainRepository persists custom domains.
	DomainRepository interface {
		CreateDomain(ctx context.Context, d model.Domain) (model.Domain, error)
		FindDomainByID(ctx context.Context, id int64) (model.Domain, error)
		// FindVerifiedDomainByHostname returns the verified domain for a
		// hostname, or ErrNotFound.
		FindVerifiedDomainByHostname(ctx context.Context, hostname string) (model.Domain, error)
		// ListDomainsByFolder returns a folder's domains, oldest first.
		ListDomainsByFolder(ctx context.Context, folderID int64) ([]model.Domain, error)
		// ListPendingDomains returns up to limit pending domains created
		// after since, least recently checked first.
		ListPendingDomains(ctx context.Context, since time.Time, limit int) ([]model.Domain, error)
		// CountVerifiedDomainsByUser counts a user's verified domains.
		CountVerifiedDomainsByUser(ctx context.Context, userID int64) (int, error)
		// UpdateDomainCheck records a check: when it ran, when the domain
		// was verified (nil while pending) and why it failed.
		UpdateDomainCheck(ctx context.Context, id int64, checkedAt time.Time, verifiedAt *time.Time, lastError string) error
		DeleteDomain(ctx context.Context, id int64) error
		// DeleteDomainsByFolder removes every domain of a folder.
		DeleteDomainsByFolder(ctx context.Context, folderID int64) error
	}

	domainRepository struct {
		db *gorm.DB
	}
)

// NewDomainRepository returns a GORM-backed domain repository.
func NewDomainRepository(db *gorm.DB) DomainRepository {
	return &domainRepository{db: db}
}

func (r *domainRepository) CreateDomain(ctx context.Context, d model.Domain) (model.Domain, error) {
	if err := r.db.WithContext(ctx).Create(&d).Error; err != nil {
		return model.Domain{}, err
	}
	return d, nil
}

func (r *domainRepository) FindDomainByID(ctx context.Context, id int64) (model.Domain, error) {
	return r.first(ctx, "id = ?", id)
}

func (r *domainRepository) FindVerifiedDomainByHostname(ctx context.Context, hostname string) (model.Domain, error) {
	return r.first(ctx, "hostname = ? AND verified_at IS NOT NULL", hostname)
}

func (r *domainRepository) ListDomainsByFolder(ctx context.Context, folderID int64) ([]model.Domain, error) {
	var domains []model.Domain
	if err := r.db.WithContext(ctx).Where("folder_id = ?", folderID).Order("id").Find(&domains).Error; err != nil {
		return nil, err
	}
	return domains, nil
}

func (r *domainRepository) ListPendingDomains(ctx context.Context, since time.Time, limit int) ([]model.Domain, error) {
	var domains []model.Domain
	err := r.db.WithContext(ctx).
		Where("verified_at IS NULL AND created_at > ?", since).
		// Never checked sorts first on both SQLite and PostgreSQL.
		Order("checked_at IS NOT NULL, checked_at, id").
		Limit(limit).
		Find(&domains).Error
	if err != nil {
		return nil, err
	}
	return domains, nil
}

func (r *domainRepository) CountVerifiedDomainsByUser(ctx context.Context, userID int64) (int, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&model.Domain{}).
		Where("user_id = ? AND verified_at IS NOT NULL", userID).
		Count(&n).Error
	return int(n), err
}

func (r *domainRepository) UpdateDomainCheck(ctx context.Context, id int64, checkedAt time.Time, verifiedAt *time.Time, lastError string) error {
	return r.db.WithContext(ctx).Model(&model.Domain{}).Where("id = ?", id).
		Updates(map[string]any{"checked_at": checkedAt, "verified_at": verifiedAt, "last_error": lastError}).Error
}

func (r *domainRepository) DeleteDomain(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Delete(&model.Domain{}, "id = ?", id).Error
}

func (r *domainRepository) DeleteDomainsByFolder(ctx context.Context, folderID int64) error {
	return r.db.WithContext(ctx).Delete(&model.Domain{}, "folder_id = ?", folderID).Error
}

func (r *domainRepository) first(ctx context.Context, query string, args ...any) (model.Domain, error) {
	var d model.Domain
	err := r.db.WithContext(ctx).Where(query, args...).First(&d).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Domain{}, ErrNotFound
	}
	if err != nil {
		return model.Domain{}, err
	}
	return d, nil
}
