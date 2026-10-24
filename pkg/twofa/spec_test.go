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
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"math/big"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The client of https://core.telegram.org/api/srp written from the page,
// without crypto.SRPUtil, which both the server and pkg/mtclient use: the
// server must accept what an official client computes.

func specH(parts ...[]byte) []byte {
	h := sha256.New()
	for _, p := range parts {
		h.Write(p)
	}
	return h.Sum(nil)
}

func specSH(data, salt []byte) []byte { return specH(salt, data, salt) }

func specPad(x *big.Int) []byte {
	out := make([]byte, 256)
	return x.FillBytes(out)
}

// specX is PH2(password, salt1, salt2).
func specX(t *testing.T, password string, salt1, salt2 []byte) *big.Int {
	t.Helper()
	ph1 := specSH(specSH([]byte(password), salt1), salt2)
	k, err := pbkdf2.Key(sha512.New, string(ph1), salt1, 100000, 64)
	if err != nil {
		t.Fatal(err)
	}
	return new(big.Int).SetBytes(specSH(k, salt2))
}

func specVerifier(t *testing.T, algo *mtproto.PasswordKdfAlgo, password string) []byte {
	g := big.NewInt(int64(algo.GetG()))
	p := new(big.Int).SetBytes(algo.GetP())
	return specPad(new(big.Int).Exp(g, specX(t, password, algo.GetSalt1(), algo.GetSalt2()), p))
}

func specProof(t *testing.T, pw *mtproto.Account_Password, password string) *mtproto.InputCheckPasswordSRP {
	t.Helper()
	algo := pw.GetCurrentAlgo()
	g := big.NewInt(int64(algo.GetG()))
	gBytes := specPad(g)
	p := new(big.Int).SetBytes(algo.GetP())
	pBytes := algo.GetP()
	gb := new(big.Int).SetBytes(pw.GetSrp_B())

	x := specX(t, password, algo.GetSalt1(), algo.GetSalt2())
	a, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 2048))
	if err != nil {
		t.Fatal(err)
	}
	ga := new(big.Int).Exp(g, a, p)
	k := new(big.Int).SetBytes(specH(pBytes, gBytes))
	u := new(big.Int).SetBytes(specH(specPad(ga), specPad(gb)))
	kv := new(big.Int).Mod(new(big.Int).Mul(k, new(big.Int).Exp(g, x, p)), p)
	base := new(big.Int).Mod(new(big.Int).Sub(gb, kv), p)
	sa := new(big.Int).Exp(base, new(big.Int).Add(a, new(big.Int).Mul(u, x)), p)
	ka := specH(specPad(sa))

	hp, hg := specH(pBytes), specH(gBytes)
	for i := range hp {
		hp[i] ^= hg[i]
	}
	m1 := specH(hp, specH(algo.GetSalt1()), specH(algo.GetSalt2()), specPad(ga), specPad(gb), ka)
	return mtproto.MakeTLInputCheckPasswordSRP(&mtproto.InputCheckPasswordSRP{
		SrpId: pw.GetSrpId().GetValue(),
		A:     specPad(ga),
		M1:    m1,
	}).To_InputCheckPasswordSRP()
}

func TestSpecClient(t *testing.T) {
	ctx := context.Background()
	e := newTestEnv(t)
	pw := e.password(t, alice)
	algo, err := mtclient.NewPasswordAlgo(pw.GetNewAlgo())
	if err != nil {
		t.Fatal(err)
	}
	settings := mtproto.MakeTLAccountPasswordInputSettings(&mtproto.Account_PasswordInputSettings{
		NewAlgo:         algo,
		NewPasswordHash: specVerifier(t, algo, "пароль from the spec"),
		Hint:            wrapperspb.String(""),
	}).To_Account_PasswordInputSettings()
	if err = e.s.Update(ctx, alice, keyOne, mtclient.NoPassword(), settings); err != nil {
		t.Fatal(err)
	}

	if err = e.s.Check(ctx, alice, keyOne, specProof(t, e.password(t, alice), "пароль from the spec")); err != nil {
		t.Fatalf("the proof of the spec's client: %v", err)
	}
	mustError(t, e.s.Check(ctx, alice, keyOne, specProof(t, e.password(t, alice), "another")), "PASSWORD_HASH_INVALID")
	// pkg/mtclient, which the e2e tests use, computes the same.
	if err = e.s.Check(ctx, alice, keyOne, e.proof(t, alice, "пароль from the spec")); err != nil {
		t.Fatalf("the proof of mtclient: %v", err)
	}
}
