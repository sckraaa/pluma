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
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/groupcallpeer"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"github.com/teamgram/teamgram-server/pkg/tde2e/peer"
)

// Conferences (task 117, ADR 0075): group calls without a chat, end-to-end
// encrypted as every group call is, joined by the link, by an invitation
// that rings as a call, or from a one-to-one call. Every session that waits
// for pushes asks updates.getState first, as the clients do.

func createConference(ctx context.Context, t *testing.T, u *user) *mtproto.GroupCall {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLPhoneCreateConferenceCall7D0444BB{
		Constructor: u.c.Constructor(mtproto.Predicate_phone_createConferenceCall7D0444BB),
		RandomId:    int32(randomID() & 0x7fffffff),
	})
	if err != nil {
		t.Fatalf("phone.createConferenceCall: %v", err)
	}
	ups := updatesOf(t, "phone.createConferenceCall", o)
	gc := groupCallIn(ups)
	// The clients take the call and its link from updateGroupCall
	// (tdesktop data_session.cpp:1472-1500, data_group_call.cpp:550).
	if gc == nil || !gc.GetConference() || !gc.GetCreator() || gc.GetInviteLink() == nil {
		t.Fatalf("phone.createConferenceCall answered %v", ups.GetUpdates())
	}
	for _, x := range ups.GetUpdates() {
		if x.GetPredicateName() == mtproto.Predicate_updateGroupCall && x.GetPeer_PEER() != nil {
			t.Fatalf("updateGroupCall of a conference with a peer: %v", x)
		}
	}
	return gc
}

func slugInput(u *user, link string) *mtproto.InputGroupCall {
	return &mtproto.InputGroupCall{Constructor: u.c.Constructor(mtproto.Predicate_inputGroupCallSlug), Slug: link[strings.LastIndex(link, "/")+1:]}
}

func inviteInput(u *user, msgId int32) *mtproto.InputGroupCall {
	return &mtproto.InputGroupCall{Constructor: u.c.Constructor(mtproto.Predicate_inputGroupCallInviteMessage), MsgId: msgId}
}

func inviteToConference(ctx context.Context, t *testing.T, from *user, call *mtproto.InputGroupCall, to *user) (*mtproto.Updates, error) {
	t.Helper()
	o, err := from.c.Invoke(ctx, &mtproto.TLPhoneInviteConferenceCallParticipant{
		Constructor: from.c.Constructor(mtproto.Predicate_phone_inviteConferenceCallParticipant),
		Call:        call,
		UserId:      inputUserOf(ctx, t, from, to),
	})
	if err != nil {
		return nil, err
	}
	return updatesOf(t, "phone.inviteConferenceCallParticipant", o), nil
}

func declineInvite(ctx context.Context, u *user, msgId int32) error {
	_, err := u.c.Invoke(ctx, &mtproto.TLPhoneDeclineConferenceCallInvite{
		Constructor: u.c.Constructor(mtproto.Predicate_phone_declineConferenceCallInvite),
		MsgId:       msgId,
	})
	return err
}

// waitInvite waits on u for an update of predicate (updateNewMessage,
// updateEditMessage) of an invitation to the conference callId whose action
// match takes, and returns its message.
func waitInvite(ctx context.Context, t *testing.T, u *user, predicate string, callId int64, what string, match func(*mtproto.MessageAction) bool) *mtproto.Message {
	t.Helper()
	var found *mtproto.Message
	wctx, cancel := context.WithTimeout(ctx, 2*groupcalls.InviteRingTimeout)
	defer cancel()
	_, err := u.c.WaitUpdate(wctx, func(us *mtproto.Updates) bool {
		for _, x := range us.GetUpdates() {
			if x.GetPredicateName() != predicate {
				continue
			}
			m := x.GetMessage_MESSAGE()
			if a := m.GetAction(); a.GetPredicateName() == mtproto.Predicate_messageActionConferenceCall && a.GetCallId() == callId && match(a) {
				found = m
				return true
			}
		}
		return false
	})
	if err != nil {
		t.Fatalf("%s: no %s of the invitation (%s): %v", u.self.GetFirstName().GetValue(), predicate, what, err)
	}
	return found
}

func ringing(a *mtproto.MessageAction) bool {
	return !a.GetActive() && !a.GetMissed() && a.GetDuration() == nil
}

func isActive(a *mtproto.MessageAction) bool { return a.GetActive() }

func isMissed(a *mtproto.MessageAction) bool { return a.GetMissed() }

func mustRPCError(t *testing.T, what string, err error, want string) {
	t.Helper()
	if !mtclient.IsRPCError(err, want) {
		t.Fatalf("%s: %v, want %s", what, err, want)
	}
}

// emojiAgree waits until the emoji of ms agree.
func emojiAgree(ctx context.Context, t *testing.T, ms ...*member) {
	t.Helper()
	var emoji []byte
	waitE2E(ctx, t, "the emoji verification", 30*time.Second, func(m *member) bool {
		h, ok := m.e2e().Call().EmojiHash()
		if ok && emoji == nil {
			emoji = h
		}
		return ok && bytes.Equal(h, emoji)
	}, ms...)
}

// A conference made as a link alone: one who has the link reads it and its
// empty chain by the slug, joins first with the zero block, a second one
// joins on it, they hear each other with keys only they have, the emoji
// agree, and every answer names the call by its id.
func TestConferenceLinkOnly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	for _, u := range []*user{alice, bob, carol} {
		getState(ctx, t, u)
	}
	gc := createConference(ctx, t, alice)
	link := gc.GetInviteLink().GetValue()
	if !strings.HasPrefix(link, "https://") || !strings.Contains(link, "/call/") {
		t.Fatalf("the link: %s", link)
	}
	exported, err := alice.c.Invoke(ctx, &mtproto.TLPhoneExportGroupCallInvite{
		Constructor: alice.c.Constructor(mtproto.Predicate_phone_exportGroupCallInvite), CanSelfUnmute: true, Call: groupCallInput(alice, gc),
	})
	if err != nil || exported.(interface {
		To_Phone_ExportedGroupCallInvite() *mtproto.Phone_ExportedGroupCallInvite
	}).To_Phone_ExportedGroupCallInvite().GetLink() != link {
		t.Fatalf("phone.exportGroupCallInvite: %v %v", exported, err)
	}

	// A stranger by the id: nothing; by the slug: the call and its chain.
	if _, err = bob.c.Invoke(ctx, &mtproto.TLPhoneGetGroupCall{Constructor: bob.c.Constructor(mtproto.Predicate_phone_getGroupCall), Call: groupCallInput(bob, gc), Limit: 5}); !mtclient.IsRPCError(err, "GROUPCALL_FORBIDDEN") {
		t.Fatalf("a stranger by the id: %v", err)
	}
	bySlug := slugInput(bob, link)
	if id := canonicalCallId(ctx, t, bob, bySlug); id != gc.GetId() {
		t.Fatalf("by the slug: call %d, want %d", id, gc.GetId())
	}
	head, err := chainUpdate(ctx, bob, bySlug, 0, -1, 1)
	if err != nil || len(head.GetBlocks()) != 0 || head.GetNextOffset() != 0 || head.GetCall_INPUTGROUPCALL().GetId() != gc.GetId() {
		t.Fatalf("the empty chain by the slug: %v %v", head, err)
	}

	mb := joinMember(ctx, t, bob, bySlug, memberOptions{})
	mc := joinMember(ctx, t, carol, slugInput(carol, link), memberOptions{})
	speakMember(ctx, t, mb)
	speakMember(ctx, t, mc)
	hearsMember(ctx, t, mb, mc)
	hearsMember(ctx, t, mc, mb)
	waitE2E(ctx, t, "the join of Carol", 20*time.Second, atHeight(1), mb, mc)
	emojiAgree(ctx, t, mb, mc)
}

// Created with the join: the answer is that of a join (the call at
// version 2, the connection, the creator's row, both sub-chains with his
// zero block), the creator connects with it, and a second one joins on his
// block.
func TestConferenceCreateJoin(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, alice)
	getState(ctx, t, bob)

	p, err := groupcallpeer.New(groupcallpeer.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	params, err := p.JoinParams()
	if err != nil {
		t.Fatal(err)
	}
	key, err := peer.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	block, err := groupcallpeer.E2EJoinBlock(alice.self.GetId(), key, nil)
	if err != nil {
		t.Fatal(err)
	}
	pub := peer.Public(key)
	o, err := alice.c.Invoke(ctx, &mtproto.TLPhoneCreateConferenceCall7D0444BB{
		Constructor:  alice.c.Constructor(mtproto.Predicate_phone_createConferenceCall7D0444BB),
		Join:         true,
		VideoStopped: true,
		RandomId:     int32(randomID() & 0x7fffffff),
		PublicKey:    pub[:],
		Block:        block,
		Params:       mtproto.MakeTLDataJSON(&mtproto.DataJSON{Data: string(params)}).To_DataJSON(),
	})
	if err != nil {
		t.Fatalf("phone.createConferenceCall with the join: %v", err)
	}
	ups := updatesOf(t, "phone.createConferenceCall", o)
	gc := groupCallIn(ups)
	me, v := rowIn(ups, gc.GetId(), alice.self.GetId())
	subs := map[int32]*mtproto.Update{}
	var conn *mtproto.Update
	for _, x := range ups.GetUpdates() {
		switch x.GetPredicateName() {
		case mtproto.Predicate_updateGroupCallChainBlocks:
			subs[x.GetSubChainId()] = x
		case mtproto.Predicate_updateGroupCallConnection:
			conn = x
		}
	}
	if !gc.GetConference() || !gc.GetCreator() || gc.GetVersion() != 2 || me == nil || !me.GetSelf() || v != 2 || conn == nil ||
		subs[0] == nil || len(subs[0].GetBlocks()) != 1 || subs[0].GetNextOffset() != 1 || subs[1] == nil || subs[1].GetNextOffset() != 0 {
		t.Fatalf("the answer of a create with the join: %v", ups.GetUpdates())
	}
	e, err := groupcallpeer.NewE2E(alice.self.GetId(), key, subs[0].GetBlocks()[0], 0)
	if err != nil {
		t.Fatalf("the call from the zero block: %v", err)
	}
	p.SetE2E(e)
	cctx, ccancel := context.WithTimeout(ctx, 20*time.Second)
	defer ccancel()
	if err = p.Connect(cctx, []byte(conn.GetParams().GetData())); err != nil {
		t.Fatalf("the creator connects: %v", err)
	}
	mb := joinMember(ctx, t, bob, slugInput(bob, gc.GetInviteLink().GetValue()), memberOptions{})
	if h := mb.e2e().Call().Height(); h != 1 {
		t.Fatalf("the second join at height %d", h)
	}
}

// An invitation rings on every device of the invitee: the one that joins
// by it makes it active for all, the inviter sees it; a declined one, one
// the inviter cancels and one no one answers are missed.
func TestConferenceInviteRings(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	phones := testPhones(t, 5)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	bobLaptop := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	dave := login(ctx, t, phones[3], "Dave")
	erin := login(ctx, t, phones[4], "Erin")
	for _, u := range []*user{alice, bob, bobLaptop, carol, dave, erin} {
		getState(ctx, t, u)
	}
	gc := createConference(ctx, t, alice)
	call := groupCallInput(alice, gc)
	_, err := inviteToConference(ctx, t, alice, call, bob)
	mustRPCError(t, "an invitation by one not in the call", err, "GROUPCALL_FORBIDDEN")
	ma := joinMember(ctx, t, alice, call, memberOptions{})

	reply, err := inviteToConference(ctx, t, alice, call, bob)
	if err != nil {
		t.Fatalf("invite Bob: %v", err)
	}
	var sent *mtproto.Message
	for _, x := range reply.GetUpdates() {
		if x.GetPredicateName() == mtproto.Predicate_updateNewMessage {
			sent = x.GetMessage_MESSAGE()
		}
	}
	if sent == nil || sent.GetAction().GetCallId() != gc.GetId() {
		t.Fatalf("the answer of an invitation: %v", reply.GetUpdates())
	}
	onPhone := waitInvite(ctx, t, bob, mtproto.Predicate_updateNewMessage, gc.GetId(), "rings on the phone", ringing)
	waitInvite(ctx, t, bobLaptop, mtproto.Predicate_updateNewMessage, gc.GetId(), "rings on the laptop", ringing)
	// Incoming in the private chat with Alice: she is its peer.
	if onPhone.GetOut() || onPhone.GetPeerId().GetUserId() != alice.self.GetId() {
		t.Fatalf("the invitation of Bob: %v", onPhone)
	}
	byInvite := inviteInput(bob, onPhone.GetId())
	mb := joinMember(ctx, t, bob, byInvite, memberOptions{})
	waitInvite(ctx, t, bobLaptop, mtproto.Predicate_updateEditMessage, gc.GetId(), "answered on the phone", isActive)
	waitInvite(ctx, t, alice, mtproto.Predicate_updateEditMessage, gc.GetId(), "Bob joined", isActive)
	speakMember(ctx, t, ma)
	speakMember(ctx, t, mb)
	hearsMember(ctx, t, ma, mb)
	hearsMember(ctx, t, mb, ma)

	// Carol declines.
	if _, err = inviteToConference(ctx, t, bob, call, carol); err != nil {
		t.Fatalf("invite Carol: %v", err)
	}
	toCarol := waitInvite(ctx, t, carol, mtproto.Predicate_updateNewMessage, gc.GetId(), "rings for Carol", ringing)
	if len(toCarol.GetAction().GetOtherParticipants()) != 1 || toCarol.GetAction().GetOtherParticipants()[0].GetUserId() != alice.self.GetId() {
		t.Fatalf("the others of Carol's invitation: %v", toCarol.GetAction())
	}
	if err = declineInvite(ctx, carol, toCarol.GetId()); err != nil {
		t.Fatalf("Carol declines: %v", err)
	}
	waitInvite(ctx, t, bob, mtproto.Predicate_updateEditMessage, gc.GetId(), "Carol declined", isMissed)

	// Alice cancels hers to Erin.
	reply, err = inviteToConference(ctx, t, alice, call, erin)
	if err != nil {
		t.Fatalf("invite Erin: %v", err)
	}
	waitInvite(ctx, t, erin, mtproto.Predicate_updateNewMessage, gc.GetId(), "rings for Erin", ringing)
	var toErin int32
	for _, x := range reply.GetUpdates() {
		if x.GetPredicateName() == mtproto.Predicate_updateNewMessage {
			toErin = x.GetMessage_MESSAGE().GetId()
		}
	}
	if err = declineInvite(ctx, alice, toErin); err != nil {
		t.Fatalf("Alice cancels: %v", err)
	}
	waitInvite(ctx, t, erin, mtproto.Predicate_updateEditMessage, gc.GetId(), "cancelled", isMissed)

	// Dave does not answer: missed after the ring time.
	if _, err = inviteToConference(ctx, t, alice, call, dave); err != nil {
		t.Fatalf("invite Dave: %v", err)
	}
	toDave := waitInvite(ctx, t, dave, mtproto.Predicate_updateNewMessage, gc.GetId(), "rings for Dave", ringing)
	start := time.Now()
	// Dave's device asks for its difference as a device that was away for
	// the ring time does: the edit is in it.
	st := getState(ctx, t, dave)
	for missed := false; !missed; {
		if time.Since(start) > 2*groupcalls.InviteRingTimeout {
			t.Fatal("Dave's invitation was not missed")
		}
		time.Sleep(5 * time.Second)
		for _, x := range updatesOfType(differenceOnce(ctx, t, dave, st), mtproto.Predicate_updateEditMessage) {
			if a := x.GetMessage_MESSAGE().GetAction(); a.GetCallId() == gc.GetId() && a.GetMissed() {
				missed = true
			}
		}
	}
	if since := time.Since(start); since < groupcalls.InviteRingTimeout-10*time.Second {
		t.Fatalf("missed after %s", since)
	}
	// A missed invitation still lets him see the conference while it lasts
	// (Android joins by a tap on it, ChatActivity.java:40439-40470).
	if id := canonicalCallId(ctx, t, dave, inviteInput(dave, toDave.GetId())); id != gc.GetId() {
		t.Fatalf("Dave's old invitation names call %d", id)
	}
	// One not invited learns nothing by a message of his.
	if _, err = dave.c.Invoke(ctx, &mtproto.TLPhoneGetGroupCall{Constructor: dave.c.Constructor(mtproto.Predicate_phone_getGroupCall), Call: inviteInput(dave, toDave.GetId()+1000), Limit: 1}); !mtclient.IsRPCError(err, "GROUPCALL_INVALID") {
		t.Fatalf("a message that is no invitation: %v", err)
	}
}

// The refusals of an invitation: the invitee's privacy of calls, one in
// the call, one the creator kicked invited by another participant.
func TestConferenceInvitePrivacy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	dave := login(ctx, t, phones[2], "Dave")
	for _, u := range []*user{alice, bob, dave} {
		getState(ctx, t, u)
	}
	gc := createConference(ctx, t, alice)
	call := groupCallInput(alice, gc)
	ma := joinMember(ctx, t, alice, call, memberOptions{})
	mb := joinMember(ctx, t, bob, slugInput(bob, gc.GetInviteLink().GetValue()), memberOptions{})

	mustSetPrivacy(ctx, t, dave, mtproto.Predicate_inputPrivacyKeyPhoneCall, ruleOf(dave, mtproto.Predicate_inputPrivacyValueDisallowAll))
	_, err := inviteToConference(ctx, t, alice, call, dave)
	mustRPCError(t, "the privacy of calls", err, "USER_PRIVACY_RESTRICTED")
	_, err = inviteToConference(ctx, t, alice, call, bob)
	mustRPCError(t, "one in the call", err, "USER_ALREADY_PARTICIPANT")

	waitE2E(ctx, t, "the join of Bob", 20*time.Second, atHeight(1), ma, mb)
	if err = invoke(ctx, alice, removal(ma, true, bob.self.GetId())); err != nil {
		t.Fatalf("the kick: %v", err)
	}
	mustSetPrivacy(ctx, t, dave, mtproto.Predicate_inputPrivacyKeyPhoneCall, ruleOf(dave, mtproto.Predicate_inputPrivacyValueAllowAll))
	joinMember(ctx, t, dave, slugInput(dave, gc.GetInviteLink().GetValue()), memberOptions{})
	_, err = inviteToConference(ctx, t, dave, call, bob)
	mustRPCError(t, "the kicked invited by a participant", err, "USER_WAS_KICKED")
}

// The creator resets the link: the old slug finds nothing, the new one
// works; another may not reset it.
func TestConferenceRevokeLink(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, alice)
	getState(ctx, t, bob)
	gc := createConference(ctx, t, alice)
	old := gc.GetInviteLink().GetValue()
	joinMember(ctx, t, bob, slugInput(bob, old), memberOptions{})
	reset := func(u *user) (*mtproto.Updates, error) {
		o, err := u.c.Invoke(ctx, &mtproto.TLPhoneToggleGroupCallSettings{
			Constructor: u.c.Constructor(mtproto.Predicate_phone_toggleGroupCallSettings), ResetInviteHash: true, Call: groupCallInput(u, gc),
		})
		if err != nil {
			return nil, err
		}
		return updatesOf(t, "phone.toggleGroupCallSettings", o), nil
	}
	_, err := reset(bob)
	mustRPCError(t, "a reset by another", err, "GROUPCALL_FORBIDDEN")
	ups, err := reset(alice)
	if err != nil {
		t.Fatalf("the reset: %v", err)
	}
	link := groupCallIn(ups).GetInviteLink().GetValue()
	if link == old || link == "" {
		t.Fatalf("the new link: %q", link)
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLPhoneGetGroupCall{Constructor: alice.c.Constructor(mtproto.Predicate_phone_getGroupCall), Call: slugInput(alice, old), Limit: 1}); !mtclient.IsRPCError(err, "GROUPCALL_INVALID") {
		t.Fatalf("the old slug: %v", err)
	}
	if id := canonicalCallId(ctx, t, bob, slugInput(bob, link)); id != gc.GetId() {
		t.Fatalf("the new slug names call %d", id)
	}
}

// The creator kicks one: he joins neither by the link nor by another
// participant's invitation; after the creator's own invitation he does.
func TestConferenceKick(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	for _, u := range []*user{alice, bob, carol} {
		getState(ctx, t, u)
	}
	gc := createConference(ctx, t, alice)
	call := groupCallInput(alice, gc)
	link := gc.GetInviteLink().GetValue()
	ma := joinMember(ctx, t, alice, call, memberOptions{})
	mb := joinMember(ctx, t, bob, slugInput(bob, link), memberOptions{noHeal: true})
	mc := joinMember(ctx, t, carol, slugInput(carol, link), memberOptions{})
	waitE2E(ctx, t, "three in the chain", 20*time.Second, atHeight(2), ma, mb, mc)
	// Only the creator kicks.
	mustRPCError(t, "a kick by another", invoke(ctx, carol, removal(mc, true, bob.self.GetId())), "GROUPCALL_FORBIDDEN")
	if err := invoke(ctx, alice, removal(ma, true, bob.self.GetId())); err != nil {
		t.Fatalf("the kick: %v", err)
	}
	mb.stop()
	// The policy refuses him before his block is looked at: he may not
	// even read the chain.
	req := joinRequest(bob, slugInput(bob, link), mustParams(t))
	pub := peer.Public(e2eKey(bob))
	req.PublicKey, req.Block = pub[:], []byte{1}
	mustRPCError(t, "the kicked joins by the link", invoke(ctx, bob, req), "GROUPCALL_FORBIDDEN")
	_, err := inviteToConference(ctx, t, carol, call, bob)
	mustRPCError(t, "the kicked invited by a participant", err, "USER_WAS_KICKED")
	if _, err = inviteToConference(ctx, t, alice, call, bob); err != nil {
		t.Fatalf("the creator invites him back: %v", err)
	}
	invitation := waitInvite(ctx, t, bob, mtproto.Predicate_updateNewMessage, gc.GetId(), "the creator's invitation", ringing)
	back := joinMember(ctx, t, bob, inviteInput(bob, invitation.GetId()), memberOptions{})
	speakMember(ctx, t, back)
	speakMember(ctx, t, ma)
	hearsMember(ctx, t, ma, back)
	hearsMember(ctx, t, back, ma)
}

// A one-to-one call goes to a conference: the call carries
// conference_supported, the caller makes a conference, joins it and ends
// the call with the move and the slug; the other side reads both and joins
// by the slug.
func TestConferenceMigrate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, alice)
	getState(ctx, t, bob)

	call := requestCall(ctx, t, alice, bob)
	callOf(ctx, t, bob, call.GetId(), mtproto.Predicate_phoneCallRequested)
	gB := make([]byte, 256)
	gB[0] = 7
	if _, err := bob.c.Invoke(ctx, &mtproto.TLPhoneAcceptCall{Constructor: bob.c.Constructor(mtproto.Predicate_phone_acceptCall), Peer: inputCall(bob, call), GB: gB, Protocol: callProtocol(bob)}); err != nil {
		t.Fatalf("phone.acceptCall: %v", err)
	}
	callOf(ctx, t, alice, call.GetId(), mtproto.Predicate_phoneCallAccepted)
	gA := make([]byte, 256)
	gA[0] = 9
	o, err := alice.c.Invoke(ctx, &mtproto.TLPhoneConfirmCall{Constructor: alice.c.Constructor(mtproto.Predicate_phone_confirmCall), Peer: inputCall(alice, call), GA: gA, KeyFingerprint: 1234, Protocol: callProtocol(alice)})
	if err != nil {
		t.Fatalf("phone.confirmCall: %v", err)
	}
	active := o.(interface {
		To_Phone_PhoneCall() *mtproto.Phone_PhoneCall
	}).To_Phone_PhoneCall().GetPhoneCall()
	bobCall := callOf(ctx, t, bob, call.GetId(), mtproto.Predicate_phoneCall)
	if !active.GetConferenceSupported() || !bobCall.GetConferenceSupported() {
		t.Fatalf("conference_supported: caller %v, callee %v", active.GetConferenceSupported(), bobCall.GetConferenceSupported())
	}

	gc := createConference(ctx, t, alice)
	ma := joinMember(ctx, t, alice, groupCallInput(alice, gc), memberOptions{})
	link := gc.GetInviteLink().GetValue()
	slug := link[strings.LastIndex(link, "/")+1:]
	if _, err = alice.c.Invoke(ctx, &mtproto.TLPhoneDiscardCall{
		Constructor: alice.c.Constructor(mtproto.Predicate_phone_discardCall),
		Peer:        inputCall(alice, call),
		Reason:      &mtproto.PhoneCallDiscardReason{Constructor: alice.c.Constructor(mtproto.Predicate_phoneCallDiscardReasonMigrateConferenceCall), Slug: slug},
	}); err != nil {
		t.Fatalf("phone.discardCall with the move: %v", err)
	}
	ended := callOf(ctx, t, bob, call.GetId(), mtproto.Predicate_phoneCallDiscarded)
	if r := ended.GetReason(); r.GetPredicateName() != mtproto.Predicate_phoneCallDiscardReasonMigrateConferenceCall || r.GetSlug() != slug {
		t.Fatalf("the reason Bob reads: %v", r)
	}
	mb := joinMember(ctx, t, bob, slugInput(bob, link), memberOptions{})
	speakMember(ctx, t, ma)
	speakMember(ctx, t, mb)
	hearsMember(ctx, t, ma, mb)
	hearsMember(ctx, t, mb, ma)

	// A slug of no conference of his is a plain hang-up.
	other := requestCall(ctx, t, bob, alice)
	callOf(ctx, t, alice, other.GetId(), mtproto.Predicate_phoneCallRequested)
	if _, err = bob.c.Invoke(ctx, &mtproto.TLPhoneDiscardCall{
		Constructor: bob.c.Constructor(mtproto.Predicate_phone_discardCall),
		Peer:        inputCall(bob, other),
		Reason:      &mtproto.PhoneCallDiscardReason{Constructor: bob.c.Constructor(mtproto.Predicate_phoneCallDiscardReasonMigrateConferenceCall), Slug: "nothing"},
	}); err != nil {
		t.Fatalf("phone.discardCall with a slug of nothing: %v", err)
	}
	if r := callOf(ctx, t, alice, other.GetId(), mtproto.Predicate_phoneCallDiscarded).GetReason(); r.GetPredicateName() != mtproto.Predicate_phoneCallDiscardReasonHangup {
		t.Fatalf("the reason of a move to nothing: %v", r)
	}
}

// A conference with one from Russia and one from Germany: the one from
// Russia gets the relay's candidates alone, as created with the join or
// joining, the one from Germany the SFU's own, and they hear each other.
func TestConferenceFromRussia(t *testing.T) {
	needEdges(t)
	port := relayPort(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	_, germany, russia := countryClients(ctx, t, "conference")
	getState(ctx, t, germany)
	getState(ctx, t, russia)

	// Created with the join from Russia: the relay's candidates.
	p, err := groupcallpeer.New(groupcallpeer.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	params, err := p.JoinParams()
	if err != nil {
		t.Fatal(err)
	}
	key, err := peer.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	block, err := groupcallpeer.E2EJoinBlock(russia.self.GetId(), key, nil)
	if err != nil {
		t.Fatal(err)
	}
	pub := peer.Public(key)
	o, err := russia.c.Invoke(ctx, &mtproto.TLPhoneCreateConferenceCall7D0444BB{
		Constructor: russia.c.Constructor(mtproto.Predicate_phone_createConferenceCall7D0444BB),
		Join:        true, VideoStopped: true, RandomId: int32(randomID() & 0x7fffffff), PublicKey: pub[:], Block: block,
		Params: mtproto.MakeTLDataJSON(&mtproto.DataJSON{Data: string(params)}).To_DataJSON(),
	})
	if err != nil {
		t.Fatalf("phone.createConferenceCall from Russia: %v", err)
	}
	for _, x := range updatesOf(t, "phone.createConferenceCall", o).GetUpdates() {
		if x.GetPredicateName() == mtproto.Predicate_updateGroupCallConnection {
			onlyRelay(t, "created from Russia", answerCandidates(t, "created from Russia", x.GetParams().GetData()), port)
		}
	}

	// A link from Russia: both join, one through the relay.
	gc := createConference(ctx, t, russia)
	link := gc.GetInviteLink().GetValue()
	mr := joinMember(ctx, t, russia, groupCallInput(russia, gc), memberOptions{})
	mg := joinMember(ctx, t, germany, slugInput(germany, link), memberOptions{})
	onlyRelay(t, "Russia", answerCandidates(t, "Russia", mr.answer), port)
	noRelay(t, "Germany", answerCandidates(t, "Germany", mg.answer), port)
	speakMember(ctx, t, mr)
	speakMember(ctx, t, mg)
	hearsMember(ctx, t, mr, mg)
	hearsMember(ctx, t, mg, mr)
}

// The creator ends the conference: those in it get groupCallDiscarded, the
// invitation of one who joined gets the duration, the conference reads as
// discarded by the link, and another may not end it.
func TestConferenceEnds(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, alice)
	getState(ctx, t, bob)
	gc := createConference(ctx, t, alice)
	call := groupCallInput(alice, gc)
	joinMember(ctx, t, alice, call, memberOptions{})
	if _, err := inviteToConference(ctx, t, alice, call, bob); err != nil {
		t.Fatalf("invite Bob: %v", err)
	}
	invitation := waitInvite(ctx, t, bob, mtproto.Predicate_updateNewMessage, gc.GetId(), "rings", ringing)
	joinMember(ctx, t, bob, inviteInput(bob, invitation.GetId()), memberOptions{})
	waitInvite(ctx, t, bob, mtproto.Predicate_updateEditMessage, gc.GetId(), "joined", isActive)

	discardReq := func(u *user) *mtproto.TLPhoneDiscardGroupCall {
		return &mtproto.TLPhoneDiscardGroupCall{Constructor: u.c.Constructor(mtproto.Predicate_phone_discardGroupCall), Call: groupCallInput(u, gc)}
	}
	mustRPCError(t, "an end by another", invoke(ctx, bob, discardReq(bob)), "GROUPCALL_FORBIDDEN")
	if err := invoke(ctx, alice, discardReq(alice)); err != nil {
		t.Fatalf("phone.discardGroupCall: %v", err)
	}
	waitCall(ctx, t, bob, gc.GetId(), "the end", func(g *mtproto.GroupCall) bool {
		return g.GetPredicateName() == mtproto.Predicate_groupCallDiscarded
	})
	waitInvite(ctx, t, bob, mtproto.Predicate_updateEditMessage, gc.GetId(), "the end", func(a *mtproto.MessageAction) bool {
		return a.GetDuration() != nil && !a.GetActive()
	})
	o, err := bob.c.Invoke(ctx, &mtproto.TLPhoneGetGroupCall{Constructor: bob.c.Constructor(mtproto.Predicate_phone_getGroupCall), Call: slugInput(bob, gc.GetInviteLink().GetValue()), Limit: 1})
	if err != nil {
		t.Fatalf("phone.getGroupCall of an ended conference: %v", err)
	}
	if g := o.(interface {
		To_Phone_GroupCall() *mtproto.Phone_GroupCall
	}).To_Phone_GroupCall().GetCall(); g.GetPredicateName() != mtproto.Predicate_groupCallDiscarded {
		t.Fatalf("an ended conference: %v", g)
	}
}

// An invitation rings on devices that are away: Android by
// CONF_CALL_REQUEST with the call and the message (PushListenerController
// .java:367-384), an iPhone with a VoIP token by PushKit alone
// (AppDelegate.swift:2345), as a call does.
func TestConferenceInvitePush(t *testing.T) {
	if fakePushAddr == "" {
		t.Skip("PLUMA_E2E_FAKEPUSH is not set: the stand pushes to the real providers")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	getState(ctx, t, alice)
	phone := signedIn(ctx, t, phones[1], "Bob")
	iphone := signedIn(ctx, t, phones[1], "Bob")
	androidDev := registerDevice(ctx, t, phone.c, 2)
	iosDev := registerDevice(ctx, t, iphone.c, 1)
	voipDev := registerDevice(ctx, t, iphone.c, 9)
	bob := signedIn(ctx, t, phones[1], "Bob")
	offline(t, phone.c)
	offline(t, iphone.c)

	gc := createConference(ctx, t, alice)
	call := groupCallInput(alice, gc)
	joinMember(ctx, t, alice, call, memberOptions{})
	if _, err := inviteToConference(ctx, t, alice, call, bob); err != nil {
		t.Fatalf("invite Bob: %v", err)
	}
	callId := strconv.FormatInt(gc.GetId(), 10)
	p, _ := waitPush(ctx, t, androidDev, "CONF_CALL_REQUEST on Android", func(p map[string]any) bool { return p["loc_key"] == "CONF_CALL_REQUEST" })
	custom, _ := p["custom"].(map[string]any)
	args, _ := p["loc_args"].([]any)
	// The ids are JSON numbers: a float64 here, which keeps the call id to
	// its 53 bits.
	if id, _ := custom["call_id"].(float64); id != float64(gc.GetId()) || custom["msg_id"] == nil || len(args) != 3 {
		t.Fatalf("the push to Android: %v", p)
	}
	v, r := waitPush(ctx, t, voipDev, "PushKit", func(p map[string]any) bool { return p["group_call_id"] != nil })
	if v["group_call_id"] != callId || v["from_id"] != strconv.FormatInt(alice.self.GetId(), 10) || v["msg_id"] == nil || r.Headers["apns-push-type"] != "voip" {
		t.Fatalf("the PushKit push: %v %v", v, r.Headers)
	}
	noPush(t, iosDev, "the extension of an iPhone with PushKit", func(p map[string]any) bool { return p["loc-key"] == "CONF_CALL_REQUEST" })
}
