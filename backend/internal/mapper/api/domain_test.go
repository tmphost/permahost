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

	"github.com/tmphost/permahost/backend/internal/data/entity"
)

const _domainRoute = "/folders/:id/domains/:domain"

func TestFromHTTPRequestToCreateDomainRequestEntity(t *testing.T) {
	read := func(userID int64, id, body string) *entity.CreateDomainRequest {
		var got *entity.CreateDomainRequest
		runAs(t, userID, "/folders/:id/domains", httptest.NewRequest(http.MethodPost, "/folders/"+id+"/domains", strings.NewReader(body)), func(c fiber.Ctx) {
			got = FromHTTPRequestToCreateDomainRequestEntity(c)
		})
		return got
	}
	assert.Equal(t, &entity.CreateDomainRequest{UserID: 7, FolderID: 1000, Hostname: "www.example.com"}, read(7, "000000000rs", `{"hostname":"www.example.com"}`))
	assert.Nil(t, read(0, "000000000rs", `{"hostname":"www.example.com"}`), "unauthenticated")
	assert.Nil(t, read(7, "bad", `{"hostname":"www.example.com"}`), "bad folder ID")
	assert.Nil(t, read(7, "000000000rs", `{`), "bad JSON")
	assert.Nil(t, read(7, "000000000rs", `{}`), "no hostname")
}

func TestFromHTTPRequestToVerifyAndDeleteDomainRequestEntity(t *testing.T) {
	id := FromIDToBase62(50)
	var verify *entity.VerifyDomainRequest
	var del *entity.DeleteDomainRequest
	read := func(userID int64, domain string) {
		runAs(t, userID, _domainRoute, httptest.NewRequest(http.MethodDelete, "/folders/000000000rs/domains/"+domain, nil), func(c fiber.Ctx) {
			verify, del = FromHTTPRequestToVerifyDomainRequestEntity(c), FromHTTPRequestToDeleteDomainRequestEntity(c)
		})
	}
	read(7, id)
	assert.Equal(t, &entity.VerifyDomainRequest{UserID: 7, FolderID: 1000, DomainID: 50}, verify)
	assert.Equal(t, &entity.DeleteDomainRequest{UserID: 7, FolderID: 1000, DomainID: 50}, del)
	read(0, id)
	assert.Nil(t, verify)
	assert.Nil(t, del)
	read(7, "-")
	assert.Nil(t, verify, "bad domain ID")
	assert.Nil(t, del)
}

func TestDomainResponses(t *testing.T) {
	at := time.Date(2026, 11, 2, 13, 0, 0, 0, time.FixedZone("x", 3600))
	pending := entity.Domain{ID: 50, FolderID: 1000, Hostname: "www.example.com", Target: "t.io", CreatedAt: at, LastError: "no CNAME"}
	active := entity.Domain{ID: 50, FolderID: 1000, Hostname: "www.example.com", Target: "t.io", CreatedAt: at, VerifiedAt: at, CheckedAt: at}
	id := FromIDToBase62(50)
	pendingJSON := `{"id":"` + id + `","folderId":"000000000rs","hostname":"www.example.com","target":"t.io","status":"pending","createdAt":"2026-11-02T12:00:00Z","lastError":"no CNAME"}`
	activeJSON := `{"id":"` + id + `","folderId":"000000000rs","hostname":"www.example.com","target":"t.io","status":"active","createdAt":"2026-11-02T12:00:00Z","verifiedAt":"2026-11-02T12:00:00Z","checkedAt":"2026-11-02T12:00:00Z"}`

	assert.JSONEq(t, `{"domain":`+pendingJSON+`}`, string(FromCreateDomainResponseEntityToHTTPResponse(&entity.CreateDomainResponse{Domain: pending})))
	assert.JSONEq(t, `{"domain":`+activeJSON+`}`, string(FromVerifyDomainResponseEntityToHTTPResponse(&entity.VerifyDomainResponse{Domain: active})))
	assert.JSONEq(t, `{"domains":[`+pendingJSON+`,`+activeJSON+`]}`, string(FromListDomainsResponseEntityToHTTPResponse(&entity.ListDomainsResponse{Domains: []entity.Domain{pending, active}})))
	assert.JSONEq(t, `{"domains":[]}`, string(FromListDomainsResponseEntityToHTTPResponse(&entity.ListDomainsResponse{})))
}

func TestUsage(t *testing.T) {
	var got *entity.GetUsageRequest
	runAs(t, 7, "/", httptest.NewRequest(http.MethodGet, "/", nil), func(c fiber.Ctx) { got = FromHTTPRequestToGetUsageRequestEntity(c) })
	assert.Equal(t, &entity.GetUsageRequest{UserID: 7}, got)
	runAs(t, 0, "/", httptest.NewRequest(http.MethodGet, "/", nil), func(c fiber.Ctx) { got = FromHTTPRequestToGetUsageRequestEntity(c) })
	assert.Nil(t, got)

	assert.JSONEq(t, `{"usage":{"folders":2,"storageBytes":30,"storageBlocks":1,"domains":1,"monthlyCredits":2000,
		"pricing":{"storageBlockBytes":26214400,"storageCredits":500,"folderCredits":500,"domainCredits":500,"domainSetupCredits":500}}}`,
		string(FromGetUsageResponseEntityToHTTPResponse(&entity.GetUsageResponse{
			Usage: entity.Usage{Folders: 2, StorageBytes: 30, Domains: 1}, StorageBlocks: 1, MonthlyCredits: 2000, Pricing: _pricing,
		})))
}
