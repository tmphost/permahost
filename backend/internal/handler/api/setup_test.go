// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	setupctrl "github.com/tmphost/tmphost/backend/internal/controller/setup"
	"github.com/tmphost/tmphost/backend/internal/data/entity"
	mapper "github.com/tmphost/tmphost/backend/internal/mapper/api"
)

func send(t *testing.T, app *fiber.App, method, target, body string, cookies ...*http.Cookie) (*http.Response, string) {
	t.Helper()
	req := httptest.NewRequest(method, target, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	res, err := app.Test(req)
	require.NoError(t, err)
	b, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return res, string(b)
}

var _rootCookie = &http.Cookie{Name: mapper.RootCookie, Value: "root-session"}

func TestSetupLogin(t *testing.T) {
	t.Run("ok sets a scoped strict cookie", func(t *testing.T) {
		app, _, setup := newFullApp(t)
		setup.EXPECT().Login(gomock.Any(), &entity.RootLoginRequest{Token: "tok"}).Return(&entity.RootLoginResponse{SessionToken: "root-session"}, nil)
		res, _ := send(t, app, http.MethodPost, "/api/v1/setup/login", `{"token":"tok"}`)
		assert.Equal(t, http.StatusNoContent, res.StatusCode)
		ck := cookieNamed(res, mapper.RootCookie)
		require.NotNil(t, ck)
		assert.Equal(t, "root-session", ck.Value)
		assert.Equal(t, "/base/api/v1/setup", ck.Path)
		assert.Equal(t, http.SameSiteStrictMode, ck.SameSite)
		assert.True(t, ck.HttpOnly)
		assert.True(t, ck.Secure)
		assert.Equal(t, int((12 * time.Hour).Seconds()), ck.MaxAge)
	})

	t.Run("bad body", func(t *testing.T) {
		app, _, _ := newFullApp(t)
		res, _ := send(t, app, http.MethodPost, "/api/v1/setup/login", `{}`)
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	t.Run("wrong token", func(t *testing.T) {
		app, _, setup := newFullApp(t)
		setup.EXPECT().Login(gomock.Any(), gomock.Any()).Return(nil, setupctrl.ErrInvalidRootToken)
		res, body := send(t, app, http.MethodPost, "/api/v1/setup/login", `{"token":"x"}`)
		assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
		assert.JSONEq(t, `{"error":"invalid root access token"}`, body)
	})

	t.Run("server error", func(t *testing.T) {
		app, _, setup := newFullApp(t)
		setup.EXPECT().Login(gomock.Any(), gomock.Any()).Return(nil, errBoom)
		res, _ := send(t, app, http.MethodPost, "/api/v1/setup/login", `{"token":"x"}`)
		assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
	})

	t.Run("rate limited", func(t *testing.T) {
		app, _, setup := newFullApp(t)
		setup.EXPECT().Login(gomock.Any(), gomock.Any()).Return(nil, setupctrl.ErrInvalidRootToken).Times(_rootLoginMax)
		for i := 0; i < _rootLoginMax; i++ {
			res, _ := send(t, app, http.MethodPost, "/api/v1/setup/login", fmt.Sprintf(`{"token":"guess-%d"}`, i))
			require.Equal(t, http.StatusUnauthorized, res.StatusCode)
		}
		res, body := send(t, app, http.MethodPost, "/api/v1/setup/login", `{"token":"one-more"}`)
		assert.Equal(t, http.StatusTooManyRequests, res.StatusCode)
		assert.JSONEq(t, `{"error":"too many attempts"}`, body)
	})
}

func TestSetupLogout(t *testing.T) {
	app, _, _ := newFullApp(t)
	res, _ := send(t, app, http.MethodPost, "/api/v1/setup/logout", "")
	assert.Equal(t, http.StatusNoContent, res.StatusCode)
	ck := cookieNamed(res, mapper.RootCookie)
	require.NotNil(t, ck)
	assert.Empty(t, ck.Value)
	assert.True(t, ck.Expires.Before(time.Now()))
}

func TestSetupSettings_RequiresRoot(t *testing.T) {
	app, _, setup := newFullApp(t)
	res, _ := send(t, app, http.MethodGet, "/api/v1/setup/settings", "")
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode)

	setup.EXPECT().Authenticate(gomock.Any(), &entity.AuthenticateRootRequest{SessionToken: "root-session"}).Return(setupctrl.ErrUnauthorized)
	res, _ = send(t, app, http.MethodPut, "/api/v1/setup/settings", "{}", _rootCookie)
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
}

func TestSetupSettings_Get(t *testing.T) {
	app, _, setup := newFullApp(t)
	setup.EXPECT().Authenticate(gomock.Any(), gomock.Any()).Return(nil)
	setup.EXPECT().GetSettings(gomock.Any()).Return(&entity.GetSettingsResponse{
		Settings:   entity.Settings{Credits: entity.CreditSettings{MonthlyGrant: 250}},
		SecretsSet: entity.SecretsSet{GoogleClientSecret: true},
	})
	res, body := send(t, app, http.MethodGet, "/api/v1/setup/settings", "", _rootCookie)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Contains(t, body, `"monthlyGrant":250`)
	assert.Contains(t, body, `"googleClientSecret":true`)
}

func TestSetupSettings_Update(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		app, _, setup := newFullApp(t)
		setup.EXPECT().Authenticate(gomock.Any(), gomock.Any()).Return(nil)
		setup.EXPECT().UpdateSettings(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ any, rq *entity.UpdateSettingsRequest) (*entity.UpdateSettingsResponse, error) {
				assert.Equal(t, 400, rq.Settings.Credits.MonthlyGrant)
				return &entity.UpdateSettingsResponse{Settings: rq.Settings, RestartRequired: true}, nil
			})
		res, body := send(t, app, http.MethodPut, "/api/v1/setup/settings", `{"settings":{"credits":{"monthlyGrant":400}}}`, _rootCookie)
		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.Contains(t, body, `"restartRequired":true`)
	})

	t.Run("bad body", func(t *testing.T) {
		app, _, setup := newFullApp(t)
		setup.EXPECT().Authenticate(gomock.Any(), gomock.Any()).Return(nil)
		res, _ := send(t, app, http.MethodPut, "/api/v1/setup/settings", `nope`, _rootCookie)
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	})

	t.Run("invalid", func(t *testing.T) {
		app, _, setup := newFullApp(t)
		setup.EXPECT().Authenticate(gomock.Any(), gomock.Any()).Return(nil)
		setup.EXPECT().UpdateSettings(gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("%w: sqlite DSN is required", setupctrl.ErrInvalidSettings))
		res, body := send(t, app, http.MethodPut, "/api/v1/setup/settings", `{}`, _rootCookie)
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
		assert.JSONEq(t, `{"error":"invalid settings: sqlite DSN is required"}`, body)
	})

	t.Run("save fails", func(t *testing.T) {
		app, _, setup := newFullApp(t)
		setup.EXPECT().Authenticate(gomock.Any(), gomock.Any()).Return(nil)
		setup.EXPECT().UpdateSettings(gomock.Any(), gomock.Any()).Return(nil, errBoom)
		res, _ := send(t, app, http.MethodPut, "/api/v1/setup/settings", `{}`, _rootCookie)
		assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
	})
}
