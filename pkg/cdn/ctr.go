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

package cdn

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
)

// MaxCipherSize is the largest file the cipher of NewEncrypter matches the
// clients on. A client restarts the counter at every request with offset/16
// in the last 4 bytes of the IV: the stream from offset 0 carries into byte
// 11 after 2^32 blocks, and iOS clamps offset/16 to a signed 32-bit number
// (FetchV2.swift:934), so 2^31 blocks. Telegram's files are far smaller
// (4 GB at most).
const MaxCipherSize = 1 << 35

var (
	errSecrets = errors.New("cdn: key must be 32 bytes and IV 16 bytes ending with 4 zero bytes")
	errOffset  = errors.New("cdn: offset must be a non-negative multiple of 16")
)

// Encrypter encrypts a file from its first byte, in order, as the CDN node
// stores it (ADR 0065, p. 3): AES-256-CTR whose counter at byte p is
// IV[0:12] ‖ uint32_be(p/16).
type Encrypter struct {
	stream cipher.Stream
}

// NewEncrypter returns the Encrypter of a file with key and iv (Secrets).
func NewEncrypter(key, iv []byte) (*Encrypter, error) {
	if !validKeyIV(key, iv) {
		return nil, errSecrets
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return &Encrypter{stream: cipher.NewCTR(block, iv)}, nil
}

// XORKeyStream encrypts src, the next bytes of the file, into dst; dst and
// src may overlap entirely.
func (e *Encrypter) XORKeyStream(dst, src []byte) { e.stream.XORKeyStream(dst, src) }

// DecryptAt decrypts in place b, the bytes of an encrypted file from offset,
// the way the clients decrypt an answer of upload.getCdnFile: a copy of iv
// with offset/16 in bytes 12-15 (big-endian), AES-256-CTR from there
// (Android FileLoadOperation.java:1900-1906, tdesktop
// download_manager_mtproto.cpp:694-698, iOS FetchV2.swift:929-939). The
// offset of every request is a multiple of 4096 (the rules of getCdnFile),
// hence of 16. Encryption and decryption are the same operation.
func DecryptAt(key, iv []byte, offset int64, b []byte) error {
	if offset < 0 || offset%aes.BlockSize != 0 {
		return errOffset
	}
	if len(key) != KeySize || len(iv) != IVSize {
		return errSecrets
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	ctr := make([]byte, IVSize)
	copy(ctr, iv)
	binary.BigEndian.PutUint32(ctr[12:], uint32(offset/aes.BlockSize))
	cipher.NewCTR(block, ctr).XORKeyStream(b, b)
	return nil
}
