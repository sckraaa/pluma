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

package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

const (
	testOrigin = "https://panel.example.ts.net"
	testRP     = "panel.example.ts.net"
)

// softKey is an authenticator in the test.
type softKey struct {
	priv *ecdsa.PrivateKey
	id   []byte
	x, y []byte
}

func newSoftKey(t *testing.T) *softKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	point, err := k.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	// The tests share a database, where a credential id is a primary key.
	id := make([]byte, 16)
	if _, err = rand.Read(id); err != nil {
		t.Fatal(err)
	}
	return &softKey{priv: k, id: id, x: point[1:33], y: point[33:]}
}

func clientDataFor(t *testing.T, typ, challengeHex, origin string) []byte {
	t.Helper()
	raw, err := hex.DecodeString(challengeHex)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(clientData{Type: typ, Challenge: base64.RawURLEncoding.EncodeToString(raw), Origin: origin})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func authDataFor(rp string, flags byte, counter uint32, extra []byte) []byte {
	sum := sha256.Sum256([]byte(rp))
	out := append([]byte{}, sum[:]...)
	out = append(out, flags)
	var c [4]byte
	binary.BigEndian.PutUint32(c[:], counter)
	out = append(out, c[:]...)
	return append(out, extra...)
}

func (k *softKey) attestation(t *testing.T, flags byte, counter uint32) []byte {
	t.Helper()
	cose, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: k.x, -3: k.y})
	if err != nil {
		t.Fatal(err)
	}
	var cred []byte
	cred = append(cred, make([]byte, 16)...) // aaguid
	var idLen [2]byte
	binary.BigEndian.PutUint16(idLen[:], uint16(len(k.id)))
	cred = append(cred, idLen[:]...)
	cred = append(cred, k.id...)
	cred = append(cred, cose...)
	att, err := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": authDataFor(testRP, flags, counter, cred)})
	if err != nil {
		t.Fatal(err)
	}
	return att
}

func (k *softKey) sign(t *testing.T, authData, client []byte) []byte {
	t.Helper()
	cdh := sha256.Sum256(client)
	digest := sha256.Sum256(append(append([]byte{}, authData...), cdh[:]...))
	der, err := ecdsa.SignASN1(rand.Reader, k.priv, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return der
}

func TestRegistrationAndAssertion(t *testing.T) {
	k := newSoftKey(t)
	challenge, err := newChallenge()
	if err != nil {
		t.Fatal(err)
	}
	client := clientDataFor(t, "webauthn.create", challenge, testOrigin)
	p, err := verifyRegistration(client, k.attestation(t, flagUserPresent|flagUserVerified|flagAttested, 0), challenge, testOrigin, testRP)
	if err != nil {
		t.Fatal(err)
	}
	if string(p.id) != string(k.id) {
		t.Fatalf("credential id %q", p.id)
	}

	challenge2, _ := newChallenge()
	clientGet := clientDataFor(t, "webauthn.get", challenge2, testOrigin)
	auth := authDataFor(testRP, flagUserPresent|flagUserVerified, 5, nil)
	sig := k.sign(t, auth, clientGet)
	count, err := verifyAssertion(p, clientGet, auth, sig, challenge2, testOrigin, testRP)
	if err != nil || count != 5 {
		t.Fatalf("assertion: %v %v", count, err)
	}
}

func TestAssertionRefused(t *testing.T) {
	k := newSoftKey(t)
	p := &passkey{id: k.id, x: k.x, y: k.y}
	challenge, _ := newChallenge()
	client := clientDataFor(t, "webauthn.get", challenge, testOrigin)
	auth := authDataFor(testRP, flagUserPresent|flagUserVerified, 1, nil)
	sig := k.sign(t, auth, client)

	wrongOrigin := clientDataFor(t, "webauthn.get", challenge, "https://other.example.ts.net")
	if _, err := verifyAssertion(p, wrongOrigin, auth, k.sign(t, auth, wrongOrigin), challenge, testOrigin, testRP); !errors.Is(err, errBadCeremony) {
		t.Errorf("another origin: %v", err)
	}
	other, _ := newChallenge()
	if _, err := verifyAssertion(p, client, auth, sig, other, testOrigin, testRP); !errors.Is(err, errBadCeremony) {
		t.Errorf("another challenge: %v", err)
	}
	noUV := authDataFor(testRP, flagUserPresent, 1, nil)
	if _, err := verifyAssertion(p, client, noUV, k.sign(t, noUV, client), challenge, testOrigin, testRP); !errors.Is(err, errBadCeremony) {
		t.Errorf("without user verification: %v", err)
	}
	wrongRP := authDataFor("evil.example", flagUserPresent|flagUserVerified, 1, nil)
	if _, err := verifyAssertion(p, client, wrongRP, k.sign(t, wrongRP, client), challenge, testOrigin, testRP); !errors.Is(err, errBadCeremony) {
		t.Errorf("another rp: %v", err)
	}
	tampered := append([]byte{}, auth...)
	tampered[33] ^= 1
	if _, err := verifyAssertion(p, client, tampered, sig, challenge, testOrigin, testRP); !errors.Is(err, errBadSig) {
		t.Errorf("tampered data: %v", err)
	}
	cloned := &passkey{id: k.id, x: k.x, y: k.y, signCount: 7}
	if _, err := verifyAssertion(cloned, client, auth, sig, challenge, testOrigin, testRP); !errors.Is(err, errBadCeremony) {
		t.Errorf("a counter going back: %v", err)
	}
	other2 := newSoftKey(t)
	if _, err := verifyAssertion(p, client, auth, other2.sign(t, auth, client), challenge, testOrigin, testRP); !errors.Is(err, errBadSig) {
		t.Errorf("another key's signature: %v", err)
	}
}

func TestPeerIsLoopback(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:55000":  true,
		"[::1]:55000":      true,
		"10.0.0.5:55000":   false,
		"100.64.0.1:55000": false,
		"nonsense":         false,
	} {
		if got := peerIsLoopback(addr); got != want {
			t.Errorf("%s: %v", addr, got)
		}
	}
}

func TestConfigCheck(t *testing.T) {
	good := config{Listen: "127.0.0.1:9100", Origin: testOrigin}
	if err := good.check(); err != nil {
		t.Fatalf("good config: %v", err)
	}
	if good.rpID() != testRP {
		t.Errorf("rp id %q", good.rpID())
	}
	for name, c := range map[string]config{
		"public listen": {Listen: "0.0.0.0:9100", Origin: testOrigin},
		"host listen":   {Listen: "panel:9100", Origin: testOrigin},
		"http origin":   {Listen: "127.0.0.1:9100", Origin: "http://panel.example.ts.net"},
		"path origin":   {Listen: "127.0.0.1:9100", Origin: "https://panel.example.ts.net/admin"},
		"port origin":   {Listen: "127.0.0.1:9100", Origin: "https://panel.example.ts.net:8443"},
	} {
		if err := c.check(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

// A step-up is for one action only: another path or payload needs its own.
func TestStepUpDigestBinds(t *testing.T) {
	d := stepUpDigest("/api/reports/1/decide", `{"action":"ban"}`)
	if d == stepUpDigest("/api/reports/2/decide", `{"action":"ban"}`) {
		t.Error("another report must have another digest")
	}
	if d == stepUpDigest("/api/reports/1/decide", `{"action":"dismiss"}`) {
		t.Error("another action must have another digest")
	}
}
