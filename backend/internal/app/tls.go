// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"crypto/tls"
	"net"
	"path/filepath"
	"strconv"

	"github.com/tmphost/tmphost/backend/internal/service/encryption"
	"github.com/tmphost/tmphost/backend/internal/service/tlscert"
)

// setupTLS prepares the certificates and the http-to-https redirect.
func (a *App) setupTLS(cfg Config, enc encryption.Service, h hosts) {
	certs := tlscert.New(tlscert.Config{
		AppHost: h.app,
		Email:   cfg.TLS.Email,
		Sites: tlscert.SitesConfig{
			Enabled:            cfg.TLS.Lego.Enabled,
			Domain:             cfg.App.SitesDomain,
			CloudflareAPIToken: cfg.TLS.Lego.CloudflareAPIToken,
			Allow:              h.isSite,
		},
		CacheDir: filepath.Join(filepath.Dir(settingsFile(cfg)), "certs"),
	}, enc)
	a.certs = certs
	a.tlsPort = cfg.TLS.Port
	a.tlsAddr = net.JoinHostPort("", strconv.Itoa(cfg.TLS.Port))
	a.redirect = httpsRedirect(h, cfg.TLS.Port)
}

// listenTLS serves HTTPS on the TLS port and the redirect on the app port.
func (a *App) listenTLS() error {
	a.certs.Start()
	ln, err := tls.Listen("tcp", a.tlsAddr, &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: a.certs.GetCertificate,
		// Let's Encrypt validates certificates over this port, with ALPN.
		NextProtos: []string{"http/1.1", tlscert.ACMEProtocol},
	})
	if err != nil {
		return err
	}
	// Whichever server stops first ends Listen: a failure, or Shutdown.
	errs := make(chan error, 2)
	go func() { errs <- a.redirect.Listen(a.addr, _listenConfig) }()
	go func() { errs <- a.fiber.Listener(ln, _listenConfig) }()
	return <-errs
}
