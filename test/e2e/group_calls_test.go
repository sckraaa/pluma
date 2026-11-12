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
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/rtp"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/groupcallpeer"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
	"github.com/teamgram/teamgram-server/pkg/tgcallsjson"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// Group calls (task 093, ADR 0059): the signalling through the MTProto
// port and the media through the SFU of the stand, with clients of
// pkg/groupcallpeer, which speak to the SFU as tgcalls does. Every session
// that waits for pushes asks updates.getState first, as the clients do.

func groupCallInput(u *user, gc *mtproto.GroupCall) *mtproto.InputGroupCall {
	return &mtproto.InputGroupCall{Constructor: u.c.Constructor(mtproto.Predicate_inputGroupCall), Id: gc.GetId(), AccessHash: gc.GetAccessHash()}
}

func inputPeerSelf(u *user) *mtproto.InputPeer {
	return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerSelf)}
}

func channelInputs(u *user, ch *mtproto.Chat) (*mtproto.InputPeer, *mtproto.InputChannel) {
	hash := ch.GetAccessHash_FLAGINT64().GetValue()
	return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: hash},
		&mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: hash}
}

func updatesOf(t *testing.T, name string, o mtproto.TLObject) *mtproto.Updates {
	t.Helper()
	r, ok := o.(interface{ To_Updates() *mtproto.Updates })
	if !ok {
		t.Fatalf("%s returned %T", name, o)
	}
	return r.To_Updates()
}

// groupCallIn returns the call of updateGroupCall in ups, nil if none.
func groupCallIn(ups *mtproto.Updates) *mtproto.GroupCall {
	for _, x := range ups.GetUpdates() {
		if x.GetPredicateName() == mtproto.Predicate_updateGroupCall {
			return x.GetCall_GROUPCALL()
		}
	}
	return nil
}

// rowIn returns the row of userId in updateGroupCallParticipants of call in
// ups and its version.
func rowIn(ups *mtproto.Updates, callId, userId int64) (*mtproto.GroupCallParticipant, int32) {
	for _, x := range ups.GetUpdates() {
		if x.GetPredicateName() != mtproto.Predicate_updateGroupCallParticipants || x.GetCall_INPUTGROUPCALL().GetId() != callId {
			continue
		}
		for _, p := range x.GetParticipants_VECTORGROUPCALLPARTICIPANT() {
			if p.GetPeer().GetUserId() == userId {
				return p, x.GetVersion()
			}
		}
	}
	return nil, 0
}

// waitRow waits on u for updateGroupCallParticipants of call with a row of
// userId for which match holds.
func waitRow(ctx context.Context, t *testing.T, u *user, callId, userId int64, what string, match func(*mtproto.GroupCallParticipant) bool) (*mtproto.GroupCallParticipant, int32) {
	t.Helper()
	var row *mtproto.GroupCallParticipant
	var version int32
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, err := u.c.WaitUpdate(cctx, func(ups *mtproto.Updates) bool {
		p, v := rowIn(ups, callId, userId)
		if p != nil && match(p) {
			row, version = p, v
			return true
		}
		return false
	})
	if err != nil {
		t.Fatalf("%s: no row of %d (%s): %v", u.self.GetFirstName(), userId, what, err)
	}
	return row, version
}

// waitCall waits on u for updateGroupCall of call for which match holds.
func waitCall(ctx context.Context, t *testing.T, u *user, callId int64, what string, match func(*mtproto.GroupCall) bool) *mtproto.GroupCall {
	t.Helper()
	var gc *mtproto.GroupCall
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, err := u.c.WaitUpdate(cctx, func(ups *mtproto.Updates) bool {
		for _, x := range ups.GetUpdates() {
			if c := x.GetCall_GROUPCALL(); x.GetPredicateName() == mtproto.Predicate_updateGroupCall && c.GetId() == callId && match(c) {
				gc = c
				return true
			}
		}
		return false
	})
	if err != nil {
		t.Fatalf("%s: no updateGroupCall (%s): %v", u.self.GetFirstName(), what, err)
	}
	return gc
}

// waitCallChat waits on u for updateGroupCall of call for which match
// holds and returns it with the chat chatId of the same Updates, nil when
// the Updates carries none.
func waitCallChat(ctx context.Context, t *testing.T, u *user, callId, chatId int64, what string, match func(*mtproto.GroupCall) bool) (*mtproto.GroupCall, *mtproto.Chat) {
	t.Helper()
	var gc *mtproto.GroupCall
	var chat *mtproto.Chat
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, err := u.c.WaitUpdate(cctx, func(ups *mtproto.Updates) bool {
		for _, x := range ups.GetUpdates() {
			if c := x.GetCall_GROUPCALL(); x.GetPredicateName() == mtproto.Predicate_updateGroupCall && c.GetId() == callId && match(c) {
				gc, chat = c, chatIn(ups.GetChats(), chatId)
				return true
			}
		}
		return false
	})
	if err != nil {
		t.Fatalf("%s: no updateGroupCall (%s): %v", u.self.GetFirstName(), what, err)
	}
	return gc, chat
}

func createGroupCall(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, title string, schedule int32) *mtproto.GroupCall {
	t.Helper()
	req := &mtproto.TLPhoneCreateGroupCall{
		Constructor: u.c.Constructor(mtproto.Predicate_phone_createGroupCall),
		Peer:        peer,
		RandomId:    int32(randomID() & 0x7fffffff),
		Title:       wrapperspb.String(title),
	}
	if schedule != 0 {
		req.ScheduleDate = wrapperspb.Int32(schedule)
	}
	o, err := u.c.Invoke(ctx, req)
	if err != nil {
		t.Fatalf("phone.createGroupCall: %v", err)
	}
	// tdesktop takes the call from updateGroupCall of the answer
	// (calls_group_call.cpp:1306).
	gc := groupCallIn(updatesOf(t, "phone.createGroupCall", o))
	if gc.GetPredicateName() != mtproto.Predicate_groupCall || gc.GetVersion() == 0 || gc.GetTitle().GetValue() != title {
		t.Fatalf("phone.createGroupCall: call %v", gc)
	}
	return gc
}

func joinRequest(u *user, call *mtproto.InputGroupCall, params []byte) *mtproto.TLPhoneJoinGroupCall {
	return &mtproto.TLPhoneJoinGroupCall{
		Constructor: u.c.Constructor(mtproto.Predicate_phone_joinGroupCall),
		Call:        call,
		JoinAs:      inputPeerSelf(u),
		Params:      mtproto.MakeTLDataJSON(&mtproto.DataJSON{Data: string(params)}).To_DataJSON(),
	}
}

// joinGroupCall joins u to call with a new client of cfg and connects it to
// the SFU with the answer, as tgcalls does; the answer has the call, the
// connection and u's own row (iOS GroupCalls.swift:786-829, Android
// VoIPService.java:2145-2160).
func joinGroupCall(ctx context.Context, t *testing.T, u *user, call *mtproto.InputGroupCall, cfg groupcallpeer.Config, edit func(*mtproto.TLPhoneJoinGroupCall)) (*groupcallpeer.Peer, *mtproto.GroupCallParticipant, int32) {
	t.Helper()
	p, err := groupcallpeer.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	params, err := p.JoinParams()
	if err != nil {
		t.Fatal(err)
	}
	req := joinRequest(u, call, params)
	req.VideoStopped = cfg.VideoLayers == 0
	if edit != nil {
		edit(req)
	}
	o, err := u.c.Invoke(ctx, req)
	if err != nil {
		t.Fatalf("%s: phone.joinGroupCall: %v", u.self.GetFirstName(), err)
	}
	ups := updatesOf(t, "phone.joinGroupCall", o)
	var conn *mtproto.Update
	for _, x := range ups.GetUpdates() {
		if x.GetPredicateName() == mtproto.Predicate_updateGroupCallConnection {
			conn = x
		}
	}
	gc := groupCallIn(ups)
	me, v := rowIn(ups, call.GetId(), u.self.GetId())
	if conn == nil || conn.GetPresentation() || gc.GetId() != call.GetId() || me == nil {
		t.Fatalf("%s: phone.joinGroupCall answered %v", u.self.GetFirstName(), ups.GetUpdates())
	}
	// A join of one still in the call (a join again after the SFU lost
	// him) is not a new join: just_joined and the next version belong to
	// a first join only, which the tests check where it is one.
	if !me.GetSelf() || me.GetMin() || me.GetLeft() || uint32(me.GetSource()) != p.AudioSSRC() || v != gc.GetVersion() {
		t.Fatalf("%s: own row %v at %d, call version %d", u.self.GetFirstName(), me, v, gc.GetVersion())
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := p.Connect(cctx, []byte(conn.GetParams().GetData())); err != nil {
		t.Fatalf("%s: connect to the SFU: %v", u.self.GetFirstName(), err)
	}
	return p, me, v
}

// hears checks that listener gets the sound of source through the SFU.
func hears(ctx context.Context, t *testing.T, who string, listener *groupcallpeer.Peer, source uint32) {
	t.Helper()
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	pkt, err := listener.AwaitRTP(cctx, groupcallpeer.BySSRC(source))
	if err != nil {
		t.Fatalf("%s: %v", who, err)
	}
	if pkt.PayloadType != tgcallsjson.PayloadTypeOpus || pkt.GetExtension(tgcallsjson.ExtAudioLevel) == nil {
		t.Fatalf("%s: a packet of payload type %d, audio level %v", who, pkt.PayloadType, pkt.GetExtension(tgcallsjson.ExtAudioLevel))
	}
}

// silent checks that listener stops getting the sound of source.
func silent(t *testing.T, who string, listener *groupcallpeer.Peer, source uint32) {
	t.Helper()
	// What the SFU forwarded before the change may still be on its way.
	time.Sleep(300 * time.Millisecond)
	if err := listener.NoRTP(1500*time.Millisecond, groupcallpeer.BySSRC(source)); err != nil {
		t.Fatalf("%s: %v", who, err)
	}
}

func speak(ctx context.Context, t *testing.T, p *groupcallpeer.Peer) {
	t.Helper()
	sctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	p.StreamAudio(sctx, 30)
}

func checkGroupCall(ctx context.Context, t *testing.T, u *user, call *mtproto.InputGroupCall, sources ...uint32) []uint32 {
	t.Helper()
	req := &mtproto.TLPhoneCheckGroupCall{Constructor: u.c.Constructor(mtproto.Predicate_phone_checkGroupCall), Call: call}
	for _, s := range sources {
		req.Sources = append(req.Sources, int32(s))
	}
	raw, err := u.c.InvokeRaw(ctx, req)
	if err != nil {
		t.Fatalf("phone.checkGroupCall: %v", err)
	}
	d := mtproto.NewDecodeBuf(raw)
	held := d.VectorInt()
	if err := d.GetError(); err != nil {
		t.Fatalf("phone.checkGroupCall: %v", err)
	}
	var out []uint32
	for _, s := range held {
		out = append(out, uint32(s))
	}
	return out
}

func editParticipant(ctx context.Context, u *user, call *mtproto.InputGroupCall, who *mtproto.InputPeer, edit func(*mtproto.TLPhoneEditGroupCallParticipant)) (*mtproto.Updates, error) {
	req := &mtproto.TLPhoneEditGroupCallParticipant{
		Constructor: u.c.Constructor(mtproto.Predicate_phone_editGroupCallParticipant),
		Call:        call,
		Participant: who,
	}
	edit(req)
	o, err := u.c.Invoke(ctx, req)
	if err != nil {
		return nil, err
	}
	return o.(interface{ To_Updates() *mtproto.Updates }).To_Updates(), nil
}

func getGroupCall(ctx context.Context, t *testing.T, u *user, call *mtproto.InputGroupCall) *mtproto.Phone_GroupCall {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLPhoneGetGroupCall{Constructor: u.c.Constructor(mtproto.Predicate_phone_getGroupCall), Call: call, Limit: 3})
	if err != nil {
		t.Fatalf("phone.getGroupCall: %v", err)
	}
	return o.(interface {
		To_Phone_GroupCall() *mtproto.Phone_GroupCall
	}).To_Phone_GroupCall()
}

func groupParticipants(ctx context.Context, t *testing.T, u *user, req *mtproto.TLPhoneGetGroupParticipants) *mtproto.Phone_GroupParticipants {
	t.Helper()
	req.Constructor = u.c.Constructor(mtproto.Predicate_phone_getGroupParticipants)
	if req.Limit == 0 {
		req.Limit = 50
	}
	o, err := u.c.Invoke(ctx, req)
	if err != nil {
		t.Fatalf("phone.getGroupParticipants: %v", err)
	}
	return o.(interface {
		To_Phone_GroupParticipants() *mtproto.Phone_GroupParticipants
	}).To_Phone_GroupParticipants()
}

func leaveGroupCall(ctx context.Context, t *testing.T, u *user, call *mtproto.InputGroupCall, source uint32) *mtproto.Updates {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLPhoneLeaveGroupCall{Constructor: u.c.Constructor(mtproto.Predicate_phone_leaveGroupCall), Call: call, Source: int32(source)})
	if err != nil {
		t.Fatalf("phone.leaveGroupCall: %v", err)
	}
	return updatesOf(t, "phone.leaveGroupCall", o)
}

func discardGroupCall(ctx context.Context, t *testing.T, u *user, call *mtproto.InputGroupCall) *mtproto.GroupCall {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLPhoneDiscardGroupCall{Constructor: u.c.Constructor(mtproto.Predicate_phone_discardGroupCall), Call: call})
	if err != nil {
		t.Fatalf("phone.discardGroupCall: %v", err)
	}
	gc := groupCallIn(updatesOf(t, "phone.discardGroupCall", o))
	if gc.GetPredicateName() != mtproto.Predicate_groupCallDiscarded || gc.GetId() != call.GetId() {
		t.Fatalf("phone.discardGroupCall: call %v", gc)
	}
	return gc
}

// groupCallActions waits until the history of peer has n group call
// service messages of the call and returns them, the newest first.
func groupCallActions(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, callId int64, n int) []*mtproto.MessageAction {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetHistory{Constructor: u.c.Constructor(mtproto.Predicate_messages_getHistory), Peer: peer, Limit: 20})
		if err != nil {
			t.Fatalf("messages.getHistory: %v", err)
		}
		var out []*mtproto.MessageAction
		for _, m := range o.(interface {
			To_Messages_Messages() *mtproto.Messages_Messages
		}).To_Messages_Messages().GetMessages() {
			switch a := m.GetAction(); a.GetPredicateName() {
			case mtproto.Predicate_messageActionGroupCall, mtproto.Predicate_messageActionGroupCallScheduled:
				if a.GetCall().GetId() == callId {
					out = append(out, a)
				}
			}
		}
		if len(out) >= n {
			return out
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d service messages of the call, want %d", len(out), n)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func fullChannel(ctx context.Context, t *testing.T, u *user, ch *mtproto.InputChannel) (*mtproto.ChatFull, *mtproto.Chat) {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLChannelsGetFullChannel{Constructor: u.c.Constructor(mtproto.Predicate_channels_getFullChannel), Channel: ch})
	if err != nil {
		t.Fatalf("channels.getFullChannel: %v", err)
	}
	full := o.(interface {
		To_Messages_ChatFull() *mtproto.Messages_ChatFull
	}).To_Messages_ChatFull()
	for _, c := range full.GetChats() {
		if c.GetId() == ch.GetChannelId() {
			return full.GetFullChat(), c
		}
	}
	t.Fatalf("channels.getFullChannel without the channel")
	return nil, nil
}

func inviteToChannel(ctx context.Context, t *testing.T, admin *user, ch *mtproto.InputChannel, members ...*user) {
	t.Helper()
	var users []*mtproto.InputUser
	for _, m := range members {
		users = append(users, inputUserOf(ctx, t, admin, m))
	}
	if _, err := admin.c.Invoke(ctx, &mtproto.TLChannelsInviteToChannelC9E33D54{
		Constructor: admin.c.Constructor(mtproto.Predicate_channels_inviteToChannelC9E33D54),
		Channel:     ch,
		Users:       users,
	}); err != nil {
		t.Fatalf("channels.inviteToChannel: %v", err)
	}
}

// supergroupCall makes a supergroup of alice with the others in it and a
// call in it; every session asked updates.getState.
func supergroupCall(ctx context.Context, t *testing.T, names ...string) ([]*user, *mtproto.Chat, *mtproto.GroupCall) {
	t.Helper()
	phones := testPhones(t, len(names))
	var us []*user
	for i, n := range names {
		us = append(us, login(ctx, t, phones[i], n))
	}
	_, ch := createChannel(ctx, t, us[0], "group call "+strings.Join(names, " "), true)
	_, input := channelInputs(us[0], ch)
	if len(us) > 1 {
		inviteToChannel(ctx, t, us[0], input, us[1:]...)
	}
	for _, u := range us {
		getState(ctx, t, u)
	}
	peer, _ := channelInputs(us[0], ch)
	return us, ch, createGroupCall(ctx, t, us[0], peer, "Standup", 0)
}

// A call in a supergroup from its start to its end: the call on the
// channel and in its history, three participants who hear each other, the
// versions of joins and leaves in order, the lookup of a source, a leave
// and a join again, the end with its duration.
func TestGroupCallSupergroup(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	us, ch, gc := supergroupCall(ctx, t, "Alice", "Bob", "Carol")
	alice, bob, carol := us[0], us[1], us[2]
	historyOf := func(u *user) *mtproto.InputPeer { p, _ := channelInputs(u, ch); return p }
	chanOf := func(u *user) *mtproto.InputChannel { _, c := channelInputs(u, ch); return c }

	// The members learn the call, the channel shows it, the history has its
	// start.
	waitCall(ctx, t, bob, gc.GetId(), "start", func(c *mtproto.GroupCall) bool { return c.GetPredicateName() == mtproto.Predicate_groupCall })
	full, chat := fullChannel(ctx, t, bob, chanOf(bob))
	if full.GetCall().GetId() != gc.GetId() || !chat.GetCallActive() || chat.GetCallNotEmpty() {
		t.Fatalf("the channel with an empty call: call %v, call_active %v, call_not_empty %v", full.GetCall(), chat.GetCallActive(), chat.GetCallNotEmpty())
	}
	if a := groupCallActions(ctx, t, bob, historyOf(bob), gc.GetId(), 1); a[0].GetDuration() != nil {
		t.Fatalf("the start message has a duration: %v", a[0])
	}

	aliceCall, bobCall, carolCall := groupCallInput(alice, gc), groupCallInput(bob, gc), groupCallInput(carol, gc)
	pa, ame, v := joinGroupCall(ctx, t, alice, aliceCall, groupcallpeer.Config{}, nil)
	pb, bme, vb := joinGroupCall(ctx, t, bob, bobCall, groupcallpeer.Config{}, nil)
	if vb != v+1 || !ame.GetJustJoined() || !ame.GetVersioned() || !bme.GetJustJoined() || !bme.GetVersioned() {
		t.Fatalf("Bob's join at version %d after %d: %v, Alice's %v", vb, v, bme, ame)
	}
	// The others get the join as a min row, versioned, the next version
	// (tdesktop data_group_call.cpp:413-441).
	row, rv := waitRow(ctx, t, alice, gc.GetId(), bob.self.GetId(), "join", func(p *mtproto.GroupCallParticipant) bool { return p.GetJustJoined() })
	if !row.GetMin() || row.GetSelf() || !row.GetVersioned() || rv != vb || uint32(row.GetSource()) != pb.AudioSSRC() {
		t.Fatalf("Bob's join seen by Alice: %v at %d", row, rv)
	}
	_, chat = fullChannel(ctx, t, bob, chanOf(bob))
	if !chat.GetCallActive() || !chat.GetCallNotEmpty() {
		t.Fatalf("the channel with a call of two: call_active %v, call_not_empty %v", chat.GetCallActive(), chat.GetCallNotEmpty())
	}

	speak(ctx, t, pa)
	speak(ctx, t, pb)
	hears(ctx, t, "Bob hears Alice", pb, pa.AudioSSRC())
	hears(ctx, t, "Alice hears Bob", pa, pb.AudioSSRC())
	// Nobody gets his own sound back.
	if err := pa.NoRTP(time.Second, groupcallpeer.BySSRC(pa.AudioSSRC())); err != nil {
		t.Fatalf("Alice hears herself: %v", err)
	}

	// A third one: everyone hears everyone.
	pc, _, vc := joinGroupCall(ctx, t, carol, carolCall, groupcallpeer.Config{}, nil)
	if vc != vb+1 {
		t.Fatalf("Carol's join at version %d after %d", vc, vb)
	}
	speak(ctx, t, pc)
	hears(ctx, t, "Carol hears Alice", pc, pa.AudioSSRC())
	hears(ctx, t, "Carol hears Bob", pc, pb.AudioSSRC())
	hears(ctx, t, "Alice hears Carol", pa, pc.AudioSSRC())
	hears(ctx, t, "Bob hears Carol", pb, pc.AudioSSRC())
	if _, rv = waitRow(ctx, t, bob, gc.GetId(), carol.self.GetId(), "join", func(p *mtproto.GroupCallParticipant) bool { return p.GetJustJoined() }); rv != vc {
		t.Fatalf("Carol's join seen by Bob at %d, want %d", rv, vc)
	}

	// A source the client does not know resolves to its participant
	// (tdesktop data_group_call.cpp:897-905); the SFU holds what was joined.
	got := groupParticipants(ctx, t, bob, &mtproto.TLPhoneGetGroupParticipants{Call: bobCall, Sources: []int32{int32(pc.AudioSSRC())}})
	if len(got.GetParticipants()) != 1 || got.GetParticipants()[0].GetPeer().GetUserId() != carol.self.GetId() || got.GetCount() != 3 || got.GetVersion() != vc {
		t.Fatalf("the source of Carol: %v", got)
	}
	if held := checkGroupCall(ctx, t, bob, bobCall, pb.AudioSSRC(), pb.AudioSSRC()+1000); !slices.Equal(held, []uint32{pb.AudioSSRC()}) {
		t.Fatalf("phone.checkGroupCall: %v, want only Bob's own", held)
	}
	whole := getGroupCall(ctx, t, bob, bobCall)
	if whole.GetCall().GetParticipantsCount() != 3 || whole.GetCall().GetVersion() != vc || len(whole.GetParticipants()) != 3 {
		t.Fatalf("phone.getGroupCall: %v", whole)
	}

	// Carol leaves: the others get it versioned, her sound stops; she comes
	// back with a new client and is heard again.
	if me, lv := rowIn(leaveGroupCall(ctx, t, carol, carolCall, pc.AudioSSRC()), gc.GetId(), carol.self.GetId()); !me.GetLeft() || lv != vc+1 {
		t.Fatalf("Carol's leave: %v at %d", me, lv)
	}
	if row, rv = waitRow(ctx, t, alice, gc.GetId(), carol.self.GetId(), "leave", func(p *mtproto.GroupCallParticipant) bool { return p.GetLeft() }); !row.GetVersioned() || rv != vc+1 {
		t.Fatalf("Carol's leave seen by Alice: %v at %d", row, rv)
	}
	silent(t, "Alice after Carol left", pa, pc.AudioSSRC())
	pc.Close()
	pc2, _, vc2 := joinGroupCall(ctx, t, carol, carolCall, groupcallpeer.Config{}, nil)
	if vc2 != vc+2 {
		t.Fatalf("Carol's second join at version %d, want %d", vc2, vc+2)
	}
	speak(ctx, t, pc2)
	hears(ctx, t, "Alice hears Carol again", pa, pc2.AudioSSRC())
	hears(ctx, t, "Carol hears Bob again", pc2, pb.AudioSSRC())

	// Only who manages the call ends it; the end has the duration, goes to
	// those in the call and to the chat, and closes the room.
	if _, err := bob.c.Invoke(ctx, &mtproto.TLPhoneDiscardGroupCall{Constructor: bob.c.Constructor(mtproto.Predicate_phone_discardGroupCall), Call: bobCall}); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Fatalf("Bob ends the call: %v", err)
	}
	time.Sleep(1100 * time.Millisecond) // a duration of at least a second
	ended := discardGroupCall(ctx, t, alice, aliceCall)
	if ended.GetDuration() < 1 {
		t.Fatalf("the ended call lasted %d s", ended.GetDuration())
	}
	waitCall(ctx, t, bob, gc.GetId(), "end", func(c *mtproto.GroupCall) bool { return c.GetPredicateName() == mtproto.Predicate_groupCallDiscarded })
	waitCall(ctx, t, carol, gc.GetId(), "end", func(c *mtproto.GroupCall) bool { return c.GetPredicateName() == mtproto.Predicate_groupCallDiscarded })
	if a := groupCallActions(ctx, t, bob, historyOf(bob), gc.GetId(), 2); a[0].GetDuration().GetValue() != ended.GetDuration() {
		t.Fatalf("the end message: %v, the call lasted %d s", a[0], ended.GetDuration())
	}
	_, chat = fullChannel(ctx, t, bob, chanOf(bob))
	if chat.GetCallActive() || chat.GetCallNotEmpty() {
		t.Fatalf("the channel after the call: call_active %v, call_not_empty %v", chat.GetCallActive(), chat.GetCallNotEmpty())
	}
	silent(t, "Bob after the end", pb, pa.AudioSSRC())
	if _, err := bob.c.Invoke(ctx, joinRequest(bob, bobCall, mustParams(t))); !mtclient.IsRPCError(err, "GROUPCALL_ALREADY_DISCARDED") {
		t.Fatalf("a join after the end: %v", err)
	}
}

func mustParams(t *testing.T) []byte {
	t.Helper()
	p, err := groupcallpeer.New(groupcallpeer.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	params, err := p.JoinParams()
	if err != nil {
		t.Fatal(err)
	}
	return params
}

// An admin takes the word and the SFU stops forwarding the sound; a member
// raises his hand, gets the word back and is heard; a member's own mute and
// volume of another stay his; an admin's volume is everyone's.
func TestGroupCallMuteAndHand(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	us, _, gc := supergroupCall(ctx, t, "Alice", "Bob")
	alice, bob := us[0], us[1]
	aliceCall, bobCall := groupCallInput(alice, gc), groupCallInput(bob, gc)
	pa, _, _ := joinGroupCall(ctx, t, alice, aliceCall, groupcallpeer.Config{}, nil)
	pb, _, _ := joinGroupCall(ctx, t, bob, bobCall, groupcallpeer.Config{}, nil)
	speak(ctx, t, pa)
	speak(ctx, t, pb)
	hears(ctx, t, "Alice hears Bob", pa, pb.AudioSSRC())

	// The word taken: muted without can_self_unmute for everyone, Bob's
	// sound no longer forwarded.
	if _, err := editParticipant(ctx, alice, aliceCall, peerOf(ctx, t, alice, bob), func(r *mtproto.TLPhoneEditGroupCallParticipant) { r.Muted = mtproto.ToBool(true) }); err != nil {
		t.Fatalf("Alice mutes Bob: %v", err)
	}
	if row, _ := waitRow(ctx, t, bob, gc.GetId(), bob.self.GetId(), "muted by the admin", func(p *mtproto.GroupCallParticipant) bool { return p.GetMuted() }); row.GetCanSelfUnmute() {
		t.Fatalf("Bob muted by the admin: %v", row)
	}
	silent(t, "Alice after muting Bob", pa, pb.AudioSSRC())
	hears(ctx, t, "Bob still hears Alice", pb, pa.AudioSSRC())
	if _, err := editParticipant(ctx, bob, bobCall, inputPeerSelf(bob), func(r *mtproto.TLPhoneEditGroupCallParticipant) { r.Muted = mtproto.ToBool(false) }); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Fatalf("Bob unmutes himself without the word: %v", err)
	}

	// The hand: a rating Alice sees; the word given lowers it.
	if _, err := editParticipant(ctx, bob, bobCall, inputPeerSelf(bob), func(r *mtproto.TLPhoneEditGroupCallParticipant) { r.RaiseHand = mtproto.ToBool(true) }); err != nil {
		t.Fatalf("Bob raises his hand: %v", err)
	}
	waitRow(ctx, t, alice, gc.GetId(), bob.self.GetId(), "hand", func(p *mtproto.GroupCallParticipant) bool { return p.GetRaiseHandRating().GetValue() > 0 })
	if _, err := editParticipant(ctx, alice, aliceCall, peerOf(ctx, t, alice, bob), func(r *mtproto.TLPhoneEditGroupCallParticipant) { r.Muted = mtproto.ToBool(false) }); err != nil {
		t.Fatalf("Alice gives Bob the word: %v", err)
	}
	row, _ := waitRow(ctx, t, bob, gc.GetId(), bob.self.GetId(), "the word", func(p *mtproto.GroupCallParticipant) bool { return p.GetCanSelfUnmute() })
	if !row.GetMuted() || row.GetRaiseHandRating() != nil {
		t.Fatalf("Bob given the word: %v", row)
	}
	if _, err := editParticipant(ctx, bob, bobCall, inputPeerSelf(bob), func(r *mtproto.TLPhoneEditGroupCallParticipant) { r.Muted = mtproto.ToBool(false) }); err != nil {
		t.Fatalf("Bob unmutes himself: %v", err)
	}
	hears(ctx, t, "Alice hears Bob again", pa, pb.AudioSSRC())

	// Bob's own mute and volume of Alice: only in his answer, the SFU still
	// forwards her to him (the client applies them, tdesktop
	// data_group_call.cpp:742-752).
	ups, err := editParticipant(ctx, bob, bobCall, peerOf(ctx, t, bob, alice), func(r *mtproto.TLPhoneEditGroupCallParticipant) {
		r.Muted = mtproto.ToBool(true)
		r.Volume = wrapperspb.Int32(5000)
	})
	if err != nil {
		t.Fatalf("Bob mutes Alice for himself: %v", err)
	}
	if mine, _ := rowIn(ups, gc.GetId(), alice.self.GetId()); !mine.GetMutedByYou() || mine.GetMuted() || mine.GetVolume().GetValue() != 5000 || mine.GetVolumeByAdmin() {
		t.Fatalf("Alice muted by Bob for himself: %v", mine)
	}
	hears(ctx, t, "Bob gets Alice's sound after his own mute", pb, pa.AudioSSRC())
	if theirs := groupParticipants(ctx, t, alice, &mtproto.TLPhoneGetGroupParticipants{Call: aliceCall, Ids: []*mtproto.InputPeer{inputPeerSelf(alice)}}); len(theirs.GetParticipants()) != 1 || theirs.GetParticipants()[0].GetMutedByYou() || theirs.GetParticipants()[0].GetMuted() {
		t.Fatalf("Alice's own row after Bob's mute: %v", theirs.GetParticipants())
	}

	// An admin's volume is everyone's.
	if _, err := editParticipant(ctx, alice, aliceCall, peerOf(ctx, t, alice, bob), func(r *mtproto.TLPhoneEditGroupCallParticipant) { r.Volume = wrapperspb.Int32(15000) }); err != nil {
		t.Fatalf("Alice sets Bob's volume: %v", err)
	}
	if row, _ = waitRow(ctx, t, bob, gc.GetId(), bob.self.GetId(), "volume", func(p *mtproto.GroupCallParticipant) bool { return p.GetVolume().GetValue() == 15000 }); !row.GetVolumeByAdmin() {
		t.Fatalf("Bob's volume set by the admin: %v", row)
	}
}

// A camera in three layers and a screen in two: the others see the
// endpoints and source groups, the SFU forwards the layer a receiver asks
// for under the first SSRC and tells the sender what is watched; the screen
// is a second connection of its own.
func TestGroupCallVideoAndScreen(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	us, _, gc := supergroupCall(ctx, t, "Alice", "Bob")
	alice, bob := us[0], us[1]
	aliceCall, bobCall := groupCallInput(alice, gc), groupCallInput(bob, gc)
	pa, me, _ := joinGroupCall(ctx, t, alice, aliceCall, groupcallpeer.Config{VideoLayers: 3}, nil)
	if me.GetVideo().GetEndpoint() != pa.Endpoint() || pa.Endpoint() == "" {
		t.Fatalf("Alice's camera: row %v, endpoint of the answer %q", me.GetVideo(), pa.Endpoint())
	}
	pb, _, _ := joinGroupCall(ctx, t, bob, bobCall, groupcallpeer.Config{}, nil)

	// Bob sees the camera as the answer named it, with its sources
	// (GroupInstanceCustomImpl.cpp:1728-1790 shows the first SSRC of SIM).
	rows := groupParticipants(ctx, t, bob, &mtproto.TLPhoneGetGroupParticipants{Call: bobCall, Ids: []*mtproto.InputPeer{peerOf(ctx, t, bob, alice)}})
	if len(rows.GetParticipants()) != 1 {
		t.Fatalf("Alice's row for Bob: %v", rows.GetParticipants())
	}
	video := rows.GetParticipants()[0].GetVideo()
	var sim []uint32
	for _, g := range video.GetSourceGroups() {
		if g.GetSemantics() == tgcallsjson.SemanticsSIM {
			for _, s := range g.GetSources() {
				sim = append(sim, uint32(s))
			}
		}
	}
	var layers []uint32
	for _, l := range pa.Layers() {
		layers = append(layers, l.SSRC)
	}
	if video.GetEndpoint() != pa.Endpoint() || !slices.Equal(sim, layers) || len(video.GetSourceGroups()) != 4 {
		t.Fatalf("Alice's camera for Bob: %v, layers %v", video, layers)
	}

	vctx, vcancel := context.WithCancel(ctx)
	defer vcancel()
	camera, err := pa.StreamVideo(vctx)
	if err != nil {
		t.Fatal(err)
	}
	wait := func(what string, f func(context.Context) error) {
		t.Helper()
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if err := f(cctx); err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	wait("Bob's data channel", pb.WaitDataChannel)
	main := pa.Layers()[0].SSRC
	layerFrom := func(ssrc uint32, layer int) func(context.Context) error {
		return func(cctx context.Context) error {
			_, err := pb.AwaitRTP(cctx, func(p *rtp.Packet) bool { return p.SSRC == ssrc && groupcallpeer.LayerOf(p) == layer })
			return err
		}
	}
	// A thumbnail, then the full picture, always under the first SSRC.
	if err := pb.SendVideoConstraints(nil, map[string]tgcallsjson.VideoConstraint{pa.Endpoint(): {MinHeight: 180, MaxHeight: 180}}); err != nil {
		t.Fatal(err)
	}
	wait("the thumbnail of the camera", layerFrom(main, 0))
	wait("SenderVideoConstraints 180", func(c context.Context) error { return camera.AwaitIdealHeight(c, 180) })
	if err := pb.SendVideoConstraints([]string{pa.Endpoint()}, map[string]tgcallsjson.VideoConstraint{pa.Endpoint(): {MinHeight: 180, MaxHeight: 720}}); err != nil {
		t.Fatal(err)
	}
	wait("the full layer of the camera", layerFrom(main, 2))
	wait("SenderVideoConstraints 720", func(c context.Context) error { return camera.AwaitIdealHeight(c, 720) })

	// The screen: a second connection, presentation in the answer and in
	// the row the others get (tdesktop calls_group_call.cpp:3165-3185).
	screen, err := groupcallpeer.New(groupcallpeer.Config{VideoLayers: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(screen.Close)
	params, err := screen.JoinParams()
	if err != nil {
		t.Fatal(err)
	}
	o, err := alice.c.Invoke(ctx, &mtproto.TLPhoneJoinGroupCallPresentation{
		Constructor: alice.c.Constructor(mtproto.Predicate_phone_joinGroupCallPresentation),
		Call:        aliceCall,
		Params:      mtproto.MakeTLDataJSON(&mtproto.DataJSON{Data: string(params)}).To_DataJSON(),
	})
	if err != nil {
		t.Fatalf("phone.joinGroupCallPresentation: %v", err)
	}
	var conn *mtproto.Update
	for _, x := range updatesOf(t, "phone.joinGroupCallPresentation", o).GetUpdates() {
		if x.GetPredicateName() == mtproto.Predicate_updateGroupCallConnection && x.GetPresentation() {
			conn = x
		}
	}
	if conn == nil {
		t.Fatal("phone.joinGroupCallPresentation without the connection of the screen")
	}
	wait("the screen connects", func(c context.Context) error { return screen.Connect(c, []byte(conn.GetParams().GetData())) })
	row, _ := waitRow(ctx, t, bob, gc.GetId(), alice.self.GetId(), "screen", func(p *mtproto.GroupCallParticipant) bool { return p.GetPresentation() != nil })
	if row.GetPresentation().GetEndpoint() != screen.Endpoint() || screen.Endpoint() == pa.Endpoint() || len(row.GetPresentation().GetSourceGroups()) != 3 {
		t.Fatalf("Alice's screen for Bob: %v, endpoint %q", row.GetPresentation(), screen.Endpoint())
	}
	if _, err := screen.StreamVideo(vctx); err != nil {
		t.Fatal(err)
	}
	if err := pb.SendVideoConstraints([]string{screen.Endpoint()}, map[string]tgcallsjson.VideoConstraint{
		pa.Endpoint():     {MinHeight: 180, MaxHeight: 180},
		screen.Endpoint(): {MinHeight: 180, MaxHeight: 720},
	}); err != nil {
		t.Fatal(err)
	}
	wait("the full layer of the screen", layerFrom(screen.Layers()[0].SSRC, 1))
	wait("the camera back to the thumbnail", layerFrom(main, 0))

	// The screen ends: the others get the row without it, its packets stop.
	if _, err := alice.c.Invoke(ctx, &mtproto.TLPhoneLeaveGroupCallPresentation{
		Constructor: alice.c.Constructor(mtproto.Predicate_phone_leaveGroupCallPresentation),
		Call:        aliceCall,
	}); err != nil {
		t.Fatalf("phone.leaveGroupCallPresentation: %v", err)
	}
	waitRow(ctx, t, bob, gc.GetId(), alice.self.GetId(), "screen off", func(p *mtproto.GroupCallParticipant) bool { return p.GetPresentation() == nil })
	silent(t, "Bob after the screen ended", pb, screen.Layers()[0].SSRC)
}

// A client on a network without UDP joins over ICE-TCP, the only fallback
// the stock clients have for group calls (GroupNetworkManager.cpp:398).
func TestGroupCallTCPOnly(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	us, _, gc := supergroupCall(ctx, t, "Alice", "Bob")
	alice, bob := us[0], us[1]
	pa, _, _ := joinGroupCall(ctx, t, alice, groupCallInput(alice, gc), groupcallpeer.Config{}, nil)
	pb, _, _ := joinGroupCall(ctx, t, bob, groupCallInput(bob, gc), groupcallpeer.Config{Networks: []ice.NetworkType{ice.NetworkTypeTCP4}}, nil)
	if n := pb.PairNetwork(); n != "tcp4" {
		t.Fatalf("Bob's pair is %q, want tcp4", n)
	}
	speak(ctx, t, pa)
	speak(ctx, t, pb)
	hears(ctx, t, "Bob over TCP hears Alice", pb, pa.AudioSSRC())
	hears(ctx, t, "Alice hears Bob over TCP", pa, pb.AudioSSRC())
}

// A call in a broadcast channel: one joins only as himself (the owner's
// decision of 2026-11-12, ADR 0060); with join_muted a subscriber listens
// and raises his hand; the speaker's link gives the word, the listener's
// does not; the end with its duration in the channel.
func TestGroupCallChannel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 4)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	dave := login(ctx, t, phones[3], "Dave")
	_, ch := createChannel(ctx, t, alice, "group call channel", false)
	alicePeer, aliceChan := channelInputs(alice, ch)
	if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsUpdateUsername{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_updateUsername),
		Channel:     aliceChan,
		Username:    fmt.Sprintf("pluma_gc_%d", ch.GetId()),
	}); err != nil {
		t.Fatalf("channels.updateUsername: %v", err)
	}
	_, bobChan := channelInputs(bob, ch)
	if _, err := bob.c.Invoke(ctx, &mtproto.TLChannelsJoinChannel7F6A1E22{Constructor: bob.c.Constructor(mtproto.Predicate_channels_joinChannel7F6A1E22), Channel: bobChan}); err != nil {
		t.Fatalf("channels.joinChannel: %v", err)
	}
	for _, u := range []*user{alice, bob, carol, dave} {
		getState(ctx, t, u)
	}
	gc := createGroupCall(ctx, t, alice, alicePeer, "Live", 0)
	aliceCall, bobCall := groupCallInput(alice, gc), groupCallInput(bob, gc)
	waitCall(ctx, t, bob, gc.GetId(), "start", func(c *mtproto.GroupCall) bool { return true })

	// Only the account itself to join as.
	o, err := bob.c.Invoke(ctx, &mtproto.TLPhoneGetGroupCallJoinAs{Constructor: bob.c.Constructor(mtproto.Predicate_phone_getGroupCallJoinAs), Peer: func() *mtproto.InputPeer { p, _ := channelInputs(bob, ch); return p }()})
	if err != nil {
		t.Fatalf("phone.getGroupCallJoinAs: %v", err)
	}
	if as := o.(interface {
		To_Phone_JoinAsPeers() *mtproto.Phone_JoinAsPeers
	}).To_Phone_JoinAsPeers(); len(as.GetPeers()) != 1 || as.GetPeers()[0].GetUserId() != bob.self.GetId() {
		t.Fatalf("phone.getGroupCallJoinAs: %v", as.GetPeers())
	}
	asChannel := joinRequest(alice, aliceCall, mustParams(t))
	asChannel.JoinAs = alicePeer
	if _, err := alice.c.Invoke(ctx, asChannel); !mtclient.IsRPCError(err, "JOIN_AS_PEER_INVALID") {
		t.Fatalf("a join as the channel: %v", err)
	}

	// join_muted: a subscriber listens.
	o, err = alice.c.Invoke(ctx, &mtproto.TLPhoneToggleGroupCallSettings{
		Constructor: alice.c.Constructor(mtproto.Predicate_phone_toggleGroupCallSettings),
		Call:        aliceCall,
		JoinMuted:   mtproto.ToBool(true),
	})
	if err != nil {
		t.Fatalf("phone.toggleGroupCallSettings: %v", err)
	}
	if c := groupCallIn(updatesOf(t, "phone.toggleGroupCallSettings", o)); !c.GetJoinMuted() || !c.GetCanChangeJoinMuted() {
		t.Fatalf("join_muted: %v", c)
	}
	pa, _, _ := joinGroupCall(ctx, t, alice, aliceCall, groupcallpeer.Config{}, nil)
	pb, me, _ := joinGroupCall(ctx, t, bob, bobCall, groupcallpeer.Config{}, nil)
	if !me.GetMuted() || me.GetCanSelfUnmute() {
		t.Fatalf("a subscriber in a call with join_muted: %v", me)
	}
	speak(ctx, t, pa)
	speak(ctx, t, pb) // a muted client sends nothing; the SFU must not forward it if it did
	hears(ctx, t, "Bob listens to Alice", pb, pa.AudioSSRC())
	silent(t, "Alice and Bob without the word", pa, pb.AudioSSRC())

	// The hand, the word, Bob is heard.
	if _, err := editParticipant(ctx, bob, bobCall, inputPeerSelf(bob), func(r *mtproto.TLPhoneEditGroupCallParticipant) { r.RaiseHand = mtproto.ToBool(true) }); err != nil {
		t.Fatalf("Bob raises his hand: %v", err)
	}
	waitRow(ctx, t, alice, gc.GetId(), bob.self.GetId(), "hand", func(p *mtproto.GroupCallParticipant) bool { return p.GetRaiseHandRating().GetValue() > 0 })
	if _, err := editParticipant(ctx, alice, aliceCall, peerOf(ctx, t, alice, bob), func(r *mtproto.TLPhoneEditGroupCallParticipant) { r.Muted = mtproto.ToBool(false) }); err != nil {
		t.Fatalf("Alice gives Bob the word: %v", err)
	}
	waitRow(ctx, t, bob, gc.GetId(), bob.self.GetId(), "the word", func(p *mtproto.GroupCallParticipant) bool { return p.GetCanSelfUnmute() })
	if _, err := editParticipant(ctx, bob, bobCall, inputPeerSelf(bob), func(r *mtproto.TLPhoneEditGroupCallParticipant) { r.Muted = mtproto.ToBool(false) }); err != nil {
		t.Fatalf("Bob unmutes himself: %v", err)
	}
	hears(ctx, t, "Alice hears Bob", pa, pb.AudioSSRC())

	// The links: only who manages the call gets the speaker's; with it one
	// who is not even subscribed joins with the word, with the listener's
	// without it (Android GroupCallActivity.java:6632-6655).
	export := func(u *user, call *mtproto.InputGroupCall, speaker bool) (string, error) {
		o, err := u.c.Invoke(ctx, &mtproto.TLPhoneExportGroupCallInvite{Constructor: u.c.Constructor(mtproto.Predicate_phone_exportGroupCallInvite), Call: call, CanSelfUnmute: speaker})
		if err != nil {
			return "", err
		}
		link := o.(interface {
			To_Phone_ExportedGroupCallInvite() *mtproto.Phone_ExportedGroupCallInvite
		}).To_Phone_ExportedGroupCallInvite().GetLink()
		_, hash, ok := strings.Cut(link, "?voicechat=")
		if !ok || hash == "" {
			return "", fmt.Errorf("link %q", link)
		}
		return hash, nil
	}
	if _, err := export(bob, bobCall, true); !mtclient.IsRPCError(err, "CHAT_ADMIN_REQUIRED") {
		t.Fatalf("Bob asks for the speaker's link: %v", err)
	}
	speakerHash, err := export(alice, aliceCall, true)
	if err != nil {
		t.Fatalf("the speaker's link: %v", err)
	}
	listenerHash, err := export(bob, bobCall, false)
	if err != nil || listenerHash == speakerHash {
		t.Fatalf("the listener's link: %q %v", listenerHash, err)
	}
	carolCall, daveCall := groupCallInput(carol, gc), groupCallInput(dave, gc)
	pc, cme, _ := joinGroupCall(ctx, t, carol, carolCall, groupcallpeer.Config{}, func(r *mtproto.TLPhoneJoinGroupCall) { r.InviteHash = wrapperspb.String(speakerHash) })
	if !cme.GetCanSelfUnmute() {
		t.Fatalf("Carol with the speaker's link: %v", cme)
	}
	_, dme, _ := joinGroupCall(ctx, t, dave, daveCall, groupcallpeer.Config{}, func(r *mtproto.TLPhoneJoinGroupCall) {
		r.InviteHash = wrapperspb.String(listenerHash)
		r.Muted = true
	})
	if dme.GetCanSelfUnmute() || !dme.GetMuted() {
		t.Fatalf("Dave with the listener's link: %v", dme)
	}
	speak(ctx, t, pc)
	hears(ctx, t, "Alice hears Carol", pa, pc.AudioSSRC())

	// A reset makes new links.
	if _, err := alice.c.Invoke(ctx, &mtproto.TLPhoneToggleGroupCallSettings{
		Constructor:     alice.c.Constructor(mtproto.Predicate_phone_toggleGroupCallSettings),
		Call:            aliceCall,
		ResetInviteHash: true,
	}); err != nil {
		t.Fatalf("reset of the links: %v", err)
	}
	if again, err := export(alice, aliceCall, true); err != nil || again == speakerHash {
		t.Fatalf("the speaker's link after a reset: %q %v", again, err)
	}

	time.Sleep(1100 * time.Millisecond)
	ended := discardGroupCall(ctx, t, alice, aliceCall)
	bobPeer, _ := channelInputs(bob, ch)
	if a := groupCallActions(ctx, t, bob, bobPeer, gc.GetId(), 2); a[0].GetDuration().GetValue() != ended.GetDuration() || ended.GetDuration() < 1 {
		t.Fatalf("the end message in the channel: %v, the call lasted %d s", a[0], ended.GetDuration())
	}
	waitCall(ctx, t, dave, gc.GetId(), "end", func(c *mtproto.GroupCall) bool { return c.GetPredicateName() == mtproto.Predicate_groupCallDiscarded })
}

// A scheduled call in a basic group: the members learn it, one subscribes,
// nobody joins before the start; the start opens it.
func TestGroupCallScheduled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	chatId := createChat(ctx, t, alice, "scheduled call", bob)
	getState(ctx, t, bob)
	at := int32(time.Now().Add(time.Hour).Unix())
	gc := createGroupCall(ctx, t, alice, chatPeer(alice, chatId), "Planning", at)
	if gc.GetScheduleDate().GetValue() != at {
		t.Fatalf("the scheduled call: %v", gc)
	}
	waitCall(ctx, t, bob, gc.GetId(), "scheduled", func(c *mtproto.GroupCall) bool { return c.GetScheduleDate().GetValue() == at })
	if a := groupCallActions(ctx, t, bob, chatPeer(bob, chatId), gc.GetId(), 1); a[0].GetPredicateName() != mtproto.Predicate_messageActionGroupCallScheduled || a[0].GetScheduleDate_INT32() != at {
		t.Fatalf("the scheduled message: %v", a[0])
	}

	bobCall := groupCallInput(bob, gc)
	o, err := bob.c.Invoke(ctx, &mtproto.TLPhoneToggleGroupCallStartSubscription{
		Constructor: bob.c.Constructor(mtproto.Predicate_phone_toggleGroupCallStartSubscription),
		Call:        bobCall,
		Subscribed:  mtproto.ToBool(true),
	})
	if err != nil {
		t.Fatalf("phone.toggleGroupCallStartSubscription: %v", err)
	}
	if c := groupCallIn(updatesOf(t, "phone.toggleGroupCallStartSubscription", o)); !c.GetScheduleStartSubscribed() {
		t.Fatalf("subscribed: %v", c)
	}
	if _, err := bob.c.Invoke(ctx, joinRequest(bob, bobCall, mustParams(t))); err == nil {
		t.Fatal("a join before the start")
	}

	if _, err := alice.c.Invoke(ctx, &mtproto.TLPhoneStartScheduledGroupCall{
		Constructor: alice.c.Constructor(mtproto.Predicate_phone_startScheduledGroupCall),
		Call:        groupCallInput(alice, gc),
	}); err != nil {
		t.Fatalf("phone.startScheduledGroupCall: %v", err)
	}
	// The chat goes with the updates whose step changes its call flags, as
	// it is after the step: tdesktop applies the flags of every chat it
	// gets (data_session.cpp:999-1016), so a chat from before the step
	// would undo it.
	_, chat := waitCallChat(ctx, t, bob, gc.GetId(), chatId, "started", func(c *mtproto.GroupCall) bool { return c.GetScheduleDate() == nil })
	if chat == nil || !chat.GetCallActive() || chat.GetCallNotEmpty() {
		t.Fatalf("the basic group with the started call: %v", chat)
	}
	if a := groupCallActions(ctx, t, bob, chatPeer(bob, chatId), gc.GetId(), 2); a[0].GetPredicateName() != mtproto.Predicate_messageActionGroupCall || a[0].GetDuration() != nil {
		t.Fatalf("the start message: %v", a[0])
	}
	pa, _, _ := joinGroupCall(ctx, t, alice, groupCallInput(alice, gc), groupcallpeer.Config{}, nil)
	_, chat = waitCallChat(ctx, t, bob, gc.GetId(), chatId, "first join", func(c *mtproto.GroupCall) bool { return c.GetParticipantsCount() == 1 })
	if chat == nil || !chat.GetCallActive() || !chat.GetCallNotEmpty() {
		t.Fatalf("the basic group after the first join: %v", chat)
	}
	pb, _, _ := joinGroupCall(ctx, t, bob, bobCall, groupcallpeer.Config{}, nil)
	speak(ctx, t, pa)
	hears(ctx, t, "Bob hears Alice in the basic group", pb, pa.AudioSSRC())

	o, err = alice.c.Invoke(ctx, &mtproto.TLPhoneDiscardGroupCall{Constructor: alice.c.Constructor(mtproto.Predicate_phone_discardGroupCall), Call: groupCallInput(alice, gc)})
	if err != nil {
		t.Fatalf("phone.discardGroupCall: %v", err)
	}
	if chat = chatIn(updatesOf(t, "phone.discardGroupCall", o).GetChats(), chatId); chat == nil || chat.GetCallActive() || chat.GetCallNotEmpty() {
		t.Fatalf("the basic group in the answer of the end: %v", chat)
	}
	_, chat = waitCallChat(ctx, t, bob, gc.GetId(), chatId, "ended", func(c *mtproto.GroupCall) bool {
		return c.GetPredicateName() == mtproto.Predicate_groupCallDiscarded
	})
	if chat == nil || chat.GetCallActive() || chat.GetCallNotEmpty() {
		t.Fatalf("the basic group after the end: %v", chat)
	}
}

// The SFU restarts: the rooms are gone, phone.checkGroupCall no longer
// lists the clients' sources, and the clients join again, as tdesktop and
// iOS do on it (calls_group_call.cpp:3746-3776,
// PresentationGroupCall.swift:2221-2226), and hear each other again. Not
// parallel: the SFU is the stand's only one.
func TestGroupCallSFURestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	us, _, gc := supergroupCall(ctx, t, "Alice", "Bob")
	alice, bob := us[0], us[1]
	aliceCall, bobCall := groupCallInput(alice, gc), groupCallInput(bob, gc)
	pa, _, _ := joinGroupCall(ctx, t, alice, aliceCall, groupcallpeer.Config{}, nil)
	pb, _, _ := joinGroupCall(ctx, t, bob, bobCall, groupcallpeer.Config{}, nil)
	speak(ctx, t, pa)
	hears(ctx, t, "Bob hears Alice", pb, pa.AudioSSRC())
	if held := checkGroupCall(ctx, t, alice, aliceCall, pa.AudioSSRC()); !slices.Equal(held, []uint32{pa.AudioSSRC()}) {
		t.Fatalf("phone.checkGroupCall before the restart: %v", held)
	}

	// The container goes away and a new one comes, at another address on
	// the network of the services, as on a crash and a redeploy.
	t.Cleanup(func() { restore(t) })
	stack(t, "rm", "--stop", "--force", "sfu")
	restore(t)

	deadline := time.Now().Add(30 * time.Second)
	for {
		held := checkGroupCall(ctx, t, alice, aliceCall, pa.AudioSSRC())
		if len(held) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("phone.checkGroupCall after the restart: %v", held)
		}
		time.Sleep(time.Second)
	}
	pa.Close()
	pb.Close()
	pa2, _, _ := joinGroupCall(ctx, t, alice, aliceCall, groupcallpeer.Config{}, nil)
	pb2, _, _ := joinGroupCall(ctx, t, bob, bobCall, groupcallpeer.Config{}, nil)
	speak(ctx, t, pa2)
	speak(ctx, t, pb2)
	hears(ctx, t, "Bob hears Alice after the restart", pb2, pa2.AudioSSRC())
	hears(ctx, t, "Alice hears Bob after the restart", pa2, pb2.AudioSSRC())
	if held := checkGroupCall(ctx, t, bob, bobCall, pb2.AudioSSRC()); !slices.Equal(held, []uint32{pb2.AudioSSRC()}) {
		t.Fatalf("phone.checkGroupCall after the join again: %v", held)
	}
	if c := getGroupCall(ctx, t, alice, aliceCall).GetCall(); c.GetParticipantsCount() != 2 {
		t.Fatalf("the call after the restart: %v", c)
	}
}
