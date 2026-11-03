// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	adminctrl "github.com/tmphost/permahost/backend/internal/controller/admin"
	mockadmin "github.com/tmphost/permahost/backend/internal/controller/admin/mocks"
	mocksetup "github.com/tmphost/permahost/backend/internal/controller/setup/mocks"
	"github.com/tmphost/permahost/backend/internal/data/entity"
	mapper "github.com/tmphost/permahost/backend/internal/mapper/api"
)

// newAdminApp is the API with a signed-in root and a mock admin controller.
func newAdminApp(t *testing.T) (*fiber.App, *mockadmin.MockController) {
	t.Helper()
	mc := gomock.NewController(t)
	setup := mocksetup.NewMockController(mc)
	setup.EXPECT().Authenticate(gomock.Any(), &entity.AuthenticateRootRequest{SessionToken: "root-session"}).Return(nil).AnyTimes()
	admin := mockadmin.NewMockController(mc)
	app := fiber.New()
	New(Params{Router: app, Setup: setup, Admin: admin, FileSettings: func() entity.FileSettings { return entity.FileSettings{} }, CookieSecure: true})
	return app, admin
}

func TestAdmin_RootOnly(t *testing.T) {
	app, _ := newAdminApp(t)
	id := mapper.FromIDToBase62(42)
	for _, r := range []struct{ method, target string }{
		{http.MethodGet, "/api/v1/setup/stats"},
		{http.MethodGet, "/api/v1/setup/users"},
		{http.MethodGet, "/api/v1/setup/users/" + id},
		{http.MethodGet, "/api/v1/setup/users/" + id + "/credits"},
		{http.MethodPost, "/api/v1/setup/users/" + id + "/credits"},
	} {
		res, _ := send(t, app, r.method, r.target, `{"amount":1,"note":"n"}`)
		assert.Equal(t, http.StatusUnauthorized, res.StatusCode, r.target)
	}
}

func TestAdmin_GetStats(t *testing.T) {
	app, admin := newAdminApp(t)
	admin.EXPECT().GetStats(gomock.Any(), &entity.GetAdminStatsRequest{}).Return(&entity.GetAdminStatsResponse{Users: 3}, nil)
	res, body := send(t, app, http.MethodGet, "/api/v1/setup/stats", "", _rootCookie)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Contains(t, body, `"users":3`)

	admin.EXPECT().GetStats(gomock.Any(), gomock.Any()).Return(nil, errBoom)
	res, body = send(t, app, http.MethodGet, "/api/v1/setup/stats", "", _rootCookie)
	assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
	assert.JSONEq(t, `{"error":"failed to load stats"}`, body)
}

func TestAdmin_GetUser(t *testing.T) {
	app, admin := newAdminApp(t)
	target := "/api/v1/setup/users/" + mapper.FromIDToBase62(42)
	admin.EXPECT().GetUser(gomock.Any(), &entity.GetAdminUserRequest{UserID: 42}).
		Return(&entity.GetAdminUserResponse{User: entity.AdminUser{User: entity.User{ID: 42, Email: "a@x"}}}, nil)
	res, body := send(t, app, http.MethodGet, target, "", _rootCookie)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Contains(t, body, `"email":"a@x"`)

	res, _ = send(t, app, http.MethodGet, "/api/v1/setup/users/!!", "", _rootCookie)
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)

	admin.EXPECT().GetUser(gomock.Any(), gomock.Any()).Return(nil, adminctrl.ErrUserNotFound)
	res, _ = send(t, app, http.MethodGet, target, "", _rootCookie)
	assert.Equal(t, http.StatusNotFound, res.StatusCode)

	admin.EXPECT().GetUser(gomock.Any(), gomock.Any()).Return(nil, errBoom)
	res, body = send(t, app, http.MethodGet, target, "", _rootCookie)
	assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
	assert.JSONEq(t, `{"error":"failed to load user"}`, body)
}

func TestAdmin_ListUsers(t *testing.T) {
	app, admin := newAdminApp(t)
	admin.EXPECT().ListUsers(gomock.Any(), &entity.ListAdminUsersRequest{Query: "ada", Limit: 5}).
		Return(&entity.ListAdminUsersResponse{Users: []entity.AdminUser{{User: entity.User{ID: 42, Email: "a@x"}}}}, nil)
	res, body := send(t, app, http.MethodGet, "/api/v1/setup/users?q=ada&limit=5", "", _rootCookie)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, fiber.MIMEApplicationJSON, res.Header.Get(fiber.HeaderContentType))
	assert.Contains(t, body, `"email":"a@x"`)
	assert.Contains(t, body, `"next":""`)

	res, _ = send(t, app, http.MethodGet, "/api/v1/setup/users?limit=x", "", _rootCookie)
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)

	admin.EXPECT().ListUsers(gomock.Any(), gomock.Any()).Return(nil, errBoom)
	res, body = send(t, app, http.MethodGet, "/api/v1/setup/users", "", _rootCookie)
	assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
	assert.JSONEq(t, `{"error":"failed to load users"}`, body)
}

func TestAdmin_AdjustCredits(t *testing.T) {
	app, admin := newAdminApp(t)
	id := mapper.FromIDToBase62(42)
	target := "/api/v1/setup/users/" + id + "/credits"
	at := time.Date(2026, 11, 2, 7, 0, 0, 0, time.UTC)

	admin.EXPECT().AdjustCredits(gomock.Any(), &entity.AdjustCreditsRequest{UserID: 42, Amount: 500, Note: "support"}).
		Return(&entity.AdjustCreditsResponse{
			User:     entity.AdminUser{User: entity.User{ID: 42, Credits: 400}},
			Activity: entity.CreditActivity{ID: 7, UserID: 42, Ops: "admin_grant", Actor: "root", Change: 500, Note: "support", Current: 400, CreatedAt: at},
		}, nil)
	res, body := send(t, app, http.MethodPost, target, `{"amount":500,"note":"support"}`, _rootCookie)
	assert.Equal(t, http.StatusCreated, res.StatusCode)
	assert.Contains(t, body, `"current":400`)
	assert.Contains(t, body, `"paused":false`)

	res, _ = send(t, app, http.MethodPost, target, `{`, _rootCookie)
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)

	for _, tc := range []struct {
		err    error
		status int
		body   string
	}{
		{adminctrl.ErrUserNotFound, http.StatusNotFound, `{"error":"user not found"}`},
		{fmt.Errorf("%w: a note is required", adminctrl.ErrInvalidAdjustment), http.StatusBadRequest, `{"error":"invalid adjustment: a note is required"}`},
		{adminctrl.ErrInsufficientCredits, http.StatusConflict, `{"error":"the deduction would take the balance below zero"}`},
		{errBoom, http.StatusInternalServerError, `{"error":"failed to adjust credits"}`},
	} {
		admin.EXPECT().AdjustCredits(gomock.Any(), gomock.Any()).Return(nil, tc.err)
		res, body = send(t, app, http.MethodPost, target, `{"amount":-1,"note":"n"}`, _rootCookie)
		assert.Equal(t, tc.status, res.StatusCode)
		assert.JSONEq(t, tc.body, body)
	}
}

func TestAdmin_ListCreditActivities(t *testing.T) {
	app, admin := newAdminApp(t)
	target := "/api/v1/setup/users/" + mapper.FromIDToBase62(42) + "/credits"

	admin.EXPECT().ListCreditActivities(gomock.Any(), &entity.ListCreditActivitiesRequest{UserID: 42}).
		Return(&entity.ListCreditActivitiesResponse{Activities: []entity.CreditActivity{}}, nil)
	res, body := send(t, app, http.MethodGet, target, "", _rootCookie)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.JSONEq(t, `{"activities":[],"next":""}`, body)

	res, _ = send(t, app, http.MethodGet, "/api/v1/setup/users/!!/credits", "", _rootCookie)
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)

	admin.EXPECT().ListCreditActivities(gomock.Any(), gomock.Any()).Return(nil, adminctrl.ErrUserNotFound)
	res, _ = send(t, app, http.MethodGet, target, "", _rootCookie)
	assert.Equal(t, http.StatusNotFound, res.StatusCode)

	admin.EXPECT().ListCreditActivities(gomock.Any(), gomock.Any()).Return(nil, errBoom)
	res, body = send(t, app, http.MethodGet, target, "", _rootCookie)
	assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
	assert.JSONEq(t, `{"error":"failed to load credit history"}`, body)
}
