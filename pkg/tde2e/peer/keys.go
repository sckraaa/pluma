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

package peer

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha512"
	"errors"

	"filippo.io/edwards25519"
)

var errPublicKey = errors.New("tde2e: not a public key")

// RawSharedSecret is td::Ed25519::compute_shared_secret: X25519 of the
// Montgomery forms of an Ed25519 key pair and another's public key. The
// private scalar is the clamped first half of SHA-512 of the seed and the
// public point u = (1+y)/(1-y), as libsodium's
// crypto_sign_ed25519_{sk,pk}_to_curve25519 make them, which Web K uses for
// the same secret (tweb src/lib/calls/e2e/crypto.ts:190-259). tdutils'
// source is not in the pinned checkout; the conversion is checked against
// tde2e's own pregenerated vector (tde2e/test/e2e.cpp:119-133). A key
// whose secret would be zero, such as the zero point, is refused, as tde2e
// refuses it (e2e.cpp:136-140).
func RawSharedSecret(key ed25519.PrivateKey, peer [32]byte) ([]byte, error) {
	p, err := new(edwards25519.Point).SetBytes(peer[:])
	if err != nil {
		return nil, errPublicKey
	}
	h := sha512.Sum512(key.Seed())
	priv, err := ecdh.X25519().NewPrivateKey(h[:32])
	if err != nil {
		return nil, err
	}
	pub, err := ecdh.X25519().NewPublicKey(p.BytesMontgomery())
	if err != nil {
		return nil, errPublicKey
	}
	raw, err := priv.ECDH(pub)
	if err != nil {
		return nil, errPublicKey
	}
	return raw, nil
}

// SharedSecret is PrivateKey::compute_shared_secret (Keys.cpp:137-141): the
// raw secret mixed by HMAC-SHA512 under "tde2e_shared_secret", its first 32
// bytes.
func SharedSecret(key ed25519.PrivateKey, peer [32]byte) ([]byte, error) {
	raw, err := RawSharedSecret(key, peer)
	if err != nil {
		return nil, err
	}
	return hmacSHA512([]byte("tde2e_shared_secret"), raw)[:32], nil
}
