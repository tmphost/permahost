// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package encryption

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _key = strings.Repeat("k", MinMasterKeyLength)

func TestNew_RejectsShortKey(t *testing.T) {
	for _, key := range []string{"", strings.Repeat("k", MinMasterKeyLength-1)} {
		_, err := New(key)
		assert.ErrorContains(t, err, MasterKeyEnv)
	}
}

func TestEncryptDecrypt(t *testing.T) {
	svc, err := New(_key)
	require.NoError(t, err)
	aad := []byte("settings")
	plaintext := []byte(`{"secret":"s3cr3t"}`)

	sealed := svc.Encrypt(plaintext, aad)
	assert.False(t, bytes.Contains(sealed, []byte("s3cr3t")))
	assert.NotEqual(t, sealed, svc.Encrypt(plaintext, aad), "random nonce per message")

	got, err := svc.Decrypt(sealed, aad)
	require.NoError(t, err)
	assert.Equal(t, plaintext, got)

	t.Run("same key in a new process decrypts", func(t *testing.T) {
		other, _ := New(_key)
		got, err := other.Decrypt(sealed, aad)
		require.NoError(t, err)
		assert.Equal(t, plaintext, got)
	})

	t.Run("failures", func(t *testing.T) {
		wrongKey, _ := New(strings.Repeat("x", MinMasterKeyLength))
		tampered := bytes.Clone(sealed)
		tampered[len(tampered)-1] ^= 1
		for name, try := range map[string]func() ([]byte, error){
			"wrong key": func() ([]byte, error) { return wrongKey.Decrypt(sealed, aad) },
			"wrong aad": func() ([]byte, error) { return svc.Decrypt(sealed, []byte("other")) },
			"tampered":  func() ([]byte, error) { return svc.Decrypt(tampered, aad) },
			"too short": func() ([]byte, error) { return svc.Decrypt(sealed[:10], aad) },
		} {
			_, err := try()
			assert.ErrorIs(t, err, ErrDecrypt, name)
		}
	})
}
