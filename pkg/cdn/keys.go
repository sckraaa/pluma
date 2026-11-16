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

// Package cdn is the home side of Pluma's CDN-DC (ADR 0065): the keys,
// tokens and hashes of the files pushed to a CDN node, their encryption,
// the request tokens of upload.reuploadCdnFile, the policy of what goes
// through the CDN and the tables that keep it all.
package cdn

import (
	"crypto/rand"
	"fmt"
)

const (
	// KeySize is the size of a file's AES-256 key: exactly what the clients
	// accept in upload.fileCdnRedirect (Android FileLoadOperation.java:2602-2611,
	// tdesktop download_manager_mtproto.cpp:1018, iOS FetchV2.swift:1002-1004).
	KeySize = 32
	// IVSize is the size of a file's IV; see KeySize.
	IVSize = 16
	// TokenSize is the size of a file_token.
	TokenSize = 32
	// RtKeySize is the size of a request_token key.
	RtKeySize = 32
)

// Secrets are what a file is encrypted and found by on every node: its
// AES-256-CTR key, its IV and its file_token (ADR 0065, p. 3).
type Secrets struct {
	Token []byte
	Key   []byte
	// IV is 12 random bytes and 4 zero ones: the clients put offset/16 in
	// the last 4 (see DecryptAt), so the cipher from offset 0 is plain CTR
	// with a counter that starts at zero.
	IV []byte
}

// NewSecrets makes the secrets of a file from crypto/rand.
func NewSecrets() (Secrets, error) {
	b := make([]byte, TokenSize+KeySize+IVSize)
	if _, err := rand.Read(b[:TokenSize+KeySize+IVSize-4]); err != nil {
		return Secrets{}, fmt.Errorf("cdn: file secrets: %w", err)
	}
	return Secrets{
		Token: b[:TokenSize:TokenSize],
		Key:   b[TokenSize : TokenSize+KeySize : TokenSize+KeySize],
		IV:    b[TokenSize+KeySize:],
	}, nil
}

// Valid reports whether s has the sizes of NewSecrets and an IV that ends
// with 4 zero bytes.
func (s Secrets) Valid() bool {
	return len(s.Token) == TokenSize && validKeyIV(s.Key, s.IV)
}

func validKeyIV(key, iv []byte) bool {
	return len(key) == KeySize && len(iv) == IVSize && iv[12] == 0 && iv[13] == 0 && iv[14] == 0 && iv[15] == 0
}

// NewRtKey makes a request_token key from crypto/rand.
func NewRtKey() ([]byte, error) {
	k := make([]byte, RtKeySize)
	if _, err := rand.Read(k); err != nil {
		return nil, fmt.Errorf("cdn: request token key: %w", err)
	}
	return k, nil
}
