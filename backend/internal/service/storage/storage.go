// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package storage keeps uploaded files, on the local disk or in an
// S3-compatible bucket. Keys are slash-separated paths such as
// "u42/7d1Kx/site/index.html".
package storage

//go:generate mockgen -source=storage.go -destination=mocks/storage.go -package=mocks

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"
)

// ErrNotFound is returned for a key that holds no object.
var ErrNotFound = errors.New("object not found")

// ErrInvalidKey is returned for a key that could escape its prefix.
var ErrInvalidKey = errors.New("invalid object key")

type (
	// Service stores objects by key.
	Service interface {
		// Put writes data under key, replacing any object already there.
		Put(ctx context.Context, key string, data []byte) error
		// Get opens the object under key. The caller closes the reader.
		Get(ctx context.Context, key string) (io.ReadCloser, Object, error)
		// List returns every object whose key starts with prefix.
		List(ctx context.Context, prefix string) ([]Object, error)
		// Delete removes the object under key. A missing object is not an
		// error.
		Delete(ctx context.Context, key string) error
		// DeletePrefix removes every object whose key starts with prefix.
		DeletePrefix(ctx context.Context, prefix string) error
	}

	// Object describes a stored object.
	Object struct {
		Key     string
		Size    int64
		ModTime time.Time
	}
)

// validKey rejects keys that are empty, absolute, or that have empty, "."
// or ".." segments, so a key can never point outside its prefix.
func validKey(key string) bool {
	if key == "" || strings.ContainsAny(key, "\\\x00") {
		return false
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
}

// validPrefix is validKey for a prefix, which ends with a slash.
func validPrefix(prefix string) bool {
	return strings.HasSuffix(prefix, "/") && validKey(strings.TrimSuffix(prefix, "/"))
}
