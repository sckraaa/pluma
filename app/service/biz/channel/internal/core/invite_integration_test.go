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

package core

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
)

func (f *diffFixture) invite(self, channelId int64, ids ...int64) (*mtproto.Updates, error) {
	return f.c.ChannelInviteToChannel(&channel.TLChannelInviteToChannel{SelfId: self, PermAuthKeyId: 1, ChannelId: channelId, Id: ids})
}

// invitedEvents counts the inbox events that tell recipient it was added.
func (f *diffFixture) invitedEvents(recipient int64) int {
	f.t.Helper()
	var n int
	if err := f.box.QueryRow("SELECT COUNT(*) FROM mq_outbox WHERE recipient_id = ? AND topic = ? AND method = ?",
		recipient, itInboxTopic, "inbox.TL_inbox_channelInvitedToInbox").Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// TestInvite: the creator adds users, each once, with one service message
// naming them and an inbox event each; a plain member adds only while the
// supergroup lets members invite; one kicked comes back only by an admin
// who may ban; nobody added writes nothing.
func TestInvite(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob, carol, dave, kicked := f.user("Creator"), f.user("Bob"), f.user("Carol"), f.user("Dave"), f.user("Kicked")
	id := f.create(creator)
	f.exec("UPDATE channels SET participants_count = 1 WHERE id = ?", id)

	r, err := f.invite(creator, id, bob, carol, bob, creator)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.GetUpdates()) != 1 {
		t.Fatalf("answer %v", r.GetUpdates())
	}
	action := r.GetUpdates()[0].GetMessage_MESSAGE().GetAction()
	if action.GetPredicateName() != mtproto.Predicate_messageActionChatAddUser || len(action.GetUsers()) != 2 {
		t.Errorf("service message %v", action)
	}
	if n, _ := f.counts(id); n != 3 {
		t.Errorf("participants_count %d, want 3", n)
	}
	if f.invitedEvents(bob) != 1 || f.invitedEvents(carol) != 1 || f.invitedEvents(creator) != 0 {
		t.Errorf("inbox events: Bob %d, Carol %d, the creator %d", f.invitedEvents(bob), f.invitedEvents(carol), f.invitedEvents(creator))
	}
	if r, err = f.invite(creator, id, bob); err != nil || len(r.GetUpdates()) != 0 || f.invitedEvents(bob) != 1 {
		t.Errorf("adding a member again: %v, %v", r.GetUpdates(), err)
	}

	f.exec("UPDATE channels SET default_banned_rights = ? WHERE id = ?", mtproto.BANNED_INVITE_USERS, id)
	_, err = f.invite(bob, id, dave)
	wantErr(t, "a member where members may not invite", err, mtproto.ErrChatAdminRequired)
	f.exec("UPDATE channels SET default_banned_rights = 0 WHERE id = ?", id)
	if _, err = f.invite(bob, id, dave); err != nil {
		t.Errorf("a member where members may invite: %v", err)
	}

	f.exec("INSERT INTO channel_participants (channel_id, user_id, state, banned_rights, `date`) VALUES (?, ?, ?, ?, 0)",
		id, kicked, channel.StateKicked, mtproto.BANNED_VIEW_MESSAGES)
	_, err = f.invite(bob, id, kicked)
	wantErr(t, "one kicked, by a member", err, mtproto.ErrUserKicked)
	if _, err = f.invite(creator, id, kicked); err != nil {
		t.Fatalf("one kicked, by the creator: %v", err)
	}
	var state int32
	if err = f.box.QueryRow("SELECT state FROM channel_participants WHERE channel_id = ? AND user_id = ?", id, kicked).Scan(&state); err != nil || state != channel.StateIn {
		t.Errorf("the kicked one is back: state %d, %v", state, err)
	}

	f.exec("UPDATE channels SET broadcast = 1, megagroup = 0 WHERE id = ?", id)
	_, err = f.invite(bob, id, f.user("Eve"))
	wantErr(t, "a channel's subscriber", err, mtproto.ErrChatAdminRequired)
}
