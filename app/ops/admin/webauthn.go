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
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/fxamacker/cbor/v2"
)

// WebAuthn of the panel (www.w3.org/TR/webauthn-3): the relying party steps
// of §7.1 (registering a passkey) and §7.2 (verifying an assertion), for
// ES256 keys only, the one algorithm the panel offers and both Touch ID and
// Face ID create (COSE alg -7).

// passkey is a registered credential.
type passkey struct {
	id        []byte
	x, y      []byte
	signCount uint32
}

// clientData is the collected client data of a ceremony.
type clientData struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	Origin    string `json:"origin"`
}

// flag bits of authenticator data.
const (
	flagUserPresent  = 0x01
	flagUserVerified = 0x04
	flagAttested     = 0x40
)

var (
	errBadCeremony = errors.New("webauthn: invalid ceremony")
	errBadSig      = errors.New("webauthn: invalid signature")
)

// newChallenge returns a fresh challenge (§13.4.3), in hex: the key of its
// row in admin_challenges.
func newChallenge() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// parseClientData checks the type, challenge and origin of a ceremony.
func parseClientData(data []byte, wantType, challenge, origin string) error {
	var c clientData
	if err := json.Unmarshal(data, &c); err != nil {
		return fmt.Errorf("%w: client data", errBadCeremony)
	}
	if c.Type != wantType {
		return fmt.Errorf("%w: type %q", errBadCeremony, c.Type)
	}
	want, err := hex.DecodeString(challenge)
	if err != nil {
		return fmt.Errorf("%w: challenge", errBadCeremony)
	}
	if c.Challenge != base64.RawURLEncoding.EncodeToString(want) {
		return fmt.Errorf("%w: challenge differs", errBadCeremony)
	}
	if c.Origin != origin {
		return fmt.Errorf("%w: origin %q", errBadCeremony, c.Origin)
	}
	return nil
}

// checkAuthData verifies the rpIdHash, user presence and verification of
// authenticator data and returns the sign counter (§7.2 steps 15-17, 21).
func checkAuthData(auth []byte, rpID string) (flags byte, counter uint32, err error) {
	if len(auth) < 37 {
		return 0, 0, fmt.Errorf("%w: short authenticator data", errBadCeremony)
	}
	want := sha256.Sum256([]byte(rpID))
	if !bytes.Equal(auth[:32], want[:]) {
		return 0, 0, fmt.Errorf("%w: rp id hash", errBadCeremony)
	}
	flags = auth[32]
	if flags&flagUserPresent == 0 || flags&flagUserVerified == 0 {
		return 0, 0, fmt.Errorf("%w: user not verified", errBadCeremony)
	}
	return flags, binary.BigEndian.Uint32(auth[33:37]), nil
}

// attestationObject is the part of the registration response we use.
// attStmt is not verified: the panel asks for "none" and trusts the
// tailnet and the logged-in owner for the device, not the attestation.
type attestationObject struct {
	AuthData []byte `cbor:"authData"`
}

// coseES256 reads a COSE_Key of an EC2 P-256 key with alg ES256 (§6.5.1).
func coseES256(raw []byte) (x, y []byte, err error) {
	var key map[int]cbor.RawMessage
	if err := cbor.Unmarshal(raw, &key); err != nil {
		return nil, nil, fmt.Errorf("%w: cose key", errBadCeremony)
	}
	intOf := func(label int) (int, bool) {
		v, ok := key[label]
		if !ok {
			return 0, false
		}
		var n int
		if cbor.Unmarshal(v, &n) != nil {
			return 0, false
		}
		return n, true
	}
	bytesOf := func(label int) []byte {
		v, ok := key[label]
		if !ok {
			return nil
		}
		var b []byte
		if cbor.Unmarshal(v, &b) != nil {
			return nil
		}
		return b
	}
	kty, _ := intOf(1)
	alg, _ := intOf(3)
	crv, _ := intOf(-1)
	if kty != 2 || alg != -7 || crv != 1 {
		return nil, nil, fmt.Errorf("%w: only ES256 keys are accepted", errBadCeremony)
	}
	x, y = bytesOf(-2), bytesOf(-3)
	if len(x) != 32 || len(y) != 32 {
		return nil, nil, fmt.Errorf("%w: key size", errBadCeremony)
	}
	return x, y, nil
}

// verifyRegistration runs §7.1 and returns the new credential.
func verifyRegistration(clientDataJSON, attestation []byte, challenge, origin, rpID string) (*passkey, error) {
	if err := parseClientData(clientDataJSON, "webauthn.create", challenge, origin); err != nil {
		return nil, err
	}
	var att attestationObject
	if err := cbor.Unmarshal(attestation, &att); err != nil {
		return nil, fmt.Errorf("%w: attestation object", errBadCeremony)
	}
	auth := att.AuthData
	flags, counter, err := checkAuthData(auth, rpID)
	if err != nil {
		return nil, err
	}
	if flags&flagAttested == 0 || len(auth) < 37+16+2 {
		return nil, fmt.Errorf("%w: no attested credential", errBadCeremony)
	}
	offset := 37 + 16 // aaguid
	idLen := int(binary.BigEndian.Uint16(auth[offset:]))
	offset += 2
	if idLen == 0 || len(auth) < offset+idLen {
		return nil, fmt.Errorf("%w: credential id", errBadCeremony)
	}
	id := auth[offset : offset+idLen]
	rest := auth[offset+idLen:]
	var keyRaw cbor.RawMessage
	if _, err := cbor.UnmarshalFirst(rest, &keyRaw); err != nil {
		return nil, fmt.Errorf("%w: credential key", errBadCeremony)
	}
	x, y, err := coseES256(keyRaw)
	if err != nil {
		return nil, err
	}
	return &passkey{id: append([]byte{}, id...), x: x, y: y, signCount: counter}, nil
}

// verifyAssertion runs §7.2 against a stored credential. It returns the
// new sign counter; a counter that does not grow, where it is used at all,
// is a cloned authenticator (§7.2 step 22).
func verifyAssertion(pk *passkey, clientDataJSON, authData, signature []byte, challenge, origin, rpID string) (uint32, error) {
	if err := parseClientData(clientDataJSON, "webauthn.get", challenge, origin); err != nil {
		return 0, err
	}
	_, counter, err := checkAuthData(authData, rpID)
	if err != nil {
		return 0, err
	}
	if pk.signCount != 0 && counter <= pk.signCount {
		return 0, fmt.Errorf("%w: sign counter went back", errBadCeremony)
	}
	key, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), append(append([]byte{4}, pk.x...), pk.y...))
	if err != nil {
		return 0, fmt.Errorf("%w: public key", errBadSig)
	}
	cdh := sha256.Sum256(clientDataJSON)
	signed := append(append([]byte{}, authData...), cdh[:]...)
	digest := sha256.Sum256(signed)
	if !ecdsa.VerifyASN1(key, digest[:], signature) {
		return 0, errBadSig
	}
	return counter, nil
}
