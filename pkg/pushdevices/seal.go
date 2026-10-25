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

package pushdevices

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// sealedV1 starts a sealed secret: version, then nonce, ciphertext and tag
// of AES-256-GCM.
const sealedV1 = 1

// ErrSealedSecretInvalid is returned for a sealed secret that does not open:
// another encryption key, another device, or a damaged row.
var ErrSealedSecretInvalid = errors.New("pushdevices: sealed secret does not open")

// Sealer seals device secrets with AES-256-GCM on the key
// PUSH_SECRET_ENCRYPTION_KEY. The additional data binds a sealed secret to
// its device (user, authorization, token type): a secret copied to another
// row does not open.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer returns a Sealer on hexKey, 64 hex digits (openssl rand -hex 32).
func NewSealer(hexKey string) (*Sealer, error) {
	key, err := hex.DecodeString(strings.TrimSpace(hexKey))
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("pushdevices: the push secret encryption key must be 64 hex digits (openssl rand -hex 32)")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

// Seal returns the sealed form of the secret of d; an empty secret stays
// empty.
func (s *Sealer) Seal(d *Device) ([]byte, error) {
	if len(d.Secret) == 0 {
		return nil, nil
	}
	out := make([]byte, 1+s.aead.NonceSize(), 1+s.aead.NonceSize()+len(d.Secret)+s.aead.Overhead())
	out[0] = sealedV1
	if _, err := rand.Read(out[1:]); err != nil {
		return nil, err
	}
	return s.aead.Seal(out, out[1:], d.Secret, additionalData(d)), nil
}

// Open returns the secret of d sealed as sealed.
func (s *Sealer) Open(d *Device, sealed []byte) ([]byte, error) {
	if len(sealed) == 0 {
		return nil, nil
	}
	n := s.aead.NonceSize()
	if sealed[0] != sealedV1 || len(sealed) < 1+n+s.aead.Overhead() {
		return nil, ErrSealedSecretInvalid
	}
	secret, err := s.aead.Open(nil, sealed[1:1+n], sealed[1+n:], additionalData(d))
	if err != nil {
		return nil, ErrSealedSecretInvalid
	}
	return secret, nil
}

func additionalData(d *Device) []byte {
	b := make([]byte, 0, 40)
	b = append(b, "pluma push secret v1"...)
	b = binary.BigEndian.AppendUint64(b, uint64(d.UserId))
	b = binary.BigEndian.AppendUint64(b, uint64(d.AuthKeyId))
	return binary.BigEndian.AppendUint32(b, uint32(d.TokenType))
}
