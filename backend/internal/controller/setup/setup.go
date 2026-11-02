// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package setup implements the root-only setup page: root sign-in with the
// root access token, and reading and changing the server settings.
package setup

//go:generate mockgen -source=setup.go -destination=mocks/setup.go -package=mocks

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/tmphost/tmphost/backend/internal/data/entity"
	mappermodel "github.com/tmphost/tmphost/backend/internal/mapper/model"
	"github.com/tmphost/tmphost/backend/internal/repository"
	"github.com/tmphost/tmphost/backend/internal/service/token"
)

const (
	// DriverSQLite selects SQLite.
	DriverSQLite = "sqlite"
	// DriverPostgres selects PostgreSQL.
	DriverPostgres = "postgres"
	// StorageLocal keeps uploaded files on the server's disk.
	StorageLocal = "local"
	// StorageS3 keeps uploaded files in an S3-compatible bucket.
	StorageS3 = "s3"

	// DefaultTLSPort is the HTTPS port used when none is set.
	DefaultTLSPort = 443

	_maxMonthlyGrant = 1_000_000
	// About ten years; anything longer is a typo, not a plan.
	_maxTTLMinutes   = 10 * 365 * 24 * 60
	_maxUploadMB     = 1024
	_maxProviders    = 20
	_maxProviderName = 64
	_maxIconBytes    = 16 << 10
)

var (
	_hostnameRE = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)*[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	_notSlugRE  = regexp.MustCompile(`[^a-z0-9]+`)
	// _builtinProviders are the provider IDs custom providers cannot take.
	_builtinProviders = map[string]bool{"google": true, "github": true}
)

// _domain is a lowercase DNS name with an optional port.
var _domain = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)*[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(:[0-9]{1,5})?$`)

var (
	// ErrInvalidRootToken is returned for a wrong root access token.
	ErrInvalidRootToken = errors.New("invalid root access token")
	// ErrUnauthorized is returned for a missing or invalid root session.
	ErrUnauthorized = errors.New("unauthorized")
	// ErrInvalidSettings wraps a settings validation failure.
	ErrInvalidSettings = errors.New("invalid settings")
)

type (
	// Controller is the setup page business logic.
	Controller interface {
		Login(ctx context.Context, rq *entity.RootLoginRequest) (*entity.RootLoginResponse, error)
		Authenticate(ctx context.Context, rq *entity.AuthenticateRootRequest) error
		GetSettings(ctx context.Context) *entity.GetSettingsResponse
		UpdateSettings(ctx context.Context, rq *entity.UpdateSettingsRequest) (*entity.UpdateSettingsResponse, error)
		// Current returns the effective settings, secrets included. It is for
		// the server itself and never reaches the API.
		Current() entity.Settings
		// MonthlyGrant returns the current monthly credit grant.
		MonthlyGrant() int
		// FileLimits returns the current upload limits.
		FileLimits() entity.FileSettings
	}

	// Params are the controller's dependencies.
	Params struct {
		Repository repository.SettingsRepository
		Token      token.Service
		// RootAccessToken is the token that unlocks the setup page.
		RootAccessToken string
		// Defaults are the settings from the config file and environment,
		// used until settings are first saved.
		Defaults entity.Settings
	}

	controller struct {
		repo      repository.SettingsRepository
		token     token.Service
		rootHash  [sha256.Size]byte
		mu        sync.RWMutex
		current   entity.Settings
		startedAt entity.Settings
	}
)

// New loads the saved settings, falling back to the defaults.
func New(p Params) (Controller, error) {
	if p.RootAccessToken == "" {
		return nil, errors.New("setup: root access token is required")
	}
	current := normalize(p.Defaults)
	saved, err := p.Repository.LoadSettings()
	switch {
	case err == nil:
		current = withDefaults(normalize(mappermodel.FromSettingsModelToSettingsEntity(saved)), normalize(p.Defaults))
	case !errors.Is(err, repository.ErrNotFound):
		return nil, err
	}
	return &controller{
		repo:      p.Repository,
		token:     p.Token,
		rootHash:  sha256.Sum256([]byte(p.RootAccessToken)),
		current:   current,
		startedAt: current,
	}, nil
}

func (c *controller) Login(_ context.Context, rq *entity.RootLoginRequest) (*entity.RootLoginResponse, error) {
	// Compare fixed-size digests in constant time so neither the token nor
	// its length leaks through timing.
	got := sha256.Sum256([]byte(rq.Token))
	if subtle.ConstantTimeCompare(got[:], c.rootHash[:]) != 1 {
		return nil, ErrInvalidRootToken
	}
	session, err := c.token.CreateRootToken()
	if err != nil {
		return nil, fmt.Errorf("create root session: %w", err)
	}
	return &entity.RootLoginResponse{SessionToken: session}, nil
}

func (c *controller) Authenticate(_ context.Context, rq *entity.AuthenticateRootRequest) error {
	if rq.SessionToken == "" || c.token.ValidateRootToken(rq.SessionToken) != nil {
		return ErrUnauthorized
	}
	return nil
}

func (c *controller) GetSettings(_ context.Context) *entity.GetSettingsResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.response()
}

func (c *controller) UpdateSettings(_ context.Context, rq *entity.UpdateSettingsRequest) (*entity.UpdateSettingsResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	next := normalize(rq.Settings)
	keepSecrets(&next, c.current)
	if err := validate(next); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSettings, err)
	}
	if err := c.repo.SaveSettings(mappermodel.FromSettingsEntityToSettingsModel(next)); err != nil {
		return nil, fmt.Errorf("save settings: %w", err)
	}
	c.current = next
	return c.response(), nil
}

func (c *controller) Current() entity.Settings {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current
}

func (c *controller) MonthlyGrant() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current.Credits.MonthlyGrant
}

func (c *controller) FileLimits() entity.FileSettings {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.current.Files
}

// withDefaults fills in the sections that settings saved by an older
// version do not have.
func withDefaults(s, defaults entity.Settings) entity.Settings {
	// Settings saved before the app URL existed keep the configured one.
	if s.Domain.AppURL == "" {
		s.Domain.AppURL = defaults.Domain.AppURL
	}
	if s.Storage.Local.Path == "" {
		s.Storage.Local.Path = defaults.Storage.Local.Path
	}
	if s.Files.MaxTTLMinutes == 0 {
		s.Files.MaxTTLMinutes = defaults.Files.MaxTTLMinutes
	}
	if s.Files.MaxUploadMB == 0 {
		s.Files.MaxUploadMB = defaults.Files.MaxUploadMB
	}
	if s.Files.AnonymousMaxTTLMinutes == 0 {
		s.Files.AnonymousMaxTTLMinutes = defaults.Files.AnonymousMaxTTLMinutes
	}
	if s.Files.PublicDomain == "" {
		s.Files.PublicDomain = defaults.Files.PublicDomain
	}
	return s
}

// response must be called with c.mu held.
func (c *controller) response() *entity.GetSettingsResponse {
	public := c.current
	public.Database.Postgres.DSN = ""
	public.Storage.S3.SecretAccessKey = ""
	public.Auth.Google.ClientSecret = ""
	public.Auth.GitHub.ClientSecret = ""
	public.TLS.Lego.CloudflareAPIToken = ""
	// Copy before blanking, so the stored providers keep their secrets.
	public.Auth.Providers = append([]entity.OAuthProviderSettings(nil), c.current.Auth.Providers...)
	providerSecrets := make(map[string]bool, len(public.Auth.Providers))
	for i := range public.Auth.Providers {
		p := &public.Auth.Providers[i]
		providerSecrets[p.ID] = p.ClientSecret != ""
		p.ClientSecret = ""
	}
	return &entity.GetSettingsResponse{
		Settings: public,
		SecretsSet: entity.SecretsSet{
			PostgresDSN:           c.current.Database.Postgres.DSN != "",
			S3SecretAccessKey:     c.current.Storage.S3.SecretAccessKey != "",
			GoogleClientSecret:    c.current.Auth.Google.ClientSecret != "",
			GitHubClientSecret:    c.current.Auth.GitHub.ClientSecret != "",
			CloudflareAPIToken:    c.current.TLS.Lego.CloudflareAPIToken != "",
			ProviderClientSecrets: providerSecrets,
		},
		RestartRequired: restartRequired(c.startedAt, c.current),
	}
}

// restartRequired reports whether anything but the live-applied credit
// grant, maximum TTLs and public URL settings changed since the server
// started.
func restartRequired(started, current entity.Settings) bool {
	for _, s := range []*entity.Settings{&started, &current} {
		s.Credits = entity.CreditSettings{}
		s.Files = entity.FileSettings{MaxUploadMB: s.Files.MaxUploadMB}
	}
	return !reflect.DeepEqual(started, current)
}

// keepSecrets keeps the current value of every secret left blank. Clearing
// a provider's client ID disables it, so its secret goes too.
func keepSecrets(next *entity.Settings, current entity.Settings) {
	keep := func(dst *string, cur string) {
		if *dst == "" {
			*dst = cur
		}
	}
	keep(&next.Database.Postgres.DSN, current.Database.Postgres.DSN)
	keep(&next.Storage.S3.SecretAccessKey, current.Storage.S3.SecretAccessKey)
	for _, p := range []struct {
		next *entity.OAuthClientSettings
		cur  entity.OAuthClientSettings
	}{{&next.Auth.Google, current.Auth.Google}, {&next.Auth.GitHub, current.Auth.GitHub}} {
		if p.next.ClientID == "" {
			p.next.ClientSecret = ""
			continue
		}
		keep(&p.next.ClientSecret, p.cur.ClientSecret)
	}
	// A custom provider keeps the secret saved under the same ID.
	saved := make(map[string]string, len(current.Auth.Providers))
	for _, p := range current.Auth.Providers {
		saved[p.ID] = p.ClientSecret
	}
	for i := range next.Auth.Providers {
		p := &next.Auth.Providers[i]
		keep(&p.ClientSecret, saved[p.ID])
	}
	keep(&next.TLS.Lego.CloudflareAPIToken, current.TLS.Lego.CloudflareAPIToken)
}

func normalize(s entity.Settings) entity.Settings {
	t := strings.TrimSpace
	s.Database.Driver = strings.ToLower(t(s.Database.Driver))
	s.Database.SQLite.DSN = t(s.Database.SQLite.DSN)
	s.Database.Postgres.DSN = t(s.Database.Postgres.DSN)
	s.Storage.Driver = strings.ToLower(t(s.Storage.Driver))
	s.Storage.Local.Path = t(s.Storage.Local.Path)
	s.Files.PublicDomain = strings.ToLower(t(s.Files.PublicDomain))
	s3 := &s.Storage.S3
	s3.Endpoint, s3.Region, s3.Bucket, s3.AccessKeyID = t(s3.Endpoint), t(s3.Region), t(s3.Bucket), t(s3.AccessKeyID)
	s.Auth.Google.ClientID = t(s.Auth.Google.ClientID)
	s.Auth.GitHub.ClientID = t(s.Auth.GitHub.ClientID)
	s.Domain.AppURL = normalizeAppURL(t(s.Domain.AppURL))
	s.Domain.SitesDomain = strings.Trim(strings.TrimPrefix(strings.ToLower(t(s.Domain.SitesDomain)), "*."), ".")
	if s.TLS.Port == 0 {
		s.TLS.Port = DefaultTLSPort
	}
	s.TLS.Email = t(s.TLS.Email)
	s.TLS.Lego.CloudflareAPIToken = t(s.TLS.Lego.CloudflareAPIToken)
	// Normalize a copy, so the caller's slice is left as it was. No
	// providers is always nil, so settings compare equal either way.
	s.Auth.Providers = append([]entity.OAuthProviderSettings(nil), s.Auth.Providers...)
	for i := range s.Auth.Providers {
		p := &s.Auth.Providers[i]
		p.Name, p.Icon, p.ClientID = t(p.Name), t(p.Icon), t(p.ClientID)
		p.ClientSecret, p.AuthURL = t(p.ClientSecret), strings.TrimRight(t(p.AuthURL), "/")
		p.ID = ProviderID(p.Name)
	}
	return s
}

// ProviderID turns a provider name into its route name: lowercase letters
// and digits, with runs of anything else as single dashes.
func ProviderID(name string) string {
	return strings.Trim(_notSlugRE.ReplaceAllString(strings.ToLower(name), "-"), "-")
}

// normalizeAppURL lowercases the scheme and host and drops a trailing
// slash. A URL that does not parse is left for validate to reject.
func normalizeAppURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	u.Scheme, u.Host = strings.ToLower(u.Scheme), strings.ToLower(u.Host)
	if u.Path == "/" {
		u.Path = ""
	}
	return u.String()
}

// AppHost is the host name (no port) of an app URL, or "" when it has none.
func AppHost(appURL string) string {
	u, err := url.Parse(appURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func validate(s entity.Settings) error {
	if err := validateDomain(s.Domain); err != nil {
		return err
	}
	if err := validateTLS(s); err != nil {
		return err
	}
	if err := validateProviders(s.Auth.Providers); err != nil {
		return err
	}
	if s.Credits.MonthlyGrant < 0 || s.Credits.MonthlyGrant > _maxMonthlyGrant {
		return fmt.Errorf("monthly credits must be between 0 and %d", _maxMonthlyGrant)
	}
	switch s.Database.Driver {
	case DriverSQLite:
		if s.Database.SQLite.DSN == "" {
			return errors.New("sqlite DSN is required")
		}
	case DriverPostgres:
		if s.Database.Postgres.DSN == "" {
			return errors.New("postgres DSN is required")
		}
		// Parsing only; it never connects. The error is not echoed because
		// it can quote the DSN, password included.
		if _, err := pgconn.ParseConfig(s.Database.Postgres.DSN); err != nil {
			return errors.New("postgres DSN is not a valid postgres:// URL or key=value connection string")
		}
	default:
		return fmt.Errorf("database driver must be %q or %q", DriverSQLite, DriverPostgres)
	}
	if err := validateStorage(s.Storage); err != nil {
		return err
	}
	if s.Files.MaxTTLMinutes < 1 || s.Files.MaxTTLMinutes > _maxTTLMinutes {
		return fmt.Errorf("max TTL must be between 1 and %d minutes", _maxTTLMinutes)
	}
	if s.Files.AnonymousMaxTTLMinutes < 1 || s.Files.AnonymousMaxTTLMinutes > s.Files.MaxTTLMinutes {
		return fmt.Errorf("max TTL without sign-in must be between 1 and %d minutes (the max TTL)", s.Files.MaxTTLMinutes)
	}
	if s.Files.MaxUploadMB < 1 || s.Files.MaxUploadMB > _maxUploadMB {
		return fmt.Errorf("max upload size must be between 1 and %d MB", _maxUploadMB)
	}
	if s.Files.Subdomains && s.Files.PublicDomain == "" {
		return errors.New("public domain is required for subdomains")
	}
	if s.Files.PublicDomain != "" && !_domain.MatchString(s.Files.PublicDomain) {
		return errors.New("public domain must be a host name, such as tmphost.io, with an optional port")
	}
	if (s.Auth.Google.ClientID == "") != (s.Auth.Google.ClientSecret == "") {
		return errors.New("google needs both client ID and secret, or neither")
	}
	if (s.Auth.GitHub.ClientID == "") != (s.Auth.GitHub.ClientSecret == "") {
		return errors.New("github needs both client ID and secret, or neither")
	}
	return nil
}

func validateDomain(d entity.DomainSettings) error {
	u, err := url.Parse(d.AppURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" ||
		u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return errors.New("app URL must be an http(s) URL with no path, e.g. https://app.tmphost.io")
	}
	if d.SitesDomain != "" && !_hostnameRE.MatchString(d.SitesDomain) {
		return errors.New("sites domain must be a domain name, e.g. tmphost.io")
	}
	return nil
}

func validateTLS(s entity.Settings) error {
	t := s.TLS
	if !t.Enabled {
		return nil
	}
	if !strings.HasPrefix(s.Domain.AppURL, "https://") {
		return errors.New("with TLS enabled the app URL must be https://")
	}
	if net.ParseIP(AppHost(s.Domain.AppURL)) != nil || !strings.Contains(AppHost(s.Domain.AppURL), ".") {
		return errors.New("with TLS enabled the app URL needs a public domain name; Let's Encrypt does not issue for IPs or localhost")
	}
	if t.Port < 1 || t.Port > 65535 {
		return errors.New("TLS port must be between 1 and 65535")
	}
	if t.Email != "" {
		if _, err := mail.ParseAddress(t.Email); err != nil {
			return errors.New("the Let's Encrypt email is not a valid email address")
		}
	}
	if t.Lego.Enabled && s.Domain.SitesDomain == "" {
		return errors.New("dynamic subdomain certificates need a sites domain")
	}
	return nil
}

func validateProviders(ps []entity.OAuthProviderSettings) error {
	if len(ps) > _maxProviders {
		return fmt.Errorf("at most %d custom sign-in providers", _maxProviders)
	}
	seen := make(map[string]bool, len(ps))
	for _, p := range ps {
		if p.ID == "" || len(p.Name) > _maxProviderName {
			return fmt.Errorf("sign-in provider names need a letter or digit and at most %d characters", _maxProviderName)
		}
		if _builtinProviders[p.ID] || seen[p.ID] {
			return fmt.Errorf("sign-in provider %q is already taken", p.ID)
		}
		seen[p.ID] = true
		icon := strings.ToLower(p.Icon)
		if len(p.Icon) > _maxIconBytes || !strings.Contains(icon, "<svg") || !strings.HasSuffix(icon, "</svg>") {
			return fmt.Errorf("sign-in provider %q needs an SVG icon of at most %d KB", p.ID, _maxIconBytes>>10)
		}
		if p.ClientID == "" || p.ClientSecret == "" {
			return fmt.Errorf("sign-in provider %q needs a client ID and secret", p.ID)
		}
		if !validAuthURL(p.AuthURL) {
			return fmt.Errorf("sign-in provider %q needs an https auth URL (its OpenID Connect issuer)", p.ID)
		}
	}
	return nil
}

// validAuthURL accepts https URLs, and plain http only on this machine.
func validAuthURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	switch h := u.Hostname(); u.Scheme {
	case "https":
		return true
	case "http":
		return h == "localhost" || h == "127.0.0.1"
	}
	return false
}

func validateStorage(s entity.StorageSettings) error {
	switch s.Driver {
	case StorageLocal:
		if s.Local.Path == "" {
			return errors.New("local storage path is required")
		}
	case StorageS3:
		if s.S3.Bucket == "" || s.S3.Region == "" {
			return errors.New("s3 bucket and region are required")
		}
		if s.S3.Endpoint != "" {
			u, err := url.Parse(s.S3.Endpoint)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return errors.New("s3 endpoint must be an http(s) URL")
			}
		}
	default:
		return fmt.Errorf("storage driver must be %q or %q", StorageLocal, StorageS3)
	}
	return nil
}
