// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"

	"github.com/tmphost/permahost/backend/internal/controller/credit"
	domainctrl "github.com/tmphost/permahost/backend/internal/controller/domain"
	mockdomain "github.com/tmphost/permahost/backend/internal/controller/domain/mocks"
	folderctrl "github.com/tmphost/permahost/backend/internal/controller/folder"
	mockfolder "github.com/tmphost/permahost/backend/internal/controller/folder/mocks"
	"github.com/tmphost/permahost/backend/internal/data/entity"
	mapper "github.com/tmphost/permahost/backend/internal/mapper/api"
)

func TestDomain_Create(t *testing.T) {
	e := newFilesApp(t)
	e.domains.EXPECT().CreateDomain(gomock.Any(), &entity.CreateDomainRequest{UserID: 7, FolderID: 1000, Hostname: "www.example.com"}).
		Return(&entity.CreateDomainResponse{Domain: entity.Domain{ID: 50, FolderID: 1000, Hostname: "www.example.com", Target: "t.io", CreatedAt: _at}}, nil)
	res, body := e.do(t, http.MethodPost, "/api/v1/folders/000000rs/domains", `{"hostname":"www.example.com"}`)
	assert.Equal(t, http.StatusCreated, res.StatusCode)
	assert.JSONEq(t, `{"domain":{"id":"`+mapper.FromIDToBase62(50)+`","folderId":"000000rs","hostname":"www.example.com","target":"t.io","status":"pending","createdAt":"2026-10-18T12:00:00Z"}}`, body)

	res, _ = e.do(t, http.MethodPost, "/api/v1/folders/000000rs/domains", `{}`)
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)

	e.domains.EXPECT().CreateDomain(gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("%w: x", domainctrl.ErrInvalidHostname))
	res, body = e.do(t, http.MethodPost, "/api/v1/folders/000000rs/domains", `{"hostname":"x"}`)
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)
	assert.JSONEq(t, `{"error":"invalid hostname: x"}`, body)
}

func TestDomain_List(t *testing.T) {
	e := newFilesApp(t)
	e.domains.EXPECT().ListDomains(gomock.Any(), &entity.ListDomainsRequest{UserID: 7, FolderID: 1000}).Return(&entity.ListDomainsResponse{}, nil)
	res, body := e.do(t, http.MethodGet, "/api/v1/folders/000000rs/domains", "")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.JSONEq(t, `{"domains":[]}`, body)

	res, _ = e.do(t, http.MethodGet, "/api/v1/folders/bad/domains", "")
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)

	e.domains.EXPECT().ListDomains(gomock.Any(), gomock.Any()).Return(nil, domainctrl.ErrNotFound)
	res, _ = e.do(t, http.MethodGet, "/api/v1/folders/000000rs/domains", "")
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
}

func TestDomain_Verify(t *testing.T) {
	e := newFilesApp(t)
	id := mapper.FromIDToBase62(50)
	path := "/api/v1/folders/000000rs/domains/" + id + "/verify"
	e.domains.EXPECT().VerifyDomain(gomock.Any(), &entity.VerifyDomainRequest{UserID: 7, FolderID: 1000, DomainID: 50}).
		Return(&entity.VerifyDomainResponse{Domain: entity.Domain{ID: 50, FolderID: 1000, Hostname: "www.example.com", Target: "t.io", CreatedAt: _at, VerifiedAt: _at, CheckedAt: _at}}, nil)
	res, body := e.do(t, http.MethodPost, path, "")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Contains(t, body, `"status":"active"`)

	res, _ = e.do(t, http.MethodPost, "/api/v1/folders/000000rs/domains/-/verify", "")
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)

	for err, status := range map[error]int{
		domainctrl.ErrNotVerified:         http.StatusUnprocessableEntity,
		domainctrl.ErrDomainTaken:         http.StatusConflict,
		domainctrl.ErrInsufficientCredits: http.StatusPaymentRequired,
		errBoom:                           http.StatusInternalServerError,
	} {
		e.domains.EXPECT().VerifyDomain(gomock.Any(), gomock.Any()).Return(nil, err)
		res, _ = e.do(t, http.MethodPost, path, "")
		assert.Equal(t, status, res.StatusCode, err.Error())
	}
}

func TestDomain_Delete(t *testing.T) {
	e := newFilesApp(t)
	path := "/api/v1/folders/000000rs/domains/" + mapper.FromIDToBase62(50)
	e.domains.EXPECT().DeleteDomain(gomock.Any(), &entity.DeleteDomainRequest{UserID: 7, FolderID: 1000, DomainID: 50}).Return(nil)
	res, _ := e.do(t, http.MethodDelete, path, "")
	assert.Equal(t, http.StatusNoContent, res.StatusCode)

	res, _ = e.do(t, http.MethodDelete, "/api/v1/folders/000000rs/domains/-", "")
	assert.Equal(t, http.StatusBadRequest, res.StatusCode)

	e.domains.EXPECT().DeleteDomain(gomock.Any(), gomock.Any()).Return(domainctrl.ErrNotFound)
	res, _ = e.do(t, http.MethodDelete, path, "")
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
}

func TestUser_Usage(t *testing.T) {
	e := newFilesApp(t)
	e.credits.EXPECT().GetUsage(gomock.Any(), &entity.GetUsageRequest{UserID: 7}).Return(&entity.GetUsageResponse{
		Usage: entity.Usage{Folders: 1, StorageBytes: 5, Domains: 0}, StorageBlocks: 1, MonthlyCredits: 1000, Pricing: credit.CurrentPricing(),
	}, nil)
	res, body := e.do(t, http.MethodGet, "/api/v1/users/me/usage", "")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Contains(t, body, `"monthlyCredits":1000`)

	e.credits.EXPECT().GetUsage(gomock.Any(), gomock.Any()).Return(nil, errBoom)
	res, _ = e.do(t, http.MethodGet, "/api/v1/users/me/usage", "")
	assert.Equal(t, http.StatusInternalServerError, res.StatusCode)

	app := fiber.New()
	app.Get("/", (&User{}).Usage)
	res, err := app.Test(httptest.NewRequest(http.MethodGet, "/", nil))
	require.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, res.StatusCode, "no user")
}

func TestCustomDomainHandler(t *testing.T) {
	mc := gomock.NewController(t)
	folders, domains := mockfolder.NewMockController(mc), mockdomain.NewMockController(mc)
	app := fiber.New()
	app.Use(NewCustomDomainHandler(folders, domains))
	get := func(method, host, target string) (*http.Response, string) {
		req := httptest.NewRequest(method, target, nil)
		req.Host = host
		res, err := app.Test(req)
		require.NoError(t, err)
		b, err := io.ReadAll(res.Body)
		require.NoError(t, err)
		return res, string(b)
	}

	domains.EXPECT().GetDomainFolder(gomock.Any(), "www.custom.io").Return(int64(1000), nil)
	folders.EXPECT().OpenPublicFile(gomock.Any(), &entity.OpenPublicFileRequest{FolderID: 1000, Path: "site/a.html"}).
		Return(&entity.OpenFileResponse{Body: io.NopCloser(strings.NewReader("<p>")), Size: 3, ContentType: "text/html", ModTime: _at}, nil)
	res, body := get(http.MethodGet, "www.custom.io", "/site/a.html")
	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "<p>", body)
	assert.Equal(t, _sandbox, res.Header.Get("Content-Security-Policy"))

	domains.EXPECT().GetDomainFolder(gomock.Any(), "www.custom.io").Return(int64(1000), nil)
	folders.EXPECT().OpenPublicFile(gomock.Any(), gomock.Any()).Return(nil, folderctrl.ErrSuspended)
	res, body = get(http.MethodGet, "www.custom.io", "/")
	assert.Equal(t, http.StatusPaymentRequired, res.StatusCode)
	assert.Contains(t, body, "paused")

	domains.EXPECT().GetDomainFolder(gomock.Any(), "evil.io").Return(int64(0), domainctrl.ErrNotFound)
	res, body = get(http.MethodGet, "evil.io", "/")
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
	assert.JSONEq(t, `{"error":"host not allowed"}`, body)

	domains.EXPECT().GetDomainFolder(gomock.Any(), "www.custom.io").Return(int64(0), errBoom)
	res, _ = get(http.MethodGet, "www.custom.io", "/")
	assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
}

func TestCustomDomainHandler_BadPath(t *testing.T) {
	mc := gomock.NewController(t)
	domains := mockdomain.NewMockController(mc)
	domains.EXPECT().GetDomainFolder(gomock.Any(), "www.custom.io").Return(int64(1000), nil)
	app := fiber.New()
	app.Use(NewCustomDomainHandler(mockfolder.NewMockController(mc), domains))
	res := rawRequest(app, "www.custom.io", "/a%zz")
	assert.Equal(t, http.StatusNotFound, res)
}

// rawRequest sends a GET for a URI net/http refuses to build, such as one
// with an invalid escape, and returns the status.
func rawRequest(app *fiber.App, host, uri string) int {
	var fctx fasthttp.RequestCtx
	fctx.Request.SetRequestURI(uri)
	fctx.Request.SetHost(host)
	app.Handler()(&fctx)
	return fctx.Response.StatusCode()
}
