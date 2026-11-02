// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package idgen

import (
	"testing"

	"github.com/cespare/xxhash/v2"
	"github.com/mustafaturan/monoflake"

	"github.com/stretchr/testify/assert"
)

func TestNextID_Unique(t *testing.T) {
	svc := New(1)
	a, b := svc.NextID(), svc.NextID()
	assert.NotEqual(t, a, b)
	assert.Positive(t, a)
}

func TestBase36(t *testing.T) {
	id := New(1).NextID()
	assert.Equal(t, id, ParseBase36(FormatBase36(id)))
	assert.Equal(t, "rs", FormatBase36(1000))
	for _, s := range []string{"", "RS", "-1", "0", "zzzzzzzzzzzzzzzzzz", "a.b"} {
		assert.Zero(t, ParseBase36(s), s)
	}
}

func TestFolderID(t *testing.T) {
	for range 1000 {
		id := NewFolderID()
		assert.Positive(t, id)
		assert.Less(t, id, int64(1)<<56)
		assert.Len(t, FormatFolderID(id), FolderIDLength)
		assert.Equal(t, id, ParseFolderID(FormatFolderID(id)))
	}
	assert.NotEqual(t, NewFolderID(), NewFolderID())
	assert.Equal(t, "000000000rs", FormatFolderID(1000))
	assert.Equal(t, int64(1000), ParseFolderID("000000000rs"))
	assert.Equal(t, "jpia9pm8jr3", FormatFolderID(1<<56-1), "the largest ID still fits")
	for _, s := range []string{"", "rs", "000000000RS", "00000000000", "0000000000rs", "00000000-rs"} {
		assert.Zero(t, ParseFolderID(s), s)
	}
}

func TestAnonymousUserID(t *testing.T) {
	id := AnonymousUserID("203.0.113.7")
	s := monoflake.ID(id).String()
	assert.Len(t, s, 11)
	assert.Equal(t, "00", s[:2])
	digits := xxhash.Sum64String("203.0.113.7") % _anonymousSpace
	assert.Equal(t, monoflake.ID(digits).String()[2:], s[2:], "9 base62 digits of xxhash64(ip)")
	assert.True(t, IsAnonymousUserID(id))

	assert.Equal(t, id, AnonymousUserID("::ffff:203.0.113.7"), "IPv4-mapped IPv6 is the IPv4 address")
	assert.NotEqual(t, id, AnonymousUserID("203.0.113.8"))
	assert.Equal(t, AnonymousUserID("2001:db8:1:2::1"), AnonymousUserID("2001:db8:1:2:ffff::9"), "one /64 is one user")
	assert.NotEqual(t, AnonymousUserID("2001:db8:1:2::1"), AnonymousUserID("2001:db8:1:3::1"))
	assert.True(t, IsAnonymousUserID(AnonymousUserID("2001:db8::1")))
	assert.Zero(t, AnonymousUserID("not-an-ip"))
	assert.Zero(t, AnonymousUserID(""))
}

func TestIsAnonymousUserID(t *testing.T) {
	assert.False(t, IsAnonymousUserID(0))
	assert.False(t, IsAnonymousUserID(-1))
	assert.True(t, IsAnonymousUserID(_anonymousSpace-1))
	assert.False(t, IsAnonymousUserID(_anonymousSpace))
	assert.False(t, IsAnonymousUserID(New(1).NextID()), "monoflake IDs are never anonymous")
}
