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

package model

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"testing"
)

// permKey has the sign bit set: the key id goes into the token as its
// two's complement, as auth.acceptLoginToken reads it back.
const permKey = int64(-0x0123456789abcdef)

func newTransaction(permAuthKeyId int64) *QRCodeTransaction {
	return &QRCodeTransaction{
		PermAuthKeyId: permAuthKeyId,
		AuthKeyId:     permAuthKeyId + 1,
		CodeHash:      NewCodeHash(),
		ExpireAt:      1_800_000_000,
		State:         QRCodeStateNew,
	}
}

func TestTokenLikeTelegrams(t *testing.T) {
	m := newTransaction(permKey)
	token := m.Token()
	if len(token) != LoginTokenSize || LoginTokenSize != 34 {
		t.Fatalf("token of %d bytes, want 34", len(token))
	}
	// The client puts it into the QR code as unpadded base64url (tweb
	// fixBase64String): 46 characters, as Telegram's tokens.
	if n := len(base64.RawURLEncoding.EncodeToString(token)); n != 46 {
		t.Fatalf("base64url token of %d characters, want 46", n)
	}
	if got := int64(binary.BigEndian.Uint64(token)); got != permKey {
		t.Fatalf("token starts with key %d, want %d", got, permKey)
	}
	if !bytes.Equal(m.Token(), token) {
		t.Fatal("the token of a transaction changed")
	}
}

func TestTokenSecretIsRandom(t *testing.T) {
	a, b := newTransaction(permKey), newTransaction(permKey)
	if len(a.CodeHash) != 2*(LoginTokenSize-8) {
		t.Fatalf("code hash %d characters", len(a.CodeHash))
	}
	if a.CodeHash == b.CodeHash || bytes.Equal(a.Token(), b.Token()) {
		t.Fatal("two transactions of one key got the same secret")
	}
}

func TestCheckByToken(t *testing.T) {
	m := newTransaction(permKey)
	token := m.Token()
	if !m.CheckByToken(token) {
		t.Fatal("the transaction's own token is rejected")
	}
	for i := range token {
		changed := bytes.Clone(token)
		changed[i] ^= 0x01
		if m.CheckByToken(changed) {
			t.Fatalf("a token with byte %d changed is accepted", i)
		}
	}
	for name, other := range map[string][]byte{
		"empty":   nil,
		"shorter": token[:LoginTokenSize-1],
		"longer":  append(bytes.Clone(token), 0),
		// Another QR code of the same key, and the code of another key.
		"another secret": newTransaction(permKey).Token(),
		"another key":    newTransaction(permKey + 1).Token(),
	} {
		if m.CheckByToken(other) {
			t.Errorf("%s token is accepted", name)
		}
	}
}

// A transaction cached before tokens had LoginTokenSize stays harmless until
// it expires: its token is not empty, and it is never accepted.
func TestTokenOfOlderTransaction(t *testing.T) {
	for name, codeHash := range map[string]string{
		"16-byte secret": "00112233445566778899aabbccddeeff",
		"not hex":        "not hex",
		"empty":          "",
	} {
		m := newTransaction(permKey)
		m.CodeHash = codeHash
		token := m.Token()
		if len(token) < 8 || int64(binary.BigEndian.Uint64(token)) != permKey {
			t.Errorf("%s: token %x", name, token)
		}
		if m.CheckByToken(token) {
			t.Errorf("%s: its own token is accepted", name)
		}
		padded := append(bytes.Clone(token), make([]byte, LoginTokenSize)...)[:LoginTokenSize]
		if m.CheckByToken(padded) {
			t.Errorf("%s: a token of the key padded to %d bytes is accepted", name, LoginTokenSize)
		}
	}
}
