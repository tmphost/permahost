// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package domain implements custom domains: a user adds a hostname to one
// of their folders and points it, with a CNAME record, at the folder's own
// host (or the app host when folders have none). Once the record is found
// the domain is verified: credit.DomainSetupCredits and the first month,
// credit.DomainCredits, are charged at once, the hostname serves the
// folder, and it gets a TLS certificate on its first visit, or right away
// through OnVerified.
//
// Pending domains are checked in the background for PendingWindow, and on
// demand. Anyone may claim a pending hostname; it is verified only for the
// folder its CNAME names, and for one folder at a time.
package domain

//go:generate mockgen -source=domain.go -destination=mocks/domain.go -package=mocks

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/tmphost/permahost/backend/internal/controller/credit"
	folderctrl "github.com/tmphost/permahost/backend/internal/controller/folder"
	"github.com/tmphost/permahost/backend/internal/data/entity"
	"github.com/tmphost/permahost/backend/internal/data/model"
	mappermodel "github.com/tmphost/permahost/backend/internal/mapper/model"
	"github.com/tmphost/permahost/backend/internal/repository"
	"github.com/tmphost/permahost/backend/internal/service/dnscheck"
)

const (
	// PendingWindow is how long after it is added a pending domain is
	// checked in the background; after that only on demand.
	PendingWindow = 7 * 24 * time.Hour
	// VerifySchedule is the cron spec of the background check.
	VerifySchedule = "*/5 * * * *"
	// MaxHostnameLength is the longest hostname DNS allows.
	MaxHostnameLength = 253

	_verifyBatch = 100
)

// _labelRE is one hostname label: letters, digits and inner hyphens.
var _labelRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

var (
	// ErrNotFound is returned for a domain or folder that does not exist
	// or belongs to someone else.
	ErrNotFound = errors.New("not found")
	// ErrInvalidHostname is returned for a hostname that is malformed or
	// belongs to the service itself.
	ErrInvalidHostname = errors.New("invalid hostname")
	// ErrDomainTaken is returned for a hostname the folder already has, or
	// that is verified for another folder.
	ErrDomainTaken = errors.New("domain already in use")
	// ErrNotVerified is returned when the CNAME record is missing or names
	// another host.
	ErrNotVerified = errors.New("domain not verified")
	// ErrInsufficientCredits is returned when the user cannot pay for a
	// domain being verified.
	ErrInsufficientCredits = errors.New("insufficient credits")
)

type (
	// Controller manages custom domains.
	Controller interface {
		CreateDomain(ctx context.Context, rq *entity.CreateDomainRequest) (*entity.CreateDomainResponse, error)
		ListDomains(ctx context.Context, rq *entity.ListDomainsRequest) (*entity.ListDomainsResponse, error)
		// VerifyDomain checks a pending domain's CNAME now, and charges
		// for it when it is right. A verified domain is returned as is.
		VerifyDomain(ctx context.Context, rq *entity.VerifyDomainRequest) (*entity.VerifyDomainResponse, error)
		DeleteDomain(ctx context.Context, rq *entity.DeleteDomainRequest) error
		// GetDomainFolder returns the folder a verified hostname serves,
		// or ErrNotFound.
		GetDomainFolder(ctx context.Context, host string) (int64, error)
		// VerifyPendingDomains checks a batch of pending domains.
		VerifyPendingDomains(ctx context.Context) (*entity.VerifyPendingDomainsResponse, error)
	}

	// Params are the controller's dependencies.
	Params struct {
		Domains repository.DomainRepository
		Folders repository.FolderRepository
		Users   repository.UserRepository
		DNS     dnscheck.Service
		// AppHost is the app's hostname, the CNAME target when folders
		// have no host of their own.
		AppHost string
		// SitesDomain is the parent of the dynamic subdomains; no custom
		// domain may be below it.
		SitesDomain string
		// Settings returns the current file settings, which choose
		// whether folders have their own host.
		Settings func() entity.FileSettings
		// OnVerified is called with a hostname once it is verified; nil
		// does nothing.
		OnVerified func(host string)
		NewID      func() int64
		Now        func() time.Time
	}

	controller struct {
		domains     repository.DomainRepository
		folders     repository.FolderRepository
		users       repository.UserRepository
		dns         dnscheck.Service
		appHost     string
		sitesDomain string
		settings    func() entity.FileSettings
		onVerified  func(host string)
		newID       func() int64
		now         func() time.Time
	}
)

// New returns the domain controller.
func New(p Params) Controller {
	c := &controller{
		domains:     p.Domains,
		folders:     p.Folders,
		users:       p.Users,
		dns:         p.DNS,
		appHost:     strings.ToLower(p.AppHost),
		sitesDomain: strings.ToLower(p.SitesDomain),
		settings:    p.Settings,
		onVerified:  p.OnVerified,
		newID:       p.NewID,
		now:         p.Now,
	}
	if c.onVerified == nil {
		c.onVerified = func(string) {}
	}
	if c.now == nil {
		c.now = time.Now
	}
	return c
}

// NormalizeHostname lowercases a hostname and drops a trailing dot. It
// returns "" for anything that is not a hostname of at least two labels
// ending in a non-numeric label (so never an IP address).
func NormalizeHostname(host string) string {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if len(host) > MaxHostnameLength {
		return ""
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return ""
	}
	for _, l := range labels {
		if !_labelRE.MatchString(l) {
			return ""
		}
	}
	if strings.Trim(labels[len(labels)-1], "0123456789") == "" {
		return ""
	}
	return host
}

func (c *controller) CreateDomain(ctx context.Context, rq *entity.CreateDomainRequest) (*entity.CreateDomainResponse, error) {
	host := NormalizeHostname(rq.Hostname)
	if host == "" {
		return nil, fmt.Errorf("%w: %q is not a hostname such as www.example.com", ErrInvalidHostname, rq.Hostname)
	}
	if c.reserved(host) {
		return nil, fmt.Errorf("%w: %s belongs to this service", ErrInvalidHostname, host)
	}
	f, err := c.ownedFolder(ctx, rq.UserID, rq.FolderID)
	if err != nil {
		return nil, err
	}
	existing, err := c.domains.ListDomainsByFolder(ctx, f.ID)
	if err != nil {
		return nil, fmt.Errorf("list domains: %w", err)
	}
	for _, d := range existing {
		if d.Hostname == host {
			return nil, fmt.Errorf("%w: the folder already has %s", ErrDomainTaken, host)
		}
	}
	if err := c.checkFree(ctx, host, 0); err != nil {
		return nil, err
	}
	d, err := c.domains.CreateDomain(ctx, model.Domain{ID: c.newID(), CreatedAt: c.now().UTC(), UserID: rq.UserID, FolderID: f.ID, Hostname: host})
	if err != nil {
		return nil, fmt.Errorf("create domain: %w", err)
	}
	return &entity.CreateDomainResponse{Domain: c.domainEntity(d)}, nil
}

func (c *controller) ListDomains(ctx context.Context, rq *entity.ListDomainsRequest) (*entity.ListDomainsResponse, error) {
	f, err := c.ownedFolder(ctx, rq.UserID, rq.FolderID)
	if err != nil {
		return nil, err
	}
	domains, err := c.domains.ListDomainsByFolder(ctx, f.ID)
	if err != nil {
		return nil, fmt.Errorf("list domains: %w", err)
	}
	out := make([]entity.Domain, len(domains))
	for i, d := range domains {
		out[i] = c.domainEntity(d)
	}
	return &entity.ListDomainsResponse{Domains: out}, nil
}

func (c *controller) VerifyDomain(ctx context.Context, rq *entity.VerifyDomainRequest) (*entity.VerifyDomainResponse, error) {
	d, err := c.ownedDomain(ctx, rq.UserID, rq.FolderID, rq.DomainID)
	if err != nil {
		return nil, err
	}
	if d.VerifiedAt == nil {
		d, err = c.verify(ctx, d)
	}
	if err != nil {
		return nil, err
	}
	return &entity.VerifyDomainResponse{Domain: c.domainEntity(d)}, nil
}

func (c *controller) DeleteDomain(ctx context.Context, rq *entity.DeleteDomainRequest) error {
	d, err := c.ownedDomain(ctx, rq.UserID, rq.FolderID, rq.DomainID)
	if err != nil {
		return err
	}
	if err := c.domains.DeleteDomain(ctx, d.ID); err != nil {
		return fmt.Errorf("delete domain: %w", err)
	}
	return nil
}

func (c *controller) GetDomainFolder(ctx context.Context, host string) (int64, error) {
	host = NormalizeHostname(host)
	if host == "" {
		return 0, ErrNotFound
	}
	d, err := c.domains.FindVerifiedDomainByHostname(ctx, host)
	if errors.Is(err, repository.ErrNotFound) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("find domain: %w", err)
	}
	return d.FolderID, nil
}

func (c *controller) VerifyPendingDomains(ctx context.Context) (*entity.VerifyPendingDomainsResponse, error) {
	rs := &entity.VerifyPendingDomainsResponse{}
	domains, err := c.domains.ListPendingDomains(ctx, c.now().Add(-PendingWindow), _verifyBatch)
	if err != nil {
		return rs, fmt.Errorf("list pending domains: %w", err)
	}
	var errs []error
	// One failing domain must not hold up the others.
	for _, d := range domains {
		rs.Checked++
		_, err := c.verify(ctx, d)
		switch {
		case err == nil:
			rs.Verified++
		case errors.Is(err, ErrNotVerified), errors.Is(err, ErrDomainTaken), errors.Is(err, ErrInsufficientCredits), errors.Is(err, ErrNotFound):
			// Recorded on the domain for its owner to see.
		default:
			errs = append(errs, fmt.Errorf("domain %d: %w", d.ID, err))
		}
	}
	return rs, errors.Join(errs...)
}

// verify looks up a pending domain's CNAME, and when it names the folder's
// target, charges for the domain and marks it verified. Every outcome is
// recorded on the domain.
func (c *controller) verify(ctx context.Context, d model.Domain) (model.Domain, error) {
	now := c.now().UTC()
	f, err := c.folders.FindFolderByID(ctx, d.FolderID)
	if errors.Is(err, repository.ErrNotFound) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, fmt.Errorf("find folder: %w", err)
	}
	if err := c.lookup(ctx, d.Hostname, c.target(f.ID)); err != nil {
		return c.fail(ctx, d, now, err)
	}
	if err := c.checkFree(ctx, d.Hostname, d.ID); err != nil {
		return c.fail(ctx, d, now, err)
	}
	cost := credit.DomainSetupCredits + credit.DomainCredits
	err = c.users.SpendCredits(ctx, d.UserID, cost)
	if errors.Is(err, repository.ErrInsufficientCredits) {
		return c.fail(ctx, d, now, fmt.Errorf("%w: %d needed to set up the domain and host it this month", ErrInsufficientCredits, cost))
	}
	if err != nil {
		return d, fmt.Errorf("spend credits: %w", err)
	}
	if err := c.domains.UpdateDomainCheck(ctx, d.ID, now, &now, ""); err != nil {
		err = fmt.Errorf("record verification: %w", err)
		if rerr := c.users.RefundCredits(ctx, d.UserID, cost); rerr != nil {
			err = errors.Join(err, fmt.Errorf("refund credits: %w", rerr))
		}
		return d, err
	}
	d.CheckedAt, d.VerifiedAt, d.LastError = &now, &now, ""
	c.onVerified(d.Hostname)
	return d, nil
}

// lookup checks that host's CNAME record names target.
func (c *controller) lookup(ctx context.Context, host, target string) error {
	cname, err := c.dns.LookupCNAME(ctx, host)
	if errors.Is(err, dnscheck.ErrNoCNAME) {
		return fmt.Errorf("%w: %s has no CNAME record; add one pointing to %s", ErrNotVerified, host, target)
	}
	if err != nil {
		return fmt.Errorf("%w: looking up %s failed, try again later", ErrNotVerified, host)
	}
	if cname != target {
		return fmt.Errorf("%w: %s points to %s, not %s", ErrNotVerified, host, cname, target)
	}
	return nil
}

// fail records why a check failed, and returns that reason.
func (c *controller) fail(ctx context.Context, d model.Domain, now time.Time, reason error) (model.Domain, error) {
	d.CheckedAt, d.LastError = &now, truncate(reason.Error(), 255)
	if err := c.domains.UpdateDomainCheck(ctx, d.ID, now, nil, d.LastError); err != nil {
		return d, errors.Join(reason, fmt.Errorf("record check: %w", err))
	}
	return d, reason
}

// checkFree returns ErrDomainTaken when host is verified for a domain
// other than self.
func (c *controller) checkFree(ctx context.Context, host string, self int64) error {
	other, err := c.domains.FindVerifiedDomainByHostname(ctx, host)
	if errors.Is(err, repository.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("find domain: %w", err)
	}
	if other.ID != self {
		return fmt.Errorf("%w: %s is verified for another folder", ErrDomainTaken, host)
	}
	return nil
}

// reserved reports whether host is the app host, or the sites or public
// domain or below them.
func (c *controller) reserved(host string) bool {
	if host == c.appHost {
		return true
	}
	for _, parent := range []string{c.sitesDomain, strings.ToLower(c.settings().PublicDomain)} {
		if parent != "" && (host == parent || strings.HasSuffix(host, "."+parent)) {
			return true
		}
	}
	return false
}

// target is what a folder's custom domains must have a CNAME record for:
// the folder's own host, or the app host when folders have none.
func (c *controller) target(folderID int64) string {
	if s := c.settings(); s.Subdomains {
		return folderctrl.FolderHost(folderID) + "." + strings.ToLower(s.PublicDomain)
	}
	return c.appHost
}

// ownedFolder finds a folder of the user's that does not expire; only
// those can have custom domains.
func (c *controller) ownedFolder(ctx context.Context, userID, folderID int64) (model.Folder, error) {
	f, err := c.folders.FindFolderByID(ctx, folderID)
	if errors.Is(err, repository.ErrNotFound) {
		return model.Folder{}, ErrNotFound
	}
	if err != nil {
		return model.Folder{}, fmt.Errorf("find folder: %w", err)
	}
	if f.UserID != userID || f.ExpiresAt != nil {
		return model.Folder{}, ErrNotFound
	}
	return f, nil
}

func (c *controller) ownedDomain(ctx context.Context, userID, folderID, domainID int64) (model.Domain, error) {
	d, err := c.domains.FindDomainByID(ctx, domainID)
	if errors.Is(err, repository.ErrNotFound) {
		return model.Domain{}, ErrNotFound
	}
	if err != nil {
		return model.Domain{}, fmt.Errorf("find domain: %w", err)
	}
	if d.UserID != userID || d.FolderID != folderID {
		return model.Domain{}, ErrNotFound
	}
	return d, nil
}

func (c *controller) domainEntity(d model.Domain) entity.Domain {
	e := mappermodel.FromDomainModelToDomainEntity(d)
	e.CreatedAt, e.VerifiedAt, e.CheckedAt = e.CreatedAt.UTC(), e.VerifiedAt.UTC(), e.CheckedAt.UTC()
	e.Target = c.target(d.FolderID)
	return e
}

// truncate cuts s to at most n bytes.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
