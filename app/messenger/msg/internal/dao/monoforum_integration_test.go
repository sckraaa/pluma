//go:build integration

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

package dao_test

import (
	"context"
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"google.golang.org/grpc/status"
)

// Messages of the direct messages of a channel (task 084, ADR 0055), on
// MySQL.

// newMonoforum switches the direct messages of the broadcast channel on,
// as biz channel does: its monoforum, linked both ways.
func (w *world) newMonoforum(creator, channelId int64) int64 {
	w.t.Helper()
	id := newChat()
	if _, err := w.d.RunBox(context.Background(), creator, nil, func(b *userbox.Box) error {
		_, err := channelbox.Create(b, channelbox.NewChannel{Id: id, AccessHash: id * 3, CreatorUserId: creator, Title: "t", Megagroup: true, Date: b.Date()})
		return err
	}); err != nil {
		w.t.Fatal(err)
	}
	w.exec("UPDATE channels SET monoforum = 1, linked_monoforum_id = ? WHERE id = ?", channelId, id)
	w.exec("UPDATE channels SET broadcast_messages_allowed = 1, linked_monoforum_id = ? WHERE id = ?", id, channelId)
	return id
}

func (w *world) adminOf(channelId, userId int64, rights int32) {
	w.t.Helper()
	w.exec("INSERT INTO channel_participants (channel_id, user_id, role, admin_rights, `date`) VALUES (?, ?, ?, ?, 1)", channelId, userId, channelperm.RoleAdmin, rights)
}

// toSublist is a text of from for the sublist peer of the monoforum (0:
// none named), as bff carries reply_to.monoforum_peer_id.
func toSublist(from, monoforumId, peer int64) dao.Outgoing {
	o := channelText(from, monoforumId, 0)
	if peer != 0 {
		o.Message.SavedPeerId = mtproto.MakePeerUser(peer)
	}
	return o
}

type sublistRow struct {
	top, inbox, outbox int32
}

func (w *world) sublist(monoforumId, peer int64) (r sublistRow, ok bool) {
	w.t.Helper()
	err := w.check.QueryRow("SELECT top_message, read_inbox_max_id, read_outbox_max_id FROM channel_monoforum_sublists WHERE channel_id = ? AND peer_id = ?",
		monoforumId, peer).Scan(&r.top, &r.inbox, &r.outbox)
	if err != nil {
		return sublistRow{}, false
	}
	return r, true
}

func (w *world) sublistOf(monoforumId int64, id int32) (peer, sendAs int64) {
	w.t.Helper()
	if err := w.check.QueryRow("SELECT saved_peer_id, send_as_id FROM channel_messages WHERE channel_id = ? AND id = ?", monoforumId, id).Scan(&peer, &sendAs); err != nil {
		w.t.Fatal(err)
	}
	return peer, sendAs
}

// shownTo counts the events that bring the monoforum into the dialogs of
// userId.
func (w *world) shownTo(monoforumId, userId int64) int {
	w.t.Helper()
	var n int
	if err := w.check.QueryRow("SELECT COUNT(*) FROM mq_outbox WHERE recipient_id = ? AND method = 'inbox.TL_inbox_channelInvitedToInbox' AND payload LIKE ?",
		userId, "%"+itoa(monoforumId)+"%").Scan(&n); err != nil {
		w.t.Fatal(err)
	}
	return n
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for ; v > 0; v /= 10 {
		b = append([]byte{byte('0' + v%10)}, b...)
	}
	return string(b)
}

func wantStatus(t *testing.T, what string, err error, message string) {
	t.Helper()
	if s, ok := status.FromError(err); !ok || err == nil || s.Message() != message {
		t.Errorf("%s: %v, want %s", what, err, message)
	}
}

// TestMonoforumSend: a subscriber writes to its own sublist as itself, an
// admin of the direct messages to the sublist it names, as the channel;
// the sublist keeps its last message and who read up to where, and those
// who read it and have no dialog with the monoforum are shown it.
func TestMonoforumSend(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	creator, manager, poster, alice, bob, outsider := newUser(), newUser(), newUser(), newUser(), newUser(), newUser()
	id := w.newChannelRow(creator, true)
	w.adminOf(id, manager, mtproto.ADMIN_MANAGE_DIRECT_MESSAGES)
	w.adminOf(id, poster, mtproto.ADMIN_POST_MESSAGES)
	w.member(id, alice, channelperm.RoleMember)
	w.member(id, bob, channelperm.RoleMember)
	mono := w.newMonoforum(creator, id)

	_, err := w.sendOutgoing(outsider, mono, toSublist(outsider, mono, 0))
	if !errors.Is(err, mtproto.ErrChannelPrivate) {
		t.Errorf("one out of the private channel writes to its monoforum: %v", err)
	}
	_, err = w.sendOutgoing(creator, mono, toSublist(creator, mono, 0))
	wantStatus(t, "an admin names no sublist", err, "REPLY_TO_MONOFORUM_PEER_INVALID")
	_, err = w.sendOutgoing(creator, mono, toSublist(creator, mono, alice))
	wantStatus(t, "an admin writes to one who never wrote", err, "REPLY_TO_MONOFORUM_PEER_INVALID")

	// Alice writes; she names another's sublist in vain.
	sent, err := w.sendOutgoing(alice, mono, toSublist(alice, mono, bob))
	if err != nil {
		t.Fatal(err)
	}
	a1 := sent[0].Message
	if a1.GetFromId().GetUserId() != alice || a1.GetSavedPeerId().GetUserId() != alice || a1.GetPeerId().GetChannelId() != mono {
		t.Errorf("alice's message: %v", a1)
	}
	if peer, as := w.sublistOf(mono, a1.Id); peer != alice || as != 0 {
		t.Errorf("alice's row: sublist %d, send_as %d", peer, as)
	}
	if r, ok := w.sublist(mono, alice); !ok || r.top != a1.Id || r.outbox != a1.Id || r.inbox != 0 {
		t.Errorf("alice's sublist: %+v, %v", r, ok)
	}
	if _, ok := w.sublist(mono, bob); ok {
		t.Error("bob has a sublist he never wrote to")
	}
	for name, c := range map[string]struct {
		user int64
		want int
	}{"the creator": {creator, 1}, "the manager": {manager, 1}, "an admin without the right": {poster, 0}, "another subscriber": {bob, 0}} {
		if got := w.shownTo(mono, c.user); got != c.want {
			t.Errorf("%s is shown the monoforum %d times, want %d", name, got, c.want)
		}
	}

	// The manager answers, as the channel.
	if sent, err = w.sendOutgoing(manager, mono, toSublist(manager, mono, alice)); err != nil {
		t.Fatal(err)
	}
	m1 := sent[0].Message
	if m1.GetFromId().GetChannelId() != id || m1.GetFromId().GetUserId() != 0 || m1.GetSavedPeerId().GetUserId() != alice || m1.GetPostAuthor() != nil {
		t.Errorf("the channel's answer: %v", m1)
	}
	if peer, as := w.sublistOf(mono, m1.Id); peer != alice || as != id {
		t.Errorf("the answer's row: sublist %d, send_as %d", peer, as)
	}
	if r, _ := w.sublist(mono, alice); r.top != m1.Id || r.inbox != m1.Id || r.outbox != a1.Id {
		t.Errorf("alice's sublist after the answer: %+v", r)
	}

	// A reply names the sublist by its message; a reply across sublists
	// does not stay a reply.
	if sent, err = w.sendOutgoing(bob, mono, toSublist(bob, mono, 0)); err != nil {
		t.Fatal(err)
	}
	b1 := sent[0].Message
	reply := toSublist(creator, mono, 0)
	reply.Message.ReplyTo = replyTo(b1.Id)
	if sent, err = w.sendOutgoing(creator, mono, reply); err != nil {
		t.Fatal(err)
	}
	if m := sent[0].Message; m.GetSavedPeerId().GetUserId() != bob || m.GetReplyTo().GetReplyToMsgId() != b1.Id || m.GetReplyTo().GetReplyToTopId() != nil {
		t.Errorf("a reply to bob's message: %v", m)
	}
	across := toSublist(alice, mono, 0)
	across.Message.ReplyTo = replyTo(b1.Id)
	if sent, err = w.sendOutgoing(alice, mono, across); err != nil {
		t.Fatal(err)
	}
	if m := sent[0].Message; m.GetSavedPeerId().GetUserId() != alice || m.GetReplyTo() != nil {
		t.Errorf("alice replies to bob's message: %v", m)
	}

	// An admin of the channel without the right is a subscriber here.
	if sent, err = w.sendOutgoing(poster, mono, toSublist(poster, mono, alice)); err != nil {
		t.Fatal(err)
	}
	if m := sent[0].Message; m.GetSavedPeerId().GetUserId() != poster || m.GetFromId().GetUserId() != poster {
		t.Errorf("an admin without the right writes: %v", m)
	}

	poll := toSublist(alice, mono, 0)
	poll.Message.Media = mtproto.MakeTLMessageMediaPoll(&mtproto.MessageMedia{Poll: mtproto.MakeTLPoll(&mtproto.Poll{Id: 1}).To_Poll()}).To_MessageMedia()
	_, err = w.sendOutgoing(alice, mono, poll)
	wantStatus(t, "a poll in a monoforum", err, "CHANNEL_MONOFORUM_UNSUPPORTED")
	suggested := toSublist(alice, mono, 0)
	suggested.Message.SuggestedPost = mtproto.MakeTLSuggestedPost(&mtproto.SuggestedPost{}).To_SuggestedPost()
	if _, err = w.sendOutgoing(alice, mono, suggested); !errors.Is(err, mtproto.ErrEnterpriseIsBlocked) {
		t.Errorf("a suggested post: %v, want ERR_ENTERPRISE_IS_BLOCKED", err)
	}

	// Outside a monoforum a message is in no sublist, and no post is
	// suggested.
	group := w.newChannelOf(creator)
	if sent, err = w.sendOutgoing(creator, group, toSublist(creator, group, alice)); err != nil || sent[0].Message.GetSavedPeerId() != nil {
		t.Errorf("a sublist named in a supergroup: %v, %v", sent, err)
	}
	suggested = toSublist(creator, group, 0)
	suggested.Message.SuggestedPost = mtproto.MakeTLSuggestedPost(&mtproto.SuggestedPost{}).To_SuggestedPost()
	_, err = w.sendOutgoing(creator, group, suggested)
	wantStatus(t, "a post suggested to a supergroup", err, "SUGGESTED_POST_PEER_INVALID")

	// Switched off, nobody writes.
	w.exec("UPDATE channels SET broadcast_messages_allowed = 0 WHERE id = ?", id)
	if _, err = w.sendOutgoing(alice, mono, toSublist(alice, mono, 0)); !errors.Is(err, mtproto.ErrChatWriteForbidden) {
		t.Errorf("a message to direct messages switched off: %v", err)
	}
	if _, err = w.sendOutgoing(creator, mono, toSublist(creator, mono, alice)); !errors.Is(err, mtproto.ErrChatWriteForbidden) {
		t.Errorf("an answer with direct messages switched off: %v", err)
	}
}

func replyTo(id int32) *mtproto.MessageReplyHeader {
	return mtproto.MakeTLMessageReplyHeader(&mtproto.MessageReplyHeader{
		ReplyToMsgId:           id,
		ReplyToMsgId_INT32:     id,
		ReplyToMsgId_FLAGINT32: mtproto.MakeFlagsInt32(id),
	}).To_MessageReplyHeader()
}

// TestMonoforumReactionsAndPins: a subscriber reacts to the messages of
// its own sublist alone; the admins of the direct messages pin, and the
// service message of a pin is the channel's, in the sublist of the message.
func TestMonoforumReactionsAndPins(t *testing.T) {
	w := newWorld(t, boxDSN(""), 0)
	creator, alice, bob := newUser(), newUser(), newUser()
	id := w.newChannelRow(creator, true)
	w.member(id, alice, channelperm.RoleMember)
	w.member(id, bob, channelperm.RoleMember)
	mono := w.newMonoforum(creator, id)
	sent, err := w.sendOutgoing(alice, mono, toSublist(alice, mono, 0))
	if err != nil {
		t.Fatal(err)
	}
	a1 := sent[0].Message.Id
	if sent, err = w.sendOutgoing(bob, mono, toSublist(bob, mono, 0)); err != nil {
		t.Fatal(err)
	}
	b1 := sent[0].Message.Id

	var reacted *dao.ChannelReactions
	react := func(user int64, msgId int32) error {
		_, err := w.d.RunBox(context.Background(), user, nil, func(b *userbox.Box) error {
			var err error
			reacted, err = w.d.SetChannelReactions(b, mono, msgId, []string{"👍"}, false)
			return err
		})
		return err
	}
	if err = react(alice, b1); !errors.Is(err, mtproto.ErrMessageIdInvalid) {
		t.Errorf("alice reacts to bob's message: %v", err)
	}
	if err = react(alice, a1); err != nil {
		t.Errorf("alice reacts to her own message: %v", err)
	}
	if err = react(creator, b1); err != nil {
		t.Errorf("the creator reacts to bob's message: %v", err)
	}
	// The update names the sublist of the message (Android counts an
	// admin's unread reactions by it).
	if u := dao.ChannelReactionsUpdate(0, mono, b1, reacted); u.GetSavedPeerId().GetUserId() != bob {
		t.Errorf("the reactions update of bob's message: %v", u)
	}

	pin := func(user int64, msgId int32) (*dao.ChannelPinned, error) {
		var out *dao.ChannelPinned
		_, err := w.d.RunBox(context.Background(), user, nil, func(b *userbox.Box) error {
			var err error
			out, err = w.d.PinChannel(b, mono, msgId, false, false, randomId())
			return err
		})
		return out, err
	}
	if _, err = pin(alice, a1); !errors.Is(err, mtproto.ErrChatAdminRequired) {
		t.Errorf("a subscriber pins: %v", err)
	}
	pinned, err := pin(creator, b1)
	if err != nil || pinned.Service == nil {
		t.Fatalf("the creator pins bob's message: %v, %v", pinned, err)
	}
	service := pinned.Service.Message
	if service.GetSavedPeerId().GetUserId() != bob || service.GetFromId().GetChannelId() != id {
		t.Errorf("the service message of the pin: %v", service)
	}
	if peer, as := w.sublistOf(mono, service.Id); peer != bob || as != id {
		t.Errorf("the service message's row: sublist %d, send_as %d", peer, as)
	}
	if r, _ := w.sublist(mono, bob); r.top != b1 {
		t.Errorf("the last message of bob's sublist after a pin: %+v, want %d", r, b1)
	}
}
