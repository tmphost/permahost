// Copyright 2026 Contextual, Inc. https://tmphost.io
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
	"github.com/valyala/fasthttp"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
)

// runAs executes fn inside a real fiber request on route, as the given
// user (0: unauthenticated).
func runAs(t *testing.T, userID int64, route string, req *http.Request, fn func(c fiber.Ctx)) {
	t.Helper()
	app := fiber.New()
	app.Add([]string{req.Method}, route, func(c fiber.Ctx) error {
		if userID != 0 {
			c.Locals(LocalUserID, userID)
		}
		fn(c)
		return nil
	})
	_, err := app.Test(req)
	require.NoError(t, err)
}

// runRaw is runAs for a URI net/http refuses to build, such as one with
// an invalid escape.
func runRaw(t *testing.T, userID int64, route, host, uri string, fn func(c fiber.Ctx)) {
	t.Helper()
	app := fiber.New()
	app.Get(route, func(c fiber.Ctx) error {
		if userID != 0 {
			c.Locals(LocalUserID, userID)
		}
		fn(c)
		return nil
	})
	var fctx fasthttp.RequestCtx
	fctx.Request.SetRequestURI(uri)
	fctx.Request.SetHost(host)
	app.Handler()(&fctx)
}

func TestFromHTTPRequestToCreateFolderRequestEntity(t *testing.T) {
	var got *entity.CreateFolderRequest
	read := func(userID int64, body string) *entity.CreateFolderRequest {
		runAs(t, userID, "/folders", httptest.NewRequest(http.MethodPost, "/folders", strings.NewReader(body)), func(c fiber.Ctx) {
			got = FromHTTPRequestToCreateFolderRequestEntity(c)
		})
		return got
	}
	assert.Equal(t, &entity.CreateFolderRequest{UserID: 7, TTLMinutes: 60}, read(7, `{"ttlMinutes":60}`))
	assert.Equal(t, &entity.CreateFolderRequest{UserID: 7}, read(7, "  "), "the body is optional")
	assert.Nil(t, read(7, `{"ttlMinutes":"x"}`))
	assert.Nil(t, read(0, `{}`))

	runAs(t, 3, "/folders", httptest.NewRequest(http.MethodPost, "/folders", nil), func(c fiber.Ctx) {
		c.Locals(LocalAnonymous, true)
		got = FromHTTPRequestToCreateFolderRequestEntity(c)
	})
	assert.Equal(t, &entity.CreateFolderRequest{UserID: 3, Anonymous: true}, got)
}

func TestFromHTTPRequestToGetLimitsRequestEntity(t *testing.T) {
	var got *entity.GetLimitsRequest
	runAs(t, 7, "/limits", httptest.NewRequest(http.MethodGet, "/limits", nil), func(c fiber.Ctx) {
		got = FromHTTPRequestToGetLimitsRequestEntity(c)
	})
	assert.Equal(t, &entity.GetLimitsRequest{}, got)
	runAs(t, 3, "/limits", httptest.NewRequest(http.MethodGet, "/limits", nil), func(c fiber.Ctx) {
		c.Locals(LocalAnonymous, true)
		got = FromHTTPRequestToGetLimitsRequestEntity(c)
	})
	assert.Equal(t, &entity.GetLimitsRequest{Anonymous: true}, got)
}

func TestFromHTTPRequestToAuthenticateAnonymousRequestEntity(t *testing.T) {
	var got *entity.AuthenticateAnonymousRequest
	req := httptest.NewRequest(http.MethodPost, "/folders", nil)
	req.Header.Set("Origin", "https://tmphost.io")
	req.Header.Set("Referer", "https://tmphost.io/upload")
	runAs(t, 0, "/folders", req, func(c fiber.Ctx) { got = FromHTTPRequestToAuthenticateAnonymousRequestEntity(c) })
	assert.Equal(t, &entity.AuthenticateAnonymousRequest{IP: "0.0.0.0", Origin: "https://tmphost.io", Referer: "https://tmphost.io/upload"}, got)
}

func TestFromHTTPRequestToFolderRequestEntities(t *testing.T) {
	var (
		list *entity.ListFoldersRequest
		get  *entity.GetFolderRequest
		del  *entity.DeleteFolderRequest
	)
	runAs(t, 7, "/folders/:id", httptest.NewRequest(http.MethodGet, "/folders/000000000rs", nil), func(c fiber.Ctx) {
		list = FromHTTPRequestToListFoldersRequestEntity(c)
		get = FromHTTPRequestToGetFolderRequestEntity(c)
		del = FromHTTPRequestToDeleteFolderRequestEntity(c)
	})
	assert.Equal(t, &entity.ListFoldersRequest{UserID: 7}, list)
	assert.Equal(t, &entity.GetFolderRequest{UserID: 7, FolderID: 1000}, get)
	assert.Equal(t, &entity.DeleteFolderRequest{UserID: 7, FolderID: 1000}, del)

	runAs(t, 0, "/folders/:id", httptest.NewRequest(http.MethodGet, "/folders/000000000rs", nil), func(c fiber.Ctx) {
		list = FromHTTPRequestToListFoldersRequestEntity(c)
		get = FromHTTPRequestToGetFolderRequestEntity(c)
		del = FromHTTPRequestToDeleteFolderRequestEntity(c)
	})
	assert.Nil(t, list)
	assert.Nil(t, get)
	assert.Nil(t, del)
}

func TestFromHTTPRequestToUpdateFolderRequestEntity(t *testing.T) {
	run := func(userID int64, target, body string) *entity.UpdateFolderRequest {
		var rq *entity.UpdateFolderRequest
		runAs(t, userID, "/folders/:id", httptest.NewRequest(http.MethodPatch, target, strings.NewReader(body)), func(c fiber.Ctx) {
			rq = FromHTTPRequestToUpdateFolderRequestEntity(c)
		})
		return rq
	}
	assert.Equal(t, &entity.UpdateFolderRequest{UserID: 7, FolderID: 1000, Title: "Photos"}, run(7, "/folders/000000000rs", `{"title":"Photos"}`))
	assert.Equal(t, &entity.UpdateFolderRequest{UserID: 7, FolderID: 1000}, run(7, "/folders/000000000rs", `{"title":""}`), "empty clears")
	assert.Nil(t, run(7, "/folders/000000000rs", `{}`), "title is required")
	assert.Nil(t, run(7, "/folders/000000000rs", `{`))
	assert.Nil(t, run(7, "/folders/-", `{"title":"x"}`))
	assert.Nil(t, run(0, "/folders/000000000rs", `{"title":"x"}`))
}

func TestFromHTTPRequestToFileRequestEntities(t *testing.T) {
	var (
		put  *entity.PutFilesRequest
		open *entity.OpenFileRequest
		del  *entity.DeleteFileRequest
	)
	runAs(t, 7, "/folders/:id/files/*", httptest.NewRequest(http.MethodPut, "/folders/000000000rs/files/site/a%20b.txt", strings.NewReader("hi")), func(c fiber.Ctx) {
		put = FromHTTPRequestToPutFilesRequestEntity(c)
		open = FromHTTPRequestToOpenFileRequestEntity(c)
		del = FromHTTPRequestToDeleteFileRequestEntity(c)
	})
	assert.Equal(t, &entity.PutFilesRequest{UserID: 7, FolderID: 1000, Files: []entity.FileUpload{{Path: "site/a b.txt", Data: []byte("hi")}}}, put)
	assert.Equal(t, &entity.OpenFileRequest{UserID: 7, FolderID: 1000, Path: "site/a b.txt"}, open)
	assert.Equal(t, &entity.DeleteFileRequest{UserID: 7, FolderID: 1000, Path: "site/a b.txt"}, del)

	runAs(t, 3, "/folders/:id/files/*", httptest.NewRequest(http.MethodPut, "/folders/000000000rs/files/a", strings.NewReader("hi")), func(c fiber.Ctx) {
		c.Locals(LocalAnonymous, true)
		put = FromHTTPRequestToPutFilesRequestEntity(c)
	})
	assert.Equal(t, &entity.PutFilesRequest{UserID: 3, Anonymous: true, FolderID: 1000, Files: []entity.FileUpload{{Path: "a", Data: []byte("hi")}}}, put)

	for name, tc := range map[string]struct {
		userID int64
		req    *http.Request
	}{
		"no user": {0, httptest.NewRequest(http.MethodPut, "/folders/000000000rs/files/a", nil)},
		"bad id":  {7, httptest.NewRequest(http.MethodPut, "/folders/RS/files/a", nil)},
		"no path": {7, httptest.NewRequest(http.MethodPut, "/folders/000000000rs/files/", nil)},
	} {
		put, del = &entity.PutFilesRequest{}, &entity.DeleteFileRequest{}
		runAs(t, tc.userID, "/folders/:id/files/*", tc.req, func(c fiber.Ctx) {
			put = FromHTTPRequestToPutFilesRequestEntity(c)
			del = FromHTTPRequestToDeleteFileRequestEntity(c)
		})
		assert.Nil(t, put, name)
		assert.Nil(t, del, name)
	}

	put = &entity.PutFilesRequest{}
	runRaw(t, 7, "/folders/:id/files/*", "tmp.host", "/folders/000000000rs/files/a%zz", func(c fiber.Ctx) {
		put = FromHTTPRequestToPutFilesRequestEntity(c)
	})
	assert.Nil(t, put, "bad escape")
}

func TestFromHTTPRequestToOpenPublicFileRequestEntity(t *testing.T) {
	read := func(domain, host string, req *http.Request) *entity.OpenPublicFileRequest {
		var got *entity.OpenPublicFileRequest
		req.Host = host
		runAs(t, 0, "/*", req, func(c fiber.Ctx) {
			got = FromHTTPRequestToOpenPublicFileRequestEntity(c, domain)
		})
		return got
	}
	get := func(target string) *http.Request { return httptest.NewRequest(http.MethodGet, target, nil) }

	assert.Equal(t, &entity.OpenPublicFileRequest{FolderID: 1000, Path: "site/a b.html"},
		read("Tmp.Host:3000", "F000000000RS.tmp.host:3000", get("/site/a%20b.html")))
	assert.Equal(t, &entity.OpenPublicFileRequest{FolderID: 1000, Path: ""}, read("tmp.host", "f000000000rs.tmp.host", get("/")))

	for name, tc := range map[string]struct {
		domain, host string
		req          *http.Request
	}{
		"app host":     {"tmp.host", "tmp.host", get("/")},
		"other domain": {"tmp.host", "f000000000rs.evil.test", get("/")},
		"no domain":    {"", "f000000000rs.", get("/")},
		"no dash":      {"tmp.host", "rs.tmp.host", get("/")},
		"bad folder":   {"tmp.host", "frs.tmp.host", get("/")},
		"no f":         {"tmp.host", "x000000000rs.tmp.host", get("/")},
	} {
		assert.Nil(t, read(tc.domain, tc.host, tc.req), name)
	}

	got := &entity.OpenPublicFileRequest{}
	runRaw(t, 0, "/*", "f000000000rs.tmp.host", "/a%zz", func(c fiber.Ctx) {
		got = FromHTTPRequestToOpenPublicFileRequestEntity(c, "tmp.host")
	})
	assert.Nil(t, got, "bad escape")
}

func TestFromHTTPRequestToOpenPublicFileByPathRequestEntity(t *testing.T) {
	read := func(target string) *entity.OpenPublicFileRequest {
		var got *entity.OpenPublicFileRequest
		runAs(t, 0, "/files/:user/:folder/*", httptest.NewRequest(http.MethodGet, target, nil), func(c fiber.Ctx) {
			got = FromHTTPRequestToOpenPublicFileByPathRequestEntity(c)
		})
		return got
	}
	assert.Equal(t, &entity.OpenPublicFileRequest{UserID: 7, FolderID: 1000, Path: "site/a b.html"}, read("/files/7/000000000rs/site/a%20b.html"))
	assert.Equal(t, &entity.OpenPublicFileRequest{UserID: 7, FolderID: 1000, Path: ""}, read("/files/7/000000000rs/"))
	assert.Equal(t, &entity.OpenPublicFileRequest{UserID: 7, FolderID: 1000, Path: "site/"}, read("/files/7/000000000rs/site/"),
		"a directory keeps its slash, for its index.html")
	assert.Nil(t, read("/files/x_y/000000000rs/a"), "bad user")
	assert.Nil(t, read("/files/7/0/a"), "bad folder")
	assert.Nil(t, read("/files/7/rs/a"), "short folder ID")

	got := &entity.OpenPublicFileRequest{}
	runRaw(t, 0, "/files/:user/:folder/*", "tmp.host", "/files/7/000000000rs/a%zz", func(c fiber.Ctx) {
		got = FromHTTPRequestToOpenPublicFileByPathRequestEntity(c)
	})
	assert.Nil(t, got, "bad escape")
}

func TestFolderResponses(t *testing.T) {
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.FixedZone("x", 3600))
	folder := entity.Folder{ID: 1000, CreatedAt: at, ExpiresAt: at, URL: "u", Title: "Photos", DefaultTitle: "a"}
	file := entity.File{Path: "a", Size: 1, UpdatedAt: at, URL: "u/a"}
	const folderJSON = `{"id":"000000000rs","createdAt":"2026-10-18T11:00:00Z","expiresAt":"2026-10-18T11:00:00Z","url":"u","title":"Photos","defaultTitle":"a"}`
	const fileJSON = `{"path":"a","size":1,"updatedAt":"2026-10-18T11:00:00Z","url":"u/a"}`

	assert.JSONEq(t, `{"defaultTtlMinutes":30,"maxTtlMinutes":60,"maxUploadBytes":5,"billingBlockMinutes":30,"billingUnitBytes":1048576}`,
		string(FromGetLimitsResponseEntityToHTTPResponse(&entity.GetLimitsResponse{DefaultTTLMinutes: 30, MaxTTLMinutes: 60, MaxUploadBytes: 5, BillingBlockMinutes: 30, BillingUnitBytes: 1 << 20})))
	assert.JSONEq(t, `{"defaultTtlMinutes":30,"maxTtlMinutes":30,"maxUploadBytes":5,"billingBlockMinutes":30,"billingUnitBytes":1048576,"anonymous":true,"fileOps":5,"fileOpsWindowMinutes":30}`,
		string(FromGetLimitsResponseEntityToHTTPResponse(&entity.GetLimitsResponse{DefaultTTLMinutes: 30, MaxTTLMinutes: 30, MaxUploadBytes: 5, BillingBlockMinutes: 30, BillingUnitBytes: 1 << 20, Anonymous: true, FileOps: 5, FileOpsWindowMinutes: 30})))
	assert.JSONEq(t, `{"folder":`+folderJSON+`}`, string(FromCreateFolderResponseEntityToHTTPResponse(&entity.CreateFolderResponse{Folder: folder})))
	assert.JSONEq(t, `{"folder":`+folderJSON+`}`, string(FromUpdateFolderResponseEntityToHTTPResponse(&entity.UpdateFolderResponse{Folder: folder})))
	assert.JSONEq(t, `{"folders":[`+folderJSON+`]}`, string(FromListFoldersResponseEntityToHTTPResponse(&entity.ListFoldersResponse{Folders: []entity.Folder{folder}})))
	assert.JSONEq(t, `{"folders":[]}`, string(FromListFoldersResponseEntityToHTTPResponse(&entity.ListFoldersResponse{})))
	assert.JSONEq(t, `{"folder":`+folderJSON+`,"files":[`+fileJSON+`]}`,
		string(FromGetFolderResponseEntityToHTTPResponse(&entity.GetFolderResponse{Folder: folder, Files: []entity.File{file}})))
	assert.JSONEq(t, `{"file":`+fileJSON+`,"creditsSpent":2}`,
		string(FromPutFilesResponseEntityToHTTPResponse(&entity.PutFilesResponse{Files: []entity.File{file}, CreditsSpent: 2})))
}

func TestFromHTTPRequestToAuthenticateRequestEntity_Bearer(t *testing.T) {
	read := func(auth string) *entity.AuthenticateRequest {
		var got *entity.AuthenticateRequest
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", auth)
		req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "cookie"})
		runAs(t, 0, "/", req, func(c fiber.Ctx) { got = FromHTTPRequestToAuthenticateRequestEntity(c) })
		return got
	}
	assert.Equal(t, &entity.AuthenticateRequest{AccessToken: "tok"}, read("Bearer tok"))
	assert.Equal(t, &entity.AuthenticateRequest{AccessToken: "tok"}, read("bearer tok"))
	assert.Equal(t, &entity.AuthenticateRequest{SessionToken: "cookie"}, read("Basic abc"), "other schemes fall back to the cookie")
	assert.Equal(t, &entity.AuthenticateRequest{SessionToken: "cookie"}, read("Bearer "))
}
