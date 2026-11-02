// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package idgen generates time-ordered, unique int64 IDs.
package idgen

//go:generate mockgen -source=idgen.go -destination=mocks/idgen.go -package=mocks

import (
	"crypto/rand"
	"encoding/binary"
	"net"
	"strconv"
	"strings"

	"github.com/cespare/xxhash/v2"
	"github.com/mustafaturan/monoflake"
)

type (
	// Service issues new IDs.
	Service interface {
		NextID() int64
	}

	service struct {
		mf *monoflake.MonoFlake
	}
)

// New returns an ID generator for the given node. Node IDs wrap at 1024.
func New(node uint16) Service {
	// monoflake.New only fails on invalid options, and none are passed.
	mf, _ := monoflake.New(node)
	return &service{mf: mf}
}

func (s *service) NextID() int64 {
	return s.mf.Next().Int64()
}

// FormatBase36 writes an ID in lowercase base36. Unlike base62 it survives
// case-insensitive contexts such as hostnames.
func FormatBase36(id int64) string {
	return strconv.FormatInt(id, 36)
}

// ParseBase36 reads an ID written by FormatBase36. It returns 0 for
// anything else, including uppercase or non-positive values.
func ParseBase36(s string) int64 {
	if s == "" || strings.ToLower(s) != s {
		return 0
	}
	id, err := strconv.ParseInt(s, 36, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

// FolderIDLength is the length of a written folder ID: 11 base36
// characters, so "f" and the ID make a dynamic subdomain's label.
const FolderIDLength = 11

// _folderIDSpace bounds folder IDs: 56 random bits fit in 11 base36
// characters.
const _folderIDSpace = 1<<56 - 1

// NewFolderID returns a random folder ID in [1, 2^56). Folder IDs are
// random rather than time-ordered so public URLs can't be guessed.
func NewFolderID() int64 {
	var b [8]byte
	// crypto/rand.Read never fails.
	_, _ = rand.Read(b[:])
	return int64(binary.BigEndian.Uint64(b[:])%_folderIDSpace) + 1
}

// FormatFolderID writes a folder ID as FolderIDLength lowercase base36
// characters, zero-padded.
func FormatFolderID(id int64) string {
	s := FormatBase36(id)
	if len(s) < FolderIDLength {
		s = strings.Repeat("0", FolderIDLength-len(s)) + s
	}
	return s
}

// ParseFolderID reads an ID written by FormatFolderID. It returns 0 for
// anything else.
func ParseFolderID(s string) int64 {
	if len(s) != FolderIDLength {
		return 0
	}
	return ParseBase36(s)
}

// _anonymousSpace is 62^9: anonymous user IDs are "00" and 9 base62
// digits, so they are all below it.
const _anonymousSpace = 13537086546263552

// _ipv6Prefix is the IPv6 network one anonymous user stands for: a single
// host usually owns a whole /64.
const _ipv6Prefix = 64

// AnonymousUserID is the user ID of an anonymous web visitor: the base62
// ID "00" followed by 9 base62 digits of xxhash64 of the visitor's IPv4
// address, or of the /64 network of an IPv6 one. It returns 0 for an
// unparsable IP.
//
// Monoflake IDs only stayed below 62^9 ("00" in base62) for their first
// 18 days after the June 2024 epoch, so no user ID issued since can ever
// equal an anonymous one.
func AnonymousUserID(ip string) int64 {
	addr := net.ParseIP(ip)
	if addr == nil {
		return 0
	}
	var key string
	if v4 := addr.To4(); v4 != nil {
		key = v4.String()
	} else {
		mask := net.CIDRMask(_ipv6Prefix, 8*net.IPv6len)
		key = (&net.IPNet{IP: addr.Mask(mask), Mask: mask}).String()
	}
	digits := xxhash.Sum64String(key) % _anonymousSpace
	// digits < 62^9, so its 11-character base62 form already starts with
	// "00".
	return monoflake.IDFromBase62("00" + monoflake.ID(digits).String()[2:]).Int64()
}

// IsAnonymousUserID reports whether id is one AnonymousUserID returns.
func IsAnonymousUserID(id int64) bool {
	return id > 0 && id < _anonymousSpace
}
