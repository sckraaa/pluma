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

package pushdevices

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

const testKey = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

func TestSealer(t *testing.T) {
	s, err := NewSealer(testKey)
	if err != nil {
		t.Fatal(err)
	}
	d := &Device{UserId: 7, AuthKeyId: -42, TokenType: TokenFCM, Secret: testSecret(0x5a)}
	sealed, err := s.Seal(d)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, d.Secret[:16]) || len(sealed) != 1+12+SecretLength+16 {
		t.Fatalf("sealed secret: %d bytes", len(sealed))
	}
	if again, _ := s.Seal(d); bytes.Equal(again, sealed) {
		t.Fatal("two seals of one secret are equal: the nonce is not random")
	}
	got, err := s.Open(d, sealed)
	if err != nil || !bytes.Equal(got, d.Secret) {
		t.Fatalf("Open: %v", err)
	}

	// Bound to the device, the key and the bytes.
	for name, f := range map[string]func() (*Sealer, *Device, []byte){
		"other user":          func() (*Sealer, *Device, []byte) { o := *d; o.UserId++; return s, &o, sealed },
		"other authorization": func() (*Sealer, *Device, []byte) { o := *d; o.AuthKeyId++; return s, &o, sealed },
		"other token type":    func() (*Sealer, *Device, []byte) { o := *d; o.TokenType = TokenAPNs; return s, &o, sealed },
		"other key": func() (*Sealer, *Device, []byte) {
			o, _ := NewSealer(strings.Repeat("ff", 32))
			return o, d, sealed
		},
		"flipped byte": func() (*Sealer, *Device, []byte) {
			b := bytes.Clone(sealed)
			b[len(b)/2] ^= 1
			return s, d, b
		},
		"version": func() (*Sealer, *Device, []byte) { b := bytes.Clone(sealed); b[0] = 2; return s, d, b },
		"short":   func() (*Sealer, *Device, []byte) { return s, d, sealed[:20] },
	} {
		sealer, dev, b := f()
		if _, err := sealer.Open(dev, b); !errors.Is(err, ErrSealedSecretInvalid) {
			t.Errorf("%s: %v, want ErrSealedSecretInvalid", name, err)
		}
	}

	if sealed, err := s.Seal(&Device{UserId: 1}); err != nil || sealed != nil {
		t.Fatalf("Seal of an empty secret: %v %v", sealed, err)
	}
	if secret, err := s.Open(d, nil); err != nil || secret != nil {
		t.Fatalf("Open of an empty secret: %v %v", secret, err)
	}
}

func TestNewSealer(t *testing.T) {
	for _, key := range []string{"", "00", strings.Repeat("0", 63), strings.Repeat("zz", 32), strings.Repeat("00", 33)} {
		if _, err := NewSealer(key); err == nil {
			t.Errorf("NewSealer(%q) accepted", key)
		}
	}
	if _, err := NewSealer(" " + testKey + "\n"); err != nil {
		t.Errorf("NewSealer with spaces around: %v", err)
	}
}
