// Copyright 2026 The Pluma Authors
//  All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package authkeys

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Auth keys at rest (task 008). Whoever reads auth_keys (a backup, a leaked
// dump, Redis) must not decrypt the traffic of the clients: the stored body
// is the key sealed with AES-256-GCM under AUTH_KEY_ENCRYPTION_KEY, a secret
// kept outside the database. The auth_key_id is the additional data, so a
// sealed key opens only as its own id: rows cannot be swapped.
//
// Stored form: "v1:" + base64(nonce (12 bytes) | ciphertext | tag). A body
// without the prefix is a key of before task 008, base64 of the plain key:
// it is still read, and authsession seals it.

// sealedPrefix marks a sealed body and its format.
const sealedPrefix = "v1:"

// EncryptionKeyLen is the length of the encryption key in bytes (AES-256).
const EncryptionKeyLen = 32

// ErrSealedKeyInvalid: a sealed body does not open with the encryption key
// for its auth_key_id (another key, a damaged or moved row).
var ErrSealedKeyInvalid = errors.New("authkeys: sealed auth key does not open")

// KeyCipher seals and opens auth keys at rest.
type KeyCipher struct {
	aead cipher.AEAD
}

// NewKeyCipher returns a cipher for hexKey, the hex of EncryptionKeyLen
// random bytes (openssl rand -hex 32).
func NewKeyCipher(hexKey string) (*KeyCipher, error) {
	key, err := hex.DecodeString(strings.TrimSpace(hexKey))
	if err != nil || len(key) != EncryptionKeyLen {
		return nil, fmt.Errorf("authkeys: the auth key encryption key must be %d bytes in hex (AUTH_KEY_ENCRYPTION_KEY, openssl rand -hex 32)", EncryptionKeyLen)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &KeyCipher{aead: aead}, nil
}

func additionalData(authKeyId int64) []byte {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uint64(authKeyId))
	return b[:]
}

// Seal returns the stored form of key, the auth key authKeyId.
func (c *KeyCipher) Seal(authKeyId int64, key []byte) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, key, additionalData(authKeyId))
	return sealedPrefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

// Open returns the auth key authKeyId from its stored form, and whether it
// was sealed (false: a plain key of before task 008, to be sealed).
func (c *KeyCipher) Open(authKeyId int64, body string) (key []byte, sealed bool, err error) {
	if !IsSealed(body) {
		key, err = base64.RawStdEncoding.DecodeString(body)
		return key, false, err
	}
	raw, err := base64.RawStdEncoding.DecodeString(body[len(sealedPrefix):])
	if err != nil || len(raw) < c.aead.NonceSize()+c.aead.Overhead() {
		return nil, true, ErrSealedKeyInvalid
	}
	nonce, ciphertext := raw[:c.aead.NonceSize()], raw[c.aead.NonceSize():]
	key, err = c.aead.Open(nil, nonce, ciphertext, additionalData(authKeyId))
	if err != nil {
		return nil, true, ErrSealedKeyInvalid
	}
	return key, true, nil
}

// IsSealed reports whether body is a sealed key, not a plain one.
func IsSealed(body string) bool {
	return strings.HasPrefix(body, sealedPrefix)
}
