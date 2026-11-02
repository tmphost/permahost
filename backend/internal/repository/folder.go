// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package repository

//go:generate mockgen -source=folder.go -destination=mocks/folder.go -package=mocks

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/tmphost/tmphost/backend/internal/data/model"
)

type (
	// FolderRepository persists folders.
	FolderRepository interface {
		CreateFolder(ctx context.Context, f model.Folder) (model.Folder, error)
		FindFolderByID(ctx context.Context, id int64) (model.Folder, error)
		// ListFoldersByUser returns a user's folders that expire after now,
		// newest first.
		ListFoldersByUser(ctx context.Context, userID int64, now time.Time) ([]model.Folder, error)
		// ListExpiredFolders returns up to limit folders that expired at or
		// before now, oldest expiry first.
		ListExpiredFolders(ctx context.Context, now time.Time, limit int) ([]model.Folder, error)
		// UpdateFolderTitle sets a folder's title.
		UpdateFolderTitle(ctx context.Context, id int64, title string) error
		DeleteFolder(ctx context.Context, id int64) error
	}

	folderRepository struct {
		db *gorm.DB
	}
)

// NewFolderRepository returns a GORM-backed folder repository.
func NewFolderRepository(db *gorm.DB) FolderRepository {
	return &folderRepository{db: db}
}

func (r *folderRepository) CreateFolder(ctx context.Context, f model.Folder) (model.Folder, error) {
	if err := r.db.WithContext(ctx).Create(&f).Error; err != nil {
		return model.Folder{}, err
	}
	return f, nil
}

func (r *folderRepository) FindFolderByID(ctx context.Context, id int64) (model.Folder, error) {
	var f model.Folder
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&f).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Folder{}, ErrNotFound
	}
	if err != nil {
		return model.Folder{}, err
	}
	return f, nil
}

func (r *folderRepository) ListFoldersByUser(ctx context.Context, userID int64, now time.Time) ([]model.Folder, error) {
	var folders []model.Folder
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND expires_at > ?", userID, now).
		Order("id DESC").
		Find(&folders).Error
	if err != nil {
		return nil, err
	}
	return folders, nil
}

func (r *folderRepository) ListExpiredFolders(ctx context.Context, now time.Time, limit int) ([]model.Folder, error) {
	var folders []model.Folder
	err := r.db.WithContext(ctx).
		Where("expires_at <= ?", now).
		Order("expires_at, id").
		Limit(limit).
		Find(&folders).Error
	if err != nil {
		return nil, err
	}
	return folders, nil
}

func (r *folderRepository) UpdateFolderTitle(ctx context.Context, id int64, title string) error {
	return r.db.WithContext(ctx).Model(&model.Folder{}).Where("id = ?", id).Update("title", title).Error
}

func (r *folderRepository) DeleteFolder(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).Delete(&model.Folder{}, "id = ?", id).Error
}
