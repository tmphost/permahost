// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"

	"github.com/gofiber/fiber/v3"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/view"
)

// RootCookie holds the root (setup) session token.
const RootCookie = "tmphost_root"

// FromHTTPRequestToRootLoginRequestEntity reads POST /setup/login.
func FromHTTPRequestToRootLoginRequestEntity(c fiber.Ctx) *entity.RootLoginRequest {
	var rq view.RootLoginRequest
	if err := json.Unmarshal(c.Body(), &rq); err != nil || rq.Token == "" {
		return nil
	}
	return &entity.RootLoginRequest{Token: rq.Token}
}

// FromHTTPRequestToAuthenticateRootRequestEntity reads the root cookie.
func FromHTTPRequestToAuthenticateRootRequestEntity(c fiber.Ctx) *entity.AuthenticateRootRequest {
	tok := c.Cookies(RootCookie)
	if tok == "" {
		return nil
	}
	return &entity.AuthenticateRootRequest{SessionToken: tok}
}

// FromHTTPRequestToUpdateSettingsRequestEntity reads PUT /setup/settings.
func FromHTTPRequestToUpdateSettingsRequestEntity(c fiber.Ctx) *entity.UpdateSettingsRequest {
	var rq view.SettingsRequest
	if err := json.Unmarshal(c.Body(), &rq); err != nil {
		return nil
	}
	return &entity.UpdateSettingsRequest{Settings: FromSettingsViewToSettingsEntity(rq.Settings)}
}

// FromGetSettingsResponseEntityToHTTPResponse renders GET /setup/settings.
func FromGetSettingsResponseEntityToHTTPResponse(rs *entity.GetSettingsResponse) []byte {
	return marshal(view.SettingsResponse{
		Settings: FromSettingsEntityToSettingsView(rs.Settings),
		SecretsSet: view.SecretsSet{
			PostgresDSN:           rs.SecretsSet.PostgresDSN,
			S3SecretAccessKey:     rs.SecretsSet.S3SecretAccessKey,
			GoogleClientSecret:    rs.SecretsSet.GoogleClientSecret,
			GitHubClientSecret:    rs.SecretsSet.GitHubClientSecret,
			CloudflareAPIToken:    rs.SecretsSet.CloudflareAPIToken,
			ProviderClientSecrets: providerSecretsSet(rs.SecretsSet.ProviderClientSecrets),
		},
		RestartRequired: rs.RestartRequired,
	})
}

// FromUpdateSettingsResponseEntityToHTTPResponse renders PUT /setup/settings.
func FromUpdateSettingsResponseEntityToHTTPResponse(rs *entity.UpdateSettingsResponse) []byte {
	return FromGetSettingsResponseEntityToHTTPResponse(rs)
}

// FromSettingsViewToSettingsEntity maps request settings to the domain.
func FromSettingsViewToSettingsEntity(v view.Settings) entity.Settings {
	return entity.Settings{
		Domain: entity.DomainSettings{AppURL: v.Domain.AppURL, SitesDomain: v.Domain.SitesDomain},
		TLS: entity.TLSSettings{
			Enabled: v.TLS.Enabled,
			Port:    v.TLS.Port,
			Email:   v.TLS.Email,
			Lego: entity.LegoSettings{
				Enabled:            v.TLS.Lego.Enabled,
				CloudflareAPIToken: v.TLS.Lego.CloudflareAPIToken,
			},
		},
		Credits: entity.CreditSettings{MonthlyGrant: v.Credits.MonthlyGrant},
		Database: entity.DatabaseSettings{
			Driver:   v.Database.Driver,
			SQLite:   entity.SQLiteSettings{DSN: v.Database.SQLite.DSN},
			Postgres: entity.PostgresSettings{DSN: v.Database.Postgres.DSN},
		},
		Storage: entity.StorageSettings{
			Driver: v.Storage.Driver,
			Local:  entity.LocalStorageSettings{Path: v.Storage.Local.Path},
			S3: entity.S3Settings{
				Endpoint:        v.Storage.S3.Endpoint,
				Region:          v.Storage.S3.Region,
				Bucket:          v.Storage.S3.Bucket,
				AccessKeyID:     v.Storage.S3.AccessKeyID,
				SecretAccessKey: v.Storage.S3.SecretAccessKey,
				UsePathStyle:    v.Storage.S3.UsePathStyle,
			},
		},
		Files: entity.FileSettings{
			MaxTTLMinutes: v.Files.MaxTTLMinutes, MaxUploadMB: v.Files.MaxUploadMB,
			AnonymousMaxTTLMinutes: v.Files.AnonymousMaxTTLMinutes,
			Subdomains:             v.Files.Subdomains, PublicDomain: v.Files.PublicDomain,
		},
		Auth: entity.AuthSettings{
			Google:    entity.OAuthClientSettings{ClientID: v.Auth.Google.ClientID, ClientSecret: v.Auth.Google.ClientSecret},
			GitHub:    entity.OAuthClientSettings{ClientID: v.Auth.GitHub.ClientID, ClientSecret: v.Auth.GitHub.ClientSecret},
			Providers: FromOAuthProviderSettingsViewsToOAuthProviderSettingsEntities(v.Auth.Providers),
		},
	}
}

// FromSettingsEntityToSettingsView maps domain settings to the response.
func FromSettingsEntityToSettingsView(e entity.Settings) view.Settings {
	return view.Settings{
		Domain: view.DomainSettings{AppURL: e.Domain.AppURL, SitesDomain: e.Domain.SitesDomain},
		TLS: view.TLSSettings{
			Enabled: e.TLS.Enabled,
			Port:    e.TLS.Port,
			Email:   e.TLS.Email,
			Lego: view.LegoSettings{
				Enabled:            e.TLS.Lego.Enabled,
				CloudflareAPIToken: e.TLS.Lego.CloudflareAPIToken,
			},
		},
		Credits: view.CreditSettings{MonthlyGrant: e.Credits.MonthlyGrant},
		Database: view.DatabaseSettings{
			Driver:   e.Database.Driver,
			SQLite:   view.SQLiteSettings{DSN: e.Database.SQLite.DSN},
			Postgres: view.PostgresSettings{DSN: e.Database.Postgres.DSN},
		},
		Storage: view.StorageSettings{
			Driver: e.Storage.Driver,
			Local:  view.LocalStorageSettings{Path: e.Storage.Local.Path},
			S3: view.S3Settings{
				Endpoint:        e.Storage.S3.Endpoint,
				Region:          e.Storage.S3.Region,
				Bucket:          e.Storage.S3.Bucket,
				AccessKeyID:     e.Storage.S3.AccessKeyID,
				SecretAccessKey: e.Storage.S3.SecretAccessKey,
				UsePathStyle:    e.Storage.S3.UsePathStyle,
			},
		},
		Files: view.FileSettings{
			MaxTTLMinutes: e.Files.MaxTTLMinutes, MaxUploadMB: e.Files.MaxUploadMB,
			AnonymousMaxTTLMinutes: e.Files.AnonymousMaxTTLMinutes,
			Subdomains:             e.Files.Subdomains, PublicDomain: e.Files.PublicDomain,
		},
		Auth: view.AuthSettings{
			Google:    view.OAuthClientSettings{ClientID: e.Auth.Google.ClientID, ClientSecret: e.Auth.Google.ClientSecret},
			GitHub:    view.OAuthClientSettings{ClientID: e.Auth.GitHub.ClientID, ClientSecret: e.Auth.GitHub.ClientSecret},
			Providers: FromOAuthProviderSettingsEntitiesToOAuthProviderSettingsViews(e.Auth.Providers),
		},
	}
}

// FromOAuthProviderSettingsViewsToOAuthProviderSettingsEntities maps custom
// sign-in providers to the domain.
func FromOAuthProviderSettingsViewsToOAuthProviderSettingsEntities(ps []view.OAuthProviderSettings) []entity.OAuthProviderSettings {
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

// FromOAuthProviderSettingsEntitiesToOAuthProviderSettingsViews maps custom
// sign-in providers from the domain.
func FromOAuthProviderSettingsEntitiesToOAuthProviderSettingsViews(es []entity.OAuthProviderSettings) []view.OAuthProviderSettings {
	out := make([]view.OAuthProviderSettings, len(es))
	for i, e := range es {
		out[i] = view.OAuthProviderSettings{
			ID: e.ID, Name: e.Name, Icon: e.Icon, ClientID: e.ClientID, ClientSecret: e.ClientSecret, AuthURL: e.AuthURL,
		}
	}
	return out
}

// providerSecretsSet always renders an object, never null.
func providerSecretsSet(m map[string]bool) map[string]bool {
	if m == nil {
		return map[string]bool{}
	}
	return m
}
