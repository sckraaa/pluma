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

package webappdata

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
)

// Signer makes the signature of initData with the server's Ed25519 key
// (WEBAPP_SIGNING_KEY, ADR 0068, p. 3). Telegram's published keys cannot be
// used: their private part is Telegram's, so third parties check the
// signatures of this server with its public key.
type Signer struct {
	key ed25519.PrivateKey
}

// NewSigner returns a Signer on hexSeed, the 32-byte Ed25519 seed in hex
// (openssl rand -hex 32).
func NewSigner(hexSeed string) (*Signer, error) {
	seed, err := hex.DecodeString(strings.TrimSpace(hexSeed))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("webappdata: the web app signing key must be 64 hex digits (openssl rand -hex 32)")
	}
	return &Signer{key: ed25519.NewKeyFromSeed(seed)}, nil
}

// PublicKey returns the public key third parties check signatures with.
func (s *Signer) PublicKey() ed25519.PublicKey {
	return s.key.Public().(ed25519.PublicKey)
}

// PublicKeyHex is PublicKey in hex, the form Telegram publishes its own keys
// in (core.telegram.org/bots/webapps, "Validating data for Third-Party Use").
func (s *Signer) PublicKeyHex() string {
	return hex.EncodeToString(s.PublicKey())
}

// sign returns the base64url form of the signature of data. The page says
// base64url and nothing of padding; the form here has none, and Verify
// accepts both.
func (s *Signer) sign(data string) string {
	return base64.RawURLEncoding.EncodeToString(ed25519.Sign(s.key, []byte(data)))
}
