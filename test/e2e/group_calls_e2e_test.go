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

//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/groupcallpeer"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"github.com/teamgram/teamgram-server/pkg/tde2e/peer"
	"github.com/teamgram/teamgram-server/pkg/tde2e/tde2etest"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The end-to-end encryption of group calls through the MTProto port and
// the SFU of the stand (task 094, ADR 0060), with clients of
// pkg/groupcallpeer that hold real keys and build their blocks and frames
// as tde2e does: the SFU and the other clients see real frames of tde2e.

// waitE2E waits until match holds for the state of every member.
func waitE2E(ctx context.Context, t *testing.T, what string, within time.Duration, match func(*member) bool, ms ...*member) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		all := true
		for _, m := range ms {
			all = all && match(m)
		}
		if all {
			return
		}
		if time.Now().After(deadline) {
			var state []string
			for _, m := range ms {
				c := m.e2e().Call()
				state = append(state, fmt.Sprintf("%s at %d (%v)", m.name(), c.Height(), c.Err()))
			}
			t.Fatalf("%s: %s", what, strings.Join(state, ", "))
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// atHeight is a member whose call applied the block of height h.
func atHeight(h int32) func(*member) bool {
	return func(m *member) bool { c := m.e2e().Call(); return c.Err() == nil && c.Height() >= h }
}

func invoke(ctx context.Context, u *user, req mtproto.TLObject) error {
	_, err := u.c.Invoke(ctx, req)
	return err
}

// privateKey is the key of m's current join.
func (m *member) privateKey() ed25519.PrivateKey {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.key
}

// joinWithKey is a join of u that the test builds by hand with key on the
// last block.
func joinWithKey(ctx context.Context, t *testing.T, u *user, call *mtproto.InputGroupCall, key ed25519.PrivateKey) *mtproto.TLPhoneJoinGroupCall {
	t.Helper()
	req := joinRequest(u, call, mustParams(t))
	pub := peer.Public(key)
	req.PublicKey = pub[:]
	last, err := lastServerBlock(ctx, u, call)
	if err != nil {
		t.Fatal(err)
	}
	if req.Block, err = groupcallpeer.E2EJoinBlock(u.self.GetId(), key, last); err != nil {
		t.Fatal(err)
	}
	return req
}

// stolenJoin is a join of u with the key of another participant: a client
// cannot even build it (the group state would hold the key twice), so the
// test writes it without the client's checks.
func stolenJoin(ctx context.Context, t *testing.T, u *user, call *mtproto.InputGroupCall, key ed25519.PrivateKey) *mtproto.TLPhoneJoinGroupCall {
	t.Helper()
	req := joinRequest(u, call, mustParams(t))
	pub := peer.Public(key)
	req.PublicKey = pub[:]
	req.Block = tde2etest.JoinBlock(lastChain(ctx, t, u, call), u.self.GetId(), key)
	return req
}

func removal(m *member, kick bool, ids ...int64) *mtproto.TLPhoneDeleteConferenceCallParticipants {
	block, err := m.e2e().RemoveBlock(ids...)
	if err != nil {
		panic(err)
	}
	return &mtproto.TLPhoneDeleteConferenceCallParticipants{
		Constructor: m.u.c.Constructor(mtproto.Predicate_phone_deleteConferenceCallParticipants),
		OnlyLeft:    !kick, Kick: kick, Call: m.call, Ids: ids, Block: block,
	}
}

// The chain and the frames of one call (ADR 0060 §2): a client without
// the encryption is not let in; the first writes block 0, the next join on
// it, and a join on a taken height is CONF_WRITE_CHAIN_INVALID; the
// participants hear each other through frames only they decrypt; a third
// joins and the keys rotate; the emoji agree; one who left is removed by
// the others, the first removal is taken, the removed one may not write and
// reads only the last block, the others go on with a new key; he comes
// back with his key, which nobody else may use; RTMP is never served.
func TestGroupCallE2EChain(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	us, ch, gc := supergroupCall(ctx, t, "Alice", "Bob", "Carol")
	alice, bob, carol := us[0], us[1], us[2]
	aliceCall, bobCall, carolCall := groupCallInput(alice, gc), groupCallInput(bob, gc), groupCallInput(carol, gc)

	// A client without the encryption is not let in. A key without a block
	// cannot be sent: both are under flags.3 of phone.joinGroupCall.
	if err := invoke(ctx, alice, joinRequest(alice, aliceCall, mustParams(t))); !mtclient.IsRPCError(err, "GROUPCALL_FORBIDDEN") {
		t.Fatalf("a join without the encryption: %v", err)
	}

	ma := joinMember(ctx, t, alice, aliceCall, memberOptions{})
	if h := ma.e2e().Call().Height(); h != 0 {
		t.Fatalf("Alice's join is at height %d, want block 0", h)
	}
	// Carol builds a join on block 0 and is late with it.
	carolKey, err := peer.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	stale := joinWithKey(ctx, t, carol, carolCall, carolKey)
	mb := joinMember(ctx, t, bob, bobCall, memberOptions{})
	if err := invoke(ctx, carol, stale); !mtclient.IsRPCError(err, "CONF_WRITE_CHAIN_INVALID") {
		t.Fatalf("a join on a taken height: %v", err)
	}
	waitE2E(ctx, t, "both at the join of Bob", 10*time.Second, atHeight(1), ma, mb)
	speakMember(ctx, t, ma)
	speakMember(ctx, t, mb)
	hearsMember(ctx, t, mb, ma)
	hearsMember(ctx, t, ma, mb)

	// A third: the keys rotate, so Carol, who has only the newest epoch,
	// hears the others, and they hear her.
	mc := joinMember(ctx, t, carol, carolCall, memberOptions{})
	waitE2E(ctx, t, "all at the join of Carol", 10*time.Second, atHeight(2), ma, mb, mc)
	speakMember(ctx, t, mc)
	hearsMember(ctx, t, mc, ma)
	hearsMember(ctx, t, mc, mb)
	hearsMember(ctx, t, ma, mc)
	hearsMember(ctx, t, mb, mc)
	// The emoji of the three agree (Call.cpp:241-255).
	var emoji []byte
	waitE2E(ctx, t, "the emoji verification", 20*time.Second, func(m *member) bool {
		h, ok := m.e2e().Call().EmojiHash()
		if ok && emoji == nil {
			emoji = h
		}
		return ok && bytes.Equal(h, emoji)
	}, ma, mb, mc)

	// Bob leaves; Alice and Carol each remove him: one removal is taken.
	bobKey := mb.privateKey()
	mb.leave(ctx, t)
	fromAlice, fromCarol := removal(ma, false, bob.self.GetId()), removal(mc, false, bob.self.GetId())
	if err := invoke(ctx, alice, fromAlice); err != nil {
		t.Fatalf("Alice removes Bob: %v", err)
	}
	if err := invoke(ctx, carol, fromCarol); !mtclient.IsRPCError(err, "CONF_WRITE_CHAIN_INVALID") {
		t.Fatalf("Carol removes Bob after Alice: %v", err)
	}
	// The removed one reads the last block only, may not write.
	if up, err := chainBlocks(ctx, t, bob, bobCall, 0, 0, 100); err != nil || len(up.GetBlocks()) != 1 || up.GetNextOffset() != 4 {
		t.Fatalf("the removed one reads %v %v", up, err)
	}
	var nonce [32]byte
	if err := invoke(ctx, bob, &mtproto.TLPhoneSendConferenceCallBroadcast{
		Constructor: bob.c.Constructor(mtproto.Predicate_phone_sendConferenceCallBroadcast), Call: bobCall,
		Block: tde2etest.Commit(lastChain(ctx, t, alice, aliceCall), bob.self.GetId(), bobKey, nonce),
	}); !mtclient.IsRPCError(err, "GROUPCALL_FORBIDDEN") {
		t.Fatalf("a broadcast of the removed one: %v", err)
	}
	// The two go on with a new key, which Bob never had.
	waitE2E(ctx, t, "both after the removal", 10*time.Second, atHeight(3), ma, mc)
	for _, m := range []*member{ma, mc} {
		if _, in := m.e2e().Call().GroupState().Participant(bob.self.GetId()); in {
			t.Fatalf("%s still has Bob in the call", m.name())
		}
	}
	time.Sleep(peer.ForgetEpochDelay + time.Second)
	hearsMember(ctx, t, ma, mc)
	hearsMember(ctx, t, mc, ma)

	// Bob comes back with his key; Carol may not take it.
	if err := invoke(ctx, bob, joinWithKey(ctx, t, bob, bobCall, bobKey)); err != nil {
		t.Fatalf("Bob back with his key: %v", err)
	}
	if err := invoke(ctx, carol, stolenJoin(ctx, t, carol, carolCall, bobKey)); !mtclient.IsRPCError(err, "BLOCK_INVALID") {
		t.Fatalf("a join with the key of another: %v", err)
	}
	for _, m := range []*member{ma, mc} {
		if h := m.heals(); len(h) != 0 {
			t.Fatalf("%s rejoined: %v", m.name(), h)
		}
	}

	// RTMP and the recording are never served.
	chatInput, _ := channelInputs(alice, ch)
	if err := invoke(ctx, alice, &mtproto.TLPhoneCreateGroupCall{
		Constructor: alice.c.Constructor(mtproto.Predicate_phone_createGroupCall), Peer: chatInput, RtmpStream: true, RandomId: 5, Title: wrapperspb.String("stream"),
	}); err == nil {
		t.Fatal("a call with an RTMP stream created")
	}
	if err := invoke(ctx, alice, &mtproto.TLPhoneGetGroupCallStreamRtmpUrl{
		Constructor: alice.c.Constructor(mtproto.Predicate_phone_getGroupCallStreamRtmpUrl), Peer: chatInput, Revoke: mtproto.ToBool(false),
	}); err == nil {
		t.Fatal("an RTMP URL given")
	}
	if err := invoke(ctx, alice, &mtproto.TLPhoneToggleGroupCallRecord{
		Constructor: alice.c.Constructor(mtproto.Predicate_phone_toggleGroupCallRecord), Call: aliceCall, Start: true,
	}); err == nil {
		t.Fatal("a recording started")
	}
}

// An admin kicks a participant out of the chain: his call fails with
// NotParticipant and the client joins again with a new key by the rule
// (ADR 0060 §5, condition 1), since he may still join; one who does not
// manage the call may not kick. A client back from a lost connection joins
// with the same key and stays the same participant; nobody else may use
// his key.
func TestGroupCallE2EKickAndReconnect(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	us, _, gc := supergroupCall(ctx, t, "Alice", "Bob", "Carol")
	alice, bob, carol := us[0], us[1], us[2]
	ma := joinMember(ctx, t, alice, groupCallInput(alice, gc), memberOptions{})
	mb := joinMember(ctx, t, bob, groupCallInput(bob, gc), memberOptions{})
	mc := joinMember(ctx, t, carol, groupCallInput(carol, gc), memberOptions{})
	for _, m := range []*member{ma, mb, mc} {
		speakMember(ctx, t, m)
	}
	waitE2E(ctx, t, "all three", 10*time.Second, atHeight(2), ma, mb, mc)
	hearsMember(ctx, t, mc, ma)

	if err := invoke(ctx, bob, removal(mb, true, alice.self.GetId())); !mtclient.IsRPCError(err, "GROUPCALL_FORBIDDEN") {
		t.Fatalf("Bob kicks Alice: %v", err)
	}
	kicked := mc.e2e().Call().PublicKey()
	if err := invoke(ctx, alice, removal(ma, true, carol.self.GetId())); err != nil {
		t.Fatalf("Alice kicks Carol: %v", err)
	}
	if why := mc.waitHealed(ctx, t, 1, 30*time.Second); why[0] != groupcallpeer.RejoinError {
		t.Fatalf("Carol rejoined for %q", why[0])
	}
	if mc.e2e().Call().PublicKey() == kicked {
		t.Fatal("Carol rejoined with the key she was kicked with")
	}
	waitE2E(ctx, t, "all after Carol is back", 10*time.Second, atHeight(4), ma, mb, mc)
	hearsMember(ctx, t, mc, ma)
	hearsMember(ctx, t, ma, mc)
	hearsMember(ctx, t, mb, mc)

	// Bob's connection drops; he comes back with the same key.
	key, height := mb.e2e().Call().PublicKey(), mb.e2e().Call().Height()
	mb.reconnect(ctx, t)
	if mb.e2e().Call().PublicKey() != key || mb.e2e().Call().Height() <= height || len(mb.heals()) != 0 {
		t.Fatalf("Bob after the join again: height %d after %d, rejoins %v", mb.e2e().Call().Height(), height, mb.heals())
	}
	hearsMember(ctx, t, ma, mb)
	hearsMember(ctx, t, mb, mc)
	// The end of his old connection, when the SFU notices it, does not
	// take out the new one.
	time.Sleep(3 * time.Second)
	got := groupParticipants(ctx, t, alice, &mtproto.TLPhoneGetGroupParticipants{Call: groupCallInput(alice, gc), Ids: []*mtproto.InputPeer{peerOf(ctx, t, alice, bob)}})
	if len(got.GetParticipants()) != 1 || got.GetParticipants()[0].GetLeft() || uint32(got.GetParticipants()[0].GetSource()) != mb.peer().AudioSSRC() {
		t.Fatalf("Bob after the join again: %v", got.GetParticipants())
	}
	// Carol may not join with Bob's key.
	if err := invoke(ctx, carol, stolenJoin(ctx, t, carol, groupCallInput(carol, gc), mb.privateKey())); !mtclient.IsRPCError(err, "BLOCK_INVALID") {
		t.Fatalf("a join with the key of another: %v", err)
	}
	for _, m := range []*member{ma, mb} {
		if h := m.heals(); len(h) != 0 {
			t.Fatalf("%s rejoined: %v", m.name(), h)
		}
	}
}

var showColumns = regexp.MustCompile(`\s{2,}`)

// callRows runs pluma-admin calls show and returns its rows by user, each
// by column.
func callRows(t *testing.T, callID int64) (string, map[int64]map[string]string) {
	t.Helper()
	out := admin(t, "calls", "show", strconv.FormatInt(callID, 10))
	var header []string
	rows := map[int64]map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		cells := showColumns.Split(strings.TrimSpace(line), -1)
		if cells[0] == "user" {
			header = cells
			continue
		}
		uid, err := strconv.ParseInt(cells[0], 10, 64)
		if err != nil || header == nil || len(cells) != len(header) {
			continue
		}
		row := map[string]string{}
		for i, h := range header {
			row[h] = cells[i]
		}
		rows[uid] = row
	}
	return out, rows
}

// waitShow polls pluma-admin calls show until match holds for the row of
// userID: the diagnostics are written every 5 s.
func waitShow(ctx context.Context, t *testing.T, callID, userID int64, what string, within time.Duration, match func(row map[string]string) bool) map[string]string {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		out, rows := callRows(t, callID)
		if row := rows[userID]; row != nil && match(row) {
			return row
		}
		if time.Now().After(deadline) {
			t.Fatalf("pluma-admin calls show, %s:\n%s", what, out)
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
}

// Broken participants and what the operator sees (ADR 0060 §5): the SFU
// sees the epoch a participant sends with and the reports reach the
// server; one who stops applying blocks goes on with his old epoch, which
// the others forget, and pluma-admin calls show explains it with the
// alarms of the lagging poll and the stale epoch and with the reports of
// the others; let go, the rule of self-healing brings him back with a new
// key and the alarms clear. One whose key is wrong decrypts nobody and
// nobody him; the rule brings him back too.
func TestGroupCallE2EDesync(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	us, _, gc := supergroupCall(ctx, t, "Alice", "Bob", "Carol", "Dave")
	alice, bob, carol, dave := us[0], us[1], us[2], us[3]
	ma := joinMember(ctx, t, alice, groupCallInput(alice, gc), memberOptions{})
	mb := joinMember(ctx, t, bob, groupCallInput(bob, gc), memberOptions{})
	mc := joinMember(ctx, t, carol, groupCallInput(carol, gc), memberOptions{noHeal: true})
	for _, m := range []*member{ma, mb, mc} {
		speakMember(ctx, t, m)
	}
	waitE2E(ctx, t, "all three", 10*time.Second, atHeight(2), ma, mb, mc)
	hearsMember(ctx, t, mc, ma)

	// The SFU sees what Alice sends with, the epoch of the current height,
	// and her reports arrive.
	waitShow(ctx, t, gc.GetId(), alice.self.GetId(), "Alice sends with the current epoch and reports", 30*time.Second, func(r map[string]string) bool {
		return strings.HasPrefix(r["sends with"], "2 (") && r["report"] != "-" && r["height"] == "2" && r["alarms"] == "-"
	})

	// Carol stops applying blocks; Dave joins.
	mc.e2e().Break(groupcallpeer.StaleChain)
	stale := mc.e2e().Call().LastHash()
	md := joinMember(ctx, t, dave, groupCallInput(dave, gc), memberOptions{})
	speakMember(ctx, t, md)
	waitE2E(ctx, t, "the others at the join of Dave", 10*time.Second, atHeight(3), ma, mb, md)
	row := waitShow(ctx, t, gc.GetId(), carol.self.GetId(), "Carol's alarms", 60*time.Second, func(r map[string]string) bool {
		return strings.Contains(r["alarms"], "poll_behind") && strings.Contains(r["alarms"], "stale_send_epoch")
	})
	if row["height"] != "2" || !strings.HasPrefix(row["block"], hex.EncodeToString(stale[:8])) || row["chain"] != "yes" || row["call"] != "yes" {
		t.Fatalf("Carol's row: %v", row)
	}
	// The others no longer decrypt her: their reports name her with
	// frames of an epoch they forgot, and none decrypted.
	unknown := regexp.MustCompile(fmt.Sprintf(`(^| )%d:0/[1-9]`, carol.self.GetId()))
	arow := waitShow(ctx, t, gc.GetId(), alice.self.GetId(), "Alice reports Carol's unknown epoch", 30*time.Second, func(r map[string]string) bool {
		return unknown.MatchString(r["senders ok/unknown/old/dup/failed"])
	})
	// Her own state is sound; the verification of the height waits for
	// Carol's commit, which concerns everyone.
	for _, a := range strings.Split(arow["alarms"], ",") {
		if a != "-" && a != "verification_stuck" {
			t.Fatalf("Alice in an alarm: %v", arow)
		}
	}

	// Let go, Carol comes back with a new key: she is behind for long.
	mc.setHeal(true)
	if why := mc.waitHealed(ctx, t, 1, 30*time.Second); why[0] != groupcallpeer.RejoinBehind {
		t.Fatalf("Carol rejoined for %q", why[0])
	}
	hearsMember(ctx, t, mc, ma)
	hearsMember(ctx, t, ma, mc)
	waitShow(ctx, t, gc.GetId(), carol.self.GetId(), "Carol's alarms clear", 60*time.Second, func(r map[string]string) bool {
		return r["alarms"] == "-" && r["joins 5m"] != "0"
	})

	// Bob's key goes wrong: nobody decrypts anybody for him, he comes back.
	mb.stop()
	mb.peer().Close()
	wrong := joinMember(ctx, t, bob, groupCallInput(bob, gc), memberOptions{wrongKey: true})
	speakMember(ctx, t, wrong)
	if why := wrong.waitHealed(ctx, t, 1, 45*time.Second); why[0] != groupcallpeer.RejoinUndecrypted {
		t.Fatalf("the wrong key rejoined for %q", why[0])
	}
	hearsMember(ctx, t, wrong, ma)
	hearsMember(ctx, t, ma, wrong)
	for _, m := range []*member{ma, md} {
		if h := m.heals(); len(h) != 0 {
			t.Fatalf("%s rejoined: %v", m.name(), h)
		}
	}
}
