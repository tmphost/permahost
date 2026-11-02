// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/mustafaturan/monoflake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
)

// run executes fn inside a real fiber request built from req.
func run(t *testing.T, route string, req *http.Request, fn func(c fiber.Ctx)) {
	t.Helper()
	app := fiber.New()
	app.Get(route, func(c fiber.Ctx) error {
		fn(c)
		return nil
	})
	_, err := app.Test(req)
	require.NoError(t, err)
}

func TestFromHTTPRequestToStartLoginRequestEntity(t *testing.T) {
	var got *entity.StartLoginRequest
	run(t, "/auth/:provider/login", httptest.NewRequest(http.MethodGet, "/auth/google/login?redirect_url=/x", nil), func(c fiber.Ctx) {
		got = FromHTTPRequestToStartLoginRequestEntity(c)
	})
	assert.Equal(t, &entity.StartLoginRequest{Provider: "google", RedirectURL: "/x"}, got)

	run(t, "/login", httptest.NewRequest(http.MethodGet, "/login", nil), func(c fiber.Ctx) {
		got = FromHTTPRequestToStartLoginRequestEntity(c)
	})
	assert.Nil(t, got)
}

func TestFromHTTPRequestToCompleteLoginRequestEntity(t *testing.T) {
	var got *entity.CompleteLoginRequest
	req := httptest.NewRequest(http.MethodGet, "/auth/github/callback?code=c&state=s", nil)
	req.AddCookie(&http.Cookie{Name: StateCookie, Value: "s"})
	run(t, "/auth/:provider/callback", req, func(c fiber.Ctx) {
		got = FromHTTPRequestToCompleteLoginRequestEntity(c)
	})
	assert.Equal(t, &entity.CompleteLoginRequest{Provider: "github", Code: "c", State: "s", StateCookie: "s"}, got)

	run(t, "/auth/:provider/callback", httptest.NewRequest(http.MethodGet, "/auth/github/callback?error=access_denied", nil), func(c fiber.Ctx) {
		got = FromHTTPRequestToCompleteLoginRequestEntity(c)
	})
	assert.Nil(t, got)
}

func TestFromHTTPRequestToAuthenticateRequestEntity(t *testing.T) {
	var got *entity.AuthenticateRequest
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "tok"})
	run(t, "/", req, func(c fiber.Ctx) { got = FromHTTPRequestToAuthenticateRequestEntity(c) })
	assert.Equal(t, &entity.AuthenticateRequest{SessionToken: "tok"}, got)

	run(t, "/", httptest.NewRequest(http.MethodGet, "/", nil), func(c fiber.Ctx) { got = FromHTTPRequestToAuthenticateRequestEntity(c) })
	assert.Nil(t, got)
}

func TestFromHTTPRequestToGetCurrentUserRequestEntity(t *testing.T) {
	cases := map[string]struct {
		local any
		want  *entity.GetCurrentUserRequest
	}{
		"ok":      {local: int64(9), want: &entity.GetCurrentUserRequest{UserID: 9}},
		"missing": {},
		"zero":    {local: int64(0)},
		"wrong":   {local: "9"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var got *entity.GetCurrentUserRequest
			run(t, "/", httptest.NewRequest(http.MethodGet, "/", nil), func(c fiber.Ctx) {
				if tc.local != nil {
					c.Locals(LocalUserID, tc.local)
				}
				got = FromHTTPRequestToGetCurrentUserRequestEntity(c)
			})
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestFromGetAuthConfigResponseEntityToHTTPResponse(t *testing.T) {
	assert.JSONEq(t, `{"providers":[{"id":"okta","name":"Okta","icon":"<svg/>"}]}`, string(FromGetAuthConfigResponseEntityToHTTPResponse(
		&entity.GetAuthConfigResponse{Providers: []entity.AuthProvider{{ID: "okta", Name: "Okta", Icon: "<svg/>"}}})))
	assert.JSONEq(t, `{"providers":[]}`, string(FromGetAuthConfigResponseEntityToHTTPResponse(&entity.GetAuthConfigResponse{})))
}

func TestFromErrorToHTTPResponse(t *testing.T) {
	assert.JSONEq(t, `{"error":"nope"}`, string(FromErrorToHTTPResponse("nope")))
}

func TestFromGetCurrentUserResponseEntityToHTTPResponse(t *testing.T) {
	ts := time.Date(2026, 10, 18, 12, 0, 0, 0, time.FixedZone("x", 3600))
	got := FromGetCurrentUserResponseEntityToHTTPResponse(&entity.GetCurrentUserResponse{User: entity.User{
		ID: 42, Email: "a@b.c", Name: "A", Picture: "p", Provider: "google",
		Credits: 10, CreditsLimit: 250, CreditsRenewAt: ts, CreatedAt: ts,
	}})
	assert.JSONEq(t, `{"user":{
		"id":"`+monoflake.ID(42).String()+`","email":"a@b.c","name":"A","picture":"p","provider":"google",
		"credits":10,"creditsLimit":250,"creditsRenewAt":"2026-10-18T11:00:00Z","createdAt":"2026-10-18T11:00:00Z"}}`, string(got))
}
