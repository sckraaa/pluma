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
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

func hashBlob(t *testing.T, b []byte, step int) []byte {
	t.Helper()
	h := NewHasher()
	for off := 0; off < len(b); off += step {
		if _, err := h.Write(b[off:min(off+step, len(b))]); err != nil {
			t.Fatal(err)
		}
	}
	if h.Size() != int64(len(b)) {
		t.Fatalf("hasher counted %d bytes of %d", h.Size(), len(b))
	}
	return h.Sum()
}

// TestHasherReference checks the blob against one computed independently
// (Python hashlib over the reference file in 128 KB slices): 25 parts, the
// last of 17 bytes.
func TestHasherReference(t *testing.T) {
	plain := refFile()
	for _, step := range []int{1 << 20, 4096, 100_003, refSize} {
		blob := hashBlob(t, plain, step)
		if len(blob) != 25*sha256.Size {
			t.Fatalf("step %d: %d hashes, want 25", step, len(blob)/sha256.Size)
		}
		sum := sha256.Sum256(blob)
		if got, want := hex.EncodeToString(sum[:]), "66b68b87ec2baf44cef79860ce3dce472fc37512d8603a674c55f1f702469dae"; got != want {
			t.Errorf("step %d: SHA-256 of the blob %s, want %s", step, got, want)
		}
		if got, want := hex.EncodeToString(blob[24*sha256.Size:]), "86d7b1a98e73a6ab5defd3270d5f233545c6c9a21c4aac66fc45e69be09c7b75"; got != want {
			t.Errorf("step %d: hash of the last part %s, want %s", step, got, want)
		}
	}
}

// TestHashesArePlaintext decrypts a 128 KB answer of the node the way
// tdesktop does and checks it against the hash with its offset
// (download_manager_mtproto.cpp:724-737): a hash of the cipher would cancel
// every download.
func TestHashesArePlaintext(t *testing.T) {
	key, iv := refKeyIV()
	plain := refFile()
	cipherText := encryptAll(t, key, iv, plain, len(plain))
	blob := hashBlob(t, plain, 1<<20)
	hashes, err := Answer(blob, refSize, 5*HashPartSize)
	if err != nil {
		t.Fatal(err)
	}
	part := bytes.Clone(cipherText[5*HashPartSize : 6*HashPartSize])
	if err := DecryptAt(key, iv, 5*HashPartSize, part); err != nil {
		t.Fatal(err)
	}
	got := sha256.Sum256(part)
	if hashes[0].GetOffset_INT64() != 5*HashPartSize || !bytes.Equal(hashes[0].GetHash(), got[:]) {
		t.Error("the hash at 640 KB is not that of the decrypted part")
	}
}

func TestAnswer(t *testing.T) {
	const size = 70*HashPartSize + 5 // 71 parts, the last of 5 bytes
	plain := bytes.Repeat([]byte{0x5a}, size)
	plain[size-1] = 1
	blob := hashBlob(t, plain, 1<<20)
	if HashParts(size) != 71 || len(blob) != 71*sha256.Size {
		t.Fatalf("%d parts, blob of %d bytes", HashParts(size), len(blob))
	}

	for _, c := range []struct {
		name        string
		offset      int64
		first, want int64 // first part, number of hashes
	}{
		{"from 0", 0, 0, MaxHashesPerAnswer},
		{"unaligned", 3*HashPartSize + 4096, 3, MaxHashesPerAnswer},
		{"near the end", 69*HashPartSize + 1, 69, 2},
		{"last part", size - 1, 70, 1},
		{"at the end", size, 0, 0},
		{"past the end", size + HashPartSize, 0, 0},
		{"negative", -4096, 0, 0},
	} {
		hashes, err := Answer(blob, size, c.offset)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if hashes == nil || int64(len(hashes)) != c.want {
			t.Errorf("%s: %d hashes, want %d", c.name, len(hashes), c.want)
			continue
		}
		for i, h := range hashes {
			p := c.first + int64(i)
			off := p * HashPartSize
			limit := int64(HashPartSize)
			if p == 70 {
				limit = 5
			}
			sum := sha256.Sum256(plain[off : off+limit])
			if h.GetOffset_INT64() != off || int64(h.GetLimit()) != limit || !bytes.Equal(h.GetHash(), sum[:]) || h.GetOffset_INT32() != int32(off) {
				t.Errorf("%s: hash %d is (%d, %d), want part %d (%d, %d)", c.name, i, h.GetOffset_INT64(), h.GetLimit(), p, off, limit)
			}
		}
	}

	if _, err := Answer(blob[:len(blob)-sha256.Size], size, 0); !errors.Is(err, ErrHashesCorrupt) {
		t.Errorf("a blob short of a hash: %v", err)
	}
	if hashes, err := Answer(nil, 0, 0); err != nil || len(hashes) != 0 {
		t.Errorf("empty file: %d hashes, %v", len(hashes), err)
	}
	// The answer does not alias the stored blob.
	hashes, _ := Answer(blob, size, 0)
	hashes[0].Hash[0] ^= 0xff
	if blob[0] == hashes[0].Hash[0] {
		t.Error("an answer shares memory with the blob")
	}
}

func TestHasherBoundaries(t *testing.T) {
	if blob := NewHasher().Sum(); len(blob) != 0 {
		t.Errorf("empty file: %d bytes of hashes", len(blob))
	}
	exact := bytes.Repeat([]byte{1}, 2*HashPartSize)
	blob := hashBlob(t, exact, 1)
	sum := sha256.Sum256(exact[:HashPartSize])
	if len(blob) != 2*sha256.Size || !bytes.Equal(blob[:sha256.Size], sum[:]) {
		t.Errorf("two whole parts written byte by byte: %d bytes of hashes", len(blob))
	}
	// Sum does not finish the hasher: more bytes may follow.
	h := NewHasher()
	h.Write(exact[:10])
	_ = h.Sum()
	h.Write(exact[10:])
	if !bytes.Equal(h.Sum(), blob) {
		t.Error("Sum in the middle changed the blob")
	}
}
