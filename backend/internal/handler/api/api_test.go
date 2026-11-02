// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
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

	authctrl "github.com/tmphost/tmphost/backend/internal/controller/auth"
	mockauth "github.com/tmphost/tmphost/backend/internal/controller/auth/mocks"
	mocksetup "github.com/tmphost/tmphost/backend/internal/controller/setup/mocks"
	"github.com/tmphost/tmphost/backend/internal/data/entity"
	mapper "github.com/tmphost/tmphost/backend/internal/mapper/api"
)

var errBoom = errors.New("boom")

func newApp(t *testing.T) (*fiber.App, *mockauth.MockController) {
	app, ctrl, _ := newFullApp(t)
	return app, ctrl
}

func newFullApp(t *testing.T) (*fiber.App, *mockauth.MockController, *mocksetup.MockController) {
	t.Helper()
	mc := gomock.NewController(t)
	ctrl := mockauth.NewMockController(mc)
	setup := mocksetup.NewMockController(mc)
	app := fiber.New()
	New(Params{Router: app, Auth: ctrl, Setup: setup, FileSettings: func() entity.FileSettings { return entity.FileSettings{} }, BasePath: "/base/", CookieSecure: true})
	return app, ctrl, setup
}

func do(t *testing.T, app *fiber.App, method, target string, cookies ...*http.Cookie) (*http.Response, string) {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	res, err := app.Test(req)
	require.NoError(t, err)
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return res, string(body)
}

func cookieNamed(res *http.Response, name string) *http.Cookie {
	for _, c := range res.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestConfig(t *testing.T) {
	app, ctrl := newApp(t)
	ctrl.EXPECT().GetAuthConfig(gomock.Any()).Return(&entity.GetAuthConfigResponse{Providers: []entity.AuthProvider{{ID: "google", Name: "Google"}}})
	res, body := do(t, app, http.MethodGet, "/auth/config")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, fiber.MIMEApplicationJSON, res.Header.Get(fiber.HeaderContentType))
	assert.JSONEq(t, `{"providers":[{"id":"google","name":"Google","icon":""}]}`, body)
}

func TestLogin(t *testing.T) {
	t.Run("redirects and pins state", func(t *testing.T) {
		app, ctrl := newApp(t)
		ctrl.EXPECT().StartLogin(gomock.Any(), &entity.StartLoginRequest{Provider: "google", RedirectURL: "/x"}).
			Return(&entity.StartLoginResponse{AuthURL: "https://accounts.google.com/o?state=st", State: "st"}, nil)
		res, _ := do(t, app, http.MethodGet, "/auth/google/login?redirect_url=/x")
		assert.Equal(t, http.StatusFound, res.StatusCode)
		assert.Equal(t, "https://accounts.google.com/o?state=st", res.Header.Get("Location"))
		ck := cookieNamed(res, mapper.StateCookie)
		require.NotNil(t, ck)
		assert.Equal(t, "st", ck.Value)
		assert.Equal(t, "/base/auth", ck.Path)
		assert.True(t, ck.HttpOnly)
		assert.True(t, ck.Secure)
		assert.Equal(t, http.SameSiteLaxMode, ck.SameSite)
		assert.Equal(t, 300, ck.MaxAge)
	})

	t.Run("unknown provider", func(t *testing.T) {
		app, ctrl := newApp(t)
		ctrl.EXPECT().StartLogin(gomock.Any(), gomock.Any()).Return(nil, authctrl.ErrUnknownProvider)
		res, body := do(t, app, http.MethodGet, "/auth/x/login")
		assert.Equal(t, http.StatusNotFound, res.StatusCode)
		assert.JSONEq(t, `{"error":"unknown provider"}`, body)
	})

	t.Run("provider unavailable", func(t *testing.T) {
		app, ctrl := newApp(t)
		ctrl.EXPECT().StartLogin(gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("%w: down", authctrl.ErrProviderUnavailable))
		res, _ := do(t, app, http.MethodGet, "/auth/okta/login")
		assert.Equal(t, http.StatusFound, res.StatusCode)
		assert.Equal(t, "/base/login?error=provider_error", res.Header.Get("Location"))
	})

	t.Run("controller error", func(t *testing.T) {
		app, ctrl := newApp(t)
		ctrl.EXPECT().StartLogin(gomock.Any(), gomock.Any()).Return(nil, errBoom)
		res, _ := do(t, app, http.MethodGet, "/auth/google/login")
		assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
	})

	t.Run("missing provider", func(t *testing.T) {
		app := fiber.New()
		h := &Auth{}
		app.Get("/login", h.Login)
		res, _ := do(t, app, http.MethodGet, "/login")
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	})
}

func TestCallback_Success(t *testing.T) {
	app, ctrl := newApp(t)
	ctrl.EXPECT().CompleteLogin(gomock.Any(), &entity.CompleteLoginRequest{
		Provider: "github", Code: "c", State: "st", StateCookie: "st",
	}).Return(&entity.CompleteLoginResponse{SessionToken: "session", RedirectURL: "/base/settings"}, nil)

	res, _ := do(t, app, http.MethodGet, "/auth/github/callback?code=c&state=st",
		&http.Cookie{Name: mapper.StateCookie, Value: "st"})
	assert.Equal(t, http.StatusFound, res.StatusCode)
	assert.Equal(t, "/base/settings", res.Header.Get("Location"))

	session := cookieNamed(res, mapper.SessionCookie)
	require.NotNil(t, session)
	assert.Equal(t, "session", session.Value)
	assert.Equal(t, "/base/", session.Path)
	assert.True(t, session.HttpOnly)
	assert.Equal(t, int((7 * 24 * time.Hour).Seconds()), session.MaxAge)

	state := cookieNamed(res, mapper.StateCookie)
	require.NotNil(t, state)
	assert.Empty(t, state.Value, "state cookie is cleared")
}

func TestCallback_Errors(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{authctrl.ErrUnknownProvider, LoginErrorUnknownProvider},
		{authctrl.ErrInvalidState, LoginErrorInvalidState},
		{authctrl.ErrEmailNotVerified, LoginErrorEmailNotVerified},
		{authctrl.ErrProviderExchange, LoginErrorProvider},
		{errBoom, LoginErrorServer},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			app, ctrl := newApp(t)
			ctrl.EXPECT().CompleteLogin(gomock.Any(), gomock.Any()).Return(nil, tc.err)
			res, _ := do(t, app, http.MethodGet, "/auth/google/callback?code=c&state=st")
			assert.Equal(t, http.StatusFound, res.StatusCode)
			assert.Equal(t, "/base/login?error="+tc.code, res.Header.Get("Location"))
			assert.Nil(t, cookieNamed(res, mapper.SessionCookie))
		})
	}

	t.Run("cancelled", func(t *testing.T) {
		app, _ := newApp(t)
		res, _ := do(t, app, http.MethodGet, "/auth/google/callback?error=access_denied")
		assert.Equal(t, "/base/login?error="+LoginErrorCancelled, res.Header.Get("Location"))
	})
}

func TestLogout(t *testing.T) {
	app, _ := newApp(t)
	res, _ := do(t, app, http.MethodPost, "/auth/logout")
	assert.Equal(t, http.StatusNoContent, res.StatusCode)
	ck := cookieNamed(res, mapper.SessionCookie)
	require.NotNil(t, ck)
	assert.Empty(t, ck.Value)
	assert.Equal(t, "/base/", ck.Path)
	assert.True(t, ck.Expires.Before(time.Now()))
}

func TestUsersMe(t *testing.T) {
	session := &http.Cookie{Name: mapper.SessionCookie, Value: "tok"}

	t.Run("no cookie", func(t *testing.T) {
		app, _ := newApp(t)
		res, body := do(t, app, http.MethodGet, "/api/v1/users/me")
		assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
		assert.JSONEq(t, `{"error":"unauthorized"}`, body)
	})

	t.Run("invalid session", func(t *testing.T) {
		app, ctrl := newApp(t)
		ctrl.EXPECT().Authenticate(gomock.Any(), &entity.AuthenticateRequest{SessionToken: "tok"}).Return(nil, authctrl.ErrUnauthorized)
		res, _ := do(t, app, http.MethodGet, "/api/v1/users/me", session)
		assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
	})

	t.Run("deleted user", func(t *testing.T) {
		app, ctrl := newApp(t)
		ctrl.EXPECT().Authenticate(gomock.Any(), gomock.Any()).Return(&entity.AuthenticateResponse{UserID: 5}, nil)
		ctrl.EXPECT().GetCurrentUser(gomock.Any(), &entity.GetCurrentUserRequest{UserID: 5}).Return(nil, authctrl.ErrUnauthorized)
		res, _ := do(t, app, http.MethodGet, "/api/v1/users/me", session)
		assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
	})

	t.Run("server error", func(t *testing.T) {
		app, ctrl := newApp(t)
		ctrl.EXPECT().Authenticate(gomock.Any(), gomock.Any()).Return(&entity.AuthenticateResponse{UserID: 5}, nil)
		ctrl.EXPECT().GetCurrentUser(gomock.Any(), gomock.Any()).Return(nil, errBoom)
		res, body := do(t, app, http.MethodGet, "/api/v1/users/me", session)
		assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
		assert.JSONEq(t, `{"error":"failed to load user"}`, body)
	})

	t.Run("ok", func(t *testing.T) {
		app, ctrl := newApp(t)
		ctrl.EXPECT().Authenticate(gomock.Any(), gomock.Any()).Return(&entity.AuthenticateResponse{UserID: 5}, nil)
		ctrl.EXPECT().GetCurrentUser(gomock.Any(), gomock.Any()).Return(&entity.GetCurrentUserResponse{User: entity.User{
			ID: 5, Email: "a@b.c", Credits: 250, CreditsLimit: 250,
		}}, nil)
		res, body := do(t, app, http.MethodGet, "/api/v1/users/me", session)
		assert.Equal(t, http.StatusOK, res.StatusCode)
		assert.Contains(t, body, `"credits":250`)
		assert.Contains(t, body, `"email":"a@b.c"`)
	})

	t.Run("missing user id", func(t *testing.T) {
		app := fiber.New()
		h := &User{}
		app.Get("/me", h.Get)
		res, _ := do(t, app, http.MethodGet, "/me")
		assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	})
}
