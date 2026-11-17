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

package botkeys

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

// sealedV1 starts a sealed key: version, then nonce, ciphertext and tag of
// AES-256-GCM, as the secrets of push devices (pkg/pushdevices/seal.go).
const sealedV1 = 1

// Purpose is what a key is for; it is bound into the sealed form.
type Purpose uint8

const (
	PurposeWebApp Purpose = 1 // Keys.WebApp
	PurposeLogin  Purpose = 2 // Keys.Login
)

// ErrSealedInvalid is returned for a sealed key that does not open: another
// encryption key, another bot or purpose, or a damaged row.
var ErrSealedInvalid = errors.New("botkeys: sealed key does not open")

// Sealer seals bot keys with AES-256-GCM on BOT_SECRET_ENCRYPTION_KEY. The
// additional data binds a sealed key to its bot and purpose: a key copied to
// another bot's row, or swapped with the other key of the row, does not open.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer returns a Sealer on hexKey, 64 hex digits (openssl rand -hex 32).
func NewSealer(hexKey string) (*Sealer, error) {
	key, err := hex.DecodeString(strings.TrimSpace(hexKey))
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("botkeys: the bot secret encryption key must be 64 hex digits (openssl rand -hex 32)")
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

// Seal returns the sealed form of key, the key of the purpose of botId.
func (s *Sealer) Seal(botId int64, purpose Purpose, key []byte) ([]byte, error) {
	if len(key) == 0 {
		return nil, errors.New("botkeys: empty key")
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := make([]byte, 0, 1+len(nonce)+len(key)+s.aead.Overhead())
	out = append(out, sealedV1)
	out = append(out, nonce...)
	return s.aead.Seal(out, nonce, key, additionalData(botId, purpose)), nil
}

// Open returns the key of the purpose of botId sealed as sealed.
func (s *Sealer) Open(botId int64, purpose Purpose, sealed []byte) ([]byte, error) {
	n := s.aead.NonceSize()
	if len(sealed) < 1+n+s.aead.Overhead() || sealed[0] != sealedV1 {
		return nil, ErrSealedInvalid
	}
	key, err := s.aead.Open(nil, sealed[1:1+n], sealed[1+n:], additionalData(botId, purpose))
	if err != nil {
		return nil, ErrSealedInvalid
	}
	return key, nil
}

func additionalData(botId int64, purpose Purpose) []byte {
	b := make([]byte, 0, 32)
	b = append(b, "pluma bot key v1"...)
	b = binary.BigEndian.AppendUint64(b, uint64(botId))
	return append(b, byte(purpose))
}
