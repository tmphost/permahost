// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/permahost/backend/internal/data/entity"
)

func TestFromHTTPRequestToListAdminUsersRequestEntity(t *testing.T) {
	read := func(target string) *entity.ListAdminUsersRequest {
		var got *entity.ListAdminUsersRequest
		run(t, "/setup/users", httptest.NewRequest(http.MethodGet, target, nil), func(c fiber.Ctx) {
			got = FromHTTPRequestToListAdminUsersRequestEntity(c)
		})
		return got
	}
	before := FromIDToBase62(1234)
	assert.Equal(t, &entity.ListAdminUsersRequest{}, read("/setup/users"))
	assert.Equal(t, &entity.ListAdminUsersRequest{Query: "ada x", Before: 1234, Limit: 5}, read("/setup/users?q=ada+x&before="+before+"&limit=5"))
	assert.Nil(t, read("/setup/users?before=!!"), "bad cursor")
	assert.Nil(t, read("/setup/users?limit=x"), "bad limit")
	assert.Nil(t, read("/setup/users?limit=-1"), "negative limit")
}

func TestFromHTTPRequestToAdjustCreditsRequestEntity(t *testing.T) {
	read := func(id, body string) *entity.AdjustCreditsRequest {
		var got *entity.AdjustCreditsRequest
		req := httptest.NewRequest(http.MethodGet, "/setup/users/"+id+"/credits", strings.NewReader(body))
		run(t, "/setup/users/:id/credits", req, func(c fiber.Ctx) {
			got = FromHTTPRequestToAdjustCreditsRequestEntity(c)
		})
		return got
	}
	id := FromIDToBase62(42)
	assert.Equal(t, &entity.AdjustCreditsRequest{UserID: 42, Amount: -5, Note: "n"}, read(id, `{"amount":-5,"note":"n"}`))
	assert.Equal(t, &entity.AdjustCreditsRequest{UserID: 42}, read(id, `{}`), "the controller validates")
	assert.Nil(t, read("!!", `{"amount":1,"note":"n"}`), "bad ID")
	assert.Nil(t, read(id, `{`), "bad JSON")
	assert.Nil(t, read(id, `{"amount":"1"}`), "amount is a number")
}

func TestFromHTTPRequestToGetAdminUserRequestEntity(t *testing.T) {
	read := func(id string) *entity.GetAdminUserRequest {
		var got *entity.GetAdminUserRequest
		run(t, "/setup/users/:id", httptest.NewRequest(http.MethodGet, "/setup/users/"+id, nil), func(c fiber.Ctx) {
			got = FromHTTPRequestToGetAdminUserRequestEntity(c)
		})
		return got
	}
	assert.Equal(t, &entity.GetAdminUserRequest{UserID: 42}, read(FromIDToBase62(42)))
	assert.Nil(t, read("!!"))
}

func TestFromHTTPRequestToListCreditActivitiesRequestEntity(t *testing.T) {
	read := func(target string) *entity.ListCreditActivitiesRequest {
		var got *entity.ListCreditActivitiesRequest
		run(t, "/setup/users/:id/credits", httptest.NewRequest(http.MethodGet, target, nil), func(c fiber.Ctx) {
			got = FromHTTPRequestToListCreditActivitiesRequestEntity(c)
		})
		return got
	}
	id, before := FromIDToBase62(42), FromIDToBase62(99)
	assert.Equal(t, &entity.ListCreditActivitiesRequest{UserID: 42}, read("/setup/users/"+id+"/credits"))
	assert.Equal(t, &entity.ListCreditActivitiesRequest{UserID: 42, Before: 99, Limit: 10}, read("/setup/users/"+id+"/credits?before="+before+"&limit=10"))
	assert.Nil(t, read("/setup/users/!!/credits"), "bad ID")
	assert.Nil(t, read("/setup/users/"+id+"/credits?limit=no"), "bad limit")
}

func TestAdminResponses(t *testing.T) {
	at := time.Date(2026, 11, 2, 7, 0, 0, 0, time.UTC)
	user := entity.AdminUser{
		User:           entity.User{ID: 42, Email: "a@x", Name: "A", Credits: -100, CreditsLimit: 2000, CreditsRenewAt: at, CreatedAt: at},
		Usage:          entity.Usage{Folders: 2, StorageBytes: 30 << 20, Domains: 1},
		StorageBlocks:  2,
		MonthlyCredits: 2500,
		Used:           1500,
	}
	userJSON := `{"id":"` + FromIDToBase62(42) + `","email":"a@x","name":"A","picture":"","provider":"","credits":-100,"creditsLimit":2000,
		"creditsRenewAt":"2026-11-02T07:00:00Z","createdAt":"2026-11-02T07:00:00Z","paused":true,"used":1500,
		"usage":{"folders":2,"storageBytes":31457280,"storageBlocks":2,"domains":1,"monthlyCredits":2500}}`
	act := entity.CreditActivity{ID: 7, UserID: 42, Ops: "admin_grant", Change: 300, Current: 200, Actor: "root", Note: "n", CreatedAt: at}
	actJSON := `{"id":"` + FromIDToBase62(7) + `","userId":"` + FromIDToBase62(42) + `","ops":"admin_grant","change":300,"current":200,"actor":"root","note":"n","createdAt":"2026-11-02T07:00:00Z"}`

	assert.JSONEq(t, `{"users":3,"credits":900,"paused":1,"used":500,"signups":2,"folders":4,"days":[{"date":"2026-11-02","used":500,"signups":2}]}`,
		string(FromGetAdminStatsResponseEntityToHTTPResponse(&entity.GetAdminStatsResponse{
			Users: 3, Credits: 900, Paused: 1, Used: 500, Signups: 2, Folders: 4,
			Days: []entity.AdminStatsDay{{Date: "2026-11-02", Used: 500, Signups: 2}},
		})))

	assert.JSONEq(t, `{"users":[`+userJSON+`],"next":"`+FromIDToBase62(42)+`"}`,
		string(FromListAdminUsersResponseEntityToHTTPResponse(&entity.ListAdminUsersResponse{Users: []entity.AdminUser{user}, Next: 42})))
	assert.JSONEq(t, `{"users":[],"next":""}`, string(FromListAdminUsersResponseEntityToHTTPResponse(&entity.ListAdminUsersResponse{})))

	assert.JSONEq(t, `{"user":`+userJSON+`}`, string(FromGetAdminUserResponseEntityToHTTPResponse(&entity.GetAdminUserResponse{User: user})))

	assert.JSONEq(t, `{"user":`+userJSON+`,"activity":`+actJSON+`}`,
		string(FromAdjustCreditsResponseEntityToHTTPResponse(&entity.AdjustCreditsResponse{User: user, Activity: act})))

	assert.JSONEq(t, `{"activities":[`+actJSON+`],"next":""}`,
		string(FromListCreditActivitiesResponseEntityToHTTPResponse(&entity.ListCreditActivitiesResponse{Activities: []entity.CreditActivity{act}})))

	user.User.Credits = 0
	assert.False(t, FromAdminUserEntityToAdminUserView(user).Paused, "zero is paid for")
	require.Equal(t, "", cursor(0))
}
