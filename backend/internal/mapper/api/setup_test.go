// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
)

func runBody(t *testing.T, body string, fn func(c fiber.Ctx)) {
	t.Helper()
	app := fiber.New()
	app.Post("/", func(c fiber.Ctx) error {
		fn(c)
		return nil
	})
	_, err := app.Test(httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body)))
	require.NoError(t, err)
}

func TestFromHTTPRequestToRootLoginRequestEntity(t *testing.T) {
	cases := map[string]*entity.RootLoginRequest{
		`{"token":"t"}`: {Token: "t"},
		`{"token":""}`:  nil,
		`not json`:      nil,
	}
	for body, want := range cases {
		var got *entity.RootLoginRequest
		runBody(t, body, func(c fiber.Ctx) { got = FromHTTPRequestToRootLoginRequestEntity(c) })
		assert.Equal(t, want, got, body)
	}
}

func TestFromHTTPRequestToAuthenticateRootRequestEntity(t *testing.T) {
	var got *entity.AuthenticateRootRequest
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: RootCookie, Value: "r"})
	run(t, "/", req, func(c fiber.Ctx) { got = FromHTTPRequestToAuthenticateRootRequestEntity(c) })
	assert.Equal(t, &entity.AuthenticateRootRequest{SessionToken: "r"}, got)

	run(t, "/", httptest.NewRequest(http.MethodGet, "/", nil), func(c fiber.Ctx) { got = FromHTTPRequestToAuthenticateRootRequestEntity(c) })
	assert.Nil(t, got)
}

const _settingsJSON = `{
	"domain": {"appUrl": "https://app.tmphost.io", "sitesDomain": "tmphost.io"},
	"tls": {"enabled": true, "port": 8443, "email": "ops@tmphost.io",
		"lego": {"enabled": true, "cloudflareApiToken": "cf"}},
	"credits": {"monthlyGrant": 300},
	"database": {"driver": "postgres", "sqlite": {"dsn": "x.db"},
		"postgres": {"dsn": "postgres://u:p@h/d"}},
	"storage": {"s3": {"enabled": true, "endpoint": "https://s3", "region": "r", "bucket": "b",
		"accessKeyId": "a", "secretAccessKey": "s", "usePathStyle": true}},
	"files": {"maxTtlMinutes": 60, "maxUploadMb": 2, "anonymousMaxTtlMinutes": 7, "subdomains": true, "publicDomain": "x.io"},
	"auth": {"google": {"clientId": "g", "clientSecret": "gs"}, "github": {"clientId": "h", "clientSecret": "hs"},
		"providers": [{"id": "okta", "name": "Okta", "icon": "<svg/>", "clientId": "o", "clientSecret": "os", "authUrl": "https://okta.example.com"}]}
}`

func TestSettingsMapping(t *testing.T) {
	var got *entity.UpdateSettingsRequest
	runBody(t, `{"settings":`+_settingsJSON+`}`, func(c fiber.Ctx) { got = FromHTTPRequestToUpdateSettingsRequestEntity(c) })
	require.NotNil(t, got)
	assert.Equal(t, 300, got.Settings.Credits.MonthlyGrant)
	assert.Equal(t, "postgres://u:p@h/d", got.Settings.Database.Postgres.DSN)
	assert.Equal(t, "hs", got.Settings.Auth.GitHub.ClientSecret)
	assert.Equal(t, entity.FileSettings{MaxTTLMinutes: 60, MaxUploadMB: 2, AnonymousMaxTTLMinutes: 7, Subdomains: true, PublicDomain: "x.io"}, got.Settings.Files)
	assert.Equal(t, entity.DomainSettings{AppURL: "https://app.tmphost.io", SitesDomain: "tmphost.io"}, got.Settings.Domain)
	assert.Equal(t, entity.TLSSettings{
		Enabled: true, Port: 8443, Email: "ops@tmphost.io",
		Lego: entity.LegoSettings{Enabled: true, CloudflareAPIToken: "cf"},
	}, got.Settings.TLS)
	assert.Equal(t, []entity.OAuthProviderSettings{{
		ID: "okta", Name: "Okta", Icon: "<svg/>", ClientID: "o", ClientSecret: "os", AuthURL: "https://okta.example.com",
	}}, got.Settings.Auth.Providers)
	// Entity -> view -> entity keeps the new sections too.
	assert.Equal(t, got.Settings, FromSettingsViewToSettingsEntity(FromSettingsEntityToSettingsView(got.Settings)))

	runBody(t, `nope`, func(c fiber.Ctx) { got = FromHTTPRequestToUpdateSettingsRequestEntity(c) })
	assert.Nil(t, got)

	// Entity -> view -> entity is lossless.
	settings := FromSettingsViewToSettingsEntity(FromSettingsEntityToSettingsView(entity.Settings{
		Database: entity.DatabaseSettings{Postgres: entity.PostgresSettings{DSN: "host=db"}},
		Storage:  entity.StorageSettings{S3: entity.S3Settings{UsePathStyle: true}},
		Files:    entity.FileSettings{Subdomains: true, PublicDomain: "x.io"},
	}))
	assert.Equal(t, entity.FileSettings{Subdomains: true, PublicDomain: "x.io"}, settings.Files)
	assert.Equal(t, "host=db", settings.Database.Postgres.DSN)
	assert.True(t, settings.Storage.S3.UsePathStyle)
}

func TestFromSettingsResponseEntityToHTTPResponse(t *testing.T) {
	rs := &entity.GetSettingsResponse{
		Settings:        FromSettingsViewToSettingsEntity(FromSettingsEntityToSettingsView(entity.Settings{Credits: entity.CreditSettings{MonthlyGrant: 7}})),
		SecretsSet:      entity.SecretsSet{PostgresDSN: true, S3SecretAccessKey: true, GoogleClientSecret: true, GitHubClientSecret: true},
		RestartRequired: true,
	}
	body := string(FromUpdateSettingsResponseEntityToHTTPResponse(rs))
	assert.Contains(t, body, `"monthlyGrant":7`)
	assert.Contains(t, body, `"secretsSet":{"postgresDsn":true,"s3SecretAccessKey":true,"googleClientSecret":true,"githubClientSecret":true,`+
		`"cloudflareApiToken":false,"providerClientSecrets":{}}`)
	assert.Contains(t, body, `"providers":[]`)

	rs.SecretsSet = entity.SecretsSet{CloudflareAPIToken: true, ProviderClientSecrets: map[string]bool{"okta": true}}
	body = string(FromGetSettingsResponseEntityToHTTPResponse(rs))
	assert.Contains(t, body, `"cloudflareApiToken":true,"providerClientSecrets":{"okta":true}`)
	assert.Contains(t, body, `"restartRequired":true`)
}
