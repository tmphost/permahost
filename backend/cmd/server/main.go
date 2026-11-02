// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Command server runs the tmphost.io API and serves the bundled frontend.
package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tmphost/tmphost/backend/internal/app"
	"github.com/tmphost/tmphost/backend/internal/service/config"
	"github.com/tmphost/tmphost/backend/internal/service/encryption"
)

// bundled holds the built frontend (public/) and the configuration.
//
//go:embed all:public _config/base.yaml
var bundled embed.FS

const _shutdownTimeout = 10 * time.Second

// fatal is swapped out in tests.
var fatal = log.Fatal

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Getenv); err != nil {
		fatal(err)
	}
}

// run serves until ctx is cancelled or the server fails.
func run(ctx context.Context, getenv func(string) string) error {
	// The file is embedded, so reading it cannot fail.
	raw, _ := bundled.ReadFile("_config/base.yaml")
	cfg, err := loadConfig(raw, getenv)
	if err != nil {
		return err
	}
	// fs.Sub only fails for an invalid path, and "public" is valid.
	web, _ := fs.Sub(bundled, "public")
	a, err := app.New(cfg, web)
	if err != nil {
		return err
	}

	errCh := make(chan error, 1)
	go func() { errCh <- a.Listen() }()
	log.Printf("tmphost.io listening on :%d", cfg.App.Port)

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), _shutdownTimeout)
		defer cancel()
		return a.Shutdown(shutdownCtx)
	}
}

func loadConfig(raw []byte, getenv func(string) string) (app.Config, error) {
	svc, err := config.New(raw, getenv)
	if err != nil {
		return app.Config{}, err
	}
	// The master key is read from the environment only, so it can never be
	// committed in a config file.
	cfg := app.Config{MasterKey: getenv(encryption.MasterKeyEnv)}
	sections := map[string]any{
		"app":      &cfg.App,
		"tls":      &cfg.TLS,
		"auth":     &cfg.Auth,
		"credits":  &cfg.Credits,
		"sqlite":   &cfg.SQLite,
		"postgres": &cfg.Postgres,
		"storage":  &cfg.Storage,
		"s3":       &cfg.S3,
		"files":    &cfg.Files,
		"idgen":    &cfg.IDGen,
	}
	for key, target := range sections {
		if err := svc.Populate(key, target); err != nil {
			return app.Config{}, err
		}
	}
	return cfg, nil
}
