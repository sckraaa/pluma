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

package twofa

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"math/big"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/crypto"
)

// The SRP group of passwordKdfAlgoSHA256SHA256PBKDF2HMACSHA512iter100000SHA256ModPow
// (https://core.telegram.org/api/srp): Telegram's 2048-bit safe prime and
// g = 3, the group official clients check (they accept only primes they
// know or can prove safe).
var srpP = []byte{
	0xc7, 0x1c, 0xae, 0xb9, 0xc6, 0xb1, 0xc9, 0x04, 0x8e, 0x6c, 0x52, 0x2f,
	0x70, 0xf1, 0x3f, 0x73, 0x98, 0x0d, 0x40, 0x23, 0x8e, 0x3e, 0x21, 0xc1,
	0x49, 0x34, 0xd0, 0x37, 0x56, 0x3d, 0x93, 0x0f, 0x48, 0x19, 0x8a, 0x0a,
	0xa7, 0xc1, 0x40, 0x58, 0x22, 0x94, 0x93, 0xd2, 0x25, 0x30, 0xf4, 0xdb,
	0xfa, 0x33, 0x6f, 0x6e, 0x0a, 0xc9, 0x25, 0x13, 0x95, 0x43, 0xae, 0xd4,
	0x4c, 0xce, 0x7c, 0x37, 0x20, 0xfd, 0x51, 0xf6, 0x94, 0x58, 0x70, 0x5a,
	0xc6, 0x8c, 0xd4, 0xfe, 0x6b, 0x6b, 0x13, 0xab, 0xdc, 0x97, 0x46, 0x51,
	0x29, 0x69, 0x32, 0x84, 0x54, 0xf1, 0x8f, 0xaf, 0x8c, 0x59, 0x5f, 0x64,
	0x24, 0x77, 0xfe, 0x96, 0xbb, 0x2a, 0x94, 0x1d, 0x5b, 0xcd, 0x1d, 0x4a,
	0xc8, 0xcc, 0x49, 0x88, 0x07, 0x08, 0xfa, 0x9b, 0x37, 0x8e, 0x3c, 0x4f,
	0x3a, 0x90, 0x60, 0xbe, 0xe6, 0x7c, 0xf9, 0xa4, 0xa4, 0xa6, 0x95, 0x81,
	0x10, 0x51, 0x90, 0x7e, 0x16, 0x27, 0x53, 0xb5, 0x6b, 0x0f, 0x6b, 0x41,
	0x0d, 0xba, 0x74, 0xd8, 0xa8, 0x4b, 0x2a, 0x14, 0xb3, 0x14, 0x4e, 0x0e,
	0xf1, 0x28, 0x47, 0x54, 0xfd, 0x17, 0xed, 0x95, 0x0d, 0x59, 0x65, 0xb4,
	0xb9, 0xdd, 0x46, 0x58, 0x2d, 0xb1, 0x17, 0x8d, 0x16, 0x9c, 0x6b, 0xc4,
	0x65, 0xb0, 0xd6, 0xff, 0x9c, 0xa3, 0x92, 0x8f, 0xef, 0x5b, 0x9a, 0xe4,
	0xe4, 0x18, 0xfc, 0x15, 0xe8, 0x3e, 0xbe, 0xa0, 0xf8, 0x7f, 0xa9, 0xff,
	0x5e, 0xed, 0x70, 0x05, 0x0d, 0xed, 0x28, 0x49, 0xf4, 0x7b, 0xf9, 0x59,
	0xd9, 0x56, 0x85, 0x0c, 0xe9, 0x29, 0x85, 0x1f, 0x0d, 0x81, 0x15, 0xf6,
	0x35, 0xb1, 0x05, 0xee, 0x2e, 0x4e, 0x15, 0xd0, 0x4b, 0x24, 0x54, 0xbf,
	0x6f, 0x4f, 0xad, 0xf0, 0x34, 0xb1, 0x04, 0x03, 0x11, 0x9c, 0xd8, 0xe3,
	0xb9, 0x2f, 0xcc, 0x5b,
}

const srpG = 3

// Sizes of the salts. The server issues the first newSalt1PrefixLen bytes of
// new_algo.salt1 and the whole new_algo.salt2; clients append 32 random
// bytes to salt1 (https://core.telegram.org/api/srp#setting-a-new-2fa-password).
const (
	newSalt1PrefixLen = 8
	salt2Len          = 16
	minSalt1Len       = newSalt1PrefixLen + 8
	maxSaltLen        = 255
	// secureSaltPrefixLen is the salt prefix of new_secure_algo (Telegram
	// Passport, which Pluma does not have).
	secureSaltPrefixLen = 8
	secureRandomLen     = 256
	srpValueLen         = 256
)

// srpGoodRange is 2^(2048-64): A must be in [srpGoodRange, p - srpGoodRange],
// the range Telegram requires of g_a and g_b
// (https://core.telegram.org/mtproto/security_guidelines#g-a-and-g-b-validation).
// An honest client misses it with probability 2^-63.
var srpGoodRange = new(big.Int).Lsh(big.NewInt(1), 2048-64)

var srpPInt = new(big.Int).SetBytes(srpP)

// SRPGroup returns Telegram's SRP prime and generator (for clients and tests).
func SRPGroup() (p []byte, g int32) {
	return bytes.Clone(srpP), srpG
}

// makeAlgo is passwordKdfAlgoSHA256SHA256PBKDF2HMACSHA512iter100000SHA256ModPow
// with salt1 and salt2 in the SRP group.
func makeAlgo(salt1, salt2 []byte) *mtproto.PasswordKdfAlgo {
	return mtproto.MakeTLPasswordKdfAlgoModPow(&mtproto.PasswordKdfAlgo{
		Salt1: bytes.Clone(salt1),
		Salt2: bytes.Clone(salt2),
		G:     srpG,
		P:     bytes.Clone(srpP),
	}).To_PasswordKdfAlgo()
}

func srpUtil(salt1, salt2 []byte) *crypto.SRPUtil {
	return crypto.MakeSRPUtil(&crypto.PasswordKdfAlgoModPow{
		Salt1: salt1,
		Salt2: salt2,
		G:     srpG,
		P:     srpP,
	})
}

// goodSRPValue reports whether x (A of the client, or a verifier v) is in
// the safe range of the group.
func goodSRPValue(x *big.Int) bool {
	return x.Cmp(srpGoodRange) >= 0 && x.Cmp(new(big.Int).Sub(srpPInt, srpGoodRange)) <= 0
}

// padSRPValue returns x as a big-endian number of exactly srpValueLen
// bytes, or nil if it is longer.
func padSRPValue(x []byte) []byte {
	x = bytes.TrimLeft(x, "\x00")
	if len(x) > srpValueLen {
		return nil
	}
	out := make([]byte, srpValueLen)
	copy(out[srpValueLen-len(x):], x)
	return out
}

// validVerifier reports whether v (new_password_hash, g^x mod p) can be a
// verifier: 256 bytes or less, 1 < v < p - 1.
func validVerifier(v []byte) bool {
	if len(v) == 0 || len(v) > srpValueLen {
		return false
	}
	n := new(big.Int).SetBytes(v)
	return n.Cmp(big.NewInt(1)) > 0 && n.Cmp(new(big.Int).Sub(srpPInt, big.NewInt(1))) < 0
}

// newSRPB returns the server's ephemeral secret b and B = k*v + g^b mod p
// for the verifier of p.
func newSRPB(p *Password) (b, B []byte) {
	return srpUtil(p.Salt1, p.Salt2).CalcSRPB(p.Verifier)
}

// verifySRP reports whether M1 proves that the client knows the password of
// p, for the server values b and B the client was given with the srp_id.
// The comparison takes the same time whatever M1 is.
func verifySRP(p *Password, b, B, A, M1 []byte) bool {
	if len(A) == 0 || len(A) > srpValueLen || len(M1) != sha256.Size {
		return false
	}
	if !goodSRPValue(new(big.Int).SetBytes(A)) {
		return false
	}
	want := srpUtil(p.Salt1, p.Salt2).CalcM(p.Salt1, p.Verifier, A, b, B)
	if len(want) != sha256.Size {
		return false
	}
	return subtle.ConstantTimeCompare(want, M1) == 1
}

// fingerprint identifies a password (its salts and verifier) without
// revealing it: an srp_id issued for one password does not check another
// (SRP_PASSWORD_CHANGED).
func fingerprint(p *Password) []byte {
	h := sha256.New()
	h.Write(p.Salt1)
	h.Write([]byte{0})
	h.Write(p.Salt2)
	h.Write([]byte{0})
	h.Write(p.Verifier)
	return h.Sum(nil)[:16]
}
