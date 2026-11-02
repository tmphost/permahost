// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package tlscert serves Let's Encrypt certificates, obtained and renewed
// with lego: one for the app host, and for the dynamic subdomains either one
// per subdomain on its first visit, or a single wildcard when a Cloudflare
// API token is set.
//
// Certificates for a host are proven over the TLS-ALPN-01 challenge, which
// Let's Encrypt runs against this server's HTTPS port, so neither port 80 nor
// DNS access is needed. A wildcard can only be proven over DNS-01, hence the
// Cloudflare token for it.
package tlscert

//go:generate mockgen -source=tlscert.go -destination=mocks/tlscert.go -package=mocks

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	mathrand "math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge/tlsalpn01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/providers/dns/cloudflare"
	"github.com/go-acme/lego/v4/registration"
	"golang.org/x/sync/singleflight"

	"github.com/tmphost/tmphost/backend/internal/service/encryption"
)

const (
	// ACMEProtocol is the ALPN protocol of the TLS-ALPN-01 challenge; the
	// TLS listener must offer it.
	ACMEProtocol = tlsalpn01.ACMETLS1Protocol

	// RenewBefore is how long before expiry a certificate is replaced.
	RenewBefore = 30 * 24 * time.Hour

	// A subdomain whose certificate could not be obtained is not retried
	// for this long, so failing handshakes cannot hammer Let's Encrypt.
	_failureBackoff = 10 * time.Minute

	// Certificates are checked daily, plus a random 1 to 5 minutes so
	// instances sharing a cache do not all check at once.
	_checkEvery     = 24 * time.Hour
	_checkJitterMin = time.Minute
	_checkJitterMax = 5 * time.Minute

	_certExt = ".cert"
)

// acmeDirectoryURL is the CA certificates are obtained from.
var acmeDirectoryURL = lego.LEDirectoryProduction

type (
	// Config chooses the certificates to obtain.
	Config struct {
		// AppHost is the app's host name; it always gets a certificate.
		AppHost string
		// Email is the optional ACME account email.
		Email string
		Sites SitesConfig
		// CacheDir keeps the certificates, encrypted, across restarts.
		CacheDir string
	}

	// SitesConfig covers the dynamic subdomains of Domain.
	SitesConfig struct {
		Enabled bool
		Domain  string
		// CloudflareAPIToken, when set, obtains one *.Domain wildcard over
		// DNS-01 instead of a certificate per subdomain.
		CloudflareAPIToken string
		// Allow reports whether a host may get a certificate on its first
		// visit. Only hosts it allows are ever sent to Let's Encrypt.
		Allow func(host string) bool
	}

	// Service picks the certificate for each TLS handshake.
	Service interface {
		// GetCertificate is a tls.Config.GetCertificate.
		GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error)
		// Start loads the cached certificates, then obtains and renews them
		// in the background.
		Start()
		// Stop ends the renewal loop.
		Stop()
	}

	certObtainer interface {
		Obtain(request certificate.ObtainRequest) (*certificate.Resource, error)
	}

	service struct {
		cfg Config
		enc encryption.Service

		mu sync.RWMutex
		// certs are keyed by the name they were obtained for (a host, or
		// "*.domain").
		certs map[string]*tls.Certificate
		// challenges are the TLS-ALPN-01 certificates being validated.
		challenges map[string]*tls.Certificate
		// failed holds when obtaining a subdomain's certificate last failed.
		failed map[string]time.Time

		// obtainMu serializes talking to the CA.
		obtainMu sync.Mutex
		user     *legoUser
		// obtainers are built on first use, keyed by wildcard (DNS-01) or
		// not (TLS-ALPN-01).
		obtainers   map[bool]certObtainer
		newObtainer func(wildcard bool) (certObtainer, error)
		onDemand    singleflight.Group
		now         func() time.Time

		stop     chan struct{}
		stopOnce sync.Once
		done     chan struct{}
	}

	// cachedPair is the plaintext of a cache file.
	cachedPair struct {
		Certificate string `json:"certificate"`
		PrivateKey  string `json:"privateKey"`
	}

	legoUser struct {
		email        string
		registration *registration.Resource
		key          crypto.PrivateKey
	}
)

func (u *legoUser) GetEmail() string                        { return u.email }
func (u *legoUser) GetRegistration() *registration.Resource { return u.registration }
func (u *legoUser) GetPrivateKey() crypto.PrivateKey        { return u.key }

// New returns the certificate service. enc seals the cached certificates.
func New(cfg Config, enc encryption.Service) Service {
	cfg.AppHost = strings.ToLower(cfg.AppHost)
	cfg.Sites.Domain = strings.ToLower(cfg.Sites.Domain)
	s := &service{
		cfg:        cfg,
		enc:        enc,
		certs:      map[string]*tls.Certificate{},
		challenges: map[string]*tls.Certificate{},
		failed:     map[string]time.Time{},
		obtainers:  map[bool]certObtainer{},
		now:        time.Now,
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
	}
	s.newObtainer = s.newACMEObtainer
	return s
}

func (s *service) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	name := strings.ToLower(hello.ServerName)
	if slices.Contains(hello.SupportedProtos, ACMEProtocol) {
		return s.challenge(name)
	}
	if name == "" {
		// Without SNI (e.g. a client dialing an IP) the app is meant.
		name = s.cfg.AppHost
	}
	if cert := s.cached(name); cert != nil {
		return cert, nil
	}
	if !s.perSiteCertificates() || !s.cfg.Sites.Allow(name) {
		return nil, fmt.Errorf("tls: no certificate for %q", name)
	}
	cert, err, _ := s.onDemand.Do(name, func() (any, error) { return s.obtainOnDemand(name) })
	if err != nil {
		return nil, err
	}
	return cert.(*tls.Certificate), nil
}

// cached returns the certificate obtained for name, or the wildcard that
// covers it.
func (s *service) cached(name string) *tls.Certificate {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if cert := s.certs[name]; cert != nil {
		return cert
	}
	if _, parent, ok := strings.Cut(name, "."); ok {
		if cert := s.certs["*."+parent]; cert != nil {
			return cert
		}
	}
	return nil
}

func (s *service) challenge(name string) (*tls.Certificate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if cert := s.challenges[name]; cert != nil {
		return cert, nil
	}
	return nil, fmt.Errorf("tls: no ACME challenge for %q", name)
}

// Present serves the TLS-ALPN-01 challenge for domain; it makes the service
// lego's challenge provider.
func (s *service) Present(domain, _, keyAuth string) error {
	// ChallengeCert only fails when crypto/rand does; a nil certificate
	// then just fails the validation.
	cert, _ := tlsalpn01.ChallengeCert(domain, keyAuth)
	s.mu.Lock()
	s.challenges[domain] = cert
	s.mu.Unlock()
	return nil
}

// CleanUp stops serving the challenge for domain.
func (s *service) CleanUp(domain, _, _ string) error {
	s.mu.Lock()
	delete(s.challenges, domain)
	s.mu.Unlock()
	return nil
}

func (s *service) perSiteCertificates() bool {
	return s.cfg.Sites.Enabled && s.cfg.Sites.CloudflareAPIToken == ""
}

// obtainOnDemand gets a subdomain's certificate during its first handshake,
// unless the last attempt failed recently.
func (s *service) obtainOnDemand(name string) (*tls.Certificate, error) {
	s.mu.RLock()
	failedAt, failed := s.failed[name]
	s.mu.RUnlock()
	if failed && s.now().Sub(failedAt) < _failureBackoff {
		return nil, fmt.Errorf("tls: obtaining a certificate for %q failed recently", name)
	}
	cert, err := s.obtainAndInstall(name)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.failed[name] = s.now()
		return nil, err
	}
	delete(s.failed, name)
	return cert, nil
}

func (s *service) Start() {
	s.loadCache()
	go s.manage()
}

func (s *service) Stop() {
	s.stopOnce.Do(func() { close(s.stop) })
	<-s.done
}

// manage checks the certificates at once, then daily.
func (s *service) manage() {
	defer close(s.done)
	for {
		s.refresh()
		select {
		case <-s.stop:
			return
		case <-time.After(checkInterval()):
		}
	}
}

func checkInterval() time.Duration {
	return _checkEvery + _checkJitterMin + mathrand.N(_checkJitterMax-_checkJitterMin+1)
}

// names are the certificates to keep: the app host, the wildcard when one is
// used, and every subdomain that already has one.
func (s *service) names() []string {
	names := []string{s.cfg.AppHost}
	if s.cfg.Sites.Enabled && !s.perSiteCertificates() {
		names = append(names, "*."+s.cfg.Sites.Domain)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for name := range s.certs {
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}

// refresh obtains every certificate that is missing or due for renewal.
func (s *service) refresh() {
	for _, name := range s.names() {
		s.mu.RLock()
		cert := s.certs[name]
		s.mu.RUnlock()
		if usable(cert, s.now()) {
			continue
		}
		if _, err := s.obtainAndInstall(name); err != nil {
			log.Printf("tls: obtaining a certificate for %s failed: %v", name, err)
			continue
		}
		log.Printf("tls: certificate for %s installed", name)
	}
}

// usable reports whether cert is not due for renewal.
func usable(cert *tls.Certificate, now time.Time) bool {
	return cert != nil && cert.Leaf.NotAfter.Sub(now) >= RenewBefore
}

func (s *service) obtainAndInstall(name string) (*tls.Certificate, error) {
	res, err := s.obtain(name)
	if err != nil {
		return nil, err
	}
	return s.install(name, res.Certificate, res.PrivateKey)
}

// obtain asks the CA for name's certificate. The ACME clients are set up,
// and the account registered, only the first time they are needed.
func (s *service) obtain(name string) (*certificate.Resource, error) {
	s.obtainMu.Lock()
	defer s.obtainMu.Unlock()
	wildcard := strings.HasPrefix(name, "*.")
	obtainer := s.obtainers[wildcard]
	if obtainer == nil {
		var err error
		if obtainer, err = s.newObtainer(wildcard); err != nil {
			return nil, err
		}
		s.obtainers[wildcard] = obtainer
	}
	return obtainer.Obtain(certificate.ObtainRequest{Domains: []string{name}, Bundle: true})
}

// newACMEObtainer builds a lego client proving names over TLS-ALPN-01, or
// over Cloudflare DNS-01 for a wildcard. Both share one ACME account. It
// must be called with obtainMu held.
func (s *service) newACMEObtainer(wildcard bool) (certObtainer, error) {
	if s.user == nil {
		// P-256 key generation from crypto/rand cannot fail.
		key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		s.user = &legoUser{email: s.cfg.Email, key: key}
	}
	config := lego.NewConfig(s.user)
	config.CADirURL = acmeDirectoryURL
	config.Certificate.KeyType = certcrypto.EC256
	client, err := lego.NewClient(config)
	if err != nil {
		return nil, fmt.Errorf("lego client: %w", err)
	}

	if wildcard {
		cf := cloudflare.NewDefaultConfig()
		cf.AuthToken = s.cfg.Sites.CloudflareAPIToken
		dns, err := cloudflare.NewDNSProviderConfig(cf)
		if err != nil {
			return nil, fmt.Errorf("cloudflare DNS provider: %w", err)
		}
		// Setting a provider never fails.
		_ = client.Challenge.SetDNS01Provider(dns)
	} else {
		_ = client.Challenge.SetTLSALPN01Provider(s)
	}

	if s.user.registration == nil {
		reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
		if err != nil {
			return nil, fmt.Errorf("ACME registration: %w", err)
		}
		s.user.registration = reg
	}
	return client.Certificate, nil
}

// install caches the pair and serves it. A pair that does not parse is
// refused before anything changes.
func (s *service) install(name string, certPEM, keyPEM []byte) (*tls.Certificate, error) {
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	if err := s.saveCache(name, certPEM, keyPEM); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.certs[name] = &pair
	s.mu.Unlock()
	return &pair, nil
}

// cacheFile maps a certificate name to its file; "*" is not portable in a
// file name.
func cacheFile(name string) string {
	return strings.Replace(name, "*", "_wildcard", 1) + _certExt
}

// cacheAAD binds a cache file to its name, so one name's certificate cannot
// be swapped in for another's.
func cacheAAD(name string) []byte {
	return []byte("tmphost.io/tls/v1/" + name)
}

// loadCache serves every cached certificate it can open; the others are
// obtained again.
func (s *service) loadCache() {
	entries, err := os.ReadDir(s.cfg.CacheDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		file, ok := strings.CutSuffix(e.Name(), _certExt)
		if !ok {
			continue
		}
		name := strings.Replace(file, "_wildcard", "*", 1)
		if err := s.loadCached(name); err != nil {
			log.Printf("tls: ignoring cached certificate %s: %v", e.Name(), err)
		}
	}
}

func (s *service) loadCached(name string) error {
	sealed, err := os.ReadFile(filepath.Join(s.cfg.CacheDir, cacheFile(name)))
	if err != nil {
		return err
	}
	plain, err := s.enc.Decrypt(sealed, cacheAAD(name))
	if err != nil {
		return err
	}
	var pair cachedPair
	if err := json.Unmarshal(plain, &pair); err != nil {
		return err
	}
	cert, err := tls.X509KeyPair([]byte(pair.Certificate), []byte(pair.PrivateKey))
	if err != nil {
		return err
	}
	if cert.Leaf.VerifyHostname(strings.Replace(name, "*", "x", 1)) != nil {
		return errors.New("certificate does not cover " + name)
	}
	s.mu.Lock()
	s.certs[name] = &cert
	s.mu.Unlock()
	return nil
}

func (s *service) saveCache(name string, certPEM, keyPEM []byte) error {
	if err := os.MkdirAll(s.cfg.CacheDir, 0o700); err != nil {
		return err
	}
	// A struct of strings always encodes.
	plain, _ := json.Marshal(cachedPair{Certificate: string(certPEM), PrivateKey: string(keyPEM)})
	path := filepath.Join(s.cfg.CacheDir, cacheFile(name))
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, s.enc.Encrypt(plain, cacheAAD(name)), 0o600); err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp) }()
	return os.Rename(tmp, path)
}
