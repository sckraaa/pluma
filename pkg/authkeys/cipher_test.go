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

package authkeys

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

const testHexKey = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

func testKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 256)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestKeyCipher(t *testing.T) {
	c, err := NewKeyCipher(testHexKey)
	if err != nil {
		t.Fatal(err)
	}
	key := testKey(t)
	body, err := c.Seal(42, key)
	if err != nil {
		t.Fatal(err)
	}
	if !IsSealed(body) || strings.Contains(body, base64.RawStdEncoding.EncodeToString(key[:16])) {
		t.Fatalf("not sealed: %q", body[:16])
	}
	if len(body) > 512 {
		t.Fatalf("sealed body of %d characters does not fit auth_keys.body", len(body))
	}
	if again, _ := c.Seal(42, key); again == body {
		t.Fatal("two seals of one key are the same: the nonce repeats")
	}
	got, sealed, err := c.Open(42, body)
	if err != nil || !sealed || !bytes.Equal(got, key) {
		t.Fatalf("open: %v, sealed %v", err, sealed)
	}

	// Another auth_key_id, another encryption key, a damaged body.
	if _, _, err = c.Open(43, body); !errors.Is(err, ErrSealedKeyInvalid) {
		t.Errorf("opened as another key id: %v", err)
	}
	other, _ := NewKeyCipher(strings.Repeat("ff", 32))
	if _, _, err = other.Open(42, body); !errors.Is(err, ErrSealedKeyInvalid) {
		t.Errorf("opened with another encryption key: %v", err)
	}
	damaged := []byte(body)
	damaged[len(damaged)-2] ^= 1
	if _, _, err = c.Open(42, string(damaged)); !errors.Is(err, ErrSealedKeyInvalid) {
		t.Errorf("opened a damaged body: %v", err)
	}
	if _, _, err = c.Open(42, "v1:short"); !errors.Is(err, ErrSealedKeyInvalid) {
		t.Errorf("opened a short body: %v", err)
	}

	// A plain key of before task 008 is still read.
	got, sealed, err = c.Open(42, base64.RawStdEncoding.EncodeToString(key))
	if err != nil || sealed || !bytes.Equal(got, key) {
		t.Fatalf("plain key: %v, sealed %v", err, sealed)
	}
}

func TestNewKeyCipherRejectsBadKeys(t *testing.T) {
	for _, k := range []string{"", "00", strings.Repeat("0", 63), strings.Repeat("0", 66), strings.Repeat("z", 64)} {
		if _, err := NewKeyCipher(k); err == nil {
			t.Errorf("key %q accepted", k)
		}
	}
}
