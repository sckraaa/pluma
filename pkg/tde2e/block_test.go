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

package tde2e

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func TestConstructorIDs(t *testing.T) {
	// The ids of the generated td/telegram/e2e_api.h, as signed int32.
	tests := []struct {
		name string
		id   uint32
		td   int32
	}{
		{"block", idBlock, 1671052726},
		{"groupBroadcastNonceCommit", idBroadcastCommit, -783209753},
		{"groupBroadcastNonceReveal", idBroadcastReveal, -2081097256},
		{"groupParticipant", idGroupParticipant, 418617119},
		{"groupState", idGroupState, 500987268},
		{"sharedKey", idSharedKey, -1971028353},
		{"changeNoop", idChangeNoop, -558586853},
		{"changeSetValue", idChangeSetValue, -33474100},
		{"changeSetGroupState", idChangeSetGroupState, 754020678},
		{"changeSetSharedKey", idChangeSetSharedKey, -1736826536},
		{"stateProof", idStateProof, -692684314},
	}
	for _, tt := range tests {
		if int32(tt.id) != tt.td {
			t.Errorf("%s: %08x, td has %d", tt.name, tt.id, tt.td)
		}
	}
}

func TestEmptyKVHash(t *testing.T) {
	// The kv_hash of every block tde2e builds without values, as in the
	// reference block.
	if got := hex.EncodeToString(EmptyKVHash[:]); got != "df3f619804a92fdb4057192dc43dd748ea778adc52bc498ce80524c014b81119" {
		t.Fatalf("EmptyKVHash = %s", got)
	}
}

func TestReferenceZeroBlock(t *testing.T) {
	data := referenceZeroBlock(t)
	b, err := ParseBlock(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b.Marshal(), data) {
		t.Fatal("Marshal differs from the bytes tde2e wrote")
	}
	if got := b.Hash(); hex.EncodeToString(got[:]) != referenceZeroBlockHash {
		t.Fatalf("hash %x", got)
	}
	key := pubKey(testKey(1))
	if b.SignaturePublicKey == nil || *b.SignaturePublicKey != key {
		t.Fatal("signature key is not the key of seed 0x01")
	}
	if err := b.VerifySignature(key); err != nil {
		t.Fatal(err)
	}
	if err := b.VerifySignature(pubKey(testKey(2))); err == nil {
		t.Fatal("signature verified with another key")
	}
	if b.Height != 0 || b.PrevBlockHash != [32]byte{} {
		t.Fatalf("height %d, prev hash %x", b.Height, b.PrevBlockHash)
	}
	if len(b.Changes) != 2 || b.Changes[0].Kind != ChangeSetGroupState || b.Changes[1].Kind != ChangeSetSharedKey {
		t.Fatalf("changes %+v", b.Changes)
	}
	g := b.Changes[0].GroupState
	want := []Participant{{UserID: 1000, PublicKey: key, Flags: 3, Version: 0}}
	if g.ExternalPermissions != 3 || len(g.Participants) != 1 || g.Participants[0] != want[0] {
		t.Fatalf("group state %+v", g)
	}
	k := b.Changes[1].SharedKey
	if len(k.EncryptedSharedKey) != 64 || len(k.DestUserIDs) != 1 || k.DestUserIDs[0] != 1000 || len(k.DestHeaders) != 1 || len(k.DestHeaders[0]) != 32 {
		t.Fatalf("shared key: %d bytes, users %v, %d headers", len(k.EncryptedSharedKey), k.DestUserIDs, len(k.DestHeaders))
	}
	if b.StateProof.KVHash != EmptyKVHash || b.StateProof.GroupState != nil || b.StateProof.SharedKey != nil {
		t.Fatalf("state proof %+v", b.StateProof)
	}
}

func TestBytesEncoding(t *testing.T) {
	tests := []struct {
		n    int
		size int
	}{
		{0, 4}, {1, 4}, {3, 4}, {4, 8}, {253, 256}, {254, 260}, {255, 260}, {256, 260}, {1000, 1004}, {70000, 70004},
	}
	for _, tt := range tests {
		in := bytes.Repeat([]byte{0xab}, tt.n)
		var w writer
		w.bytes(in)
		if len(w.buf) != tt.size {
			t.Errorf("%d bytes: encoded in %d, want %d", tt.n, len(w.buf), tt.size)
		}
		r := reader{buf: w.buf}
		out := r.bytes()
		if err := r.end(); err != nil || !bytes.Equal(in, out) {
			t.Errorf("%d bytes: read %d bytes, %v", tt.n, len(out), err)
		}
	}
}

func TestReaderRefuses(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		read func(r *reader)
	}{
		{"string length byte 255", []byte{255, 0, 0, 0, 0, 0, 0, 0}, func(r *reader) { r.bytes() }},
		{"string longer than the data", []byte{254, 0, 1, 0, 1, 2, 3, 4}, func(r *reader) { r.bytes() }},
		{"negative vector", []byte{0xff, 0xff, 0xff, 0xff}, func(r *reader) { r.count(4) }},
		{"vector longer than the data", []byte{3, 0, 0, 0, 1, 2, 3, 4, 5, 6, 7, 8}, func(r *reader) { r.count(4) }},
		{"short int64", []byte{1, 2, 3, 4}, func(r *reader) { r.int64() }},
		{"wrong constructor", binary.LittleEndian.AppendUint32(nil, idGroupState), func(r *reader) { r.expect(idSharedKey) }},
		{"trailing data", []byte{1, 2, 3, 4, 5}, func(r *reader) { r.uint32() }},
	}
	for _, tt := range tests {
		r := reader{buf: tt.data}
		tt.read(&r)
		if r.end() == nil {
			t.Errorf("%s: accepted", tt.name)
		}
	}
}

// richBlock has every change and both parts of the state proof, so that a
// round trip covers every constructor.
func richBlock(withKey bool) *Block {
	alice, bob := testKey(1), testKey(2)
	group := &GroupState{Participants: []Participant{member(1, 3, alice), {UserID: -7, PublicKey: pubKey(bob), Flags: 1, Version: -2}}, ExternalPermissions: 3}
	key := &SharedKey{EK: [32]byte{9, 8, 7}, EncryptedSharedKey: bytes.Repeat([]byte{1}, 300), DestUserIDs: []int64{1, -7}, DestHeaders: [][]byte{bytes.Repeat([]byte{2}, 32), {}}}
	b := &Block{
		PrevBlockHash: [32]byte{1, 2, 3},
		Changes: []Change{
			{Kind: ChangeNoop, Nonce: [32]byte{5}},
			{Kind: ChangeSetValue, Key: bytes.Repeat([]byte{3}, 32), Value: []byte("value")},
			{Kind: ChangeSetGroupState, GroupState: group},
			{Kind: ChangeSetSharedKey, SharedKey: key},
		},
		Height:     41,
		StateProof: StateProof{KVHash: EmptyKVHash, GroupState: group, SharedKey: key},
	}
	if withKey {
		signBlock(b, alice)
	} else {
		signBlockNoKey(b, alice)
	}
	return b
}

func TestBlockRoundTrip(t *testing.T) {
	tests := []struct {
		name  string
		block *Block
	}{
		{"every change, with the signature key", richBlock(true)},
		{"every change, without the signature key", richBlock(false)},
		{"no changes, empty proof", &Block{Height: -5, StateProof: StateProof{KVHash: EmptyKVHash}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := tt.block.Marshal()
			b, err := ParseBlock(data)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(b.Marshal(), data) {
				t.Fatal("round trip changed the bytes")
			}
			if b.Hash() != sha(data) {
				t.Fatal("Hash is not SHA-256 of the bytes")
			}
			if b.Height != tt.block.Height || len(b.Changes) != len(tt.block.Changes) {
				t.Fatalf("height %d, %d changes", b.Height, len(b.Changes))
			}
			if (b.SignaturePublicKey == nil) != (tt.block.SignaturePublicKey == nil) {
				t.Fatal("signature key presence changed")
			}
		})
	}
	b, err := ParseBlock(richBlock(true).Marshal())
	if err != nil {
		t.Fatal(err)
	}
	if err := b.VerifySignature(pubKey(testKey(1))); err != nil {
		t.Fatal(err)
	}
	if g := b.Changes[2].GroupState; g.Participants[1].UserID != -7 || g.Participants[1].Version != -2 {
		t.Fatalf("participant %+v", g.Participants[1])
	}
}

func TestParseBlockRefuses(t *testing.T) {
	ref := referenceZeroBlock(t)
	for n := 0; n < len(ref); n++ {
		if _, err := ParseBlock(ref[:n]); err == nil {
			t.Fatalf("a block cut to %d bytes is accepted", n)
		}
	}
	server, err := ToServer(ref)
	if err != nil {
		t.Fatal(err)
	}
	unknownChange := append([]byte{}, ref[:4+64+4+32]...)
	unknownChange = binary.LittleEndian.AppendUint32(unknownChange, 1)
	unknownChange = binary.LittleEndian.AppendUint32(unknownChange, 0x12345678)
	unknownChange = append(unknownChange, make([]byte, 64)...)
	hugeVector := append([]byte{}, ref[:4+64+4+32]...)
	hugeVector = binary.LittleEndian.AppendUint32(hugeVector, 1<<30)
	// A field of type # with bit 31 set: the flags of the block, of the
	// participant and of the state proof.
	negativeFlags := func(at int) []byte {
		data := append([]byte{}, ref...)
		binary.LittleEndian.PutUint32(data[at:], binary.LittleEndian.Uint32(data[at:])|1<<31)
		return data
	}
	tests := []struct {
		name string
		data []byte
	}{
		{"trailing data", append(append([]byte{}, ref...), 0, 0, 0, 0)},
		{"server form", server},
		{"broadcast", (&Broadcast{Kind: BroadcastCommit}).Marshal()},
		{"unknown change", unknownChange},
		{"vector longer than the data", hugeVector},
		{"negative block flags", negativeFlags(4 + 64)},
		{"negative participant flags", negativeFlags(4 + 64 + 4 + 32 + 4 + 4 + 4 + 4 + 4 + 8 + 32)},
		{"negative state proof flags", negativeFlags(len(ref) - 32 - 32 - 4)},
	}
	for _, tt := range tests {
		_, err := ParseBlock(tt.data)
		wantErr(t, err, KindInvalid, CodeInvalidBlock)
	}
}

func TestUndefinedFlagsDropped(t *testing.T) {
	// tde2e reads a block's flags and writes back only bit 0
	// (Blockchain.cpp:209-221), so the hash and the signature cover the
	// rebuilt bytes and an extra bit changes neither.
	ref := referenceZeroBlock(t)
	data := append([]byte{}, ref...)
	const flagsAt = 4 + 64
	binary.LittleEndian.PutUint32(data[flagsAt:], binary.LittleEndian.Uint32(data[flagsAt:])|1<<5)
	b, err := ParseBlock(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(b.Marshal(), ref) {
		t.Fatal("undefined flag bit kept")
	}
	if err := b.VerifySignature(pubKey(testKey(1))); err != nil {
		t.Fatal(err)
	}
	a, err := NewChain().Join(Join{UserID: 1000, PublicKey: pubKey(testKey(1)), Block: data})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes, ref) {
		t.Fatal("the stored form is not the rebuilt block")
	}
}

func TestWireForm(t *testing.T) {
	for _, id := range []uint32{idBlock, idBroadcastCommit, idBroadcastReveal} {
		local := binary.LittleEndian.AppendUint32(nil, id)
		local = append(local, 1, 2, 3)
		keep := append([]byte{}, local...)
		server, err := ToServer(local)
		if err != nil {
			t.Fatal(err)
		}
		if got := binary.LittleEndian.Uint32(server); got != id+1 || !bytes.Equal(server[4:], local[4:]) {
			t.Fatalf("%08x: server form %x", id, server)
		}
		if !bytes.Equal(local, keep) {
			t.Fatal("ToServer changed its input")
		}
		if !IsFromServer(server) || IsFromServer(local) {
			t.Fatalf("%08x: IsFromServer wrong", id)
		}
		back, err := ToLocal(server)
		if err != nil || !bytes.Equal(back, local) {
			t.Fatalf("%08x: ToLocal %x, %v", id, back, err)
		}
		if _, err := ToLocal(local); err == nil {
			t.Fatalf("%08x: ToLocal accepted the local form", id)
		}
		if _, err := ToServer(server); err == nil {
			t.Fatalf("%08x: ToServer accepted the server form", id)
		}
	}
	for _, short := range [][]byte{nil, {1, 2, 3}} {
		if _, err := ToServer(short); err == nil {
			t.Fatal("ToServer accepted a short block")
		}
		if _, err := ToLocal(short); err == nil {
			t.Fatal("ToLocal accepted a short block")
		}
		if IsFromServer(short) {
			t.Fatal("IsFromServer of a short block")
		}
	}
	// The reference block as the server hands it out starts with b7 3d 9a 63.
	server, err := ToServer(referenceZeroBlock(t))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(server[:4], []byte{0xb7, 0x3d, 0x9a, 0x63}) {
		t.Fatalf("server form starts with %x", server[:4])
	}
}
