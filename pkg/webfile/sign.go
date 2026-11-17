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

package webfile

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"fmt"
)

// The server proxies only the addresses it signed itself (ADR 0062, p. 5):
// the access_hash of a webDocument is a MAC of its address, and
// upload.getWebFile takes an inputWebFileLocation only with that hash. A
// client cannot make the server fetch an address no bot gave it.

// signLabel separates these MACs from any other use of the key.
const signLabel = "pluma-webfile-v1\x00"

// MinKeyLength is the shortest key accepted, in bytes.
const MinKeyLength = 32

// Key checks the key of the signatures, WEBFILE_HMAC_KEY: empty is no key
// (the proxy is off), otherwise it is at least MinKeyLength bytes, taken
// as they are (generate it with `openssl rand -base64 32`).
func Key(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	if len(s) < MinKeyLength {
		return nil, fmt.Errorf("webfile: the key must be at least %d bytes (WEBFILE_HMAC_KEY, see docs/ops/security-setup.md)", MinKeyLength)
	}
	return []byte(s), nil
}

// Sign is the access_hash of the address url under key: the first 8 bytes
// of HMAC-SHA256(key, signLabel || url), big-endian, as a non-negative
// int64.
func Sign(key []byte, url string) int64 {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(signLabel))
	mac.Write([]byte(url))
	sum := mac.Sum(nil)
	return int64(binary.BigEndian.Uint64(sum[:8]) &^ (1 << 63))
}

// verify reports, in constant time, whether hash is the signature of url.
func verify(key []byte, url string, hash int64) bool {
	var want, got [8]byte
	binary.BigEndian.PutUint64(want[:], uint64(Sign(key, url)))
	binary.BigEndian.PutUint64(got[:], uint64(hash))
	return subtle.ConstantTimeCompare(want[:], got[:]) == 1
}
