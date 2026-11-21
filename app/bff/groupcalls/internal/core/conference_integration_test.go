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

package core

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/teamgram/proto/mtproto"
	msg_client "github.com/teamgram/teamgram-server/app/messenger/msg/msg/client"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	message_client "github.com/teamgram/teamgram-server/app/service/biz/message/client"
	messagepb "github.com/teamgram/teamgram-server/app/service/biz/message/message"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/groupcalls"
	"github.com/teamgram/teamgram-server/pkg/tde2e"
	"github.com/teamgram/teamgram-server/pkg/tde2e/tde2etest"

	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// boxes are the private chats of the conference tests: each message in the
// box of its sender and of its recipient, with one dialog_message_id, as
// msg keeps them; the edits of the sender apply to both.
type boxes struct {
	mu     sync.Mutex
	next   int32
	byUser map[int64]map[int32]*mtproto.MessageBox
	edits  []*msg.TLMsgEditMessageV2
}

func (b *boxes) add(userId, peerId int64, dialogMessageId int64, m *mtproto.Message, out bool) *mtproto.MessageBox {
	b.next++
	cp := proto.Clone(m).(*mtproto.Message)
	cp.Id, cp.Out = b.next, out
	box := &mtproto.MessageBox{UserId: userId, MessageId: b.next, PeerType: mtproto.PEER_USER, PeerId: peerId, DialogMessageId: dialogMessageId, Message: cp}
	if b.byUser[userId] == nil {
		b.byUser[userId] = map[int32]*mtproto.MessageBox{}
	}
	b.byUser[userId][b.next] = box
	return box
}

// actionOf is the action of the copy of a message in the box of userId.
func (b *boxes) actionOf(userId int64, dialogMessageId int64) *mtproto.MessageAction {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, box := range b.byUser[userId] {
		if box.DialogMessageId == dialogMessageId {
			return box.Message.GetAction()
		}
	}
	return nil
}

// last is the newest message in the box of userId.
func (b *boxes) last(userId int64) *mtproto.MessageBox {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out *mtproto.MessageBox
	for _, box := range b.byUser[userId] {
		if out == nil || box.MessageId > out.MessageId {
			out = box
		}
	}
	return out
}

type boxMsg struct {
	msg_client.MsgClient
	b *boxes
}

func (f boxMsg) MsgSendMessageV2(_ context.Context, in *msg.TLMsgSendMessageV2) (*mtproto.Updates, error) {
	f.b.mu.Lock()
	defer f.b.mu.Unlock()
	m := in.GetMessage()[0]
	dm := int64(f.b.next) + 1_000_000
	mine := f.b.add(in.GetUserId(), in.GetPeerId(), dm, m.GetMessage(), true)
	f.b.add(in.GetPeerId(), in.GetUserId(), dm, m.GetMessage(), false)
	return mtproto.MakeTLUpdates(&mtproto.Updates{Updates: []*mtproto.Update{
		mtproto.MakeTLUpdateMessageID(&mtproto.Update{Id_INT32: mine.MessageId, RandomId: m.GetRandomId()}).To_Update(),
		mtproto.MakeTLUpdateNewMessage(&mtproto.Update{Message_MESSAGE: mine.Message}).To_Update(),
	}}).To_Updates(), nil
}

func (f boxMsg) MsgEditMessageV2(_ context.Context, in *msg.TLMsgEditMessageV2) (*mtproto.Updates, error) {
	f.b.mu.Lock()
	defer f.b.mu.Unlock()
	mine := f.b.byUser[in.GetUserId()][in.GetDstMessage().GetMessageId()]
	if mine == nil {
		return nil, mtproto.ErrMessageIdInvalid
	}
	f.b.edits = append(f.b.edits, in)
	for _, box := range f.b.byUser[in.GetPeerId()] {
		if box.DialogMessageId == mine.DialogMessageId {
			box.Message.Action = proto.Clone(in.GetNewMessage().GetMessage().GetAction()).(*mtproto.MessageAction)
		}
	}
	mine.Message.Action = proto.Clone(in.GetNewMessage().GetMessage().GetAction()).(*mtproto.MessageAction)
	return mtproto.MakeTLUpdates(&mtproto.Updates{Updates: []*mtproto.Update{
		mtproto.MakeTLUpdateEditMessage(&mtproto.Update{Message_MESSAGE: mine.Message}).To_Update(),
	}}).To_Updates(), nil
}

type boxMessages struct {
	message_client.MessageClient
	b *boxes
}

func (f boxMessages) MessageGetUserMessage(_ context.Context, in *messagepb.TLMessageGetUserMessage) (*mtproto.MessageBox, error) {
	f.b.mu.Lock()
	defer f.b.mu.Unlock()
	box := f.b.byUser[in.GetUserId()][in.GetId()]
	if box == nil {
		return nil, mtproto.ErrMessageIdInvalid
	}
	return proto.Clone(box).(*mtproto.MessageBox), nil
}

// confUsers are the users of the conference tests: 9 keeps his calls to
// no one, 8 blocked the others.
type confUsers struct{ fakeUsers }

func (confUsers) user(id int64) *mtproto.ImmutableUser {
	rule := mtproto.MakeTLPrivacyValueAllowAll(nil).To_PrivacyRule()
	if id == 9 {
		rule = mtproto.MakeTLPrivacyValueDisallowAll(nil).To_PrivacyRule()
	}
	return &mtproto.ImmutableUser{
		User:             &mtproto.UserData{Id: id, FirstName: "u"},
		KeysPrivacyRules: []*mtproto.PrivacyKeyRules{{Key: mtproto.PHONE_CALL, Rules: []*mtproto.PrivacyRule{rule}}},
	}
}

func (u confUsers) UserGetMutableUsersV2(_ context.Context, in *userpb.TLUserGetMutableUsersV2) (*mtproto.MutableUsers, error) {
	out := &mtproto.MutableUsers{}
	for _, id := range in.GetId() {
		out.Users = append(out.Users, u.user(id))
	}
	return out, nil
}

func (confUsers) UserBlockedByUser(_ context.Context, in *userpb.TLUserBlockedByUser) (*mtproto.Bool, error) {
	return mtproto.ToBool(in.GetUserId() == 8), nil
}

func newConferenceWorld(t *testing.T) (*world, *boxes) {
	w := newWorld(t)
	b := &boxes{byUser: map[int64]map[int32]*mtproto.MessageBox{}}
	w.svcCtx.Dao.MsgClient = boxMsg{b: b}
	w.svcCtx.Dao.MessageClient = boxMessages{b: b}
	w.svcCtx.Dao.UserClient = confUsers{}
	return w, b
}

func apiError(t *testing.T, what string, err error, want string) {
	t.Helper()
	st, _ := status.FromError(err)
	if err == nil || !strings.HasPrefix(st.Message(), want) {
		t.Fatalf("%s: %v, want %s", what, err, want)
	}
}

func slugOf(link string) string { return link[strings.LastIndex(link, "/")+1:] }

// joinConference joins userId into the conference by in, with his block on
// top of its chain.
func (w *world) joinConference(t *testing.T, userId int64, callId int64, in *mtproto.InputGroupCall, ssrc uint32) (*mtproto.Updates, error) {
	t.Helper()
	ch := w.chainOf(t, callId)
	pk := tde2etest.Pub(keyOf(userId))
	return w.as(userId).PhoneJoinGroupCall(&mtproto.TLPhoneJoinGroupCall{
		Call: in, JoinAs: self(), Params: params(ssrc, false),
		PublicKey: pk[:], Block: tde2etest.JoinBlock(ch, userId, keyOf(userId)),
	})
}

// A conference from its link to its end: the three references, the
// creator's rights, the invitations and their messages, the kick.
func TestConferenceFlow(t *testing.T) {
	w, b := newConferenceWorld(t)
	const a, bob, carol, dave = 21, 22, 23, 24

	// A link made ahead: one updateGroupCall, no peer, the link.
	created, err := w.as(a).PhoneCreateConferenceCall7D0444BB(&mtproto.TLPhoneCreateConferenceCall7D0444BB{RandomId: 5})
	if err != nil {
		t.Fatal(err)
	}
	ups := updatesOf(created, mtproto.Predicate_updateGroupCall)
	if len(ups) != 1 || ups[0].GetPeer_PEER() != nil {
		t.Fatalf("the answer of a link: %v", created.GetUpdates())
	}
	gc := ups[0].GetCall_GROUPCALL()
	if !gc.GetConference() || !gc.GetCreator() || !strings.Contains(gc.GetInviteLink().GetValue(), "/call/") || gc.GetTitle() != nil {
		t.Fatalf("the conference: %v", gc)
	}
	again, err := w.as(a).PhoneCreateConferenceCall7D0444BB(&mtproto.TLPhoneCreateConferenceCall7D0444BB{RandomId: 5})
	if err != nil || callOf(t, again).GetId() != gc.GetId() {
		t.Fatalf("a repeat: %v", err)
	}
	canon := mtproto.MakeTLInputGroupCall(&mtproto.InputGroupCall{Id: gc.GetId(), AccessHash: gc.GetAccessHash()}).To_InputGroupCall()
	slug := slugOf(gc.GetInviteLink().GetValue())
	bySlug := mtproto.MakeTLInputGroupCallSlug(&mtproto.InputGroupCall{Slug: slug}).To_InputGroupCall()

	// A stranger sees nothing by the id; by the slug he does.
	if _, err = w.as(bob).PhoneGetGroupCall(&mtproto.TLPhoneGetGroupCall{Call: canon, Limit: 5}); err == nil {
		t.Fatal("a stranger read the conference by its id")
	}
	got, err := w.as(bob).PhoneGetGroupCall(&mtproto.TLPhoneGetGroupCall{Call: bySlug, Limit: 5})
	if err != nil || got.GetCall().GetId() != gc.GetId() || got.GetCall().GetCreator() {
		t.Fatalf("by the slug: %v %v", got, err)
	}
	head, err := w.as(bob).PhoneGetGroupCallChainBlocks(&mtproto.TLPhoneGetGroupCallChainBlocks{Call: bySlug, SubChainId: 0, Offset: -1, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if up := chainUpdates(head)[0]; up.GetCall_INPUTGROUPCALL().GetPredicateName() != mtproto.Predicate_inputGroupCall || len(up.GetBlocks()) != 0 || up.GetNextOffset() != 0 {
		t.Fatalf("the empty chain by the slug: %v", up)
	}

	// The first joiner writes the zero block, by the slug; the answer names
	// the call canonically.
	joined, err := w.joinConference(t, bob, gc.GetId(), bySlug, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, up := range joined.GetUpdates() {
		if in := up.GetCall_INPUTGROUPCALL(); in != nil && in.GetPredicateName() != mtproto.Predicate_inputGroupCall {
			t.Fatalf("a non-canonical call in the answer: %v", up)
		}
	}
	if _, err = w.joinConference(t, a, gc.GetId(), canon, 101); err != nil {
		t.Fatal(err)
	}

	// Invitations: privacy, block, a stranger cannot invite.
	invite := func(from, to int64) (*mtproto.Updates, error) {
		return w.as(from).PhoneInviteConferenceCallParticipant(&mtproto.TLPhoneInviteConferenceCallParticipant{
			Call: canon, UserId: mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: to}).To_InputUser(),
		})
	}
	_, err = invite(bob, 9)
	apiError(t, "the privacy of calls", err, "USER_PRIVACY_RESTRICTED")
	_, err = invite(bob, 8)
	apiError(t, "a block", err, "USER_PRIVACY_RESTRICTED")
	_, err = invite(carol, dave)
	apiError(t, "an invitation of one not in the call", err, "GROUPCALL_FORBIDDEN")
	_, err = invite(bob, a)
	apiError(t, "an invitation of one in the call", err, "USER_ALREADY_PARTICIPANT")

	// Carol rings, joins by her message: the message becomes active in both
	// boxes.
	reply, err := invite(bob, carol)
	if err != nil {
		t.Fatal(err)
	}
	if len(updatesOf(reply, mtproto.Predicate_updateNewMessage)) != 1 {
		t.Fatalf("the answer of an invitation: %v", reply.GetUpdates())
	}
	carolMsg := b.last(carol)
	if a := carolMsg.Message.GetAction(); a.GetCallId() != gc.GetId() || a.GetActive() || a.GetMissed() || len(a.GetOtherParticipants()) != 1 {
		t.Fatalf("the invitation: %v", a)
	}
	byInvite := mtproto.MakeTLInputGroupCallInviteMessage(&mtproto.InputGroupCall{MsgId: carolMsg.MessageId}).To_InputGroupCall()
	if _, err = w.as(dave).PhoneGetGroupCall(&mtproto.TLPhoneGetGroupCall{Call: byInvite, Limit: 3}); err == nil {
		t.Fatal("another's message gave the conference")
	}
	if _, err = w.joinConference(t, carol, gc.GetId(), byInvite, 102); err != nil {
		t.Fatal(err)
	}
	for _, uid := range []int64{bob, carol} {
		if act := b.actionOf(uid, carolMsg.DialogMessageId); !act.GetActive() {
			t.Fatalf("the invitation in the box of %d after the join: %v", uid, act)
		}
	}

	// Dave declines: missed; a repeat changes nothing.
	if _, err = invite(a, dave); err != nil {
		t.Fatal(err)
	}
	daveMsg := b.last(dave)
	if _, err = w.as(dave).PhoneDeclineConferenceCallInvite(&mtproto.TLPhoneDeclineConferenceCallInvite{MsgId: daveMsg.MessageId}); err != nil {
		t.Fatal(err)
	}
	edits := len(b.edits)
	if _, err = w.as(dave).PhoneDeclineConferenceCallInvite(&mtproto.TLPhoneDeclineConferenceCallInvite{MsgId: daveMsg.MessageId}); err != nil || len(b.edits) != edits {
		t.Fatalf("a repeated decline: %v, %d edits after %d", err, len(b.edits), edits)
	}
	if act := b.actionOf(a, daveMsg.DialogMessageId); !act.GetMissed() {
		t.Fatalf("a declined invitation: %v", act)
	}

	// The link is the creator's: reset by him, not by another.
	_, err = w.as(bob).PhoneToggleGroupCallSettings(&mtproto.TLPhoneToggleGroupCallSettings{Call: canon, ResetInviteHash: true})
	apiError(t, "a reset by another", err, "GROUPCALL_FORBIDDEN")
	reset, err := w.as(a).PhoneToggleGroupCallSettings(&mtproto.TLPhoneToggleGroupCallSettings{Call: canon, ResetInviteHash: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.as(dave).PhoneGetGroupCall(&mtproto.TLPhoneGetGroupCall{Call: bySlug, Limit: 1}); err == nil {
		t.Fatal("the old slug still finds the conference")
	}
	link, err := w.as(carol).PhoneExportGroupCallInvite(&mtproto.TLPhoneExportGroupCallInvite{Call: canon})
	if err != nil || link.GetLink() != callOf(t, reset).GetInviteLink().GetValue() {
		t.Fatalf("the exported link: %v %v", link, err)
	}

	// The creator kicks Bob: he does not come back by the link nor by
	// Carol's invitation; the creator's invitation lets him in.
	ch := w.chainOf(t, gc.GetId())
	if _, err = w.as(a).PhoneDeleteConferenceCallParticipants(&mtproto.TLPhoneDeleteConferenceCallParticipants{
		Kick: true, Call: canon, Ids: []int64{bob}, Block: tde2etest.RemoveBlock(ch, keyOf(a), bob),
	}); err != nil {
		t.Fatal(err)
	}
	newSlug := mtproto.MakeTLInputGroupCallSlug(&mtproto.InputGroupCall{Slug: slugOf(link.GetLink())}).To_InputGroupCall()
	_, err = w.joinConference(t, bob, gc.GetId(), newSlug, 103)
	apiError(t, "the join of the kicked", err, "GROUPCALL_FORBIDDEN")
	_, err = invite(carol, bob)
	apiError(t, "a participant's invitation of the kicked", err, "USER_WAS_KICKED")
	if _, err = invite(a, bob); err != nil {
		t.Fatal(err)
	}
	if _, err = w.joinConference(t, bob, gc.GetId(), newSlug, 104); err != nil {
		t.Fatalf("the join after the creator's invitation: %v", err)
	}

	// Only the creator ends it; Carol's invitation gets the duration, and
	// the conference stays readable as discarded.
	_, err = w.as(carol).PhoneDiscardGroupCall(&mtproto.TLPhoneDiscardGroupCall{Call: canon})
	apiError(t, "an end by another", err, "GROUPCALL_FORBIDDEN")
	if _, err = w.as(a).PhoneDiscardGroupCall(&mtproto.TLPhoneDiscardGroupCall{Call: canon}); err != nil {
		t.Fatal(err)
	}
	if act := b.actionOf(carol, carolMsg.DialogMessageId); act.GetDuration() == nil || act.GetActive() {
		t.Fatalf("the invitation after the end: %v", act)
	}
	over, err := w.as(carol).PhoneGetGroupCall(&mtproto.TLPhoneGetGroupCall{Call: byInvite, Limit: 3})
	if err != nil || over.GetCall().GetPredicateName() != mtproto.Predicate_groupCallDiscarded {
		t.Fatalf("an ended conference: %v %v", over, err)
	}
	if _, err = w.as(a).PhoneDiscardGroupCall(&mtproto.TLPhoneDiscardGroupCall{Call: canon}); err != nil {
		t.Fatalf("a repeated end of a conference: %v", err)
	}
}

// An invitation no one answers is missed by the collector, and an edit
// that failed is made again.
func TestConferenceInviteRingsOut(t *testing.T) {
	w, b := newConferenceWorld(t)
	ctx := context.Background()
	const a, bob = 31, 32
	key := tde2etest.Pub(keyOf(a))
	created, err := w.as(a).PhoneCreateConferenceCall7D0444BB(&mtproto.TLPhoneCreateConferenceCall7D0444BB{
		Join: true, RandomId: 9, PublicKey: key[:], Block: tde2etest.JoinBlock(tde2e.NewChain(), a, keyOf(a)), Params: params(200, false),
	})
	if err != nil {
		t.Fatal(err)
	}
	gc := callOf(t, created)
	if gc.GetVersion() != 2 || len(chainUpdates(created)) != 2 || len(updatesOf(created, mtproto.Predicate_updateGroupCallConnection)) != 1 {
		t.Fatalf("the answer of a create with the join: %v", created.GetUpdates())
	}
	canon := mtproto.MakeTLInputGroupCall(&mtproto.InputGroupCall{Id: gc.GetId(), AccessHash: gc.GetAccessHash()}).To_InputGroupCall()
	if _, err = w.as(a).PhoneInviteConferenceCallParticipant(&mtproto.TLPhoneInviteConferenceCallParticipant{
		Call: canon, UserId: mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: bob}).To_InputUser(),
	}); err != nil {
		t.Fatal(err)
	}
	bobMsg := b.last(bob)
	inv, err := w.svcCtx.Dao.Store.InviteOf(ctx, bobMsg.DialogMessageId, bob)
	if err != nil {
		t.Fatal(err)
	}
	// The edit fails once: the state is moved, the message is not.
	edit := w.svcCtx.Dao.MsgClient
	w.svcCtx.Dao.MsgClient = failingEdit{edit}
	c := w.as(a)
	c.moveInvite(inv, groupcalls.InviteRinging, groupcalls.InviteMissed, 0)
	if act := b.actionOf(bob, bobMsg.DialogMessageId); act.GetMissed() {
		t.Fatal("the failed edit changed the message")
	}
	w.svcCtx.Dao.MsgClient = edit
	background(ctx, w.svcCtx).sweepInvites()
	if act := b.actionOf(bob, bobMsg.DialogMessageId); !act.GetMissed() {
		t.Fatalf("the collector did not make the edit again: %v", act)
	}
}

type failingEdit struct{ msg_client.MsgClient }

func (failingEdit) MsgEditMessageV2(context.Context, *msg.TLMsgEditMessageV2) (*mtproto.Updates, error) {
	return nil, errors.New("msg is down")
}
