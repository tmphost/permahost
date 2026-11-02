// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package setup

import (
	"context"
	"errors"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/data/model"
	mappermodel "github.com/tmphost/tmphost/backend/internal/mapper/model"
	"github.com/tmphost/tmphost/backend/internal/repository"
	mockrepo "github.com/tmphost/tmphost/backend/internal/repository/mocks"
	mocktoken "github.com/tmphost/tmphost/backend/internal/service/token/mocks"
)

var errBoom = errors.New("boom")

func defaults() entity.Settings {
	return entity.Settings{
		Domain:  entity.DomainSettings{AppURL: "http://localhost:2027"},
		TLS:     entity.TLSSettings{Port: DefaultTLSPort},
		Credits: entity.CreditSettings{MonthlyGrant: 250},
		Database: entity.DatabaseSettings{
			Driver:   DriverSQLite,
			SQLite:   entity.SQLiteSettings{DSN: "./_storage/tmphost.db"},
			Postgres: entity.PostgresSettings{DSN: "postgres://postgres:pgpass@localhost:5432/tmphost?sslmode=disable"},
		},
		Storage: entity.StorageSettings{
			Driver: StorageLocal,
			Local:  entity.LocalStorageSettings{Path: "./_storage/files"},
			S3:     entity.S3Settings{SecretAccessKey: "s3secret"},
		},
		Files: entity.FileSettings{MaxTTLMinutes: 432000, MaxUploadMB: 16, AnonymousMaxTTLMinutes: 10, PublicDomain: "tmphost.io"},
		Auth: entity.AuthSettings{
			Google: entity.OAuthClientSettings{ClientID: "g", ClientSecret: "gsecret"},
		},
	}
}

type env struct {
	ctrl  Controller
	repo  *mockrepo.MockSettingsRepository
	token *mocktoken.MockService
}

func newEnv(t *testing.T) *env {
	t.Helper()
	mc := gomock.NewController(t)
	e := &env{repo: mockrepo.NewMockSettingsRepository(mc), token: mocktoken.NewMockService(mc)}
	e.repo.EXPECT().LoadSettings().Return(model.Settings{}, repository.ErrNotFound)
	ctrl, err := New(Params{Repository: e.repo, Token: e.token, RootAccessToken: "root-token", Defaults: defaults()})
	require.NoError(t, err)
	e.ctrl = ctrl
	return e
}

func TestNew(t *testing.T) {
	mc := gomock.NewController(t)

	t.Run("requires a root token", func(t *testing.T) {
		_, err := New(Params{})
		assert.Error(t, err)
	})

	t.Run("saved settings win over defaults", func(t *testing.T) {
		repo := mockrepo.NewMockSettingsRepository(mc)
		saved := defaults()
		saved.Credits.MonthlyGrant = 500
		repo.EXPECT().LoadSettings().Return(mappermodel.FromSettingsEntityToSettingsModel(saved), nil)
		ctrl, err := New(Params{Repository: repo, RootAccessToken: "x", Defaults: defaults()})
		require.NoError(t, err)
		assert.Equal(t, 500, ctrl.MonthlyGrant())
		assert.Equal(t, saved, ctrl.Current())
	})

	t.Run("saved settings without an app URL keep the configured one", func(t *testing.T) {
		repo := mockrepo.NewMockSettingsRepository(mc)
		saved := defaults()
		saved.Domain.AppURL = ""
		saved.TLS.Port = 0
		repo.EXPECT().LoadSettings().Return(mappermodel.FromSettingsEntityToSettingsModel(saved), nil)
		ctrl, err := New(Params{Repository: repo, RootAccessToken: "x", Defaults: defaults()})
		require.NoError(t, err)
		assert.Equal(t, defaults(), ctrl.Current())
	})

	t.Run("sections missing from older saved settings take the defaults", func(t *testing.T) {
		repo := mockrepo.NewMockSettingsRepository(mc)
		saved := defaults()
		saved.Storage.Local.Path, saved.Files = "", entity.FileSettings{}
		repo.EXPECT().LoadSettings().Return(mappermodel.FromSettingsEntityToSettingsModel(saved), nil)
		ctrl, err := New(Params{Repository: repo, RootAccessToken: "x", Defaults: defaults()})
		require.NoError(t, err)
		assert.Equal(t, defaults(), ctrl.Current())
		assert.Equal(t, entity.FileSettings{MaxTTLMinutes: 432000, MaxUploadMB: 16, AnonymousMaxTTLMinutes: 10, PublicDomain: "tmphost.io"}, ctrl.FileLimits())
	})

	t.Run("load error", func(t *testing.T) {
		repo := mockrepo.NewMockSettingsRepository(mc)
		repo.EXPECT().LoadSettings().Return(model.Settings{}, errBoom)
		_, err := New(Params{Repository: repo, RootAccessToken: "x"})
		assert.ErrorIs(t, err, errBoom)
	})
}

func TestLogin(t *testing.T) {
	ctx := context.Background()

	t.Run("wrong token", func(t *testing.T) {
		e := newEnv(t)
		for _, tok := range []string{"", "root", "root-token-but-longer"} {
			_, err := e.ctrl.Login(ctx, &entity.RootLoginRequest{Token: tok})
			assert.ErrorIs(t, err, ErrInvalidRootToken, tok)
		}
	})

	t.Run("ok", func(t *testing.T) {
		e := newEnv(t)
		e.token.EXPECT().CreateRootToken().Return("root-session", nil)
		rs, err := e.ctrl.Login(ctx, &entity.RootLoginRequest{Token: "root-token"})
		require.NoError(t, err)
		assert.Equal(t, "root-session", rs.SessionToken)
	})

	t.Run("session error", func(t *testing.T) {
		e := newEnv(t)
		e.token.EXPECT().CreateRootToken().Return("", errBoom)
		_, err := e.ctrl.Login(ctx, &entity.RootLoginRequest{Token: "root-token"})
		assert.ErrorIs(t, err, errBoom)
	})
}

func TestAuthenticate(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	assert.ErrorIs(t, e.ctrl.Authenticate(ctx, &entity.AuthenticateRootRequest{}), ErrUnauthorized)

	e.token.EXPECT().ValidateRootToken("bad").Return(errBoom)
	assert.ErrorIs(t, e.ctrl.Authenticate(ctx, &entity.AuthenticateRootRequest{SessionToken: "bad"}), ErrUnauthorized)

	e.token.EXPECT().ValidateRootToken("good").Return(nil)
	assert.NoError(t, e.ctrl.Authenticate(ctx, &entity.AuthenticateRootRequest{SessionToken: "good"}))
}

func TestGetSettings_HidesSecrets(t *testing.T) {
	e := newEnv(t)
	rs := e.ctrl.GetSettings(context.Background())
	assert.Empty(t, rs.Settings.Database.Postgres.DSN)
	assert.Empty(t, rs.Settings.Storage.S3.SecretAccessKey)
	assert.Empty(t, rs.Settings.Auth.Google.ClientSecret)
	assert.Equal(t, entity.SecretsSet{
		PostgresDSN: true, S3SecretAccessKey: true, GoogleClientSecret: true, ProviderClientSecrets: map[string]bool{},
	}, rs.SecretsSet)
	assert.Equal(t, "g", rs.Settings.Auth.Google.ClientID)
	assert.False(t, rs.RestartRequired)
}

func TestUpdateSettings_CreditsApplyLive(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	next := e.ctrl.GetSettings(ctx).Settings // secrets blank: kept
	next.Credits.MonthlyGrant = 400
	next.Files.MaxTTLMinutes = 60
	next.Files.AnonymousMaxTTLMinutes = 5
	next.Files.Subdomains = true
	next.Files.PublicDomain = " Files.Example.com:8443 "

	e.repo.EXPECT().SaveSettings(gomock.Any()).DoAndReturn(func(m model.Settings) error {
		assert.Equal(t, 400, m.Credits.MonthlyGrant)
		assert.Equal(t, "postgres://postgres:pgpass@localhost:5432/tmphost?sslmode=disable", m.Database.Postgres.DSN)
		assert.Equal(t, "s3secret", m.Storage.S3.SecretAccessKey)
		assert.Equal(t, "gsecret", m.Auth.Google.ClientSecret)
		return nil
	})
	rs, err := e.ctrl.UpdateSettings(ctx, &entity.UpdateSettingsRequest{Settings: next})
	require.NoError(t, err)
	assert.False(t, rs.RestartRequired, "credits, max TTLs and public URLs apply without a restart")
	assert.Equal(t, 400, e.ctrl.MonthlyGrant())
	assert.Equal(t, entity.FileSettings{MaxTTLMinutes: 60, MaxUploadMB: 16, AnonymousMaxTTLMinutes: 5, Subdomains: true, PublicDomain: "files.example.com:8443"}, e.ctrl.FileLimits())
	assert.Empty(t, rs.Settings.Auth.Google.ClientSecret)
}

func TestUpdateSettings_RestartRequired(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	next := e.ctrl.GetSettings(ctx).Settings
	next.Database.Driver = " Postgres "
	next.Database.Postgres.DSN = " host=db user=app password=newpass dbname=prod "
	next.Auth.Google.ClientID = "" // disables Google, dropping its secret
	next.Auth.GitHub = entity.OAuthClientSettings{ClientID: " h ", ClientSecret: "hsecret"}

	e.repo.EXPECT().SaveSettings(gomock.Any()).Return(nil)
	rs, err := e.ctrl.UpdateSettings(ctx, &entity.UpdateSettingsRequest{Settings: next})
	require.NoError(t, err)
	assert.True(t, rs.RestartRequired)

	cur := e.ctrl.Current()
	assert.Equal(t, DriverPostgres, cur.Database.Driver)
	assert.Equal(t, "host=db user=app password=newpass dbname=prod", cur.Database.Postgres.DSN)
	assert.Equal(t, entity.OAuthClientSettings{}, cur.Auth.Google)
	assert.Equal(t, entity.OAuthClientSettings{ClientID: "h", ClientSecret: "hsecret"}, cur.Auth.GitHub)
}

func TestUpdateSettings_SaveFails(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	next := e.ctrl.GetSettings(ctx).Settings
	next.Credits.MonthlyGrant = 1
	e.repo.EXPECT().SaveSettings(gomock.Any()).Return(errBoom)
	_, err := e.ctrl.UpdateSettings(ctx, &entity.UpdateSettingsRequest{Settings: next})
	assert.ErrorIs(t, err, errBoom)
	assert.Equal(t, 250, e.ctrl.MonthlyGrant(), "unchanged on failure")
}

func TestUpdateSettings_Validation(t *testing.T) {
	cases := map[string]func(*entity.Settings){
		"negative credits":   func(s *entity.Settings) { s.Credits.MonthlyGrant = -1 },
		"huge credits":       func(s *entity.Settings) { s.Credits.MonthlyGrant = _maxMonthlyGrant + 1 },
		"unknown driver":     func(s *entity.Settings) { s.Database.Driver = "mysql" },
		"sqlite without dsn": func(s *entity.Settings) { s.Database.SQLite.DSN = " " },
		"postgres bad dsn url": func(s *entity.Settings) {
			s.Database.Driver = DriverPostgres
			s.Database.Postgres.DSN = "postgres://u:p@db:notaport/x"
		},
		"postgres bad dsn keywords": func(s *entity.Settings) {
			s.Database.Driver = DriverPostgres
			s.Database.Postgres.DSN = "host=db sslmode=sometimes"
		},
		"unknown storage":    func(s *entity.Settings) { s.Storage.Driver = "ftp" },
		"local without path": func(s *entity.Settings) { s.Storage.Local.Path = " " },
		"s3 without bucket": func(s *entity.Settings) {
			s.Storage.Driver, s.Storage.S3 = StorageS3, entity.S3Settings{Region: "r"}
		},
		"s3 bad endpoint": func(s *entity.Settings) {
			s.Storage.Driver, s.Storage.S3 = StorageS3, entity.S3Settings{Region: "r", Bucket: "b", Endpoint: "ftp://x"}
		},
		"s3 unparsable endpoint": func(s *entity.Settings) {
			s.Storage.Driver, s.Storage.S3 = StorageS3, entity.S3Settings{Region: "r", Bucket: "b", Endpoint: "http://[::1"}
		},
		"zero max ttl":           func(s *entity.Settings) { s.Files.MaxTTLMinutes = 0 },
		"huge max ttl":           func(s *entity.Settings) { s.Files.MaxTTLMinutes = _maxTTLMinutes + 1 },
		"zero max upload":        func(s *entity.Settings) { s.Files.MaxUploadMB = 0 },
		"zero anonymous max ttl": func(s *entity.Settings) { s.Files.AnonymousMaxTTLMinutes = 0 },
		"anonymous max ttl over max ttl": func(s *entity.Settings) {
			s.Files.MaxTTLMinutes, s.Files.AnonymousMaxTTLMinutes = 30, 31
		},
		"huge max upload": func(s *entity.Settings) { s.Files.MaxUploadMB = _maxUploadMB + 1 },
		"subdomains without domain": func(s *entity.Settings) {
			s.Files.Subdomains, s.Files.PublicDomain = true, " "
		},
		"domain with scheme":       func(s *entity.Settings) { s.Files.PublicDomain = "https://tmphost.io" },
		"domain with path":         func(s *entity.Settings) { s.Files.PublicDomain = "tmphost.io/x" },
		"domain with bad label":    func(s *entity.Settings) { s.Files.PublicDomain = "-x.tmphost.io" },
		"github id without secret": func(s *entity.Settings) { s.Auth.GitHub = entity.OAuthClientSettings{ClientID: "h"} },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			next := defaults()
			mutate(&next)
			_, err := e.ctrl.UpdateSettings(context.Background(), &entity.UpdateSettingsRequest{Settings: next})
			assert.ErrorIs(t, err, ErrInvalidSettings)
		})
	}
}

func TestUpdateSettings_ProviderIDWithoutAnySecret(t *testing.T) {
	// Nothing stored to keep, and no new secret: rejected.
	e := newEnvWith(t, func(s *entity.Settings) { s.Auth.Google = entity.OAuthClientSettings{} })
	next := defaults()
	next.Auth.Google = entity.OAuthClientSettings{ClientID: "g2"}
	_, err := e.ctrl.UpdateSettings(context.Background(), &entity.UpdateSettingsRequest{Settings: next})
	assert.ErrorIs(t, err, ErrInvalidSettings)
}

func TestUpdateSettings_PostgresDSN(t *testing.T) {
	t.Run("required when nothing is stored", func(t *testing.T) {
		e := newEnvWith(t, func(s *entity.Settings) { s.Database.Postgres.DSN = "" })
		next := defaults()
		next.Database.Driver = DriverPostgres
		next.Database.Postgres.DSN = ""
		_, err := e.ctrl.UpdateSettings(context.Background(), &entity.UpdateSettingsRequest{Settings: next})
		assert.ErrorIs(t, err, ErrInvalidSettings)
	})

	t.Run("invalid DSN is not echoed", func(t *testing.T) {
		e := newEnv(t)
		next := defaults()
		next.Database.Driver = DriverPostgres
		next.Database.Postgres.DSN = "postgres://u:hunter2@db:notaport/x"
		_, err := e.ctrl.UpdateSettings(context.Background(), &entity.UpdateSettingsRequest{Settings: next})
		assert.ErrorIs(t, err, ErrInvalidSettings)
		assert.NotContains(t, err.Error(), "hunter2")
	})
}

func TestUpdateSettings_ValidS3(t *testing.T) {
	e := newEnv(t)
	next := defaults()
	next.Storage.Driver = " S3 "
	next.Storage.S3 = entity.S3Settings{Region: "auto", Bucket: "b", Endpoint: "https://r2.example.com"}
	e.repo.EXPECT().SaveSettings(gomock.Any()).Return(nil)
	rs, err := e.ctrl.UpdateSettings(context.Background(), &entity.UpdateSettingsRequest{Settings: next})
	require.NoError(t, err)
	assert.True(t, rs.SecretsSet.S3SecretAccessKey, "kept from current")
	assert.Equal(t, StorageS3, rs.Settings.Storage.Driver)
	assert.True(t, rs.RestartRequired, "storage changes need a restart")
}

func newEnvWith(t *testing.T, mutate func(*entity.Settings)) *env {
	t.Helper()
	mc := gomock.NewController(t)
	e := &env{repo: mockrepo.NewMockSettingsRepository(mc), token: mocktoken.NewMockService(mc)}
	d := defaults()
	mutate(&d)
	e.repo.EXPECT().LoadSettings().Return(model.Settings{}, repository.ErrNotFound)
	ctrl, err := New(Params{Repository: e.repo, Token: e.token, RootAccessToken: "root-token", Defaults: d})
	require.NoError(t, err)
	e.ctrl = ctrl
	return e
}
