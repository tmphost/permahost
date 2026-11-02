// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidKey(t *testing.T) {
	for _, k := range []string{"a", "u1/f1/a.txt", "u1/f1/dir/.hidden"} {
		assert.True(t, validKey(k), k)
	}
	for _, k := range []string{"", "/a", "a/", "a//b", "./a", "a/../b", "..", "a\\b", "a\x00b"} {
		assert.False(t, validKey(k), k)
	}
	assert.True(t, validPrefix("u1/f1/"))
	assert.False(t, validPrefix("u1/f1"))
	assert.False(t, validPrefix("/"))
}
