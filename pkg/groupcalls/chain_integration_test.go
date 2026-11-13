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

//go:build integration

package groupcalls

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/pkg/tde2e"
	"github.com/teamgram/teamgram-server/pkg/tde2e/tde2etest"
)

func kindOf(err error) (tde2e.Kind, bool) {
	var e *tde2e.Error
	if errors.As(err, &e) {
		return e.Kind, true
	}
	return 0, false
}

// Ten joins built on the same last block race: exactly one block takes the
// height, the others are stale and leave nothing behind; the versions have
// no gap.
func TestChainJoinsRaceOneHeight(t *testing.T) {
	s, ctx := itStore(), context.Background()
	c := newCall(t, s)
	first := itId()
	if _, err := join(s, c.Id, first, 1); err != nil {
		t.Fatal(err)
	}
	ch, err := s.Chain(ctx, c.Id)
	if err != nil {
		t.Fatal(err)
	}
	base := itId() * 100
	var (
		wg              sync.WaitGroup
		mu              sync.Mutex
		won, stale, bad int
	)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(uid int64, source uint32) {
			defer wg.Done()
			key := userKey(uid)
			_, err := s.Join(ctx, JoinParams{
				CallId: c.Id, UserId: uid, Source: source,
				State: func(call *Call, prev *Participant) (bool, bool) { return JoinState(call, prev, false, false, false) },
				Chain: &ChainJoin{PublicKey: tde2etest.Pub(key), Block: tde2etest.JoinBlock(ch, uid, key)},
			})
			mu.Lock()
			defer mu.Unlock()
			switch k, ok := kindOf(err); {
			case err == nil:
				won++
			case ok && k == tde2e.KindStale:
				stale++
			default:
				bad++
				t.Errorf("join of %d: %v", uid, err)
			}
		}(base+int64(i), uint32(100+i))
	}
	wg.Wait()
	if won != 1 || stale != 9 || bad != 0 {
		t.Fatalf("won %d, stale %d, other %d", won, stale, bad)
	}
	after, err := s.Get(ctx, c.Id)
	if err != nil {
		t.Fatal(err)
	}
	if after.ChainHeight != 1 || after.ParticipantsCount != 2 || after.Version != 3 {
		t.Fatalf("after the race: height %d, %d in the call, version %d", after.ChainHeight, after.ParticipantsCount, after.Version)
	}
	// The losers join on the new last block, one by one: the versions go
	// on without a gap, the heights too.
	for i := 0; i < 10; i++ {
		uid := base + int64(i)
		if p, err := s.ParticipantOf(ctx, c.Id, uid); err == nil && p.InCall {
			continue
		}
		r, err := join(s, c.Id, uid, uint32(100+i))
		if err != nil {
			t.Fatal(err)
		}
		after = r.Call
	}
	if after.ChainHeight != 10 || after.Version != 12 || after.ParticipantsCount != 11 {
		t.Fatalf("after all joined: height %d, version %d, %d in the call", after.ChainHeight, after.Version, after.ParticipantsCount)
	}
	blocks, err := s.Blocks(ctx, c.Id, SubChainMain, 0, 100)
	if err != nil || len(blocks) != 11 {
		t.Fatalf("blocks: %d %v", len(blocks), err)
	}
	// The chain of the stored blocks is whole: each on the one before.
	replay := tde2e.NewChain()
	for _, b := range blocks {
		server, err := b.Server()
		if err != nil || !tde2e.IsFromServer(server) {
			t.Fatalf("block %d in the server form: %v", b.Idx, err)
		}
		if err := tde2etest.Follow(replay, server); err != nil {
			t.Fatalf("block %d: %v", b.Idx, err)
		}
	}
	if replay.LastHash() != after.ChainHash {
		t.Fatal("the stored blocks do not end at the chain state")
	}
}

// After an SFU restart every participant joins again at once, each with a
// block on the same height (ADR 0060 §5). Each round takes one; the others
// read the last block again and retry. A client gives up only after
// chainRetries refusals in a row with the chain standing still: while it
// moves, the refusal is the race, and all get in. A cap on refusals in a
// row whatever the chain does drops those who lose most often once there
// are more joiners than the cap.
func TestChainJoinsHerd(t *testing.T) {
	const (
		herd         = 30
		chainRetries = 10
	)
	s, ctx := itStore(), context.Background()
	c := newCall(t, s)
	base := itId() * 100
	for i := 0; i < herd; i++ {
		if _, err := join(s, c.Id, base+int64(i), uint32(1000+i)); err != nil {
			t.Fatal(err)
		}
	}
	start, err := s.Get(ctx, c.Id)
	if err != nil {
		t.Fatal(err)
	}
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		failed  []int64
		longest int
	)
	gate := make(chan struct{})
	for i := 0; i < herd; i++ {
		wg.Add(1)
		go func(uid int64, source uint32) {
			defer wg.Done()
			<-gate
			key := userKey(uid)
			still, lost, built := 0, 0, int32(-2)
			for {
				ch, err := s.Chain(ctx, c.Id)
				if err != nil {
					t.Error(err)
					return
				}
				if ch.Height() > built {
					still = 0
				}
				built = ch.Height()
				_, err = s.Join(ctx, JoinParams{
					CallId: c.Id, UserId: uid, Source: source,
					State: func(call *Call, prev *Participant) (bool, bool) { return JoinState(call, prev, false, false, false) },
					Chain: &ChainJoin{PublicKey: tde2etest.Pub(key), Block: tde2etest.JoinBlock(ch, uid, key)},
				})
				if k, ok := kindOf(err); ok && k == tde2e.KindStale {
					lost++
					if still++; still < chainRetries {
						continue
					}
				}
				mu.Lock()
				longest = max(longest, lost)
				if err != nil {
					failed = append(failed, uid)
					t.Errorf("join again of %d after %d refusals: %v", uid, lost, err)
				}
				mu.Unlock()
				return
			}
		}(base+int64(i), uint32(2000+i))
	}
	close(gate)
	wg.Wait()
	if len(failed) > 0 {
		t.Fatalf("%d of %d did not get in", len(failed), herd)
	}
	t.Logf("the unluckiest joiner lost %d times", longest)
	after, err := s.Get(ctx, c.Id)
	if err != nil {
		t.Fatal(err)
	}
	if after.ChainHeight != start.ChainHeight+herd || after.Version != start.Version+herd || after.ParticipantsCount != herd {
		t.Fatalf("after the herd: height %d (from %d), version %d (from %d), %d in the call",
			after.ChainHeight, start.ChainHeight, after.Version, start.Version, after.ParticipantsCount)
	}
}

// A key is its first user's for the life of the call: he joins with it
// again, in the call or after his removal; no one else may.
func TestChainKeyOwner(t *testing.T) {
	s, ctx := itStore(), context.Background()
	c := newCall(t, s)
	a, b := itId(), itId()
	for i, uid := range []int64{a, b} {
		if _, err := join(s, c.Id, uid, uint32(10+i)); err != nil {
			t.Fatal(err)
		}
	}
	// a restores his transport with the same key: a block replaces his
	// entry.
	if _, err := join(s, c.Id, a, 12); err != nil {
		t.Fatalf("a join again with the same key: %v", err)
	}
	ch, err := s.Chain(ctx, c.Id)
	if err != nil {
		t.Fatal(err)
	}
	stolen := userKey(a)
	_, err = s.Join(ctx, JoinParams{
		CallId: c.Id, UserId: b, Source: 13,
		State: func(call *Call, prev *Participant) (bool, bool) { return JoinState(call, prev, false, false, false) },
		Chain: &ChainJoin{PublicKey: tde2etest.Pub(stolen), Block: tde2etest.JoinBlock(ch, b, stolen)},
	})
	if k, ok := kindOf(err); !ok || k != tde2e.KindInvalid {
		t.Fatalf("a join with the key of another: %v", err)
	}
	// a leaves and b removes him from the chain; a comes back with his
	// key.
	if _, _, err = s.Leave(ctx, c.Id, a, LeaveCond{}); err != nil {
		t.Fatal(err)
	}
	w, err := s.RemoveFromChain(ctx, RemoveParams{
		CallId: c.Id, CallerId: b, Ids: []int64{a}, Block: tde2etest.RemoveBlock(ch, userKey(b), a),
		Check: func(_ *Call, rows []*Participant) error {
			for _, p := range rows {
				if p.InCall {
					return errors.New("in the call")
				}
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Members) != 2 || w.Next != ch.Height()+2 {
		t.Fatalf("the removal goes to %v, next %d", w.Members, w.Next)
	}
	if after, _ := s.Chain(ctx, c.Id); after == nil || len(after.GroupState().Participants) != 1 {
		t.Fatal("a is still in the group state")
	}
	if _, err := join(s, c.Id, a, 14); err != nil {
		t.Fatalf("a join with the own key after the removal: %v", err)
	}
}

// A removal the caller's check refuses writes nothing; a kick takes the
// users out of the call, versioned; a writer not in the group state is
// forbidden.
func TestChainRemoval(t *testing.T) {
	s, ctx := itStore(), context.Background()
	c := newCall(t, s)
	a, b, d := itId(), itId(), itId()
	for i, uid := range []int64{a, b, d} {
		if _, err := join(s, c.Id, uid, uint32(20+i)); err != nil {
			t.Fatal(err)
		}
	}
	ch, err := s.Chain(ctx, c.Id)
	if err != nil {
		t.Fatal(err)
	}
	refused := errors.New("still in the call")
	if _, err = s.RemoveFromChain(ctx, RemoveParams{
		CallId: c.Id, CallerId: a, Ids: []int64{b}, Block: tde2etest.RemoveBlock(ch, userKey(a), b),
		Check: func(*Call, []*Participant) error { return refused },
	}); !errors.Is(err, refused) {
		t.Fatalf("a refused removal: %v", err)
	}
	if got, _ := s.Get(ctx, c.Id); got.ChainHeight != ch.Height() {
		t.Fatal("a refused removal moved the chain")
	}
	before, _ := s.Get(ctx, c.Id)
	w, err := s.RemoveFromChain(ctx, RemoveParams{
		CallId: c.Id, CallerId: a, Ids: []int64{b, d}, Block: tde2etest.RemoveBlock(ch, userKey(a), b, d), Kick: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Changes) != 2 || w.Call.Version != before.Version+2 || w.Call.ParticipantsCount != 1 {
		t.Fatalf("the kick: %d changes, version %d after %d, %d in the call", len(w.Changes), w.Call.Version, before.Version, w.Call.ParticipantsCount)
	}
	ch, _ = s.Chain(ctx, c.Id)
	if _, err = s.RemoveFromChain(ctx, RemoveParams{
		CallId: c.Id, CallerId: b, Ids: []int64{a}, Block: tde2etest.RemoveBlock(ch, userKey(b), a),
	}); func() bool { k, ok := kindOf(err); return !ok || k != tde2e.KindForbidden }() {
		t.Fatalf("a removal by one removed: %v", err)
	}
}

// The broadcasts of sub-chain 1: indexes over the life of the call, the
// verification restored from the stored ones at every write, an old height
// answered and not kept, a writer not in the group state forbidden.
func TestChainBroadcasts(t *testing.T) {
	s, ctx := itStore(), context.Background()
	c := newCall(t, s)
	a, b := itId(), itId()
	for i, uid := range []int64{a, b} {
		if _, err := join(s, c.Id, uid, uint32(30+i)); err != nil {
			t.Fatal(err)
		}
	}
	ch, err := s.Chain(ctx, c.Id)
	if err != nil {
		t.Fatal(err)
	}
	na, nb := [32]byte{1}, [32]byte{2}
	steps := []struct {
		user int64
		data []byte
	}{
		{a, tde2etest.Commit(ch, a, userKey(a), na)},
		{b, tde2etest.Commit(ch, b, userKey(b), nb)},
		{b, tde2etest.Reveal(ch, b, userKey(b), nb)},
		{a, tde2etest.Reveal(ch, a, userKey(a), na)},
	}
	for i, st := range steps {
		w, err := s.AppendBroadcast(ctx, c.Id, st.user, st.data)
		if err != nil {
			t.Fatalf("broadcast %d: %v", i, err)
		}
		if w.Block == nil || w.Block.Idx != int32(i) || w.Next != int32(i+1) || len(w.Members) != 2 {
			t.Fatalf("broadcast %d: %+v", i, w)
		}
	}
	if _, v, err := s.Verification(ctx, c.Id); err != nil || v.Phase() != tde2e.PhaseEnd {
		t.Fatalf("the verification is not over: %v", err)
	}
	// A third user joins: a new height; a broadcast of the old one is
	// answered and not kept, the count goes on.
	d := itId()
	if _, err := join(s, c.Id, d, 33); err != nil {
		t.Fatal(err)
	}
	w, err := s.AppendBroadcast(ctx, c.Id, a, tde2etest.Commit(ch, a, userKey(a), na))
	if err != nil || w.Block != nil || w.Next != 4 {
		t.Fatalf("an old broadcast: %+v %v", w, err)
	}
	if _, err = s.AppendBroadcast(ctx, c.Id, itId(), tde2etest.Commit(ch, a, userKey(a), na)); func() bool { k, ok := kindOf(err); return !ok || k != tde2e.KindForbidden }() {
		t.Fatalf("a broadcast of one not in the group state: %v", err)
	}
	stored, err := s.Blocks(ctx, c.Id, SubChainBroadcast, 0, 100)
	if err != nil || len(stored) != 4 {
		t.Fatalf("sub-chain 1: %d %v", len(stored), err)
	}
}

// The collector deletes the chain of a call over for longer than kept, and
// only of such a call.
func TestClearChains(t *testing.T) {
	s, ctx := itStore(), context.Background()
	over, live := newCall(t, s), newCall(t, s)
	for _, c := range []*Call{over, live} {
		if _, err := join(s, c.Id, itId(), 40); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, _, err := s.Discard(ctx, over.Id); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveDiag(ctx, over.Id, 1, time.Now(), &E2EDiag{Rejoins: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClearChains(ctx, time.Now().Add(-time.Hour), 100); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, over.Id); got.ChainHeight != 0 {
		t.Fatalf("the chain of a call over less than an hour ago: %d", got.ChainHeight)
	}
	if _, err := s.ClearChains(ctx, time.Now().Add(2*time.Second), 1000); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(ctx, over.Id); got.ChainHeight != -1 {
		t.Fatalf("the chain of the ended call: %d", got.ChainHeight)
	}
	if b, _ := s.Blocks(ctx, over.Id, SubChainMain, 0, 10); len(b) != 0 {
		t.Fatal("blocks of the ended call stay")
	}
	if d, _ := s.Diags(ctx, over.Id); len(d) != 0 {
		t.Fatal("diagnostics of the ended call stay")
	}
	if got, _ := s.Get(ctx, live.Id); got.ChainHeight != 0 {
		t.Fatalf("the chain of the live call: %d", got.ChainHeight)
	}
}
