// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package tlscert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-acme/lego/v4/certificate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tmphost/tmphost/backend/internal/service/encryption"
)

const _site = "f0123456789a.example.com"

func newPair(t *testing.T, validFor time.Duration, names ...string) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: names[0]},
		DNSNames:     names,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(validFor),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

func newEnc(t *testing.T) encryption.Service {
	t.Helper()
	enc, err := encryption.New("a-master-key-of-at-least-32-characters")
	require.NoError(t, err)
	return enc
}

// fakeObtainer stands in for the CA: it issues a self-signed certificate
// for whatever is asked, unless err is set.
type fakeObtainer struct {
	t        *testing.T
	err      error
	validFor time.Duration
	garbage  bool
	requests atomic.Int32
	called   chan string
}

func (f *fakeObtainer) Obtain(req certificate.ObtainRequest) (*certificate.Resource, error) {
	f.requests.Add(1)
	if f.called != nil {
		f.called <- req.Domains[0]
	}
	if f.err != nil {
		return nil, f.err
	}
	if f.garbage {
		return &certificate.Resource{Certificate: []byte("x"), PrivateKey: []byte("y")}, nil
	}
	validFor := f.validFor
	if validFor == 0 {
		validFor = 90 * 24 * time.Hour
	}
	certPEM, keyPEM := newPair(f.t, validFor, req.Domains...)
	return &certificate.Resource{Certificate: certPEM, PrivateKey: keyPEM}, nil
}

type testEnv struct {
	s         *service
	alpn, dns *fakeObtainer
}

func newTestService(t *testing.T, sites SitesConfig) *testEnv {
	t.Helper()
	if sites.Allow == nil {
		sites.Allow = func(host string) bool { return host == _site }
	}
	s := New(Config{AppHost: "App.Example.com", Sites: sites, CacheDir: filepath.Join(t.TempDir(), "certs")}, newEnc(t)).(*service)
	e := &testEnv{s: s, alpn: &fakeObtainer{t: t}, dns: &fakeObtainer{t: t}}
	s.newObtainer = func(wildcard bool) (certObtainer, error) {
		if wildcard {
			return e.dns, nil
		}
		return e.alpn, nil
	}
	return e
}

func names(t *testing.T, s Service, name string) []string {
	t.Helper()
	c, err := s.GetCertificate(&tls.ClientHelloInfo{ServerName: name})
	require.NoError(t, err)
	return c.Leaf.DNSNames
}

func TestRefresh_AppHostOnly(t *testing.T) {
	e := newTestService(t, SitesConfig{})
	_, err := e.s.GetCertificate(&tls.ClientHelloInfo{ServerName: "app.example.com"})
	assert.ErrorContains(t, err, "no certificate")

	e.s.refresh()
	assert.Equal(t, []string{"app.example.com"}, names(t, e.s, "APP.example.com"))
	assert.Equal(t, []string{"app.example.com"}, names(t, e.s, ""), "no SNI means the app")
	_, err = e.s.GetCertificate(&tls.ClientHelloInfo{ServerName: _site})
	assert.ErrorContains(t, err, "no certificate", "subdomains off")

	// Still valid: the CA is not asked again.
	e.s.refresh()
	assert.EqualValues(t, 1, e.alpn.requests.Load())

	// The cache is encrypted and owner-only, and a restart serves it.
	path := filepath.Join(e.s.cfg.CacheDir, "app.example.com.cert")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "PRIVATE KEY")
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	restarted := newTestService(t, SitesConfig{})
	restarted.s.cfg.CacheDir = e.s.cfg.CacheDir
	restarted.s.loadCache()
	assert.Equal(t, []string{"app.example.com"}, names(t, restarted.s, "app.example.com"))
}

func TestRefresh_RenewsExpiring(t *testing.T) {
	e := newTestService(t, SitesConfig{})
	e.alpn.validFor = 24 * time.Hour
	e.s.refresh()
	e.s.refresh()
	assert.EqualValues(t, 2, e.alpn.requests.Load())
}

func TestRefresh_Wildcard(t *testing.T) {
	e := newTestService(t, SitesConfig{Enabled: true, Domain: "Example.com", CloudflareAPIToken: "cf"})
	e.s.refresh()
	assert.EqualValues(t, 1, e.dns.requests.Load())
	assert.Equal(t, []string{"*.example.com"}, names(t, e.s, _site))
	assert.Equal(t, []string{"app.example.com"}, names(t, e.s, "app.example.com"))

	// Restarted, the wildcard comes from the cache.
	restarted := newTestService(t, SitesConfig{Enabled: true, Domain: "example.com", CloudflareAPIToken: "cf"})
	restarted.s.cfg.CacheDir = e.s.cfg.CacheDir
	restarted.s.loadCache()
	assert.Equal(t, []string{"*.example.com"}, names(t, restarted.s, _site))
	// With a wildcard, nothing is obtained per subdomain.
	_, err := restarted.s.GetCertificate(&tls.ClientHelloInfo{ServerName: "fzzzzzzzzzzz.other.com"})
	assert.Error(t, err)
	assert.EqualValues(t, 0, restarted.alpn.requests.Load())
}

func TestGetCertificate_OnDemand(t *testing.T) {
	e := newTestService(t, SitesConfig{Enabled: true, Domain: "example.com"})
	assert.Equal(t, []string{_site}, names(t, e.s, _site))
	assert.Equal(t, []string{_site}, names(t, e.s, _site))
	assert.EqualValues(t, 1, e.alpn.requests.Load(), "obtained once")

	_, err := e.s.GetCertificate(&tls.ClientHelloInfo{ServerName: "evil.example.com"})
	assert.ErrorContains(t, err, "no certificate", "only allowed hosts go to the CA")
	assert.EqualValues(t, 1, e.alpn.requests.Load())

	// The daily check keeps the subdomain's certificate.
	assert.ElementsMatch(t, []string{"app.example.com", _site}, e.s.names())
}

func TestGetCertificate_OnDemandBackoff(t *testing.T) {
	e := newTestService(t, SitesConfig{Enabled: true, Domain: "example.com"})
	now := time.Now()
	e.s.now = func() time.Time { return now }
	e.alpn.err = errors.New("refused")

	_, err := e.s.GetCertificate(&tls.ClientHelloInfo{ServerName: _site})
	assert.ErrorContains(t, err, "refused")
	_, err = e.s.GetCertificate(&tls.ClientHelloInfo{ServerName: _site})
	assert.ErrorContains(t, err, "failed recently")
	assert.EqualValues(t, 1, e.alpn.requests.Load())

	now = now.Add(_failureBackoff)
	e.alpn.err = nil
	assert.Equal(t, []string{_site}, names(t, e.s, _site))
	assert.Empty(t, e.s.failed)
}

func TestChallenge(t *testing.T) {
	e := newTestService(t, SitesConfig{})
	hello := &tls.ClientHelloInfo{ServerName: "App.example.com", SupportedProtos: []string{ACMEProtocol}}
	_, err := e.s.GetCertificate(hello)
	assert.ErrorContains(t, err, "no ACME challenge")

	require.NoError(t, e.s.Present("app.example.com", "token", "key-auth"))
	cert, err := e.s.GetCertificate(hello)
	require.NoError(t, err)
	assert.Equal(t, []string{"app.example.com"}, cert.Leaf.DNSNames)

	require.NoError(t, e.s.CleanUp("app.example.com", "token", "key-auth"))
	_, err = e.s.GetCertificate(hello)
	assert.Error(t, err)
}

func TestRefresh_Failures(t *testing.T) {
	t.Run("obtainer setup fails", func(t *testing.T) {
		e := newTestService(t, SitesConfig{})
		e.s.newObtainer = func(bool) (certObtainer, error) { return nil, errors.New("no CA") }
		e.s.refresh()
		assert.Empty(t, e.s.obtainers)
		assert.Empty(t, e.s.certs)
	})

	t.Run("CA returns garbage", func(t *testing.T) {
		e := newTestService(t, SitesConfig{})
		e.alpn.garbage = true
		e.s.refresh()
		assert.Empty(t, e.s.certs)
	})

	t.Run("cache dir not creatable", func(t *testing.T) {
		e := newTestService(t, SitesConfig{})
		notADir := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(notADir, nil, 0o600))
		e.s.cfg.CacheDir = filepath.Join(notADir, "certs")
		e.s.refresh()
		assert.Empty(t, e.s.certs, "not served when it cannot be cached")
	})

	t.Run("cache file not writable", func(t *testing.T) {
		e := newTestService(t, SitesConfig{})
		require.NoError(t, os.MkdirAll(filepath.Join(e.s.cfg.CacheDir, "app.example.com.cert.tmp"), 0o700))
		e.s.refresh()
		assert.Empty(t, e.s.certs)
	})
}

func TestLoadCache_SkipsWhatItCannotOpen(t *testing.T) {
	e := newTestService(t, SitesConfig{})
	e.s.loadCache() // no directory yet

	dir := e.s.cfg.CacheDir
	require.NoError(t, os.MkdirAll(dir, 0o700))
	seal := func(name, plain string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, cacheFile(name)), e.s.enc.Encrypt([]byte(plain), cacheAAD(name)), 0o600))
	}
	certPEM, keyPEM := newPair(t, time.Hour, "a.example.com")
	pair := fmt.Sprintf(`{"certificate":%q,"privateKey":%q}`, certPEM, keyPEM)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), nil, 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, cacheFile("tampered.example.com")), []byte("x"), 0o600))
	seal("json.example.com", "not json")
	seal("pair.example.com", `{"certificate":"x","privateKey":"y"}`)
	seal("other.example.com", pair) // a.example.com's certificate under another name
	require.NoError(t, os.Mkdir(filepath.Join(dir, cacheFile("dir.example.com")), 0o700))
	seal("a.example.com", pair)

	e.s.loadCache()
	assert.Len(t, e.s.certs, 1)
	assert.NotNil(t, e.s.certs["a.example.com"])
	assert.Equal(t, "_wildcard.example.com.cert", cacheFile("*.example.com"))
}

func TestStartChecksAtOnceAndStops(t *testing.T) {
	e := newTestService(t, SitesConfig{})
	e.alpn.err = errors.New("CA unavailable")
	e.alpn.called = make(chan string, 1)
	e.s.Start()
	select {
	case name := <-e.alpn.called:
		assert.Equal(t, "app.example.com", name)
	case <-time.After(5 * time.Second):
		t.Fatal("the certificates were not checked at start")
	}
	e.s.Stop()
	e.s.Stop()
}

func TestCheckIntervalIsADayPlusJitter(t *testing.T) {
	for range 50 {
		d := checkInterval()
		assert.GreaterOrEqual(t, d, 24*time.Hour+time.Minute)
		assert.LessOrEqual(t, d, 24*time.Hour+5*time.Minute)
	}
}

func TestLegoUser(t *testing.T) {
	u := &legoUser{email: "a@b.c", key: "k"}
	assert.Equal(t, "a@b.c", u.GetEmail())
	assert.Nil(t, u.GetRegistration())
	assert.Equal(t, "k", u.GetPrivateKey())
}

// fakeACME is just enough of an ACME server for an account to register.
// Orders are refused.
func fakeACME(t *testing.T, accountStatus int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var srv *httptest.Server
	accounts := new(atomic.Int32)
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Replay-Nonce", fmt.Sprintf("nonce-%d", time.Now().UnixNano()))
		switch r.URL.Path {
		case "/directory":
			_, _ = fmt.Fprintf(w, `{"newNonce":%q,"newAccount":%q,"newOrder":%q,"revokeCert":%q,"keyChange":%q}`,
				srv.URL+"/nonce", srv.URL+"/account", srv.URL+"/order", srv.URL+"/revoke", srv.URL+"/key")
		case "/nonce":
			w.WriteHeader(http.StatusOK)
		case "/account":
			accounts.Add(1)
			if accountStatus != http.StatusCreated {
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(accountStatus)
				_, _ = fmt.Fprint(w, `{"type":"urn:ietf:params:acme:error:malformed","detail":"registration refused"}`)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Location", srv.URL+"/account/1")
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprint(w, `{"status":"valid"}`)
		default:
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusForbidden)
			_, _ = fmt.Fprint(w, `{"type":"urn:ietf:params:acme:error:unauthorized","detail":"orders refused"}`)
		}
	}))
	t.Cleanup(srv.Close)
	// lego insists on HTTPS, and trusts the roots this variable names.
	ca := filepath.Join(t.TempDir(), "ca.pem")
	require.NoError(t, os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600))
	t.Setenv("LEGO_CA_CERTIFICATES", ca)
	return srv, accounts
}

func useACMEDirectory(t *testing.T, url string) {
	t.Helper()
	old := acmeDirectoryURL
	acmeDirectoryURL = url
	t.Cleanup(func() { acmeDirectoryURL = old })
	// A Cloudflare setting from the environment must not leak in.
	for _, k := range []string{"CLOUDFLARE_TTL", "CF_API_TOKEN", "CLOUDFLARE_DNS_API_TOKEN", "CLOUDFLARE_EMAIL", "CLOUDFLARE_API_KEY"} {
		t.Setenv(k, "")
	}
}

func acmeService(t *testing.T) *service {
	t.Helper()
	return New(Config{AppHost: "app.example.com", Email: "ops@example.com", Sites: SitesConfig{
		Enabled: true, Domain: "example.com", CloudflareAPIToken: "cf",
	}}, newEnc(t)).(*service)
}

func TestObtain_OneAccountTwoClients(t *testing.T) {
	srv, accounts := fakeACME(t, http.StatusCreated)
	useACMEDirectory(t, srv.URL+"/directory")
	s := acmeService(t)

	_, err := s.obtain("app.example.com")
	assert.ErrorContains(t, err, "orders refused")
	_, err = s.obtain("*.example.com")
	assert.ErrorContains(t, err, "orders refused")
	assert.Len(t, s.obtainers, 2)
	assert.EqualValues(t, 1, accounts.Load(), "the account is registered once")

	first := s.obtainers[false]
	_, _ = s.obtain("app.example.com")
	assert.Same(t, first, s.obtainers[false], "clients are kept")
}

func TestNewACMEObtainer_Failures(t *testing.T) {
	t.Run("directory unreachable", func(t *testing.T) {
		srv, _ := fakeACME(t, http.StatusCreated)
		useACMEDirectory(t, srv.URL+"/no-directory-here")
		_, err := acmeService(t).newACMEObtainer(false)
		assert.ErrorContains(t, err, "lego client")
	})
	t.Run("cloudflare misconfigured", func(t *testing.T) {
		srv, _ := fakeACME(t, http.StatusCreated)
		useACMEDirectory(t, srv.URL+"/directory")
		t.Setenv("CLOUDFLARE_TTL", "1")
		_, err := acmeService(t).newACMEObtainer(true)
		assert.ErrorContains(t, err, "cloudflare DNS provider")
	})
	t.Run("registration refused", func(t *testing.T) {
		srv, _ := fakeACME(t, http.StatusBadRequest)
		useACMEDirectory(t, srv.URL+"/directory")
		_, err := acmeService(t).newACMEObtainer(false)
		assert.ErrorContains(t, err, "ACME registration")
	})
}
