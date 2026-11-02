// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package app wires the services, controllers and handlers into a server.
package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"gorm.io/gorm"

	authctrl "github.com/tmphost/tmphost/backend/internal/controller/auth"
	"github.com/tmphost/tmphost/backend/internal/controller/credit"
	folderctrl "github.com/tmphost/tmphost/backend/internal/controller/folder"
	oauthctrl "github.com/tmphost/tmphost/backend/internal/controller/oauthserver"
	setupctrl "github.com/tmphost/tmphost/backend/internal/controller/setup"
	"github.com/tmphost/tmphost/backend/internal/data/entity"
	"github.com/tmphost/tmphost/backend/internal/handler/api"
	mcphandler "github.com/tmphost/tmphost/backend/internal/handler/mcp"
	"github.com/tmphost/tmphost/backend/internal/repository"
	"github.com/tmphost/tmphost/backend/internal/service/encryption"
	"github.com/tmphost/tmphost/backend/internal/service/idgen"
	"github.com/tmphost/tmphost/backend/internal/service/oauth"
	"github.com/tmphost/tmphost/backend/internal/service/quota"
	"github.com/tmphost/tmphost/backend/internal/service/scheduler"
	"github.com/tmphost/tmphost/backend/internal/service/storage"
	"github.com/tmphost/tmphost/backend/internal/service/tlscert"
	"github.com/tmphost/tmphost/backend/internal/service/token"
)

const (
	_renewalTimeout = 10 * time.Minute
	_sweepTimeout   = 5 * time.Minute
	// MCPPath is where the MCP server is mounted.
	MCPPath = "/mcp"
)

// _listenConfig is how every server listens; main logs the address.
var _listenConfig = fiber.ListenConfig{DisableStartupMessage: true}

type (
	// Config is the server configuration, one field per config section.
	Config struct {
		// MasterKey encrypts the settings file. It comes only from the
		// TMPHOSTIO_MASTER_KEY environment variable, never from YAML.
		MasterKey string
		App       AppConfig
		TLS       TLSConfig
		Auth      AuthConfig
		Credits   CreditsConfig
		SQLite    SQLiteConfig
		Postgres  PostgresConfig
		Storage   StorageConfig
		S3        S3Config
		Files     FilesConfig
		IDGen     IDGenConfig
	}

	// AppConfig is the "app" section.
	AppConfig struct {
		Port int `yaml:"port"`
		// BaseURL is the public URL of the app. Its host is the app host,
		// and OAuth callbacks are built on it.
		BaseURL string `yaml:"baseUrl"`
		// SitesDomain is the parent domain of the dynamic subdomains
		// (f<11 characters>.<SitesDomain>); empty disables them.
		SitesDomain string `yaml:"sitesDomain"`
		// BasePath is the path prefix browsers reach the app under when a
		// proxy strips it before forwarding ("" or "/x").
		BasePath     string `yaml:"basePath"`
		CookieSecure bool   `yaml:"cookieSecure"`
		// SettingsFile is where the setup page saves settings. When it
		// exists, it overrides the matching config values.
		SettingsFile string `yaml:"settingsFile"`
		// TrustedProxies is a comma-separated list of proxy IPs or CIDR
		// ranges. Requests from them are attributed to the client IP in
		// ProxyHeader, which the per-IP limits of anonymous uploads count.
		TrustedProxies string `yaml:"trustedProxies"`
		// ProxyHeader is the header a trusted proxy puts the client IP in.
		ProxyHeader string `yaml:"proxyHeader"`
	}

	// TLSConfig is the "tls" section: HTTPS with Let's Encrypt
	// certificates, obtained and renewed automatically.
	TLSConfig struct {
		Enabled bool `yaml:"enabled"`
		// Port is the HTTPS port; the app port then redirects to it.
		Port int `yaml:"port"`
		// Email is the optional ACME account email.
		Email string     `yaml:"email"`
		Lego  LegoConfig `yaml:"lego"`
	}

	// LegoConfig is the "tls.lego" section: certificates for the dynamic
	// subdomains, one per subdomain, or a wildcard over Cloudflare DNS-01
	// when the token is set.
	LegoConfig struct {
		Enabled            bool   `yaml:"enabled"`
		CloudflareAPIToken string `yaml:"cloudflareApiToken"`
	}

	// AuthConfig is the "auth" section.
	AuthConfig struct {
		JWTSecret string `yaml:"jwtSecret"`
		// RootAccessToken unlocks the setup page. A random one is generated
		// (and logged) at startup when it is empty.
		RootAccessToken string         `yaml:"rootAccessToken"`
		Google          OAuthAppConfig `yaml:"google"`
		GitHub          OAuthAppConfig `yaml:"github"`
		// Providers are the custom OpenID Connect providers, set only on
		// the setup page.
		Providers []entity.OAuthProviderSettings `yaml:"-"`
	}

	// OAuthAppConfig is an OAuth client registration. A provider without a
	// client ID is disabled.
	OAuthAppConfig struct {
		ClientID     string `yaml:"clientId"`
		ClientSecret string `yaml:"clientSecret"`
	}

	// CreditsConfig is the "credits" section.
	CreditsConfig struct {
		MonthlyGrant int `yaml:"monthlyGrant"`
	}

	// PoolConfig tunes a database connection pool.
	PoolConfig struct {
		MaxIdleConns    int           `yaml:"maxIdleConns"`
		MaxOpenConns    int           `yaml:"maxOpenConns"`
		MaxConnLifetime time.Duration `yaml:"maxConnLifetime"`
	}

	// SQLiteConfig is the "sqlite" section.
	SQLiteConfig struct {
		Enabled    bool   `yaml:"enabled"`
		DSN        string `yaml:"dsn"`
		PoolConfig `yaml:",inline"`
	}

	// PostgresConfig is the "postgres" section. PostgreSQL wins when both
	// databases are enabled.
	PostgresConfig struct {
		Enabled bool `yaml:"enabled"`
		// DSN is a postgres:// URL or a key=value connection string.
		DSN        string `yaml:"dsn"`
		PoolConfig `yaml:",inline"`
	}

	// StorageConfig is the "storage" section.
	StorageConfig struct {
		// Driver is "local" or "s3".
		Driver    string `yaml:"driver"`
		LocalPath string `yaml:"localPath"`
	}

	// S3Config is the "s3" section (an S3-compatible bucket).
	S3Config struct {
		Endpoint        string `yaml:"endpoint"`
		Region          string `yaml:"region"`
		Bucket          string `yaml:"bucket"`
		AccessKeyID     string `yaml:"accessKeyId"`
		SecretAccessKey string `yaml:"secretAccessKey"`
		UsePathStyle    bool   `yaml:"usePathStyle"`
	}

	// FilesConfig is the "files" section.
	FilesConfig struct {
		MaxTTLMinutes          int `yaml:"maxTtlMinutes"`
		MaxUploadMB            int `yaml:"maxUploadMb"`
		AnonymousMaxTTLMinutes int `yaml:"anonymousMaxTtlMinutes"`
		// Subdomains serves each folder on its own host,
		// <folderID>-<userID>.<PublicDomain>, instead of below BaseURL.
		Subdomains bool `yaml:"subdomains"`
		// PublicDomain defaults to the BaseURL host.
		PublicDomain string `yaml:"publicDomain"`
	}

	// IDGenConfig is the "idgen" section.
	IDGenConfig struct {
		Node uint16 `yaml:"node"`
	}

	// App is a configured, runnable server.
	App struct {
		fiber     *fiber.App
		addr      string
		db        *gorm.DB
		credits   credit.Controller
		folders   folderctrl.Controller
		scheduler scheduler.Service
		// The rest is set only with TLS enabled.
		tlsAddr  string
		certs    tlscert.Service
		tlsPort  int
		redirect *fiber.App
	}
)

// New builds the server. web holds the built frontend; it may be empty.
func New(cfg Config, web fs.FS) (*App, error) {
	tokens, err := token.New(token.Config{Secret: cfg.Auth.JWTSecret})
	if err != nil {
		return nil, err
	}
	enc, err := encryption.New(cfg.MasterKey)
	if err != nil {
		return nil, err
	}
	setup, err := setupctrl.New(setupctrl.Params{
		Repository:      repository.NewSettingsRepository(settingsFile(cfg), enc),
		Token:           tokens,
		RootAccessToken: rootAccessToken(cfg),
		Defaults:        settingsFromConfig(cfg),
	})
	if err != nil {
		return nil, err
	}
	// Saved settings override the config file and environment.
	cfg = applySettings(cfg, setup.Current())

	store, err := openStorage(cfg)
	if err != nil {
		return nil, err
	}
	db, err := openDatabase(cfg)
	if err != nil {
		return nil, err
	}

	users := repository.NewUserRepository(db)
	ids := idgen.New(cfg.IDGen.Node)
	basePath := strings.TrimRight(cfg.App.BasePath, "/")
	baseURL := strings.TrimRight(cfg.App.BaseURL, "/")
	auth := authctrl.New(authctrl.Params{
		Providers: providers(cfg),
		Token:     tokens,
		Users:     users,
		IDGen:     ids,
		Grant:     setup.MonthlyGrant,
		BasePath:  basePath,
		BaseURL:   baseURL,
	})
	folders := folderctrl.New(folderctrl.Params{
		Folders:   repository.NewFolderRepository(db),
		Users:     users,
		Storage:   store,
		Anonymous: quota.New(quota.Params{Limit: folderctrl.AnonymousOps, Window: folderctrl.AnonymousWindow}),
		Limits:    setup.FileLimits,
		BaseURL:   baseURL,
	})
	// Uploads reach the MCP server base64-encoded, a third bigger. The
	// folder controller enforces the real limit on the decoded bytes.
	bodyLimit := cfg.Files.MaxUploadMB<<20*4/3 + 1<<20

	h := newHosts(cfg.App.BaseURL, cfg.App.SitesDomain)
	f := fiber.New(fiberConfig(cfg.App, bodyLimit))
	f.Use(recover.New())
	api.New(api.Params{
		Router:       f,
		Hosts:        hostRouter(h),
		Auth:         auth,
		Setup:        setup,
		Folders:      folders,
		OAuth:        oauthctrl.New(oauthctrl.Params{Token: tokens, Users: users, Issuer: baseURL, Resource: baseURL + MCPPath}),
		FileSettings: setup.FileLimits,
		BasePath:     basePath,
		// A session cookie for an https app must never travel over plain
		// http.
		CookieSecure: cfg.App.CookieSecure || strings.HasPrefix(cfg.App.BaseURL, "https://"),
	})
	f.All(MCPPath, adaptor.HTTPHandler(mcphandler.New(mcphandler.Params{
		Folders:             folders,
		Auth:                auth,
		MaxBodyBytes:        int64(bodyLimit),
		ResourceMetadataURL: baseURL + api.ResourceMetadataPath + MCPPath,
	})))
	f.Get("/*", spaHandler(web, basePath))

	a := &App{
		fiber:     f,
		addr:      net.JoinHostPort("", strconv.Itoa(cfg.App.Port)),
		db:        db,
		credits:   credit.New(credit.Params{Users: users, Grant: setup.MonthlyGrant}),
		folders:   folders,
		scheduler: scheduler.New(),
	}
	// Both are valid constant specs, so these cannot fail.
	_ = a.scheduler.AddFunc(credit.RenewalSchedule, a.renewCredits)
	_ = a.scheduler.AddFunc(folderctrl.SweepSchedule, a.sweepFolders)
	if cfg.TLS.Enabled {
		a.setupTLS(cfg, enc, h)
	}
	return a, nil
}

// fiberConfig is the app server's config. With trusted proxies, c.IP() is
// the client IP that they forward in the proxy header.
func fiberConfig(app AppConfig, bodyLimit int) fiber.Config {
	cfg := fiber.Config{BodyLimit: bodyLimit}
	var proxies []string
	for p := range strings.SplitSeq(app.TrustedProxies, ",") {
		if p = strings.TrimSpace(p); p != "" {
			proxies = append(proxies, p)
		}
	}
	if len(proxies) == 0 {
		return cfg
	}
	cfg.TrustProxy = true
	cfg.TrustProxyConfig = fiber.TrustProxyConfig{Proxies: proxies}
	cfg.ProxyHeader = app.ProxyHeader
	if cfg.ProxyHeader == "" {
		cfg.ProxyHeader = fiber.HeaderXForwardedFor
	}
	// The header may list several hops; c.IP() then takes the rightmost IP
	// that is not a trusted proxy, so a client can't spoof its own.
	cfg.EnableIPValidation = true
	return cfg
}

// Handler exposes the HTTP handler, for tests.
func (a *App) Handler() *fiber.App { return a.fiber }

// Listen starts the background jobs and serves until Shutdown is called.
// With TLS enabled the app serves HTTPS, and the app port redirects to it.
func (a *App) Listen() error {
	// Catch up on a renewal missed while the server was down.
	go a.renewCredits()
	a.scheduler.Start()
	if a.certs == nil {
		return a.fiber.Listen(a.addr, _listenConfig)
	}
	return a.listenTLS()
}

// Shutdown stops the server and the jobs gracefully, then closes the
// database.
func (a *App) Shutdown(ctx context.Context) error {
	err := a.fiber.ShutdownWithContext(ctx)
	if a.certs != nil {
		err = errors.Join(err, a.redirect.ShutdownWithContext(ctx))
		a.certs.Stop()
	}
	a.scheduler.Stop()
	return errors.Join(err, a.closeDB())
}

func (a *App) closeDB() error {
	// The dialectors are backed by *sql.DB, so DB() cannot fail.
	sqlDB, _ := a.db.DB()
	return sqlDB.Close()
}

func (a *App) renewCredits() {
	ctx, cancel := context.WithTimeout(context.Background(), _renewalTimeout)
	defer cancel()
	rs, err := a.credits.RenewDueCredits(ctx)
	if err != nil {
		log.Printf("credit renewal: renewed %d before failing: %v", rs.Renewed, err)
		return
	}
	log.Printf("credit renewal: renewed %d accounts", rs.Renewed)
}

func (a *App) sweepFolders() {
	ctx, cancel := context.WithTimeout(context.Background(), _sweepTimeout)
	defer cancel()
	rs, err := a.folders.DeleteExpiredFolders(ctx)
	if err != nil {
		log.Printf("folder sweep: deleted %d, failed: %v", rs.Deleted, err)
		return
	}
	if rs.Deleted > 0 {
		log.Printf("folder sweep: deleted %d expired folders", rs.Deleted)
	}
}

func openStorage(cfg Config) (storage.Service, error) {
	switch cfg.Storage.Driver {
	case setupctrl.StorageLocal:
		return storage.NewLocal(cfg.Storage.LocalPath)
	case setupctrl.StorageS3:
		s3 := cfg.S3
		return storage.NewS3(context.Background(), storage.S3Config{
			Endpoint: s3.Endpoint, Region: s3.Region, Bucket: s3.Bucket,
			AccessKeyID: s3.AccessKeyID, SecretAccessKey: s3.SecretAccessKey, UsePathStyle: s3.UsePathStyle,
		})
	default:
		return nil, fmt.Errorf("storage: unknown driver %q", cfg.Storage.Driver)
	}
}

func providers(cfg Config) []oauth.Provider {
	callback := func(name string) string {
		return strings.TrimRight(cfg.App.BaseURL, "/") + api.AuthPrefix + "/" + name + "/callback"
	}
	var out []oauth.Provider
	if g := cfg.Auth.Google; g.ClientID != "" {
		out = append(out, oauth.NewGoogle(oauth.Credentials{ClientID: g.ClientID, ClientSecret: g.ClientSecret, RedirectURL: callback(oauth.ProviderGoogle)}))
	}
	if g := cfg.Auth.GitHub; g.ClientID != "" {
		out = append(out, oauth.NewGitHub(oauth.Credentials{ClientID: g.ClientID, ClientSecret: g.ClientSecret, RedirectURL: callback(oauth.ProviderGitHub)}))
	}
	for _, p := range cfg.Auth.Providers {
		out = append(out, oauth.NewOIDC(oauth.OIDCParams{
			Name: p.ID, Label: p.Name, Icon: p.Icon, Issuer: p.AuthURL,
			Credentials: oauth.Credentials{ClientID: p.ClientID, ClientSecret: p.ClientSecret, RedirectURL: callback(p.ID)},
		}))
	}
	return out
}
