// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package cmd

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

const domainsJSON = `{"domains":[` +
	`{"id":"d1","folderId":"rs","hostname":"www.example.com","target":"rs.permahost.test","status":"active","createdAt":"2026-10-18T12:00:00Z","verifiedAt":"2026-10-18T12:00:00Z"},` +
	`{"id":"d2","folderId":"rs","hostname":"docs.example.com","target":"rs.permahost.test","status":"pending","createdAt":"2026-10-18T12:00:00Z","lastError":"no CNAME record"}]}`

func TestDomainAdd(t *testing.T) {
	h := newHarness(t)
	var body string
	h.mux.HandleFunc("POST /api/v1/folders/rs/domains", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(201)
		_, _ = io.WriteString(w, `{"domain":{"id":"d2","folderId":"rs","hostname":"docs.example.com","target":"rs.permahost.test","status":"pending","createdAt":"2026-10-18T12:00:00Z"}}`)
	})
	h.reply("POST /api/v1/folders/zz/domains", 409, `{"error":"domain is taken"}`)

	assert.Equal(t, 0, h.run("domain", "add", "--id", "rs", "docs.example.com"), h.stderr.String())
	assert.JSONEq(t, `{"hostname":"docs.example.com"}`, body)
	assert.Equal(t, "Added docs.example.com to folder rs. Create this DNS record:\n\n"+
		"  docs.example.com  CNAME  rs.permahost.test\n\n"+
		"then run \"permahost domain verify --id rs docs.example.com\".\n", h.stdout.String())

	assert.Equal(t, 0, h.run("domain", "add", "--id", "rs", "docs.example.com", "--json"))
	assert.Equal(t, "rs.permahost.test", h.decode()["domain"].(map[string]any)["target"])

	assert.Equal(t, 1, h.run("domain", "add", "--id", "zz", "docs.example.com"))
	assert.Equal(t, "error: domain is taken\n", h.stderr.String())

	assert.Equal(t, 1, h.run("domain", "add", "docs.example.com"))
	assert.Contains(t, h.stderr.String(), `required flag(s) "id" not set`)
}

func TestDomainLs(t *testing.T) {
	h := newHarness(t)
	h.reply("GET /api/v1/folders/rs/domains", 200, domainsJSON)
	h.reply("GET /api/v1/folders/ab/domains", 200, `{"domains":[]}`)
	h.reply("GET /api/v1/folders/zz/domains", 404, `{"error":"not found"}`)

	assert.Equal(t, 0, h.run("domain", "ls", "--id", "rs"))
	assert.Equal(t, "ID  HOSTNAME          STATUS   CNAME TARGET       LAST ERROR\n"+
		"d1  www.example.com   active   rs.permahost.test  \n"+
		"d2  docs.example.com  pending  rs.permahost.test  no CNAME record\n", h.stdout.String())

	assert.Equal(t, 0, h.run("domain", "ls", "--id", "rs", "--json"))
	assert.Len(t, h.decode()["domains"], 2)

	assert.Equal(t, 0, h.run("domain", "ls", "--id", "ab"))
	assert.Equal(t, "No domains.\n", h.stdout.String())

	assert.Equal(t, 1, h.run("domain", "ls", "--id", "zz"))
	assert.Equal(t, "error: not found\n", h.stderr.String())
}

func TestDomainVerify(t *testing.T) {
	h := newHarness(t)
	h.reply("GET /api/v1/folders/rs/domains", 200, domainsJSON)
	h.reply("GET /api/v1/folders/zz/domains", 404, `{"error":"not found"}`)
	h.reply("POST /api/v1/folders/rs/domains/d2/verify", 200, `{"domain":{"id":"d2","folderId":"rs","hostname":"docs.example.com","target":"rs.permahost.test","status":"active"}}`)
	h.reply("POST /api/v1/folders/rs/domains/d3/verify", 422, `{"error":"domain not verified: no CNAME record"}`)
	h.reply("POST /api/v1/folders/rs/domains/d4/verify", 402, `{"error":"insufficient credits"}`)

	assert.Equal(t, 0, h.run("domain", "verify", "--id", "rs", "Docs.Example.com."), h.stderr.String())
	assert.Equal(t, "docs.example.com is active and serves folder rs.\n", h.stdout.String())

	assert.Equal(t, 0, h.run("domain", "verify", "--id", "rs", "d2", "--json"))
	assert.Equal(t, "active", h.decode()["domain"].(map[string]any)["status"])

	for args, want := range map[string]string{
		"d3":            "error: domain not verified: no CNAME record: check the CNAME record \"permahost domain ls --id rs\" shows; DNS changes can take a while to spread\n",
		"d4":            "error: insufficient credits\n",
		"a.example.com": "error: folder rs has no domain a.example.com\n",
	} {
		assert.Equal(t, 1, h.run("domain", "verify", "--id", "rs", args), args)
		assert.Equal(t, want, h.stderr.String(), args)
	}

	assert.Equal(t, 1, h.run("domain", "verify", "--id", "zz", "www.example.com"))
	assert.Equal(t, "error: not found\n", h.stderr.String())
}

func TestDomainRm(t *testing.T) {
	h := newHarness(t)
	h.reply("GET /api/v1/folders/rs/domains", 200, domainsJSON)
	h.reply("DELETE /api/v1/folders/rs/domains/d1", 204, ``)
	h.reply("DELETE /api/v1/folders/rs/domains/d9", 404, `{"error":"not found"}`)

	assert.Equal(t, 0, h.run("domain", "rm", "--id", "rs", "www.example.com"), h.stderr.String())
	assert.Equal(t, "Removed www.example.com from folder rs.\n", h.stdout.String())

	assert.Equal(t, 0, h.run("domain", "rm", "--id", "rs", "d1", "--json"))
	assert.Equal(t, map[string]any{"deleted": true, "id": "rs", "domainId": "d1"}, h.decode())

	assert.Equal(t, 1, h.run("domain", "rm", "--id", "rs", "d9"))
	assert.Equal(t, "error: not found\n", h.stderr.String())

	assert.Equal(t, 1, h.run("domain", "rm", "--id", "rs", "nope.example.com"))
	assert.Equal(t, "error: folder rs has no domain nope.example.com\n", h.stderr.String())
}
