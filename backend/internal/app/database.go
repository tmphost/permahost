// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/tmphost/tmphost/backend/internal/data/model"
)

// openDatabase opens PostgreSQL when it is enabled, otherwise SQLite.
// PostgreSQL enabled but unreachable is an error, never a quiet switch to a
// local SQLite file other instances cannot see.
func openDatabase(cfg Config) (*gorm.DB, error) {
	dialector, pool, err := selectDialector(cfg)
	if err != nil {
		return nil, err
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Discard})
	if err != nil {
		return nil, fmt.Errorf("%s: open: %w", dialector.Name(), err)
	}
	// Both dialectors are backed by *sql.DB, so this cannot fail.
	sqlDB, _ := db.DB()
	if pool.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(pool.MaxIdleConns)
	}
	if pool.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(pool.MaxOpenConns)
	}
	if pool.MaxConnLifetime > 0 {
		sqlDB.SetConnMaxLifetime(pool.MaxConnLifetime)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Folder{}); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("%s: migrate: %w", dialector.Name(), err)
	}
	return db, nil
}

func selectDialector(cfg Config) (gorm.Dialector, PoolConfig, error) {
	switch {
	case cfg.Postgres.Enabled:
		if cfg.Postgres.DSN == "" {
			return nil, PoolConfig{}, errors.New("postgres: dsn is required")
		}
		return postgres.Open(cfg.Postgres.DSN), cfg.Postgres.PoolConfig, nil
	case cfg.SQLite.Enabled:
		dsn := cfg.SQLite.DSN
		if dsn == "" {
			return nil, PoolConfig{}, errors.New("sqlite: dsn is required")
		}
		if !strings.HasPrefix(dsn, ":memory:") && !strings.HasPrefix(dsn, "file:") {
			if err := os.MkdirAll(filepath.Dir(dsn), 0o755); err != nil {
				return nil, PoolConfig{}, fmt.Errorf("sqlite: create dir: %w", err)
			}
		}
		return sqlite.Open(dsn), cfg.SQLite.PoolConfig, nil
	default:
		return nil, PoolConfig{}, errors.New("no database enabled: set sqlite.enabled or postgres.enabled")
	}
}
