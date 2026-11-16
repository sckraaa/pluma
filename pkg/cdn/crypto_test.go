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
	"testing"
)

// The reference file of the tests: 3 MiB + 17 bytes, byte i is
// (131·i + i/512) mod 256.
const refSize = 3<<20 + 17

func refFile() []byte {
	b := make([]byte, refSize)
	for i := range b {
		b[i] = byte(i*131 + i>>9)
	}
	return b
}

func refKeyIV() (key, iv []byte) {
	key = make([]byte, KeySize)
	for i := range key {
		key[i] = byte(i)
	}
	iv = make([]byte, IVSize)
	for i := 0; i < 12; i++ {
		iv[i] = byte(0xf0 + i)
	}
	return key, iv
}

func encryptAll(t *testing.T, key, iv, plain []byte, step int) []byte {
	t.Helper()
	e, err := NewEncrypter(key, iv)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]byte, len(plain))
	for off := 0; off < len(plain); off += step {
		end := min(off+step, len(plain))
		e.XORKeyStream(out[off:end], plain[off:end])
	}
	return out
}

// TestEncrypterOpenSSL checks the cipher against one computed
// independently: openssl enc -aes-256-ctr -K 0001…1f -iv f0f1…fb00000000
// (OpenSSL 4.0.3) on the reference file.
func TestEncrypterOpenSSL(t *testing.T) {
	key, iv := refKeyIV()
	plain := refFile()
	// An odd step: the stream must not depend on how the file is cut.
	cipherText := encryptAll(t, key, iv, plain, 100_003)
	sum := sha256.Sum256(cipherText)
	if got, want := hex.EncodeToString(sum[:]), "b9425178cc7df9f630a39a83ecf8370000cc7d67a5e46fdad459d9339248610c"; got != want {
		t.Errorf("SHA-256 of the cipher is %s, OpenSSL's is %s", got, want)
	}
	for _, c := range []struct {
		off  int
		want string
	}{
		{0, "a3198460696d555ba505690598ae2d464a328931b0710599fd269acc1121885a"},
		{1 << 20, "a166cb80ff5ea1f0b2cbe02ac4d6dc675ebe74524f3eb84553c2a52ee252fba3"},
		{3 << 20, "c00ce4247c2c90829773fa8ae1b1008de6"},
	} {
		end := min(c.off+32, len(cipherText))
		if got := hex.EncodeToString(cipherText[c.off:end]); got != c.want {
			t.Errorf("cipher at %d: %s, OpenSSL's %s", c.off, got, c.want)
		}
	}
}

// TestDecryptAtClients decrypts parts of the cipher the way the clients
// decrypt the answers of upload.getCdnFile, each from its own offset.
func TestDecryptAtClients(t *testing.T) {
	key, iv := refKeyIV()
	plain := refFile()
	cipherText := encryptAll(t, key, iv, plain, len(plain))
	for _, c := range []struct{ off, n int }{
		{0, 4096},
		{4096, 4096},
		{1<<20 - 4096, 4096},
		{128 << 10, 128 << 10},
		{5 * 128 << 10, 128 << 10},
		{1 << 20, 1 << 20},
		{2 << 20, 512 << 10},
		{3 << 20, 17}, // the last, short part
		{refSize - 1 - (refSize-1)%16, (refSize-1)%16 + 1}, // the last, incomplete AES block
	} {
		part := bytes.Clone(cipherText[c.off : c.off+c.n])
		if err := DecryptAt(key, iv, int64(c.off), part); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(part, plain[c.off:c.off+c.n]) {
			t.Errorf("part at %d of %d bytes does not decrypt to the file", c.off, c.n)
		}
	}
}

func TestCipherArguments(t *testing.T) {
	key, iv := refKeyIV()
	if _, err := NewEncrypter(key[:16], iv); err == nil {
		t.Error("NewEncrypter took a 16-byte key")
	}
	bad := bytes.Clone(iv)
	bad[15] = 1
	if _, err := NewEncrypter(key, bad); err == nil {
		t.Error("NewEncrypter took an IV whose counter bytes are not zero")
	}
	if err := DecryptAt(key, iv, 8, make([]byte, 16)); err == nil {
		t.Error("DecryptAt took an offset that is not a multiple of 16")
	}
	if err := DecryptAt(key, iv, -16, make([]byte, 16)); err == nil {
		t.Error("DecryptAt took a negative offset")
	}
}

func TestNewSecrets(t *testing.T) {
	a, err := NewSecrets()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewSecrets()
	if err != nil {
		t.Fatal(err)
	}
	if !a.Valid() || !b.Valid() {
		t.Fatalf("invalid secrets: token %d, key %d, IV %d bytes", len(a.Token), len(a.Key), len(a.IV))
	}
	if bytes.Equal(a.Token, b.Token) || bytes.Equal(a.Key, b.Key) || bytes.Equal(a.IV[:12], b.IV[:12]) {
		t.Error("two files got the same secrets")
	}
	// The parts are separate slices: appending to one cannot write another.
	if cap(a.Token) != TokenSize || cap(a.Key) != KeySize {
		t.Errorf("token and key share room with the next part: caps %d, %d", cap(a.Token), cap(a.Key))
	}
}
