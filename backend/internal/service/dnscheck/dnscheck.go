// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package dnscheck looks up the CNAME record of a custom domain. It asks
// public recursive resolvers directly rather than the system resolver, so
// a record is seen as the rest of the internet sees it, and the record
// itself is read rather than the end of its CNAME chain.
package dnscheck

//go:generate mockgen -source=dnscheck.go -destination=mocks/dnscheck.go -package=mocks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// DefaultServers are the resolvers asked when none are configured.
var DefaultServers = []string{"1.1.1.1:53", "8.8.8.8:53"}

const _defaultTimeout = 5 * time.Second

var (
	// ErrNoCNAME is returned for a hostname without a CNAME record.
	ErrNoCNAME = errors.New("no CNAME record found")
	// ErrNoAnswer is returned when no resolver answered.
	ErrNoAnswer = errors.New("no DNS server answered")
)

type (
	// Service looks up CNAME records.
	Service interface {
		// LookupCNAME returns the lowercase target of host's CNAME record,
		// without the trailing dot, or ErrNoCNAME.
		LookupCNAME(ctx context.Context, host string) (string, error)
	}

	// Config chooses the resolvers.
	Config struct {
		// Servers are host:port resolvers, asked in order until one
		// answers; DefaultServers when empty.
		Servers []string
		// Timeout bounds each query; 5 seconds when zero.
		Timeout time.Duration
	}

	exchanger interface {
		ExchangeContext(ctx context.Context, m *dns.Msg, address string) (*dns.Msg, time.Duration, error)
	}

	service struct {
		servers []string
		client  exchanger
	}
)

// New returns the CNAME lookup service.
func New(cfg Config) Service {
	servers, timeout := cfg.Servers, cfg.Timeout
	if len(servers) == 0 {
		servers = DefaultServers
	}
	if timeout == 0 {
		timeout = _defaultTimeout
	}
	return &service{servers: servers, client: &dns.Client{Timeout: timeout}}
}

func (s *service) LookupCNAME(ctx context.Context, host string) (string, error) {
	q := new(dns.Msg)
	q.SetQuestion(dns.Fqdn(strings.ToLower(host)), dns.TypeCNAME)
	q.RecursionDesired = true
	var errs []error
	for _, server := range s.servers {
		rs, _, err := s.client.ExchangeContext(ctx, q, server)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", server, err))
			continue
		}
		// A failing resolver is not the domain's fault; ask the next.
		if rs.Rcode != dns.RcodeSuccess && rs.Rcode != dns.RcodeNameError {
			errs = append(errs, fmt.Errorf("%s: %s", server, dns.RcodeToString[rs.Rcode]))
			continue
		}
		for _, rr := range rs.Answer {
			if cname, ok := rr.(*dns.CNAME); ok && strings.EqualFold(cname.Hdr.Name, q.Question[0].Name) {
				return strings.TrimSuffix(strings.ToLower(cname.Target), "."), nil
			}
		}
		return "", ErrNoCNAME
	}
	return "", errors.Join(append([]error{ErrNoAnswer}, errs...)...)
}
