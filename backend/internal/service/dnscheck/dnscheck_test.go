// Copyright 2026 Contextual, Inc. https://permahost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package dnscheck

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serve runs a DNS server on a local UDP port that answers with records,
// and NXDOMAIN for names it has none for.
func serve(t *testing.T, records map[string]string) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	mux := dns.NewServeMux()
	mux.HandleFunc(".", func(w dns.ResponseWriter, q *dns.Msg) {
		rs := new(dns.Msg)
		rs.SetReply(q)
		target, ok := records[q.Question[0].Name]
		if !ok {
			rs.Rcode = dns.RcodeNameError
		} else if target != "" {
			rs.Answer = append(rs.Answer, &dns.CNAME{
				Hdr:    dns.RR_Header{Name: q.Question[0].Name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: 60},
				Target: target,
			})
		}
		_ = w.WriteMsg(rs)
	})
	srv := &dns.Server{PacketConn: pc, Handler: mux}
	started := make(chan struct{})
	srv.NotifyStartedFunc = func() { close(started) }
	go func() { _ = srv.ActivateAndServe() }()
	<-started
	t.Cleanup(func() { _ = srv.Shutdown() })
	return pc.LocalAddr().String()
}

func TestLookupCNAME(t *testing.T) {
	ctx := context.Background()
	addr := serve(t, map[string]string{
		"www.example.com.":   "F000000000RS.Sites.Permahost.IO.",
		"empty.example.com.": "",
	})
	s := New(Config{Servers: []string{addr}, Timeout: time.Second})

	got, err := s.LookupCNAME(ctx, "WWW.example.com")
	require.NoError(t, err)
	assert.Equal(t, "f000000000rs.sites.permahost.io", got, "lowercase, no trailing dot")

	_, err = s.LookupCNAME(ctx, "empty.example.com")
	assert.ErrorIs(t, err, ErrNoCNAME, "the name exists without a CNAME")
	_, err = s.LookupCNAME(ctx, "missing.example.com")
	assert.ErrorIs(t, err, ErrNoCNAME, "NXDOMAIN")
}

func TestNew_Defaults(t *testing.T) {
	s := New(Config{}).(*service)
	assert.Equal(t, DefaultServers, s.servers)
	assert.Equal(t, _defaultTimeout, s.client.(*dns.Client).Timeout)
}

type fakeExchanger func(server string) (*dns.Msg, error)

func (f fakeExchanger) ExchangeContext(_ context.Context, _ *dns.Msg, server string) (*dns.Msg, time.Duration, error) {
	rs, err := f(server)
	return rs, 0, err
}

func TestLookupCNAME_ResolverFailures(t *testing.T) {
	ctx := context.Background()
	servFail := &dns.Msg{MsgHdr: dns.MsgHdr{Rcode: dns.RcodeServerFailure}}
	answer := &dns.Msg{Answer: []dns.RR{
		&dns.A{Hdr: dns.RR_Header{Name: "www.example.com.", Rrtype: dns.TypeA}},
		&dns.CNAME{Hdr: dns.RR_Header{Name: "other.example.com.", Rrtype: dns.TypeCNAME}, Target: "x."},
		&dns.CNAME{Hdr: dns.RR_Header{Name: "www.example.com.", Rrtype: dns.TypeCNAME}, Target: "target.io."},
	}}
	asked := []string{}
	s := &service{servers: []string{"a", "b", "c"}, client: fakeExchanger(func(server string) (*dns.Msg, error) {
		asked = append(asked, server)
		switch server {
		case "a":
			return nil, errors.New("timeout")
		case "b":
			return servFail, nil
		default:
			return answer, nil
		}
	})}
	got, err := s.LookupCNAME(ctx, "www.example.com")
	require.NoError(t, err)
	assert.Equal(t, "target.io", got, "only the record for the name asked")
	assert.Equal(t, []string{"a", "b", "c"}, asked, "failing resolvers are skipped")

	s.servers = []string{"a", "b"}
	_, err = s.LookupCNAME(ctx, "www.example.com")
	assert.ErrorIs(t, err, ErrNoAnswer)
	assert.ErrorContains(t, err, "a: timeout")
	assert.ErrorContains(t, err, "b: SERVFAIL")
}
