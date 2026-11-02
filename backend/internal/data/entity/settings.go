// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package entity

type (
	// Settings are the server settings root can change from the setup page.
	Settings struct {
		Domain   DomainSettings
		TLS      TLSSettings
		Credits  CreditSettings
		Database DatabaseSettings
		Storage  StorageSettings
		Files    FileSettings
		Auth     AuthSettings
	}

	// DomainSettings choose where the app and the dynamic subdomains are
	// served.
	DomainSettings struct {
		// AppURL is the public URL of the app, e.g. https://app.tmphost.io.
		// Its host is the app host, and OAuth callbacks are built on it.
		AppURL string
		// SitesDomain is the parent domain of the dynamic subdomains, e.g.
		// tmphost.io for f<11 characters>.tmphost.io. Empty disables them.
		SitesDomain string
	}

	// TLSSettings make the server terminate HTTPS itself, with Let's
	// Encrypt certificates it obtains and renews over the TLS-ALPN-01
	// challenge.
	TLSSettings struct {
		Enabled bool
		// Port is the HTTPS port; the app port then redirects to it.
		Port int
		// Email is the optional ACME account email.
		Email string
		Lego  LegoSettings
	}

	// LegoSettings serve the dynamic subdomains over HTTPS. Without a
	// Cloudflare API token each subdomain gets its own certificate on its
	// first visit; with one, a single *.<sites domain> wildcard is obtained
	// over the Cloudflare DNS-01 challenge.
	LegoSettings struct {
		Enabled            bool
		CloudflareAPIToken string
	}

	// CreditSettings configure the operation credit grant.
	CreditSettings struct {
		MonthlyGrant int
	}

	// DatabaseSettings choose and configure the database.
	DatabaseSettings struct {
		// Driver is "sqlite" or "postgres".
		Driver   string
		SQLite   SQLiteSettings
		Postgres PostgresSettings
	}

	// SQLiteSettings configure SQLite.
	SQLiteSettings struct {
		DSN string
	}

	// PostgresSettings configure PostgreSQL.
	PostgresSettings struct {
		// DSN is a postgres:// URL or a key=value connection string. It
		// usually holds the password, so it is a secret.
		DSN string
	}

	// StorageSettings choose and configure where uploaded files live.
	StorageSettings struct {
		// Driver is "local" or "s3".
		Driver string
		Local  LocalStorageSettings
		S3     S3Settings
	}

	// LocalStorageSettings configure storage on the server's disk.
	LocalStorageSettings struct {
		Path string
	}

	// S3Settings configure an S3-compatible bucket.
	S3Settings struct {
		Endpoint        string
		Region          string
		Bucket          string
		AccessKeyID     string
		SecretAccessKey string
		UsePathStyle    bool
	}

	// FileSettings limit uploads and choose how folders are served.
	FileSettings struct {
		// MaxTTLMinutes is the longest lifetime a folder can be given.
		MaxTTLMinutes int
		// MaxUploadMB caps the bytes a single upload request carries.
		MaxUploadMB int
		// AnonymousMaxTTLMinutes is the longest lifetime of a folder made
		// without signing in.
		AnonymousMaxTTLMinutes int
		// Subdomains serves each folder on its own host,
		// <folderID>-<userID>.<PublicDomain>; otherwise folders are served
		// at <base URL>/files/<userID>/<folderID>/.
		Subdomains bool
		// PublicDomain is the host[:port] folder hosts are subdomains of.
		PublicDomain string
	}

	// AuthSettings configure the sign-in providers.
	AuthSettings struct {
		Google OAuthClientSettings
		GitHub OAuthClientSettings
		// Providers are custom OpenID Connect providers.
		Providers []OAuthProviderSettings
	}

	// OAuthProviderSettings are a custom OpenID Connect sign-in provider.
	OAuthProviderSettings struct {
		// ID is the provider's route name, derived from Name.
		ID   string
		Name string
		// Icon is the SVG markup shown on the sign-in button.
		Icon         string
		ClientID     string
		ClientSecret string
		// AuthURL is the OpenID Connect issuer; the endpoints are discovered
		// from its /.well-known/openid-configuration.
		AuthURL string
	}

	// OAuthClientSettings are an OAuth app registration.
	OAuthClientSettings struct {
		ClientID     string
		ClientSecret string
	}

	// SecretsSet reports which secrets have a value, since secrets are never
	// returned.
	SecretsSet struct {
		PostgresDSN        bool
		S3SecretAccessKey  bool
		GoogleClientSecret bool
		GitHubClientSecret bool
		CloudflareAPIToken bool
		// ProviderClientSecrets is keyed by custom provider ID.
		ProviderClientSecrets map[string]bool
	}

	// RootLoginRequest carries a root access token.
	RootLoginRequest struct {
		Token string
	}

	// RootLoginResponse carries a root session.
	RootLoginResponse struct {
		SessionToken string
	}

	// AuthenticateRootRequest carries a root session token.
	AuthenticateRootRequest struct {
		SessionToken string
	}

	// GetSettingsResponse carries the settings with secrets removed.
	GetSettingsResponse struct {
		Settings   Settings
		SecretsSet SecretsSet
		// RestartRequired is true when saved settings differ from those the
		// server started with in a way that only a restart applies.
		RestartRequired bool
	}

	// UpdateSettingsRequest replaces the settings. Empty secrets keep their
	// current value.
	UpdateSettingsRequest struct {
		Settings Settings
	}

	// UpdateSettingsResponse is the saved state.
	UpdateSettingsResponse = GetSettingsResponse
)
