// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package domain

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/permahost/backend/internal/controller/credit"
	"github.com/tmphost/permahost/backend/internal/data/entity"
	"github.com/tmphost/permahost/backend/internal/data/model"
	"github.com/tmphost/permahost/backend/internal/repository"
	mockrepo "github.com/tmphost/permahost/backend/internal/repository/mocks"
	"github.com/tmphost/permahost/backend/internal/service/dnscheck"
	mockdns "github.com/tmphost/permahost/backend/internal/service/dnscheck/mocks"
)

const (
	_target = "000000rs.file.sites.permahost.io"
	_cost   = credit.DomainSetupCredits + credit.DomainCredits
)

var (
	_now    = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
)

type env struct {
	ctrl     Controller
	domains  *mockrepo.MockDomainRepository
	folders  *mockrepo.MockFolderRepository
	users    *mockrepo.MockUserRepository
	dns      *mockdns.MockService
	settings entity.FileSettings
	verified []string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	mc := gomock.NewController(t)
	e := &env{
		domains:  mockrepo.NewMockDomainRepository(mc),
		folders:  mockrepo.NewMockFolderRepository(mc),
		users:    mockrepo.NewMockUserRepository(mc),
		dns:      mockdns.NewMockService(mc),
		settings: entity.FileSettings{Subdomains: true, PublicDomain: "Sites.Permahost.IO"},
	}
	e.ctrl = New(Params{
		Domains:     e.domains,
		Folders:     e.folders,
		Users:       e.users,
		DNS:         e.dns,
		AppHost:     "App.Permahost.IO",
		SitesDomain: "dyn.permahost.io",
		Settings:    func() entity.FileSettings { return e.settings },
		OnVerified:  func(host string) { e.verified = append(e.verified, host) },
		NewID:       func() int64 { return 50 },
		Now:         func() time.Time { return _now },
	})
	return e
}

// folder expects a lookup of folder 1000, user 7's folder.
func (e *env) folder() {
	e.folders.EXPECT().FindFolderByID(gomock.Any(), int64(1000)).Return(model.Folder{ID: 1000, UserID: 7}, nil)
}

// pending expects a lookup of domain 50, pending for folder 1000.
func (e *env) pending() model.Domain {
	d := model.Domain{ID: 50, CreatedAt: _now.Add(-time.Hour), UserID: 7, FolderID: 1000, Hostname: "www.example.com"}
	e.domains.EXPECT().FindDomainByID(gomock.Any(), int64(50)).Return(d, nil)
	return d
}

// failed expects a check of domain 50 recorded as failed for reason.
func (e *env) failed(t *testing.T, reason string) {
	e.domains.EXPECT().UpdateDomainCheck(gomock.Any(), int64(50), _now, nil, gomock.Any()).
		DoAndReturn(func(_ context.Context, _ int64, _ time.Time, _ *time.Time, lastError string) error {
			assert.Contains(t, lastError, reason)
			return nil
		})
}

func TestNew_Defaults(t *testing.T) {
	c := New(Params{}).(*controller)
	assert.WithinDuration(t, time.Now(), c.now(), time.Second)
	c.onVerified("x.io") // does nothing
}

func TestNormalizeHostname(t *testing.T) {
	for in, want := range map[string]string{
		"www.example.com":    "www.example.com",
		" WWW.Example.COM. ": "www.example.com",
		"a-b.c1.io":          "a-b.c1.io",
		"xn--bcher-kva.de":   "xn--bcher-kva.de",
		"example":            "",
		"":                   "",
		"-a.example.com":     "",
		"a-.example.com":     "",
		"a..example.com":     "",
		"a_b.example.com":    "",
		"192.168.0.1":        "",
		"www.example.com/x":  "",
		"ex ample.com":       "",
	} {
		assert.Equal(t, want, NormalizeHostname(in), in)
	}
	long := ""
	for range 64 {
		long += "abc."
	}
	assert.Empty(t, NormalizeHostname(long+"io"), "over 253 characters")
}

func TestCreateDomain(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.folder()
	gomock.InOrder(
		e.domains.EXPECT().ListDomainsByFolder(ctx, int64(1000)).Return([]model.Domain{{Hostname: "other.example.com"}}, nil),
		e.domains.EXPECT().FindVerifiedDomainByHostname(ctx, "www.example.com").Return(model.Domain{}, repository.ErrNotFound),
		e.domains.EXPECT().CreateDomain(ctx, model.Domain{ID: 50, CreatedAt: _now, UserID: 7, FolderID: 1000, Hostname: "www.example.com"}).
			DoAndReturn(func(_ context.Context, d model.Domain) (model.Domain, error) { return d, nil }),
	)
	rs, err := e.ctrl.CreateDomain(ctx, &entity.CreateDomainRequest{UserID: 7, FolderID: 1000, Hostname: "WWW.Example.com."})
	require.NoError(t, err)
	assert.Equal(t, entity.Domain{ID: 50, UserID: 7, FolderID: 1000, Hostname: "www.example.com", Target: _target, CreatedAt: _now}, rs.Domain)
	assert.Empty(t, e.verified, "pending until checked")
}

func TestCreateDomain_Errors(t *testing.T) {
	ctx := context.Background()
	rq := func(host string) *entity.CreateDomainRequest {
		return &entity.CreateDomainRequest{UserID: 7, FolderID: 1000, Hostname: host}
	}

	t.Run("invalid", func(t *testing.T) {
		e := newEnv(t)
		for _, host := range []string{"localhost", "1.2.3.4", "app.permahost.io", "x.dyn.permahost.io", "sites.permahost.io", "f1.sites.permahost.io"} {
			_, err := e.ctrl.CreateDomain(ctx, rq(host))
			assert.ErrorIs(t, err, ErrInvalidHostname, host)
		}
	})
	t.Run("folder", func(t *testing.T) {
		e := newEnv(t)
		e.folders.EXPECT().FindFolderByID(ctx, int64(1000)).Return(model.Folder{}, repository.ErrNotFound)
		_, err := e.ctrl.CreateDomain(ctx, rq("www.example.com"))
		assert.ErrorIs(t, err, ErrNotFound)

		e.folders.EXPECT().FindFolderByID(ctx, int64(1000)).Return(model.Folder{}, errBoom)
		_, err = e.ctrl.CreateDomain(ctx, rq("www.example.com"))
		assert.ErrorIs(t, err, errBoom)

		e.folders.EXPECT().FindFolderByID(ctx, int64(1000)).Return(model.Folder{ID: 1000, UserID: 8}, nil)
		_, err = e.ctrl.CreateDomain(ctx, rq("www.example.com"))
		assert.ErrorIs(t, err, ErrNotFound, "someone else's")

		expires := _now.Add(time.Minute)
		e.folders.EXPECT().FindFolderByID(ctx, int64(1000)).Return(model.Folder{ID: 1000, UserID: 7, ExpiresAt: &expires}, nil)
		_, err = e.ctrl.CreateDomain(ctx, rq("www.example.com"))
		assert.ErrorIs(t, err, ErrNotFound, "folders that expire have no domains")
	})
	t.Run("list fails", func(t *testing.T) {
		e := newEnv(t)
		e.folder()
		e.domains.EXPECT().ListDomainsByFolder(ctx, int64(1000)).Return(nil, errBoom)
		_, err := e.ctrl.CreateDomain(ctx, rq("www.example.com"))
		assert.ErrorIs(t, err, errBoom)
	})
	t.Run("already on the folder", func(t *testing.T) {
		e := newEnv(t)
		e.folder()
		e.domains.EXPECT().ListDomainsByFolder(ctx, int64(1000)).Return([]model.Domain{{Hostname: "www.example.com"}}, nil)
		_, err := e.ctrl.CreateDomain(ctx, rq("www.example.com"))
		assert.ErrorIs(t, err, ErrDomainTaken)
	})
	t.Run("verified elsewhere", func(t *testing.T) {
		e := newEnv(t)
		e.folder()
		e.domains.EXPECT().ListDomainsByFolder(ctx, int64(1000)).Return(nil, nil)
		e.domains.EXPECT().FindVerifiedDomainByHostname(ctx, "www.example.com").Return(model.Domain{ID: 9}, nil)
		_, err := e.ctrl.CreateDomain(ctx, rq("www.example.com"))
		assert.ErrorIs(t, err, ErrDomainTaken)
	})
	t.Run("lookup fails", func(t *testing.T) {
		e := newEnv(t)
		e.folder()
		e.domains.EXPECT().ListDomainsByFolder(ctx, int64(1000)).Return(nil, nil)
		e.domains.EXPECT().FindVerifiedDomainByHostname(ctx, "www.example.com").Return(model.Domain{}, errBoom)
		_, err := e.ctrl.CreateDomain(ctx, rq("www.example.com"))
		assert.ErrorIs(t, err, errBoom)
	})
	t.Run("create fails", func(t *testing.T) {
		e := newEnv(t)
		e.folder()
		e.domains.EXPECT().ListDomainsByFolder(ctx, int64(1000)).Return(nil, nil)
		e.domains.EXPECT().FindVerifiedDomainByHostname(ctx, "www.example.com").Return(model.Domain{}, repository.ErrNotFound)
		e.domains.EXPECT().CreateDomain(ctx, gomock.Any()).Return(model.Domain{}, errBoom)
		_, err := e.ctrl.CreateDomain(ctx, rq("www.example.com"))
		assert.ErrorIs(t, err, errBoom)
	})
}

func TestListDomains(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.settings.Subdomains = false
	verified := _now.Add(-time.Minute)
	e.folder()
	e.domains.EXPECT().ListDomainsByFolder(ctx, int64(1000)).Return([]model.Domain{
		{ID: 1, UserID: 7, FolderID: 1000, Hostname: "a.example.com", CreatedAt: _now, VerifiedAt: &verified, CheckedAt: &verified},
		{ID: 2, UserID: 7, FolderID: 1000, Hostname: "b.example.com", CreatedAt: _now, LastError: "no CNAME"},
	}, nil)
	rs, err := e.ctrl.ListDomains(ctx, &entity.ListDomainsRequest{UserID: 7, FolderID: 1000})
	require.NoError(t, err)
	assert.Equal(t, []entity.Domain{
		{ID: 1, UserID: 7, FolderID: 1000, Hostname: "a.example.com", Target: "app.permahost.io", CreatedAt: _now, VerifiedAt: verified, CheckedAt: verified},
		{ID: 2, UserID: 7, FolderID: 1000, Hostname: "b.example.com", Target: "app.permahost.io", CreatedAt: _now, LastError: "no CNAME"},
	}, rs.Domains, "the app host is the target without folder hosts")

	e.folders.EXPECT().FindFolderByID(ctx, int64(1000)).Return(model.Folder{ID: 1000, UserID: 8}, nil)
	_, err = e.ctrl.ListDomains(ctx, &entity.ListDomainsRequest{UserID: 7, FolderID: 1000})
	assert.ErrorIs(t, err, ErrNotFound)

	e.folder()
	e.domains.EXPECT().ListDomainsByFolder(ctx, int64(1000)).Return(nil, errBoom)
	_, err = e.ctrl.ListDomains(ctx, &entity.ListDomainsRequest{UserID: 7, FolderID: 1000})
	assert.ErrorIs(t, err, errBoom)
}

func TestVerifyDomain(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.pending()
	e.folder()
	gomock.InOrder(
		e.dns.EXPECT().LookupCNAME(ctx, "www.example.com").Return(_target, nil),
		e.domains.EXPECT().FindVerifiedDomainByHostname(ctx, "www.example.com").Return(model.Domain{}, repository.ErrNotFound),
		e.users.EXPECT().SpendCredits(ctx, int64(7), _cost).Return(nil),
		e.domains.EXPECT().UpdateDomainCheck(ctx, int64(50), _now, &_now, "").Return(nil),
	)
	rs, err := e.ctrl.VerifyDomain(ctx, &entity.VerifyDomainRequest{UserID: 7, FolderID: 1000, DomainID: 50})
	require.NoError(t, err)
	assert.Equal(t, _now, rs.Domain.VerifiedAt)
	assert.Equal(t, _now, rs.Domain.CheckedAt)
	assert.Equal(t, _target, rs.Domain.Target)
	assert.Equal(t, []string{"www.example.com"}, e.verified, "the certificate is prepared")
}

func TestVerifyDomain_AlreadyVerified(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	verified := _now.Add(-time.Hour)
	e.domains.EXPECT().FindDomainByID(ctx, int64(50)).Return(model.Domain{ID: 50, UserID: 7, FolderID: 1000, Hostname: "www.example.com", VerifiedAt: &verified}, nil)
	rs, err := e.ctrl.VerifyDomain(ctx, &entity.VerifyDomainRequest{UserID: 7, FolderID: 1000, DomainID: 50})
	require.NoError(t, err, "no lookup, no charge")
	assert.Equal(t, verified, rs.Domain.VerifiedAt)
}

func TestVerifyDomain_NotVerified(t *testing.T) {
	ctx := context.Background()
	rq := &entity.VerifyDomainRequest{UserID: 7, FolderID: 1000, DomainID: 50}
	for name, tc := range map[string]struct {
		cname  string
		err    error
		reason string
	}{
		"no record":     {"", dnscheck.ErrNoCNAME, "www.example.com has no CNAME record; add one pointing to " + _target},
		"lookup failed": {"", errBoom, "looking up www.example.com failed"},
		"wrong target":  {"elsewhere.io", nil, "www.example.com points to elsewhere.io, not " + _target},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.pending()
			e.folder()
			e.dns.EXPECT().LookupCNAME(ctx, "www.example.com").Return(tc.cname, tc.err)
			e.domains.EXPECT().UpdateDomainCheck(ctx, int64(50), _now, nil, gomock.Any()).
				DoAndReturn(func(_ context.Context, _ int64, _ time.Time, _ *time.Time, lastError string) error {
					assert.Contains(t, lastError, tc.reason)
					return nil
				})
			_, err := e.ctrl.VerifyDomain(ctx, rq)
			assert.ErrorIs(t, err, ErrNotVerified)
			assert.ErrorContains(t, err, tc.reason)
			assert.Empty(t, e.verified)
		})
	}
}

func TestVerifyDomain_Errors(t *testing.T) {
	ctx := context.Background()
	rq := &entity.VerifyDomainRequest{UserID: 7, FolderID: 1000, DomainID: 50}
	cnameOK := func(e *env) {
		e.pending()
		e.folder()
		e.dns.EXPECT().LookupCNAME(ctx, "www.example.com").Return(_target, nil)
	}

	t.Run("domain", func(t *testing.T) {
		e := newEnv(t)
		e.domains.EXPECT().FindDomainByID(ctx, int64(50)).Return(model.Domain{}, repository.ErrNotFound)
		_, err := e.ctrl.VerifyDomain(ctx, rq)
		assert.ErrorIs(t, err, ErrNotFound)

		e.domains.EXPECT().FindDomainByID(ctx, int64(50)).Return(model.Domain{}, errBoom)
		_, err = e.ctrl.VerifyDomain(ctx, rq)
		assert.ErrorIs(t, err, errBoom)

		e.domains.EXPECT().FindDomainByID(ctx, int64(50)).Return(model.Domain{ID: 50, UserID: 8, FolderID: 1000}, nil)
		_, err = e.ctrl.VerifyDomain(ctx, rq)
		assert.ErrorIs(t, err, ErrNotFound, "someone else's")

		e.domains.EXPECT().FindDomainByID(ctx, int64(50)).Return(model.Domain{ID: 50, UserID: 7, FolderID: 1001}, nil)
		_, err = e.ctrl.VerifyDomain(ctx, rq)
		assert.ErrorIs(t, err, ErrNotFound, "another folder's")
	})
	t.Run("folder gone", func(t *testing.T) {
		e := newEnv(t)
		e.pending()
		e.folders.EXPECT().FindFolderByID(ctx, int64(1000)).Return(model.Folder{}, repository.ErrNotFound)
		_, err := e.ctrl.VerifyDomain(ctx, rq)
		assert.ErrorIs(t, err, ErrNotFound)

		e.pending()
		e.folders.EXPECT().FindFolderByID(ctx, int64(1000)).Return(model.Folder{}, errBoom)
		_, err = e.ctrl.VerifyDomain(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
	})
	t.Run("taken", func(t *testing.T) {
		e := newEnv(t)
		cnameOK(e)
		e.domains.EXPECT().FindVerifiedDomainByHostname(ctx, "www.example.com").Return(model.Domain{ID: 9}, nil)
		e.failed(t, "verified for another folder")
		_, err := e.ctrl.VerifyDomain(ctx, rq)
		assert.ErrorIs(t, err, ErrDomainTaken)
	})
	t.Run("insufficient credits", func(t *testing.T) {
		e := newEnv(t)
		cnameOK(e)
		e.domains.EXPECT().FindVerifiedDomainByHostname(ctx, "www.example.com").Return(model.Domain{}, repository.ErrNotFound)
		e.users.EXPECT().SpendCredits(ctx, int64(7), _cost).Return(repository.ErrInsufficientCredits)
		e.failed(t, "1000 needed")
		_, err := e.ctrl.VerifyDomain(ctx, rq)
		assert.ErrorIs(t, err, ErrInsufficientCredits)
	})
	t.Run("spend fails", func(t *testing.T) {
		e := newEnv(t)
		cnameOK(e)
		e.domains.EXPECT().FindVerifiedDomainByHostname(ctx, "www.example.com").Return(model.Domain{}, repository.ErrNotFound)
		e.users.EXPECT().SpendCredits(ctx, int64(7), _cost).Return(errBoom)
		_, err := e.ctrl.VerifyDomain(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
	})
	t.Run("record fails: refunded", func(t *testing.T) {
		e := newEnv(t)
		cnameOK(e)
		e.domains.EXPECT().FindVerifiedDomainByHostname(ctx, "www.example.com").Return(model.Domain{ID: 50}, nil)
		e.users.EXPECT().SpendCredits(ctx, int64(7), _cost).Return(nil)
		e.domains.EXPECT().UpdateDomainCheck(ctx, int64(50), _now, &_now, "").Return(errBoom)
		e.users.EXPECT().RefundCredits(ctx, int64(7), _cost).Return(nil)
		_, err := e.ctrl.VerifyDomain(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
		assert.NotContains(t, err.Error(), "refund")
		assert.Empty(t, e.verified)
	})
	t.Run("refund fails too", func(t *testing.T) {
		e := newEnv(t)
		cnameOK(e)
		refundErr := errors.New("refund failed")
		e.domains.EXPECT().FindVerifiedDomainByHostname(ctx, "www.example.com").Return(model.Domain{}, repository.ErrNotFound)
		e.users.EXPECT().SpendCredits(ctx, int64(7), _cost).Return(nil)
		e.domains.EXPECT().UpdateDomainCheck(ctx, int64(50), _now, &_now, "").Return(errBoom)
		e.users.EXPECT().RefundCredits(ctx, int64(7), _cost).Return(refundErr)
		_, err := e.ctrl.VerifyDomain(ctx, rq)
		assert.ErrorIs(t, err, errBoom)
		assert.ErrorIs(t, err, refundErr)
	})
	t.Run("failure not recorded", func(t *testing.T) {
		e := newEnv(t)
		e.pending()
		e.folder()
		e.dns.EXPECT().LookupCNAME(ctx, "www.example.com").Return("", dnscheck.ErrNoCNAME)
		e.domains.EXPECT().UpdateDomainCheck(ctx, int64(50), _now, nil, gomock.Any()).Return(errBoom)
		_, err := e.ctrl.VerifyDomain(ctx, rq)
		assert.ErrorIs(t, err, ErrNotVerified)
		assert.ErrorIs(t, err, errBoom)
	})
}

func TestDeleteDomain(t *testing.T) {
	ctx := context.Background()
	rq := &entity.DeleteDomainRequest{UserID: 7, FolderID: 1000, DomainID: 50}
	e := newEnv(t)
	e.pending()
	e.domains.EXPECT().DeleteDomain(ctx, int64(50)).Return(nil)
	require.NoError(t, e.ctrl.DeleteDomain(ctx, rq))

	e.pending()
	e.domains.EXPECT().DeleteDomain(ctx, int64(50)).Return(errBoom)
	assert.ErrorIs(t, e.ctrl.DeleteDomain(ctx, rq), errBoom)

	e.domains.EXPECT().FindDomainByID(ctx, int64(50)).Return(model.Domain{}, repository.ErrNotFound)
	assert.ErrorIs(t, e.ctrl.DeleteDomain(ctx, rq), ErrNotFound)
}

func TestGetDomainFolder(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	_, err := e.ctrl.GetDomainFolder(ctx, "localhost")
	assert.ErrorIs(t, err, ErrNotFound, "not a hostname: no lookup")

	e.domains.EXPECT().FindVerifiedDomainByHostname(ctx, "www.example.com").Return(model.Domain{FolderID: 1000}, nil)
	id, err := e.ctrl.GetDomainFolder(ctx, "WWW.example.com")
	require.NoError(t, err)
	assert.Equal(t, int64(1000), id)

	e.domains.EXPECT().FindVerifiedDomainByHostname(ctx, "www.example.com").Return(model.Domain{}, repository.ErrNotFound)
	_, err = e.ctrl.GetDomainFolder(ctx, "www.example.com")
	assert.ErrorIs(t, err, ErrNotFound)

	e.domains.EXPECT().FindVerifiedDomainByHostname(ctx, "www.example.com").Return(model.Domain{}, errBoom)
	_, err = e.ctrl.GetDomainFolder(ctx, "www.example.com")
	assert.ErrorIs(t, err, errBoom)
}

func TestVerifyPendingDomains(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	domain := func(id int64, host string) model.Domain {
		return model.Domain{ID: id, UserID: 7, FolderID: 1000, Hostname: host}
	}
	e.domains.EXPECT().ListPendingDomains(ctx, _now.Add(-PendingWindow), _verifyBatch).Return([]model.Domain{
		domain(1, "ok.example.com"), domain(2, "none.example.com"), domain(3, "broken.example.com"),
	}, nil)
	e.folders.EXPECT().FindFolderByID(ctx, int64(1000)).Return(model.Folder{ID: 1000, UserID: 7}, nil).Times(2)
	e.folders.EXPECT().FindFolderByID(ctx, int64(1000)).Return(model.Folder{}, errBoom)
	e.dns.EXPECT().LookupCNAME(ctx, "ok.example.com").Return(_target, nil)
	e.domains.EXPECT().FindVerifiedDomainByHostname(ctx, "ok.example.com").Return(model.Domain{}, repository.ErrNotFound)
	e.users.EXPECT().SpendCredits(ctx, int64(7), _cost).Return(nil)
	e.domains.EXPECT().UpdateDomainCheck(ctx, int64(1), _now, &_now, "").Return(nil)
	e.dns.EXPECT().LookupCNAME(ctx, "none.example.com").Return("", dnscheck.ErrNoCNAME)
	e.domains.EXPECT().UpdateDomainCheck(ctx, int64(2), _now, nil, gomock.Any()).Return(nil)

	rs, err := e.ctrl.VerifyPendingDomains(ctx)
	assert.ErrorIs(t, err, errBoom, "only unexpected failures are errors")
	assert.ErrorContains(t, err, "domain 3")
	assert.Equal(t, &entity.VerifyPendingDomainsResponse{Checked: 3, Verified: 1}, rs)
	assert.Equal(t, []string{"ok.example.com"}, e.verified)

	e.domains.EXPECT().ListPendingDomains(ctx, gomock.Any(), _verifyBatch).Return(nil, errBoom)
	rs, err = e.ctrl.VerifyPendingDomains(ctx)
	assert.ErrorIs(t, err, errBoom)
	assert.Zero(t, rs.Checked)
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "abc", truncate("abc", 3))
	assert.Equal(t, "ab", truncate("abc", 2))
}
