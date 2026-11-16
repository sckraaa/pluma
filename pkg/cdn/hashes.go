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
	"crypto/sha256"
	"errors"
	"hash"
	"math"

	"github.com/teamgram/proto/mtproto"
)

const (
	// HashPartSize is the size of the parts of a file that the fileHash
	// vectors cover: the clients check the decrypted bytes of every 128 KB
	// at a multiple of 128 KB against a hash with that offset (Android
	// FileLoadOperation.java:168, 1889-1897; tdesktop
	// download_manager_mtproto.h:26 and download_manager_mtproto.cpp:704,
	// 724-737, by the exact offset of a 128 KB request; iOS FetchV2.swift:616-617).
	HashPartSize = 128 << 10
	// MaxHashesPerAnswer is how many hashes an answer carries at most (8 MB
	// of the file). Our number: the documentation is silent and the clients
	// ask for more with upload.getCdnFileHashes (ADR 0065, p. 3).
	MaxHashesPerAnswer = 64
)

// ErrHashesCorrupt means a stored hash blob does not match the size of its
// file.
var ErrHashesCorrupt = errors.New("cdn: hashes do not match the file size")

// Hasher computes the hash blob of a file read in order: the SHA-256 of
// every HashPartSize part of the plaintext, the last one shorter, 32 bytes
// each. A hash of the cipher would fail every download: the clients hash
// what they decrypted.
type Hasher struct {
	part hash.Hash
	n    int64 // bytes written
	blob []byte
}

// NewHasher returns an empty Hasher.
func NewHasher() *Hasher { return &Hasher{part: sha256.New()} }

// Write adds the next bytes of the file; it never fails.
func (h *Hasher) Write(p []byte) (int, error) {
	written := len(p)
	for len(p) > 0 {
		room := HashPartSize - int(h.n%HashPartSize)
		k := min(room, len(p))
		h.part.Write(p[:k])
		h.n += int64(k)
		p = p[k:]
		if k == room {
			h.blob = h.part.Sum(h.blob)
			h.part.Reset()
		}
	}
	return written, nil
}

// Size is the number of bytes written.
func (h *Hasher) Size() int64 { return h.n }

// Sum returns the hash blob of the bytes written: the hash of a last short
// part is added. An empty file has no parts.
func (h *Hasher) Sum() []byte {
	blob := append([]byte(nil), h.blob...)
	if h.n%HashPartSize != 0 {
		blob = h.part.Sum(blob)
	}
	return blob
}

// HashParts is the number of parts of a file of size bytes.
func HashParts(size int64) int64 {
	if size <= 0 {
		return 0
	}
	return (size + HashPartSize - 1) / HashPartSize
}

// Answer returns the hashes of a file of size bytes with the blob of a
// Hasher for an answer about offset: from offset rounded down to
// HashPartSize, at most MaxHashesPerAnswer parts. It is empty for an offset
// at or past the end of the file, or a negative one.
func Answer(blob []byte, size, offset int64) ([]*mtproto.FileHash, error) {
	parts := HashParts(size)
	if int64(len(blob)) != parts*sha256.Size {
		return nil, ErrHashesCorrupt
	}
	if offset < 0 || offset >= size {
		return []*mtproto.FileHash{}, nil
	}
	first := offset / HashPartSize
	last := min(parts, first+MaxHashesPerAnswer)
	hashes := make([]*mtproto.FileHash, 0, last-first)
	for i := first; i < last; i++ {
		off := i * HashPartSize
		h := &mtproto.FileHash{
			Offset_INT64: off,
			Limit:        int32(min(HashPartSize, size-off)),
			Hash:         append([]byte(nil), blob[i*sha256.Size:(i+1)*sha256.Size]...),
		}
		// The codec writes the field of the layer it encodes for.
		if off <= math.MaxInt32 {
			h.Offset_INT32 = int32(off)
		}
		hashes = append(hashes, mtproto.MakeTLFileHash(h).To_FileHash())
	}
	return hashes, nil
}
