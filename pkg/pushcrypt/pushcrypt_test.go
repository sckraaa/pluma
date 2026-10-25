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

package pushcrypt

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func testSecret(t *testing.T) []byte {
	t.Helper()
	s := make([]byte, SecretLength)
	if _, err := rand.Read(s); err != nil {
		t.Fatal(err)
	}
	return s
}

// The server side (the vendored MTProto encryptor) and the client side
// (Decrypt, written after Account.swift) agree, for every padding length.
func TestRoundTrip(t *testing.T) {
	secret := testSecret(t)
	for n := 0; n < 70; n++ {
		payload := bytes.Repeat([]byte("ж"), n)
		p, err := Encrypt(secret, payload)
		if err != nil {
			t.Fatal(err)
		}
		if (len(p)-24)%16 != 0 {
			t.Fatalf("len %d: encrypted part is not whole blocks", len(p))
		}
		got, err := Decrypt(secret, p)
		if err != nil || !bytes.Equal(got, payload) {
			t.Fatalf("len %d: Decrypt = %q, %v", n, got, err)
		}
		// Padding as MTProto 2.0 wants it: 12 to 1024 bytes.
		if pad := len(p) - 24 - 4 - len(payload); pad < 12 || pad > 1024 {
			t.Fatalf("len %d: padding %d", n, pad)
		}
	}
}

func TestKeyId(t *testing.T) {
	secret := testSecret(t)
	h := sha1.Sum(secret)
	p, _ := Encrypt(secret, []byte(`{}`))
	if !bytes.Equal(KeyId(secret), h[len(h)-8:]) || !bytes.Equal(p[:8], h[12:]) {
		t.Fatalf("key id %x, want the last 8 bytes of %x", p[:8], h)
	}
}

func TestDecryptRejects(t *testing.T) {
	secret := testSecret(t)
	p, err := Encrypt(secret, []byte(`{"loc_key":"MESSAGE_TEXT"}`))
	if err != nil {
		t.Fatal(err)
	}
	other := testSecret(t)
	for name, b := range map[string][]byte{
		"other secret": nil, // with other below
		"key id":       flip(p, 0),
		"msg_key":      flip(p, 10),
		"data":         flip(p, len(p)-1),
		"short":        p[:30],
		"not blocks":   p[:len(p)-1],
	} {
		s := secret
		if b == nil {
			s, b = other, p
		}
		if _, err := Decrypt(s, b); !errors.Is(err, ErrDecrypt) {
			t.Errorf("%s: %v, want ErrDecrypt", name, err)
		}
	}
	if _, err := Encrypt(secret[:255], nil); err == nil {
		t.Fatal("Encrypt with a short secret")
	}
}

func TestEncodeP(t *testing.T) {
	secret := testSecret(t)
	payload := []byte(`{"loc_key":"MESSAGE_TEXT","loc_args":["Анна","привет"]}`)
	p, err := EncodeP(secret, payload)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(p, "+/") {
		t.Fatalf("p is not base64url: %s", p)
	}
	// Android decodes Base64.URL_SAFE, iOS pads again: both forms work.
	for _, s := range []string{p, strings.TrimRight(p, "=")} {
		got, err := DecodeP(secret, s)
		if err != nil || !bytes.Equal(got, payload) {
			t.Fatalf("DecodeP(%q): %q %v", s, got, err)
		}
	}
	if _, err := DecodeP(secret, base64.StdEncoding.EncodeToString([]byte("+++/"))); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("DecodeP of garbage: %v", err)
	}
}

func flip(b []byte, i int) []byte {
	c := bytes.Clone(b)
	c[i] ^= 0x40
	return c
}
