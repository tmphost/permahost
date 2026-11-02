// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package model

import (
	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/model"
)

// FromSettingsModelToSettingsEntity maps stored settings to the domain.
func FromSettingsModelToSettingsEntity(m model.Settings) entity.Settings {
	return entity.Settings{
		Domain: entity.DomainSettings{AppURL: m.Domain.AppURL, SitesDomain: m.Domain.SitesDomain},
		TLS: entity.TLSSettings{
			Enabled: m.TLS.Enabled,
			Port:    m.TLS.Port,
			Email:   m.TLS.Email,
			Lego: entity.LegoSettings{
				Enabled:            m.TLS.Lego.Enabled,
				CloudflareAPIToken: m.TLS.Lego.CloudflareAPIToken,
			},
		},
		Credits: entity.CreditSettings{MonthlyGrant: m.Credits.MonthlyGrant},
		Database: entity.DatabaseSettings{
			Driver:   m.Database.Driver,
			SQLite:   entity.SQLiteSettings{DSN: m.Database.SQLite.DSN},
			Postgres: entity.PostgresSettings{DSN: m.Database.Postgres.DSN},
		},
		Storage: entity.StorageSettings{
			Driver: storageDriver(m.Storage),
			Local:  entity.LocalStorageSettings{Path: m.Storage.Local.Path},
			S3: entity.S3Settings{
				Endpoint:        m.Storage.S3.Endpoint,
				Region:          m.Storage.S3.Region,
				Bucket:          m.Storage.S3.Bucket,
				AccessKeyID:     m.Storage.S3.AccessKeyID,
				SecretAccessKey: m.Storage.S3.SecretAccessKey,
				UsePathStyle:    m.Storage.S3.UsePathStyle,
			},
		},
		Files: entity.FileSettings{
			MaxTTLMinutes: m.Files.MaxTTLMinutes, MaxUploadMB: m.Files.MaxUploadMB,
			AnonymousMaxTTLMinutes: m.Files.AnonymousMaxTTLMinutes,
			Subdomains:             m.Files.Subdomains, PublicDomain: m.Files.PublicDomain,
		},
		Auth: entity.AuthSettings{
			Google:    entity.OAuthClientSettings{ClientID: m.Auth.Google.ClientID, ClientSecret: m.Auth.Google.ClientSecret},
			GitHub:    entity.OAuthClientSettings{ClientID: m.Auth.GitHub.ClientID, ClientSecret: m.Auth.GitHub.ClientSecret},
			Providers: FromOAuthProviderSettingsModelsToOAuthProviderSettingsEntities(m.Auth.Providers),
		},
	}
}

// FromSettingsEntityToSettingsModel maps domain settings to storage.
func FromSettingsEntityToSettingsModel(e entity.Settings) model.Settings {
	return model.Settings{
		Domain: model.DomainSettings{AppURL: e.Domain.AppURL, SitesDomain: e.Domain.SitesDomain},
		TLS: model.TLSSettings{
			Enabled: e.TLS.Enabled,
			Port:    e.TLS.Port,
			Email:   e.TLS.Email,
			Lego: model.LegoSettings{
				Enabled:            e.TLS.Lego.Enabled,
				CloudflareAPIToken: e.TLS.Lego.CloudflareAPIToken,
			},
		},
		Credits: model.CreditSettings{MonthlyGrant: e.Credits.MonthlyGrant},
		Database: model.DatabaseSettings{
			Driver:   e.Database.Driver,
			SQLite:   model.SQLiteSettings{DSN: e.Database.SQLite.DSN},
			Postgres: model.PostgresSettings{DSN: e.Database.Postgres.DSN},
		},
		Storage: model.StorageSettings{
			Driver: e.Storage.Driver,
			Local:  model.LocalStorageSettings{Path: e.Storage.Local.Path},
			S3: model.S3Settings{
				Endpoint:        e.Storage.S3.Endpoint,
				Region:          e.Storage.S3.Region,
				Bucket:          e.Storage.S3.Bucket,
				AccessKeyID:     e.Storage.S3.AccessKeyID,
				SecretAccessKey: e.Storage.S3.SecretAccessKey,
				UsePathStyle:    e.Storage.S3.UsePathStyle,
			},
		},
		Files: model.FileSettings{
			MaxTTLMinutes: e.Files.MaxTTLMinutes, MaxUploadMB: e.Files.MaxUploadMB,
			AnonymousMaxTTLMinutes: e.Files.AnonymousMaxTTLMinutes,
			Subdomains:             e.Files.Subdomains, PublicDomain: e.Files.PublicDomain,
		},
		Auth: model.AuthSettings{
			Google:    model.OAuthClientSettings{ClientID: e.Auth.Google.ClientID, ClientSecret: e.Auth.Google.ClientSecret},
			GitHub:    model.OAuthClientSettings{ClientID: e.Auth.GitHub.ClientID, ClientSecret: e.Auth.GitHub.ClientSecret},
			Providers: FromOAuthProviderSettingsEntitiesToOAuthProviderSettingsModels(e.Auth.Providers),
		},
	}
}

// FromOAuthProviderSettingsModelsToOAuthProviderSettingsEntities maps custom
// sign-in providers to the domain.
func FromOAuthProviderSettingsModelsToOAuthProviderSettingsEntities(ps []model.OAuthProviderSettings) []entity.OAuthProviderSettings {
	if len(ps) == 0 {
		return nil
	}
	out := make([]entity.OAuthProviderSettings, len(ps))
	for i, p := range ps {
		out[i] = entity.OAuthProviderSettings{
			ID: p.ID, Name: p.Name, Icon: p.Icon, ClientID: p.ClientID, ClientSecret: p.ClientSecret, AuthURL: p.AuthURL,
		}
	}
	return out
}

// FromOAuthProviderSettingsEntitiesToOAuthProviderSettingsModels maps custom
// sign-in providers from the domain.
func FromOAuthProviderSettingsEntitiesToOAuthProviderSettingsModels(es []entity.OAuthProviderSettings) []model.OAuthProviderSettings {
	out := make([]model.OAuthProviderSettings, len(es))
	for i, e := range es {
		out[i] = model.OAuthProviderSettings{
			ID: e.ID, Name: e.Name, Icon: e.Icon, ClientID: e.ClientID, ClientSecret: e.ClientSecret, AuthURL: e.AuthURL,
		}
	}
	return out
}

// storageDriver reads the driver, falling back to the enabled flag of
// settings saved before the driver existed.
func storageDriver(m model.StorageSettings) string {
	switch {
	case m.Driver != "":
		return m.Driver
	case m.S3.Enabled:
		return "s3"
	default:
		return "local"
	}
}
