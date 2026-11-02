// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package encryption seals data at rest with a key derived from the master
// key, which comes only from the environment.
package encryption

//go:generate mockgen -source=encryption.go -destination=mocks/encryption.go -package=mocks

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
)

const (
	// MasterKeyEnv is the environment variable holding the master key. It is
	// never read from a config file.
	MasterKeyEnv = "TMPHOSTIO_MASTER_KEY"
	// MinMasterKeyLength is the shortest master key accepted.
	MinMasterKeyLength = 32
	// Algorithm names the cipher, for stored envelopes.
	Algorithm = "AES-256-GCM"

	_keyInfo = "tmphost.io data-at-rest v1"
)

// ErrDecrypt is returned when data cannot be opened: a wrong master key, or
// data that was changed.
var ErrDecrypt = errors.New("decrypt failed: wrong " + MasterKeyEnv + " or corrupted data")

type (
	// Service encrypts and decrypts with authenticated encryption. aad is
	// bound to the ciphertext and must match on decryption.
	Service interface {
		Encrypt(plaintext, aad []byte) []byte
		Decrypt(ciphertext, aad []byte) ([]byte, error)
	}

	service struct {
		aead cipher.AEAD
	}
)

// New derives an AES-256-GCM key from masterKey with HKDF-SHA256.
func New(masterKey string) (Service, error) {
	if len(masterKey) < MinMasterKeyLength {
		return nil, fmt.Errorf("%s must be set to at least %d characters", MasterKeyEnv, MinMasterKeyLength)
	}
	// HKDF only fails for a key length beyond 255 hash sizes; 32 is fine.
	key, _ := hkdf.Key(sha256.New, []byte(masterKey), nil, _keyInfo, 32)
	// A 32-byte key always makes an AES block, and AES always makes a GCM.
	block, _ := aes.NewCipher(key)
	aead, _ := cipher.NewGCM(block)
	return &service{aead: aead}, nil
}

// Encrypt returns a random nonce followed by the sealed plaintext.
func (s *service) Encrypt(plaintext, aad []byte) []byte {
	nonce := make([]byte, s.aead.NonceSize(), s.aead.NonceSize()+len(plaintext)+s.aead.Overhead())
	// crypto/rand.Read never returns an error.
	_, _ = rand.Read(nonce)
	return s.aead.Seal(nonce, nonce, plaintext, aad)
}

func (s *service) Decrypt(ciphertext, aad []byte) ([]byte, error) {
	n := s.aead.NonceSize()
	if len(ciphertext) < n+s.aead.Overhead() {
		return nil, ErrDecrypt
	}
	plaintext, err := s.aead.Open(nil, ciphertext[:n], ciphertext[n:], aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plaintext, nil
}
