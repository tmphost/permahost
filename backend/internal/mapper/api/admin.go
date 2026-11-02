// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/mustafaturan/monoflake"

	"github.com/tmphost/permahost/backend/internal/data/entity"
	"github.com/tmphost/permahost/backend/internal/data/view"
)

// FromHTTPRequestToListAdminUsersRequestEntity reads GET
// /setup/users?q=&before=&limit=.
func FromHTTPRequestToListAdminUsersRequestEntity(c fiber.Ctx) *entity.ListAdminUsersRequest {
	before, limit, ok := pageQuery(c)
	if !ok {
		return nil
	}
	return &entity.ListAdminUsersRequest{Query: c.Query("q"), Before: before, Limit: limit}
}

// FromHTTPRequestToAdjustCreditsRequestEntity reads POST
// /setup/users/:id/credits.
func FromHTTPRequestToAdjustCreditsRequestEntity(c fiber.Ctx) *entity.AdjustCreditsRequest {
	id := paramID(c, "id")
	var body view.AdjustCreditsRequest
	if id == 0 || json.Unmarshal(c.Body(), &body) != nil {
		return nil
	}
	return &entity.AdjustCreditsRequest{UserID: id, Amount: body.Amount, Note: body.Note}
}

// FromHTTPRequestToGetAdminUserRequestEntity reads GET /setup/users/:id.
func FromHTTPRequestToGetAdminUserRequestEntity(c fiber.Ctx) *entity.GetAdminUserRequest {
	id := paramID(c, "id")
	if id == 0 {
		return nil
	}
	return &entity.GetAdminUserRequest{UserID: id}
}

// FromHTTPRequestToListCreditActivitiesRequestEntity reads GET
// /setup/users/:id/credits?before=&limit=.
func FromHTTPRequestToListCreditActivitiesRequestEntity(c fiber.Ctx) *entity.ListCreditActivitiesRequest {
	id := paramID(c, "id")
	before, limit, ok := pageQuery(c)
	if id == 0 || !ok {
		return nil
	}
	return &entity.ListCreditActivitiesRequest{UserID: id, Before: before, Limit: limit}
}

// FromGetAdminStatsResponseEntityToHTTPResponse renders GET /setup/stats.
func FromGetAdminStatsResponseEntityToHTTPResponse(rs *entity.GetAdminStatsResponse) []byte {
	days := make([]view.AdminStatsDay, len(rs.Days))
	for i, d := range rs.Days {
		days[i] = view.AdminStatsDay{Date: d.Date, Used: d.Used, Signups: d.Signups}
	}
	return marshal(view.AdminStats{
		Users: rs.Users, Credits: rs.Credits, Paused: rs.Paused, Used: rs.Used,
		Signups: rs.Signups, Folders: rs.Folders, Days: days,
	})
}

// FromListAdminUsersResponseEntityToHTTPResponse renders GET /setup/users.
func FromListAdminUsersResponseEntityToHTTPResponse(rs *entity.ListAdminUsersResponse) []byte {
	users := make([]view.AdminUser, len(rs.Users))
	for i, u := range rs.Users {
		users[i] = FromAdminUserEntityToAdminUserView(u)
	}
	return marshal(view.ListAdminUsersResponse{Users: users, Next: cursor(rs.Next)})
}

// FromGetAdminUserResponseEntityToHTTPResponse renders GET /setup/users/:id.
func FromGetAdminUserResponseEntityToHTTPResponse(rs *entity.GetAdminUserResponse) []byte {
	return marshal(view.GetAdminUserResponse{User: FromAdminUserEntityToAdminUserView(rs.User)})
}

// FromAdjustCreditsResponseEntityToHTTPResponse renders POST
// /setup/users/:id/credits.
func FromAdjustCreditsResponseEntityToHTTPResponse(rs *entity.AdjustCreditsResponse) []byte {
	return marshal(view.AdjustCreditsResponse{
		User:     FromAdminUserEntityToAdminUserView(rs.User),
		Activity: FromCreditActivityEntityToCreditActivityView(rs.Activity),
	})
}

// FromListCreditActivitiesResponseEntityToHTTPResponse renders GET
// /setup/users/:id/credits.
func FromListCreditActivitiesResponseEntityToHTTPResponse(rs *entity.ListCreditActivitiesResponse) []byte {
	as := make([]view.CreditActivity, len(rs.Activities))
	for i, a := range rs.Activities {
		as[i] = FromCreditActivityEntityToCreditActivityView(a)
	}
	return marshal(view.ListCreditActivitiesResponse{Activities: as, Next: cursor(rs.Next)})
}

// FromAdminUserEntityToAdminUserView maps a user as root sees it.
func FromAdminUserEntityToAdminUserView(e entity.AdminUser) view.AdminUser {
	return view.AdminUser{
		User:   FromUserEntityToUserView(e.User),
		Paused: e.User.Credits < 0,
		Used:   e.Used,
		Usage: view.AdminUsage{
			Folders:        e.Usage.Folders,
			StorageBytes:   e.Usage.StorageBytes,
			StorageBlocks:  e.StorageBlocks,
			Domains:        e.Usage.Domains,
			MonthlyCredits: e.MonthlyCredits,
		},
	}
}

// FromCreditActivityEntityToCreditActivityView maps a credit activity.
func FromCreditActivityEntityToCreditActivityView(e entity.CreditActivity) view.CreditActivity {
	return view.CreditActivity{
		ID:        FromIDToBase62(e.ID),
		UserID:    FromIDToBase62(e.UserID),
		Ops:       e.Ops,
		Change:    e.Change,
		Current:   e.Current,
		Actor:     e.Actor,
		Note:      e.Note,
		CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// paramID is the positive base62 ID in route parameter name, else 0.
func paramID(c fiber.Ctx, name string) int64 {
	return max(monoflake.IDFromBase62(c.Params(name)).Int64(), 0)
}

// pageQuery reads the before cursor (a base62 ID) and the page size of a
// list request; both are optional. ok is false when either is malformed.
func pageQuery(c fiber.Ctx) (before int64, limit int, ok bool) {
	if s := c.Query("before"); s != "" {
		if before = monoflake.IDFromBase62(s).Int64(); before <= 0 {
			return 0, 0, false
		}
	}
	if s := c.Query("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return 0, 0, false
		}
		limit = n
	}
	return before, limit, true
}

// cursor renders a next-page cursor; 0 (no next page) is empty.
func cursor(id int64) string {
	if id == 0 {
		return ""
	}
	return FromIDToBase62(id)
}
