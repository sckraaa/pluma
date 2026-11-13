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

package peer

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/pkg/tde2e"
)

func unhex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The vectors of tde2e/test/EncryptionTestVectors.h, checked as
// tde2e/test/encryption.cpp:35-66 checks them: with the deterministic
// padding the encryption is the vector's, and both decrypt.
func TestEncryptionVectors(t *testing.T) {
	const secret = "f9fb473b9887e50ea38eef7380c82361432cd4b22c5f9b3700809990d8ed344c"
	const header = "bd29703cf44551710ca14d091a6c98ee347931b2b8140faaaef2dbb40719df12"
	for _, v := range []struct{ name, data, extra, encrypted, encryptedHeader string }{
		{"empty_message", "", "",
			"d28eb3e3d1328f06dafedabd67a353d5ea6e164d2f34c162a16f8a1164663a03",
			"4060edd7bcacca6dd0f4fe81d6ec63a8859fa9d520598043bc4748919f3fdeda"},
		{"simple_message", "48656c6c6f2c20576f726c6421", "",
			"967f5245b03e07ab7be6044174306a4af811e96708ae3ad2ab427aa5495508b1c319ca0353531c0a2921e307f2455856",
			"9e7910949e526b6ad51a59aad8022c826b00f379e28592ed3216aabc6be252e0"},
		{"random_message",
			"fed306c137017ab008c22d1f74bc104e5138d3c19b42fb303c768b083912a1102d06ac1e0f4440e3b32b9144a50e6fc0f190273bd4dec7ef847bf7d46680bb67",
			"736d616c6c206578747261",
			"ee198e4860c888ab18bcecba5083eb539f402be1fbde51e0c49e398145d40ebb7b5a52bdc83d6200e63a70c47cc6a5e6fa7a71f6e24b722b5f0314b6b52768dbd2e438a582d1cf2a54d4de7ba30e36e68afd8379b63a345483dbcc33380fd07f",
			"f282da8a41f17a5fa7f793c6c134c5bb2b960a2fea43bdc15b58a69cb7dafb43"},
		{"message_with_unicode", "48656c6c6f2c20e4b896e7958c21", "",
			"cb0d460ca3daf8e3fd5623965b39b5c1de840e92d39f6caf4662b7a7983c53b29fe644bf45acea2644507ac01f0617a2",
			"ebe8636326b11d90f9a670e63086e2fcd02b78c0aa5cacdb4f887e511d1ae4c9"},
	} {
		t.Run(v.name, func(t *testing.T) {
			s, data, extra := unhex(t, secret), unhex(t, v.data), unhex(t, v.extra)
			want, wantHeader := unhex(t, v.encrypted), unhex(t, v.encryptedHeader)
			got, _ := encryptWithPrefix(append(padding(len(data), true), data...), s, extra)
			if !bytes.Equal(got, want) {
				t.Fatalf("encrypted %x\nwant      %x", got, want)
			}
			h, err := EncryptHeader(unhex(t, header), got, s)
			if err != nil || !bytes.Equal(h, wantHeader) {
				t.Fatalf("header %x %v, want %x", h, err, wantHeader)
			}
			plain, _, err := DecryptData(want, s, extra)
			if err != nil || !bytes.Equal(plain, data) {
				t.Fatalf("decrypted %x %v", plain, err)
			}
			if h, err := DecryptHeader(wantHeader, want, s); err != nil || !bytes.Equal(h, unhex(t, header)) {
				t.Fatalf("decrypted header %x %v", h, err)
			}
			if _, _, err := DecryptData(want, s, append(extra, 1)); !errors.Is(err, errMsgID) {
				t.Fatalf("other additional data: %v", err)
			}
		})
	}
}

// tde2e's pregenerated handshake (tde2e/test/e2e.cpp:119-133): the raw
// secret of Alice's private and Bob's public key, both ways; the zero key
// is refused (:136-140).
func TestSharedSecretVector(t *testing.T) {
	dec := func(s string) []byte {
		b, err := base64.RawURLEncoding.DecodeString(s)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	alice := ed25519.NewKeyFromSeed(dec("8NZGWKfRCJfiks74RG9_xHmYydarLiRsoq8VcJGPglg"))
	bob := ed25519.NewKeyFromSeed(dec("YMGoowtnZ99roUM2y5JRwiQrwGaNJ-ZRE5boy-l4aHg"))
	if pub := Public(alice); !bytes.Equal(pub[:], dec("RvG0CT5i8D-CYnfhp2akVC1tPRBIw-4X6ZqNBjH-mZI")) {
		t.Fatalf("Alice's public key %x", pub)
	}
	want := dec("CU6NsPBw59neM9crFvxKELbtKgAkI7G8tDHsb4CmyVA")
	ab, err := RawSharedSecret(alice, Public(bob))
	if err != nil || !bytes.Equal(ab, want) {
		t.Fatalf("Alice's secret %x %v, want %x", ab, err, want)
	}
	ba, err := RawSharedSecret(bob, Public(alice))
	if err != nil || !bytes.Equal(ba, want) {
		t.Fatalf("Bob's secret %x %v", ba, err)
	}
	if _, err := RawSharedSecret(alice, [32]byte{}); err == nil {
		t.Fatal("a secret with the zero key")
	}
	s1, _ := SharedSecret(alice, Public(bob))
	s2, _ := SharedSecret(bob, Public(alice))
	if len(s1) != 32 || !bytes.Equal(s1, s2) || bytes.Equal(s1, want[:32]) {
		t.Fatalf("mixed secrets %x %x", s1, s2)
	}
}

// clock is a time the test moves.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// server is the chain as the server keeps it: what it accepts it hands out
// in the server form.
type server struct {
	t      *testing.T
	chain  *tde2e.Chain
	blocks [][]byte
}

func newServer(t *testing.T) *server {
	return &server{t: t, chain: tde2e.NewChain()}
}

func (s *server) accept(local []byte) []byte {
	s.t.Helper()
	applied, err := s.chain.Apply(local)
	if err != nil {
		s.t.Fatalf("the server refuses the block: %v", err)
	}
	out, err := tde2e.ToServer(applied.Bytes)
	if err != nil {
		s.t.Fatal(err)
	}
	s.blocks = append(s.blocks, out)
	return out
}

func (s *server) last() []byte {
	return s.blocks[len(s.blocks)-1]
}

func key(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	k, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// join adds userID to the server's call as a client does: block 0 on an
// empty chain, a self-add block on the last one; the call starts from the
// block of the join.
func join(t *testing.T, s *server, userID int64, clk *clock, opts ...Option) *Call {
	t.Helper()
	k := key(t)
	var block []byte
	var err error
	if len(s.blocks) == 0 {
		block, err = ZeroBlock(userID, k)
	} else {
		block, err = JoinBlock(userID, k, s.last())
	}
	if err != nil {
		t.Fatal(err)
	}
	c, err := New(userID, k, s.accept(block), append([]Option{WithClock(clk.now)}, opts...)...)
	if err != nil {
		t.Fatalf("user %d starts the call: %v", userID, err)
	}
	return c
}

func follow(t *testing.T, s *server, calls ...*Call) {
	t.Helper()
	for _, c := range calls {
		if err := c.ApplyBlock(s.last()); err != nil {
			t.Fatalf("user %d applies the block: %v", c.UserID(), err)
		}
	}
}

func roundTrip(t *testing.T, from, to *Call, prefix int) error {
	t.Helper()
	data := []byte("\x10\x02\x00\x9d\x01\x2a\x80\x02\x68\x01 an encoded frame")
	frame, err := from.Encrypt(0, data, prefix)
	if err != nil {
		t.Fatalf("user %d encrypts: %v", from.UserID(), err)
	}
	if !bytes.Equal(frame[:prefix], data[:prefix]) {
		t.Fatalf("the prefix is not open: %x", frame[:prefix])
	}
	got, err := to.Decrypt(from.UserID(), 0, frame)
	if err == nil && !bytes.Equal(got, data) {
		t.Fatalf("user %d decrypts %q", to.UserID(), got)
	}
	return err
}

// Three participants join one by one; each block is a new epoch, frames
// name the epochs they are encrypted with and decrypt for everyone with or
// without an open prefix; the epoch before a block is forgotten 10 s after
// it; a frame of a forgotten epoch is an unknown epoch.
func TestCallJoinsAndEpochs(t *testing.T) {
	clk := &clock{t: time.Unix(1000, 0)}
	s := newServer(t)
	a := join(t, s, 1, clk)
	if a.Height() != 0 || len(a.Epochs()) != 1 || a.Epochs()[0] != a.LastHash() {
		t.Fatalf("Alice after block 0: height %d, epochs %d", a.Height(), len(a.Epochs()))
	}
	b := join(t, s, 2, clk)
	follow(t, s, a)
	for _, prefix := range []int{0, 1, 10} {
		if err := roundTrip(t, a, b, prefix); err != nil {
			t.Fatalf("Bob decrypts Alice, prefix %d: %v", prefix, err)
		}
		if err := roundTrip(t, b, a, prefix); err != nil {
			t.Fatalf("Alice decrypts Bob, prefix %d: %v", prefix, err)
		}
	}
	// Alice still has the epoch of block 0 for 10 s and encrypts with both.
	frame, err := a.Encrypt(0, []byte("x"), 0)
	if err != nil {
		t.Fatal(err)
	}
	f, err := ParseFrame(frame)
	if err != nil || len(f.Epochs) != 2 || f.Epochs[1] != a.LastHash() {
		t.Fatalf("the epochs of Alice's frame: %v %v", f, err)
	}

	clk.add(11 * time.Second)
	// A frame Bob makes before Carol joins, delivered late.
	old, err := b.Encrypt(0, []byte("late"), 0)
	if err != nil {
		t.Fatal(err)
	}
	c := join(t, s, 3, clk)
	follow(t, s, a, b)
	if a.LastHash() != c.LastHash() || b.Height() != 2 {
		t.Fatalf("the chain differs: %x %x", a.LastHash(), c.LastHash())
	}
	for _, pair := range [][2]*Call{{a, c}, {c, a}, {b, c}, {c, b}, {a, b}} {
		if err := roundTrip(t, pair[0], pair[1], 0); err != nil {
			t.Fatalf("%d to %d: %v", pair[0].UserID(), pair[1].UserID(), err)
		}
	}
	// Carol never had the epoch of Bob's late frame; Alice forgets it 10 s
	// after the next block.
	if _, err := c.Decrypt(2, 0, old); ErrorCode(err) != tde2e.CodeDecryptUnknownEpoch {
		t.Fatalf("Carol decrypts a frame from before her: %v", err)
	}
	clk.add(9 * time.Second)
	if len(a.Epochs()) != 2 {
		t.Fatalf("Alice keeps %d epochs 9 s after the block", len(a.Epochs()))
	}
	clk.add(2 * time.Second)
	if len(a.Epochs()) != 1 {
		t.Fatalf("Alice keeps %d epochs 11 s after the block", len(a.Epochs()))
	}
	if _, err := a.Decrypt(2, 0, old); ErrorCode(err) != tde2e.CodeDecryptUnknownEpoch {
		t.Fatalf("a frame of a forgotten epoch: %v", err)
	}
}

// The replay window: the same frame twice is a duplicate, one below the
// window too old; one's own frame and a frame under another sender's name
// are refused.
func TestCallReplayAndSenders(t *testing.T) {
	clk := &clock{t: time.Unix(1000, 0)}
	s := newServer(t)
	a := join(t, s, 1, clk)
	b := join(t, s, 2, clk)
	follow(t, s, a)
	first, err := a.Encrypt(0, []byte("first"), 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < seenWindow+1; i++ {
		frame, err := a.Encrypt(0, []byte("next"), 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := b.Decrypt(1, 0, frame); err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if i == 0 {
			if _, err := b.Decrypt(1, 0, frame); !errors.Is(err, ErrDuplicate) {
				t.Fatalf("a frame twice: %v", err)
			}
		}
	}
	if _, err := b.Decrypt(1, 0, first); !errors.Is(err, ErrTooOld) {
		t.Fatalf("a frame below the window: %v", err)
	}
	own, _ := b.Encrypt(0, []byte("own"), 0)
	if _, err := b.Decrypt(2, 0, own); err == nil {
		t.Fatal("one's own frame decrypted")
	}
	frame, _ := a.Encrypt(0, []byte("from Alice"), 0)
	if _, err := b.Decrypt(3, 0, frame); err == nil {
		t.Fatal("a frame of a user not in the call decrypted")
	}
	if _, err := a.Encrypt(maxChannelID+1, []byte("x"), 0); ErrorCode(err) != tde2e.CodeInvalidCallChannelID {
		t.Fatalf("channel 1024: %v", err)
	}
}

// A participant removed by a block fails for good with NotParticipant
// (Call.cpp:715-718); the others go on with a new key he does not have. A
// key starts one call of the process only.
func TestCallRemoved(t *testing.T) {
	clk := &clock{t: time.Unix(1000, 0)}
	s := newServer(t)
	a := join(t, s, 1, clk)
	b := join(t, s, 2, clk)
	follow(t, s, a)
	c := join(t, s, 3, clk)
	follow(t, s, a, b)
	block, err := a.ChangeBlock(2)
	if err != nil {
		t.Fatal(err)
	}
	s.accept(block)
	follow(t, s, a, c)
	if err := b.ApplyBlock(s.last()); ErrorCode(b.Err()) != tde2e.CodeNotParticipant || err == nil {
		t.Fatalf("Bob after his removal: %v, stays %v", err, b.Err())
	}
	if _, err := b.Encrypt(0, []byte("x"), 0); ErrorCode(err) != tde2e.CodeCallFailed {
		t.Fatalf("Bob encrypts after the failure: %v", err)
	}
	clk.add(11 * time.Second)
	if err := roundTrip(t, a, c, 0); err != nil {
		t.Fatalf("Carol decrypts Alice after Bob: %v", err)
	}
	if _, err := New(1, a.key, s.last()); ErrorCode(err) != tde2e.CodeCallKeyAlreadyUsed {
		t.Fatalf("a second call with Alice's key: %v", err)
	}
}

// A participant whose key is wrong encrypts what nobody opens and opens
// nothing; the others are not affected.
func TestCallBrokenKey(t *testing.T) {
	clk := &clock{t: time.Unix(1000, 0)}
	s := newServer(t)
	a := join(t, s, 1, clk)
	b := join(t, s, 2, clk, WithBrokenKey())
	follow(t, s, a)
	c := join(t, s, 3, clk)
	follow(t, s, a, b)
	if err := roundTrip(t, b, a, 0); err == nil || ErrorCode(err) == tde2e.CodeDecryptUnknownEpoch {
		t.Fatalf("Alice decrypts the broken Bob: %v", err)
	}
	if err := roundTrip(t, a, b, 0); err == nil {
		t.Fatal("the broken Bob decrypts Alice")
	}
	if err := roundTrip(t, a, c, 0); err != nil {
		t.Fatalf("Carol decrypts Alice: %v", err)
	}
}

// The emoji verification of three: everyone commits, then reveals, and
// all get the same hash; a new block starts it again.
func TestCallEmoji(t *testing.T) {
	clk := &clock{t: time.Unix(1000, 0)}
	s := newServer(t)
	a := join(t, s, 1, clk)
	b := join(t, s, 2, clk)
	follow(t, s, a)
	c := join(t, s, 3, clk)
	follow(t, s, a, b)
	calls := []*Call{a, b, c}
	sv := tde2e.NewVerification()
	sv.Reset(s.chain)
	// The server's sub-chain 1: every broadcast it takes goes to everyone.
	for round := 0; round < 3; round++ {
		for _, from := range calls {
			out, err := from.PullBroadcasts()
			if err != nil {
				t.Fatal(err)
			}
			for _, local := range out {
				if _, err := sv.Accept(from.UserID(), local); err != nil {
					t.Fatalf("the server refuses a broadcast of %d: %v", from.UserID(), err)
				}
				served, _ := tde2e.ToServer(local)
				for _, to := range calls {
					if err := to.ApplyBroadcast(served); err != nil {
						t.Fatalf("%d takes a broadcast of %d: %v", to.UserID(), from.UserID(), err)
					}
				}
			}
		}
	}
	var hash []byte
	for _, x := range calls {
		h, ok := x.EmojiHash()
		if !ok || len(h) != 64 || hash != nil && !bytes.Equal(h, hash) {
			t.Fatalf("user %d: emoji hash %x %v", x.UserID(), h, ok)
		}
		hash = h
	}
	if sv.Phase() != tde2e.PhaseEnd {
		t.Fatalf("the server's phase %d", sv.Phase())
	}
	d := join(t, s, 4, clk)
	follow(t, s, a)
	if _, ok := a.EmojiHash(); ok {
		t.Fatal("an emoji hash of the last height after a new block")
	}
	if out, _ := d.PullBroadcasts(); len(out) != 1 {
		t.Fatalf("Dave's first broadcasts: %d", len(out))
	}
}
