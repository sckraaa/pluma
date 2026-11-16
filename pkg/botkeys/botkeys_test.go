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
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
)

func testKey(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// The values were computed apart from this package, with Python:
// hmac.new(b"WebAppData", token, hashlib.sha256) and hashlib.sha256(token).
func TestDerive(t *testing.T) {
	keys := Derive("123456:AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsaw")
	if got := hex.EncodeToString(keys.WebApp); got != "e2ff1f56a55e53e65df418a68aec9716c6e7053693c42657d9d6a3550e3da6cf" {
		t.Errorf("web app key %s", got)
	}
	if got := hex.EncodeToString(keys.Login); got != "3e016c7d5024e2cb629d45e8dc8e276985315acb2e5e46a2f4b1a6f6a3022bca" {
		t.Errorf("login key %s", got)
	}
	if len(keys.WebApp) != KeySize || len(keys.Login) != KeySize {
		t.Errorf("sizes %d, %d", len(keys.WebApp), len(keys.Login))
	}
}

func TestSealOpen(t *testing.T) {
	s, err := NewSealer(testKey(t))
	if err != nil {
		t.Fatal(err)
	}
	key := Derive("7:secret").WebApp
	sealed, err := s.Seal(7, PurposeWebApp, key)
	if err != nil {
		t.Fatal(err)
	}
	if len(sealed) > 96 {
		t.Errorf("sealed key of %d bytes does not fit bot_secret_keys", len(sealed))
	}
	if bytes.Contains(sealed, key) {
		t.Error("the sealed form holds the key")
	}
	got, err := s.Open(7, PurposeWebApp, sealed)
	if err != nil || !bytes.Equal(got, key) {
		t.Fatalf("open: %x, %v", got, err)
	}
	again, err := s.Seal(7, PurposeWebApp, key)
	if err != nil || bytes.Equal(again, sealed) {
		t.Error("two seals of one key are equal: the nonce is not random")
	}

	// Another bot, another purpose, another encryption key, a damaged row.
	if _, err := s.Open(8, PurposeWebApp, sealed); !errors.Is(err, ErrSealedInvalid) {
		t.Errorf("another bot: %v", err)
	}
	if _, err := s.Open(7, PurposeLogin, sealed); !errors.Is(err, ErrSealedInvalid) {
		t.Errorf("another purpose: %v", err)
	}
	other, err := NewSealer(testKey(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Open(7, PurposeWebApp, sealed); !errors.Is(err, ErrSealedInvalid) {
		t.Errorf("another encryption key: %v", err)
	}
	damaged := append([]byte(nil), sealed...)
	damaged[len(damaged)-1] ^= 1
	for name, b := range map[string][]byte{"damaged": damaged, "empty": nil, "short": sealed[:10], "version": append([]byte{2}, sealed[1:]...)} {
		if _, err := s.Open(7, PurposeWebApp, b); !errors.Is(err, ErrSealedInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := s.Seal(7, PurposeWebApp, nil); err == nil {
		t.Error("an empty key was sealed")
	}
}

func TestConfig(t *testing.T) {
	for _, k := range []string{"", "  "} {
		if s, err := (Config{EncryptionKey: k}).Sealer(); s != nil || err != nil {
			t.Errorf("%q: %v, %v", k, s, err)
		}
	}
	for _, k := range []string{"zz", "abcd", testKey(t)[:62]} {
		if _, err := (Config{EncryptionKey: k}).Sealer(); err == nil {
			t.Errorf("%q accepted", k)
		}
	}
	if s, err := (Config{EncryptionKey: " " + testKey(t) + "\n"}).Sealer(); s == nil || err != nil {
		t.Errorf("a key with spaces around: %v", err)
	}
}

func TestDisabledStore(t *testing.T) {
	s := NewStore(nil, nil)
	if s.Enabled() {
		t.Fatal("enabled without a sealer")
	}
	if wrote, err := s.Ensure(t.Context(), 1, "1:x", "h"); wrote || err != nil {
		t.Errorf("ensure: %v, %v", wrote, err)
	}
	if _, ok, err := s.Get(t.Context(), 1); ok || err != nil {
		t.Errorf("get: %v, %v", ok, err)
	}
}
