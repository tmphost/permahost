// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mockauth "github.com/tmphost/tmphost/backend/internal/controller/auth/mocks"
	folderctrl "github.com/tmphost/tmphost/backend/internal/controller/folder"
	mockfolder "github.com/tmphost/tmphost/backend/internal/controller/folder/mocks"
	mockoauth "github.com/tmphost/tmphost/backend/internal/controller/oauthserver/mocks"
	mocksetup "github.com/tmphost/tmphost/backend/internal/controller/setup/mocks"
	"github.com/tmphost/tmphost/backend/internal/data/entity"
)

const _bearer = "Bearer access"

var _at = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

type filesEnv struct {
	app      *fiber.App
	auth     *mockauth.MockController
	folders  *mockfolder.MockController
	oauth    *mockoauth.MockController
	settings entity.FileSettings
}

// newFilesApp is the API with a request authenticated as user 7 whenever
// it carries _bearer.
func newFilesApp(t *testing.T) *filesEnv {
	t.Helper()
	mc := gomock.NewController(t)
	auth := mockauth.NewMockController(mc)
	auth.EXPECT().Authenticate(gomock.Any(), &entity.AuthenticateRequest{AccessToken: "access"}).Return(&entity.AuthenticateResponse{UserID: 7}, nil).AnyTimes()
	e := &filesEnv{
		app: fiber.New(), auth: auth, folders: mockfolder.NewMockController(mc), oauth: mockoauth.NewMockController(mc),
		settings: entity.FileSettings{Subdomains: true, PublicDomain: "tmp.host"},
	}
	New(Params{
		Router: e.app, Auth: auth, Setup: mocksetup.NewMockController(mc), Folders: e.folders, OAuth: e.oauth,
		FileSettings: func() entity.FileSettings { return e.settings }, BasePath: "/base",
		// Only tmp.host and httptest's example.com pass the host check.
		Hosts: func(c fiber.Ctx) error {
			if host := string(c.Request().Host()); host != "tmp.host" && host != "example.com" {
				return c.SendStatus(fiber.StatusMisdirectedRequest)
			}
			return c.Next()
		},
	})
	return e
}

func (e *filesEnv) do(t *testing.T, method, target, body string) (*http.Response, string) {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Authorization", _bearer)
	res, err := e.app.Test(req)
	require.NoError(t, err)
	b, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return res, string(b)
}

func TestFolder_Limits(t *testing.T) {
	e := newFilesApp(t)
	e.folders.EXPECT().GetLimits(gomock.Any(), &entity.GetLimitsRequest{}).Return(&entity.GetLimitsResponse{DefaultTTLMinutes: 30, MaxTTLMinutes: 60, MaxUploadBytes: 1024, BillingBlockMinutes: 30, BillingUnitBytes: 1 << 20})
	res, body := e.do(t, http.MethodGet, "/api/v1/limits", "")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.JSONEq(t, `{"defaultTtlMinutes":30,"maxTtlMinutes":60,"maxUploadBytes":1024,"billingBlockMinutes":30,"billingUnitBytes":1048576}`, body)
}

// anonymous is a request without credentials from the web app at
// https://tmphost.io, which the auth mock lets in as anonymous user 3.
func (e *filesEnv) anonymous(t *testing.T, method, target, body string) (*http.Response, string) {
	t.Helper()
	e.auth.EXPECT().AuthenticateAnonymous(gomock.Any(), &entity.AuthenticateAnonymousRequest{IP: "0.0.0.0", Origin: "https://tmphost.io"}).
		Return(&entity.AuthenticateResponse{UserID: 3, Anonymous: true}, nil)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Origin", "https://tmphost.io")
	res, err := e.app.Test(req)
	require.NoError(t, err)
	b, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return res, string(b)
}

func TestFolder_Anonymous(t *testing.T) {
	e := newFilesApp(t)
	e.folders.EXPECT().GetLimits(gomock.Any(), &entity.GetLimitsRequest{Anonymous: true}).
		Return(&entity.GetLimitsResponse{DefaultTTLMinutes: 30, MaxTTLMinutes: 30, MaxUploadBytes: 4 << 20, BillingBlockMinutes: 30, BillingUnitBytes: 1 << 20, Anonymous: true, FileOps: 5, FileOpsWindowMinutes: 30})
	res, body := e.anonymous(t, http.MethodGet, "/api/v1/limits", "")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.JSONEq(t, `{"defaultTtlMinutes":30,"maxTtlMinutes":30,"maxUploadBytes":4194304,"billingBlockMinutes":30,"billingUnitBytes":1048576,"anonymous":true,"fileOps":5,"fileOpsWindowMinutes":30}`, body)

	e.folders.EXPECT().CreateFolder(gomock.Any(), &entity.CreateFolderRequest{UserID: 3, Anonymous: true}).
		Return(&entity.CreateFolderResponse{Folder: entity.Folder{ID: 1000, CreatedAt: _at, ExpiresAt: _at, URL: "u"}}, nil)
	res, _ = e.anonymous(t, http.MethodPost, "/api/v1/folders", "")
	assert.Equal(t, http.StatusCreated, res.StatusCode)

	e.folders.EXPECT().PutFiles(gomock.Any(), &entity.PutFilesRequest{UserID: 3, Anonymous: true, FolderID: 1000, Files: []entity.FileUpload{{Path: "a", Data: []byte("hi")}}}).
		Return(&entity.PutFilesResponse{Files: []entity.File{{Path: "a", Size: 2, UpdatedAt: _at, URL: "u/a"}}}, nil)
	res, _ = e.anonymous(t, http.MethodPut, "/api/v1/folders/000000000rs/files/a", "hi")
	assert.Equal(t, http.StatusOK, res.StatusCode)

	for _, route := range []struct{ method, target string }{
		{http.MethodGet, "/api/v1/folders"},
		{http.MethodGet, "/api/v1/folders/000000000rs"},
		{http.MethodDelete, "/api/v1/folders/000000000rs"},
		{http.MethodGet, "/api/v1/folders/000000000rs/files/a"},
		{http.MethodDelete, "/api/v1/folders/000000000rs/files/a"},
		{http.MethodGet, "/api/v1/users/me"},
	} {
		req := httptest.NewRequest(route.method, route.target, nil)
		req.Header.Set("Origin", "https://tmphost.io")
		res, err := e.app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusUnauthorized, res.StatusCode, "%s %s needs a sign-in", route.method, route.target)
	}
}

func TestFolder_AnonymousRejected(t *testing.T) {
	e := newFilesApp(t)
	e.auth.EXPECT().AuthenticateAnonymous(gomock.Any(), &entity.AuthenticateAnonymousRequest{IP: "0.0.0.0"}).Return(nil, errBoom)
	res, err := e.app.Test(httptest.NewRequest(http.MethodPost, "/api/v1/folders", nil))
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode, "not from the web app")

	e.auth.EXPECT().Authenticate(gomock.Any(), &entity.AuthenticateRequest{AccessToken: "bad"}).Return(nil, errBoom)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/folders", nil)
	req.Header.Set("Authorization", "Bearer bad")
	req.Header.Set("Origin", "https://tmphost.io")
	res, err = e.app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode, "a bad access token never falls back to anonymous")
}

func TestFolder_AnonymousWithExpiredSession(t *testing.T) {
	e := newFilesApp(t)
	e.auth.EXPECT().Authenticate(gomock.Any(), &entity.AuthenticateRequest{SessionToken: "expired"}).Return(nil, errBoom)
	e.auth.EXPECT().AuthenticateAnonymous(gomock.Any(), &entity.AuthenticateAnonymousRequest{IP: "0.0.0.0", Origin: "https://tmphost.io"}).
		Return(&entity.AuthenticateResponse{UserID: 3, Anonymous: true}, nil)
	e.folders.EXPECT().GetLimits(gomock.Any(), &entity.GetLimitsRequest{Anonymous: true}).Return(&entity.GetLimitsResponse{Anonymous: true})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/limits", nil)
	req.AddCookie(&http.Cookie{Name: "tmphost_session", Value: "expired"})
	req.Header.Set("Origin", "https://tmphost.io")
	res, err := e.app.Test(req)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, res.StatusCode, "a browser whose session expired is anonymous")
}

func TestFolder_RequiresAuth(t *testing.T) {
	e := newFilesApp(t)
	res, err := e.app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/folders", nil))
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode)
}

func TestFolder_Create(t *testing.T) {
	e := newFilesApp(t)
	e.folders.EXPECT().CreateFolder(gomock.Any(), &entity.CreateFolderRequest{UserID: 7, TTLMinutes: 60}).
		Return(&entity.CreateFolderResponse{Folder: entity.Folder{ID: 1000, CreatedAt: _at, ExpiresAt: _at, URL: "u"}}, nil)
	res, body := e.do(t, http.MethodPost, "/api/v1/folders", `{"ttlMinutes":60}`)
	assert.Equal(t, http.StatusCreated, res.StatusCode)
	assert.JSONEq(t, `{"folder":{"id":"000000000rs","createdAt":"2026-10-18T12:00:00Z","expiresAt":"2026-10-18T12:00:00Z","url":"u","title":"","defaultTitle":""}}`, body)

	res, _ = e.do(t, http.MethodPost, "/api/v1/folders", `{`)
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)

	e.folders.EXPECT().CreateFolder(gomock.Any(), &entity.CreateFolderRequest{UserID: 7}).Return(nil, folderctrl.ErrInvalidTTL)
	res, body = e.do(t, http.MethodPost, "/api/v1/folders", "")
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	assert.JSONEq(t, `{"error":"invalid ttl"}`, body)
}

func TestFolder_ListGetDelete(t *testing.T) {
	e := newFilesApp(t)
	folder := entity.Folder{ID: 1000, CreatedAt: _at, ExpiresAt: _at, URL: "u"}
	e.folders.EXPECT().ListFolders(gomock.Any(), &entity.ListFoldersRequest{UserID: 7}).Return(&entity.ListFoldersResponse{Folders: []entity.Folder{folder}}, nil)
	res, body := e.do(t, http.MethodGet, "/api/v1/folders", "")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Contains(t, body, `"folders":[{"id":"000000000rs"`)

	e.folders.EXPECT().ListFolders(gomock.Any(), gomock.Any()).Return(nil, errBoom)
	res, _ = e.do(t, http.MethodGet, "/api/v1/folders", "")
	assert.Equal(t, http.StatusInternalServerError, res.StatusCode)

	e.folders.EXPECT().GetFolder(gomock.Any(), &entity.GetFolderRequest{UserID: 7, FolderID: 1000}).Return(&entity.GetFolderResponse{
		Folder: folder, Files: []entity.File{{Path: "a.txt", Size: 1, UpdatedAt: _at, URL: "u/a.txt"}},
	}, nil)
	res, body = e.do(t, http.MethodGet, "/api/v1/folders/000000000rs", "")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Contains(t, body, `"files":[{"path":"a.txt","size":1,"updatedAt":"2026-10-18T12:00:00Z","url":"u/a.txt"}]`)

	res, _ = e.do(t, http.MethodGet, "/api/v1/folders/RS", "")
	assert.Equal(t, http.StatusBadRequest, res.StatusCode, "ids are lowercase base36")

	e.folders.EXPECT().GetFolder(gomock.Any(), gomock.Any()).Return(nil, folderctrl.ErrNotFound)
	res, body = e.do(t, http.MethodGet, "/api/v1/folders/000000000rs", "")
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
	assert.JSONEq(t, `{"error":"not found"}`, body)

	e.folders.EXPECT().DeleteFolder(gomock.Any(), &entity.DeleteFolderRequest{UserID: 7, FolderID: 1000}).Return(nil)
	res, _ = e.do(t, http.MethodDelete, "/api/v1/folders/000000000rs", "")
	assert.Equal(t, http.StatusNoContent, res.StatusCode)

	res, _ = e.do(t, http.MethodDelete, "/api/v1/folders/-", "")
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)

	e.folders.EXPECT().DeleteFolder(gomock.Any(), gomock.Any()).Return(errBoom)
	res, _ = e.do(t, http.MethodDelete, "/api/v1/folders/000000000rs", "")
	assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
}

func TestFolder_Update(t *testing.T) {
	e := newFilesApp(t)
	e.folders.EXPECT().UpdateFolder(gomock.Any(), &entity.UpdateFolderRequest{UserID: 7, FolderID: 1000, Title: "Photos"}).
		Return(&entity.UpdateFolderResponse{Folder: entity.Folder{ID: 1000, CreatedAt: _at, ExpiresAt: _at, URL: "u", Title: "Photos", DefaultTitle: "a.jpg"}}, nil)
	res, body := e.do(t, http.MethodPatch, "/api/v1/folders/000000000rs", `{"title":"Photos"}`)
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.JSONEq(t, `{"folder":{"id":"000000000rs","createdAt":"2026-10-18T12:00:00Z","expiresAt":"2026-10-18T12:00:00Z","url":"u","title":"Photos","defaultTitle":"a.jpg"}}`, body)

	res, _ = e.do(t, http.MethodPatch, "/api/v1/folders/000000000rs", `{}`)
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)

	e.folders.EXPECT().UpdateFolder(gomock.Any(), gomock.Any()).Return(nil, folderctrl.ErrInvalidTitle)
	res, body = e.do(t, http.MethodPatch, "/api/v1/folders/000000000rs", `{"title":"x"}`)
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	assert.JSONEq(t, `{"error":"invalid folder title"}`, body)

	e.folders.EXPECT().UpdateFolder(gomock.Any(), gomock.Any()).Return(nil, folderctrl.ErrNotFound)
	res, _ = e.do(t, http.MethodPatch, "/api/v1/folders/000000000rs", `{"title":"x"}`)
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
}

func TestFolder_PutFile(t *testing.T) {
	e := newFilesApp(t)
	e.folders.EXPECT().PutFiles(gomock.Any(), &entity.PutFilesRequest{UserID: 7, FolderID: 1000, Files: []entity.FileUpload{{Path: "site/a b.txt", Data: []byte("hi")}}}).
		Return(&entity.PutFilesResponse{Files: []entity.File{{Path: "site/a b.txt", Size: 2, UpdatedAt: _at, URL: "u"}}, CreditsSpent: 1}, nil)
	res, body := e.do(t, http.MethodPut, "/api/v1/folders/000000000rs/files/site/a%20b.txt", "hi")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.JSONEq(t, `{"file":{"path":"site/a b.txt","size":2,"updatedAt":"2026-10-18T12:00:00Z","url":"u"},"creditsSpent":1}`, body)

	res, _ = e.do(t, http.MethodPut, "/api/v1/folders/000000000rs/files/", "hi")
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)

	for err, status := range map[error]int{
		folderctrl.ErrInsufficientCredits: http.StatusPaymentRequired,
		folderctrl.ErrTooLarge:            http.StatusRequestEntityTooLarge,
		folderctrl.ErrInvalidPath:         http.StatusBadRequest,
		folderctrl.ErrNoFiles:             http.StatusBadRequest,
		folderctrl.ErrDuplicatePath:       http.StatusBadRequest,
		folderctrl.ErrQuotaExceeded:       http.StatusTooManyRequests,
	} {
		e.folders.EXPECT().PutFiles(gomock.Any(), gomock.Any()).Return(nil, err)
		res, body = e.do(t, http.MethodPut, "/api/v1/folders/000000000rs/files/a", "x")
		assert.Equal(t, status, res.StatusCode, err.Error())
		assert.Contains(t, body, err.Error())
	}
}

func TestFolder_GetFile(t *testing.T) {
	e := newFilesApp(t)
	e.folders.EXPECT().OpenFile(gomock.Any(), &entity.OpenFileRequest{UserID: 7, FolderID: 1000, Path: "site/page.html"}).
		Return(&entity.OpenFileResponse{Body: io.NopCloser(strings.NewReader("<p>")), Size: 3, ContentType: "text/html"}, nil)
	res, body := e.do(t, http.MethodGet, "/api/v1/folders/000000000rs/files/site/page.html", "")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "<p>", body)
	assert.Equal(t, fiber.MIMEOctetStream, res.Header.Get("Content-Type"))
	assert.Equal(t, "nosniff", res.Header.Get("X-Content-Type-Options"))
	assert.Contains(t, res.Header.Get("Content-Disposition"), `attachment; filename="page.html"`)

	res, _ = e.do(t, http.MethodGet, "/api/v1/folders/0/files/a", "")
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)

	e.folders.EXPECT().OpenFile(gomock.Any(), gomock.Any()).Return(nil, folderctrl.ErrNotFound)
	res, _ = e.do(t, http.MethodGet, "/api/v1/folders/000000000rs/files/a", "")
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
}

func TestFolder_DeleteFile(t *testing.T) {
	e := newFilesApp(t)
	e.folders.EXPECT().DeleteFile(gomock.Any(), &entity.DeleteFileRequest{UserID: 7, FolderID: 1000, Path: "a.txt"}).Return(nil)
	res, _ := e.do(t, http.MethodDelete, "/api/v1/folders/000000000rs/files/a.txt", "")
	assert.Equal(t, http.StatusNoContent, res.StatusCode)

	res, _ = e.do(t, http.MethodDelete, "/api/v1/folders/000000000rs/files/", "")
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)

	e.folders.EXPECT().DeleteFile(gomock.Any(), gomock.Any()).Return(folderctrl.ErrNotFound)
	res, _ = e.do(t, http.MethodDelete, "/api/v1/folders/000000000rs/files/a.txt", "")
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
}

func TestPublic(t *testing.T) {
	e := newFilesApp(t)
	get := func(method, host, target string) (*http.Response, string) {
		req := httptest.NewRequest(method, target, nil)
		req.Host = host
		res, err := e.app.Test(req)
		require.NoError(t, err)
		b, _ := io.ReadAll(res.Body)
		return res, string(b)
	}

	e.folders.EXPECT().OpenPublicFile(gomock.Any(), &entity.OpenPublicFileRequest{FolderID: 1000, Path: "site/a b.html"}).
		Return(&entity.OpenFileResponse{Body: io.NopCloser(strings.NewReader("<p>")), Size: 3, ContentType: "text/html; charset=utf-8", ModTime: _at}, nil)
	res, body := get(http.MethodGet, "F000000000RS.TMP.HOST", "/site/a%20b.html")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "<p>", body)
	assert.Equal(t, "text/html; charset=utf-8", res.Header.Get("Content-Type"))
	assert.Equal(t, _sandbox, res.Header.Get("Content-Security-Policy"))
	assert.Equal(t, "nosniff", res.Header.Get("X-Content-Type-Options"))
	assert.Equal(t, "Sun, 18 Oct 2026 12:00:00 GMT", res.Header.Get("Last-Modified"))

	res, _ = get(http.MethodPost, "f000000000rs.tmp.host", "/")
	assert.Equal(t, http.StatusMethodNotAllowed, res.StatusCode)

	e.folders.EXPECT().OpenPublicFile(gomock.Any(), &entity.OpenPublicFileRequest{FolderID: 1000, Path: ""}).Return(nil, folderctrl.ErrNotFound)
	res, body = get(http.MethodGet, "f000000000rs.tmp.host", "/")
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
	assert.Equal(t, "Not found. The file may have expired.", body)
	assert.Contains(t, res.Header.Get("Content-Type"), "text/plain")

	e.folders.EXPECT().OpenPublicFile(gomock.Any(), gomock.Any()).Return(nil, errBoom)
	res, _ = get(http.MethodGet, "f000000000rs.tmp.host", "/")
	assert.Equal(t, http.StatusInternalServerError, res.StatusCode)

	// Any other host falls through to the app routes.
	res, _ = get(http.MethodGet, "tmp.host", "/api/v1/folders")
	assert.Equal(t, http.StatusUnauthorized, res.StatusCode)

	// So do folder paths.
	res, _ = get(http.MethodGet, "tmp.host", "/files/7/000000000rs/a")
	assert.Equal(t, http.StatusNotFound, res.StatusCode)

	// Folder hosts are answered before the host check; other hosts are
	// not.
	res, _ = get(http.MethodGet, "evil.test", "/api/v1/folders")
	assert.Equal(t, http.StatusMisdirectedRequest, res.StatusCode)

	// Without subdomains, folder hosts are not served at all.
	e.settings.Subdomains = false
	res, _ = get(http.MethodGet, "f000000000rs.tmp.host", "/api/v1/folders")
	assert.Equal(t, http.StatusMisdirectedRequest, res.StatusCode)
}

func TestPublic_Path(t *testing.T) {
	e := newFilesApp(t)
	e.settings.Subdomains = false
	get := func(method, target string) (*http.Response, string) {
		res, err := e.app.Test(httptest.NewRequest(method, target, nil))
		require.NoError(t, err)
		b, _ := io.ReadAll(res.Body)
		return res, string(b)
	}

	e.folders.EXPECT().OpenPublicFile(gomock.Any(), &entity.OpenPublicFileRequest{UserID: 7, FolderID: 1000, Path: "site/a b.html"}).
		Return(&entity.OpenFileResponse{Body: io.NopCloser(strings.NewReader("<p>")), Size: 3, ContentType: "text/html; charset=utf-8", ModTime: _at}, nil)
	res, body := get(http.MethodGet, "/files/7/000000000rs/site/a%20b.html")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "<p>", body)
	assert.Equal(t, _sandbox, res.Header.Get("Content-Security-Policy"))

	e.folders.EXPECT().OpenPublicFile(gomock.Any(), &entity.OpenPublicFileRequest{UserID: 7, FolderID: 1000, Path: ""}).Return(nil, folderctrl.ErrNotFound)
	res, body = get(http.MethodGet, "/files/7/000000000rs/")
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
	assert.Equal(t, "Not found. The file may have expired.", body)

	res, _ = get(http.MethodGet, "/files/7/000000000rs")
	assert.Equal(t, http.StatusMovedPermanently, res.StatusCode)
	assert.Equal(t, "000000000rs/", res.Header.Get("Location"))

	res, _ = get(http.MethodGet, "/files/x_y/000000000rs/a")
	assert.Equal(t, http.StatusNotFound, res.StatusCode, "bad ids")

	res, _ = get(http.MethodPut, "/files/7/000000000rs/a")
	assert.Equal(t, http.StatusMethodNotAllowed, res.StatusCode)
}

func TestHandlers_MissingUserID(t *testing.T) {
	app := fiber.New()
	app.Get("/folders", (&Folder{}).List)
	res, _ := do(t, app, http.MethodGet, "/folders")
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)
}
