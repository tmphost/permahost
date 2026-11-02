// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/permahost/backend/internal/controller/credit"
	"github.com/tmphost/permahost/backend/internal/data/model"
)

// dnsServer is a local DNS server answering CNAME queries from cnames.
type dnsServer struct {
	mu     sync.Mutex
	cnames map[string]string
}

func (d *dnsServer) set(host, target string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cnames[dns.Fqdn(host)] = dns.Fqdn(target)
}

func serveDNS(t *testing.T) (*dnsServer, string) {
	t.Helper()
	d := &dnsServer{cnames: map[string]string{}}
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	srv := &dns.Server{PacketConn: pc, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, q *dns.Msg) {
		rs := new(dns.Msg)
		rs.SetReply(q)
		d.mu.Lock()
		target, ok := d.cnames[q.Question[0].Name]
		d.mu.Unlock()
		if ok {
			rs.Answer = append(rs.Answer, &dns.CNAME{Hdr: dns.RR_Header{Name: q.Question[0].Name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET}, Target: target})
		} else {
			rs.Rcode = dns.RcodeNameError
		}
		_ = w.WriteMsg(rs)
	})}
	started := make(chan struct{})
	srv.NotifyStartedFunc = func() { close(started) }
	go func() { _ = srv.ActivateAndServe() }()
	<-started
	t.Cleanup(func() { _ = srv.Shutdown() })
	return d, pc.LocalAddr().String()
}

// TestCustomDomains adds a custom domain to a folder, verifies its CNAME
// through a real DNS lookup, charges for it and serves the folder on it.
func TestCustomDomains(t *testing.T) {
	records, resolver := serveDNS(t)
	cfg := testConfig(t)
	cfg.App.SitesDomain = "localhost"
	cfg.Files = FilesConfig{Subdomains: true, PublicDomain: "sites.localhost"}
	cfg.DNS.Resolvers = " " + resolver + " ,"
	a, err := New(cfg, fstest.MapFS{})
	require.NoError(t, err)
	require.NoError(t, a.db.Create(&model.User{ID: 7, Email: "a@x", Credits: 2000, CreditsRenewAt: time.Now().Add(time.Hour)}).Error)
	key := accessToken(t, a, "00000000001")

	call := func(method, target, host, body string) (*http.Response, string) {
		t.Helper()
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req.Host = host
		req.Header.Set("Authorization", "Bearer "+key)
		res, err := a.Handler().Test(req)
		require.NoError(t, err)
		b, _ := io.ReadAll(res.Body)
		return res, string(b)
	}
	credits := func() int {
		var u model.User
		require.NoError(t, a.db.First(&u, 7).Error)
		return u.Credits
	}

	res, body := call(http.MethodPost, "/api/v1/folders", "localhost:3000", `{}`)
	require.Equal(t, http.StatusCreated, res.StatusCode, body)
	var folder struct{ Folder struct{ ID string } }
	require.NoError(t, json.Unmarshal([]byte(body), &folder))
	id := folder.Folder.ID
	res, body = call(http.MethodPut, "/api/v1/folders/"+id+"/files/index.html", "localhost:3000", "<p>custom</p>")
	require.Equal(t, http.StatusOK, res.StatusCode, body)
	assert.Equal(t, 1000, credits())

	domains := "/api/v1/folders/" + id + "/domains"
	res, body = call(http.MethodPost, domains, "localhost:3000", `{"hostname":"WWW.Custom.IO"}`)
	require.Equal(t, http.StatusCreated, res.StatusCode, body)
	var created struct {
		Domain struct{ ID, Hostname, Target, Status string }
	}
	require.NoError(t, json.Unmarshal([]byte(body), &created))
	assert.Equal(t, "www.custom.io", created.Domain.Hostname)
	assert.Equal(t, "f"+id+".sites.localhost", created.Domain.Target)
	assert.Equal(t, "pending", created.Domain.Status)

	res, _ = call(http.MethodGet, "/", "www.custom.io", "")
	assert.Equal(t, http.StatusNotFound, res.StatusCode, "not served while pending")

	verify := domains + "/" + created.Domain.ID + "/verify"
	res, body = call(http.MethodPost, verify, "localhost:3000", "")
	assert.Equal(t, http.StatusUnprocessableEntity, res.StatusCode)
	assert.Contains(t, body, "no CNAME record")

	// The background check finds the record and charges the setup and
	// the first month.
	records.set("www.custom.io", created.Domain.Target)
	a.verifyDomains()
	assert.Equal(t, 1000-credit.DomainSetupCredits-credit.DomainCredits, credits())
	res, body = call(http.MethodGet, domains, "localhost:3000", "")
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Contains(t, body, `"status":"active"`)
	res, body = call(http.MethodPost, verify, "localhost:3000", "")
	require.Equal(t, http.StatusOK, res.StatusCode, body)
	assert.Equal(t, 0, credits(), "verified once, charged once")

	res, body = call(http.MethodGet, "/", "www.custom.io", "")
	require.Equal(t, http.StatusOK, res.StatusCode, body)
	assert.Equal(t, "<p>custom</p>", body)

	// The renewal charges the folder, the storage and the domain.
	require.NoError(t, a.db.Model(&model.User{}).Where("id = 7").Update("credits_renew_at", time.Now().Add(-time.Minute)).Error)
	a.renewCredits()
	assert.Equal(t, credit.DefaultMonthlyGrant-1500, credits())

	// Deleting the folder takes its domains along.
	res, _ = call(http.MethodDelete, "/api/v1/folders/"+id, "localhost:3000", "")
	require.Equal(t, http.StatusNoContent, res.StatusCode)
	res, _ = call(http.MethodGet, "/", "www.custom.io", "")
	assert.Equal(t, http.StatusNotFound, res.StatusCode)

	a.prepareCertificate("www.custom.io") // no TLS: nothing to do

	// A failing check is logged, not fatal.
	sqlDB, err := a.db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	a.verifyDomains()
}
