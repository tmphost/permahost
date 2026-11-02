// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/url"
	"strings"

	"github.com/tmphost/tmphost/backend/internal/controller/credit"
	"github.com/tmphost/tmphost/backend/internal/controller/folder"
	setupctrl "github.com/tmphost/tmphost/backend/internal/controller/setup"
	"github.com/tmphost/tmphost/backend/internal/data/entity"
)

const (
	_defaultSettingsFile = "./_storage/settings.json"
	_defaultFilesDir     = "./_storage/files"
)

func settingsFile(cfg Config) string {
	if cfg.App.SettingsFile != "" {
		return cfg.App.SettingsFile
	}
	return _defaultSettingsFile
}

// rootAccessToken returns the configured root token, or generates and logs
// one so the setup page can always be reached.
func rootAccessToken(cfg Config) string {
	if cfg.Auth.RootAccessToken != "" {
		return cfg.Auth.RootAccessToken
	}
	b := make([]byte, 24)
	// crypto/rand.Read never returns an error.
	_, _ = rand.Read(b)
	tok := hex.EncodeToString(b)
	log.Printf("setup: generated root access token %s (set TMPHOSTIO_AUTH_ROOT_ACCESS_TOKEN to keep one across restarts)", tok)
	return tok
}

// settingsFromConfig is what the setup page shows before anything is saved.
func settingsFromConfig(cfg Config) entity.Settings {
	driver := setupctrl.DriverSQLite
	if cfg.Postgres.Enabled {
		driver = setupctrl.DriverPostgres
	}
	grant := cfg.Credits.MonthlyGrant
	if grant == 0 {
		grant = credit.DefaultMonthlyGrant
	}
	s3 := cfg.S3
	tlsPort := cfg.TLS.Port
	if tlsPort == 0 {
		tlsPort = setupctrl.DefaultTLSPort
	}
	storageDriver := cfg.Storage.Driver
	if storageDriver == "" {
		storageDriver = setupctrl.StorageLocal
	}
	localPath := cfg.Storage.LocalPath
	if localPath == "" {
		localPath = _defaultFilesDir
	}
	files := entity.FileSettings(cfg.Files)
	if files.MaxTTLMinutes == 0 {
		files.MaxTTLMinutes = folder.DefaultMaxTTLMinutes
	}
	if files.MaxUploadMB == 0 {
		files.MaxUploadMB = folder.DefaultMaxUploadMB
	}
	if files.AnonymousMaxTTLMinutes == 0 {
		files.AnonymousMaxTTLMinutes = folder.DefaultAnonymousMaxTTLMinutes
	}
	if files.PublicDomain == "" {
		files.PublicDomain = strings.ToLower(cfg.App.SitesDomain)
	}
	if files.PublicDomain == "" {
		if u, err := url.Parse(cfg.App.BaseURL); err == nil {
			files.PublicDomain = strings.ToLower(u.Host)
		}
	}
	return entity.Settings{
		Domain: entity.DomainSettings{AppURL: cfg.App.BaseURL, SitesDomain: cfg.App.SitesDomain},
		TLS: entity.TLSSettings{
			Enabled: cfg.TLS.Enabled, Port: tlsPort, Email: cfg.TLS.Email,
			Lego: entity.LegoSettings{Enabled: cfg.TLS.Lego.Enabled, CloudflareAPIToken: cfg.TLS.Lego.CloudflareAPIToken},
		},
		Credits: entity.CreditSettings{MonthlyGrant: grant},
		Database: entity.DatabaseSettings{
			Driver:   driver,
			SQLite:   entity.SQLiteSettings{DSN: cfg.SQLite.DSN},
			Postgres: entity.PostgresSettings{DSN: cfg.Postgres.DSN},
		},
		Storage: entity.StorageSettings{
			Driver: storageDriver,
			Local:  entity.LocalStorageSettings{Path: localPath},
			S3: entity.S3Settings{
				Endpoint: s3.Endpoint, Region: s3.Region, Bucket: s3.Bucket,
				AccessKeyID: s3.AccessKeyID, SecretAccessKey: s3.SecretAccessKey, UsePathStyle: s3.UsePathStyle,
			},
		},
		Files: files,
		Auth: entity.AuthSettings{
			Google: entity.OAuthClientSettings{ClientID: cfg.Auth.Google.ClientID, ClientSecret: cfg.Auth.Google.ClientSecret},
			GitHub: entity.OAuthClientSettings{ClientID: cfg.Auth.GitHub.ClientID, ClientSecret: cfg.Auth.GitHub.ClientSecret},
		},
	}
}

// applySettings overlays the effective settings onto the config.
func applySettings(cfg Config, s entity.Settings) Config {
	cfg.Credits.MonthlyGrant = s.Credits.MonthlyGrant
	cfg.SQLite.Enabled = s.Database.Driver == setupctrl.DriverSQLite
	cfg.SQLite.DSN = s.Database.SQLite.DSN
	cfg.Postgres.Enabled = s.Database.Driver == setupctrl.DriverPostgres
	cfg.Postgres.DSN = s.Database.Postgres.DSN
	s3 := s.Storage.S3
	cfg.Storage = StorageConfig{Driver: s.Storage.Driver, LocalPath: s.Storage.Local.Path}
	cfg.Files = FilesConfig(s.Files)
	cfg.S3 = S3Config{
		Endpoint: s3.Endpoint, Region: s3.Region, Bucket: s3.Bucket,
		AccessKeyID: s3.AccessKeyID, SecretAccessKey: s3.SecretAccessKey, UsePathStyle: s3.UsePathStyle,
	}
	cfg.Auth.Google = OAuthAppConfig{ClientID: s.Auth.Google.ClientID, ClientSecret: s.Auth.Google.ClientSecret}
	cfg.Auth.GitHub = OAuthAppConfig{ClientID: s.Auth.GitHub.ClientID, ClientSecret: s.Auth.GitHub.ClientSecret}
	cfg.Auth.Providers = s.Auth.Providers
	cfg.App.BaseURL = s.Domain.AppURL
	cfg.App.SitesDomain = s.Domain.SitesDomain
	cfg.TLS = TLSConfig{
		Enabled: s.TLS.Enabled, Port: s.TLS.Port, Email: s.TLS.Email,
		Lego: LegoConfig{Enabled: s.TLS.Lego.Enabled, CloudflareAPIToken: s.TLS.Lego.CloudflareAPIToken},
	}
	return cfg
}
