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
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
)

// user writes a row of users named name.
func (f *diffFixture) user(name string) int64 {
	f.t.Helper()
	id := itId()
	f.exec("INSERT INTO users (id, access_hash, phone, country_code, first_name) VALUES (?, 1, ?, 'US', ?)", id, fmt.Sprint("1999", id), name)
	return id
}

// public gives the channel a username.
func (f *diffFixture) public(channelId int64) {
	f.t.Helper()
	f.exec("INSERT INTO username (username, peer_type, peer_id, editable, active, order2) VALUES (?, ?, ?, 1, 1, 0)",
		fmt.Sprintf("m%d", channelId), mtproto.PEER_CHANNEL, channelId)
}

// link gives the channel an invite link of admin.
func (f *diffFixture) link(channelId, admin int64, expire int64, usageLimit int32) string {
	f.t.Helper()
	link := fmt.Sprintf("L%019d", itId())
	f.exec("INSERT INTO chat_invites (chat_id, admin_id, link, permanent, expire_date, usage_limit, date2) VALUES (?, ?, ?, 0, ?, ?, ?)",
		channelId, admin, link, expire, usageLimit, time.Now().Unix())
	return link
}

func (f *diffFixture) join(self, channelId int64, link string) (*mtproto.Updates, error) {
	return f.c.ChannelJoinChannel(&channel.TLChannelJoinChannel{SelfId: self, PermAuthKeyId: 1, ChannelId: channelId, Link: link})
}

func (f *diffFixture) leave(self, channelId int64) (*mtproto.Updates, error) {
	return f.c.ChannelLeaveChannel(&channel.TLChannelLeaveChannel{SelfId: self, PermAuthKeyId: 1, ChannelId: channelId})
}

func (f *diffFixture) counts(channelId int64) (participants, admins int32) {
	f.t.Helper()
	if err := f.box.QueryRow("SELECT participants_count, admins_count FROM channels WHERE id = ?", channelId).Scan(&participants, &admins); err != nil {
		f.t.Fatal(err)
	}
	return
}

func wantErr(t *testing.T, what string, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Errorf("%s: %v, want %v", what, err, want)
	}
}

// TestJoinLeave: a public supergroup is joined by itself, a private one by
// its link only; the join writes the service message, the dialog and the
// count; repeating it is USER_ALREADY_PARTICIPANT; leaving undoes it and
// the creator may not leave.
func TestJoinLeave(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob := f.user("Creator"), f.user("Bob")
	id := f.create(creator)
	f.exec("UPDATE channels SET participants_count = 1 WHERE id = ?", id)

	_, err := f.join(bob, id, "")
	wantErr(t, "a private channel without a link", err, mtproto.ErrChannelPrivate)

	f.public(id)
	r, err := f.join(bob, id, "")
	if err != nil {
		t.Fatal(err)
	}
	var msg *mtproto.Message
	for _, u := range r.GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateNewChannelMessage {
			msg = u.GetMessage_MESSAGE()
		}
	}
	if msg.GetAction().GetPredicateName() != mtproto.Predicate_messageActionChatAddUser || !msg.GetOut() || msg.GetFromId().GetUserId() != bob {
		t.Fatalf("service message %v", msg)
	}
	if len(r.GetChats()) != 1 || r.GetChats()[0].GetPredicateName() != mtproto.Predicate_channel || r.GetChats()[0].GetLeft() {
		t.Errorf("chats %v", r.GetChats())
	}
	if n, _ := f.counts(id); n != 2 {
		t.Errorf("participants_count %d, want 2", n)
	}
	var top, read int32
	if err = f.box.QueryRow("SELECT top_message, read_inbox_max_id FROM dialogs WHERE user_id = ? AND peer_type = ? AND peer_id = ? AND deleted = 0",
		bob, mtproto.PEER_CHANNEL, id).Scan(&top, &read); err != nil {
		t.Fatalf("Bob's dialog: %v", err)
	}
	if top != msg.GetId() || read != top {
		t.Errorf("dialog top %d read %d, want %d", top, read, msg.GetId())
	}
	_, err = f.join(bob, id, "")
	wantErr(t, "again", err, mtproto.ErrUserAlreadyParticipant)

	_, err = f.leave(creator, id)
	wantErr(t, "the creator leaves", err, mtproto.ErrUserCreator)
	r, err = f.leave(bob, id)
	if err != nil {
		t.Fatal(err)
	}
	if c := r.GetChats()[0]; c.GetPredicateName() != mtproto.Predicate_channel || !c.GetLeft() {
		t.Errorf("the public channel after leaving: %s left %v", c.GetPredicateName(), c.GetLeft())
	}
	if n, _ := f.counts(id); n != 1 {
		t.Errorf("participants_count %d after leaving, want 1", n)
	}
	_, err = f.leave(bob, id)
	wantErr(t, "leave again", err, mtproto.ErrUserNotParticipant)
	if d := f.diff(bob, id, channelbox.BasePts, 100); d.GetPredicateName() == "" {
		t.Error("no difference of a public channel after leaving")
	}

	// Back in; the dialog comes back.
	if _, err = f.join(bob, id, ""); err != nil {
		t.Fatalf("join again: %v", err)
	}
	var deleted bool
	if err = f.box.QueryRow("SELECT deleted FROM dialogs WHERE user_id = ? AND peer_type = ? AND peer_id = ?", bob, mtproto.PEER_CHANNEL, id).Scan(&deleted); err != nil || deleted {
		t.Errorf("dialog after joining again: deleted %v, %v", deleted, err)
	}
}

// TestJoinByLink: a link joins a private channel and counts its use; one
// expired or used up is INVITE_HASH_EXPIRED; one kicked may not join; in a
// supergroup with its history hidden the newcomer sees it from the join on;
// checkChatInvite shows the channel, or the chat to a member.
func TestJoinByLink(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob, carol, dave := f.user("Creator"), f.user("Bob"), f.user("Carol"), f.user("Dave")
	id := f.create(creator)
	f.send(id, creator, 3)
	f.exec("UPDATE channels SET hidden_prehistory = 1, participants_count = 1 WHERE id = ?", id)
	link := f.link(id, creator, 0, 1)

	inv, err := f.c.ChannelCheckChatInvite(&channel.TLChannelCheckChatInvite{SelfId: bob, Link: link})
	if err != nil {
		t.Fatal(err)
	}
	if inv.GetPredicateName() != mtproto.Predicate_chatInvite || !inv.GetChannel() || !inv.GetMegagroup() || inv.GetParticipantsCount() != 1 || inv.GetTitle() != "t" {
		t.Errorf("chatInvite %v", inv)
	}
	r, err := f.join(bob, 0, link)
	if err != nil {
		t.Fatal(err)
	}
	var joinId int32
	for _, u := range r.GetUpdates() {
		if m := u.GetMessage_MESSAGE(); m != nil {
			if m.GetAction().GetPredicateName() != mtproto.Predicate_messageActionChatJoinedByLink || m.GetAction().GetInviterId() != creator {
				t.Errorf("service message %v", m.GetAction())
			}
			joinId = m.GetId()
		}
	}
	h, _, err := f.c.svcCtx.Dao.History(f.c.ctx, bob, id, 0, 0, 0, 10, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 1 || h[0].GetId() != joinId {
		t.Errorf("Bob sees %v of the history, want only the join %d", ids(h), joinId)
	}
	if inv, err = f.c.ChannelCheckChatInvite(&channel.TLChannelCheckChatInvite{SelfId: bob, Link: link}); err != nil || inv.GetPredicateName() != mtproto.Predicate_chatInviteAlready {
		t.Errorf("checkChatInvite of a member: %v, %v", inv.GetPredicateName(), err)
	}

	_, err = f.join(carol, 0, link)
	wantErr(t, "a used up link", err, mtproto.ErrInviteHashExpired)
	_, err = f.join(carol, 0, f.link(id, creator, time.Now().Unix()-1, 0))
	wantErr(t, "an expired link", err, mtproto.ErrInviteHashExpired)
	_, err = f.join(carol, 0, "Lnone000000000000000")
	wantErr(t, "an unknown link", err, mtproto.ErrInviteHashInvalid)

	f.exec("INSERT INTO channel_participants (channel_id, user_id, state, banned_rights, kicked_by, `date`) VALUES (?, ?, ?, ?, ?, 0)",
		id, dave, channel.StateBanned, mtproto.BANNED_VIEW_MESSAGES, creator)
	_, err = f.join(dave, 0, f.link(id, creator, 0, 0))
	wantErr(t, "one kicked", err, mtproto.ErrChannelPrivate)
}

// TestJoinConcurrent: joins and leaves at once keep the count right.
func TestJoinConcurrent(t *testing.T) {
	f := newDiffFixture(t)
	creator := f.user("Creator")
	id := f.create(creator)
	f.public(id)
	f.exec("UPDATE channels SET participants_count = 1 WHERE id = ?", id)
	users := make([]int64, 12)
	for i := range users {
		users[i] = f.user(fmt.Sprintf("U%d", i))
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2*len(users))
	for _, u := range users {
		wg.Add(1)
		go func(u int64) {
			defer wg.Done()
			if _, err := f.join(u, id, ""); err != nil {
				errs <- err
				return
			}
			if u%2 == 0 {
				if _, err := f.leave(u, id); err != nil {
					errs <- err
				}
			}
		}(u)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	stay := int32(1)
	for _, u := range users {
		if u%2 != 0 {
			stay++
		}
	}
	var members int32
	if err := f.box.QueryRow("SELECT COUNT(*) FROM channel_participants WHERE channel_id = ? AND state = 0", id).Scan(&members); err != nil {
		t.Fatal(err)
	}
	if n, _ := f.counts(id); n != stay || members != stay {
		t.Errorf("participants_count %d, rows %d, want %d", n, members, stay)
	}
}

// TestParticipants: the filters of channels.getParticipants; a channel's
// list is its admins'.
func TestParticipants(t *testing.T) {
	f := newDiffFixture(t)
	creator, admin, anna, bob, kicked := f.user("Creator"), f.user("Admin"), f.user("Anna"), f.user("Bob"), f.user("Kicked")
	id := f.create(creator)
	f.exec("INSERT INTO channel_participants (channel_id, user_id, role, admin_rights, `date`) VALUES (?, ?, ?, ?, 2)", id, admin, channel.RoleAdmin, mtproto.ADMIN_INVITE_USERS)
	f.exec("INSERT INTO channel_participants (channel_id, user_id, `date`) VALUES (?, ?, 3), (?, ?, 4)", id, anna, id, bob)
	f.exec("INSERT INTO channel_participants (channel_id, user_id, state, banned_rights, `date`) VALUES (?, ?, ?, ?, 5)", id, kicked, channel.StateBanned, mtproto.BANNED_VIEW_MESSAGES)

	get := func(self int64, filter, q string) (*mtproto.Channels_ChannelParticipants, error) {
		return f.c.ChannelGetParticipants(&channel.TLChannelGetParticipants{SelfId: self, ChannelId: id, Filter: filter, Q: q, Limit: 100})
	}
	for _, c := range []struct {
		filter, q string
		want      int32
	}{
		{mtproto.Predicate_channelParticipantsRecent, "", 4},
		{mtproto.Predicate_channelParticipantsAdmins, "", 2},
		{mtproto.Predicate_channelParticipantsKicked, "", 1},
		{mtproto.Predicate_channelParticipantsBanned, "", 0},
		{mtproto.Predicate_channelParticipantsSearch, "an", 1},
		{mtproto.Predicate_channelParticipantsSearch, "%", 0},
		{mtproto.Predicate_channelParticipantsMentions, "b", 1},
	} {
		r, err := get(bob, c.filter, c.q)
		if err != nil {
			t.Fatalf("%s %q: %v", c.filter, c.q, err)
		}
		if r.GetCount() != c.want || len(r.GetParticipants()) != int(c.want) {
			t.Errorf("%s %q: count %d, %d rows, want %d", c.filter, c.q, r.GetCount(), len(r.GetParticipants()), c.want)
		}
	}
	r, _ := get(bob, mtproto.Predicate_channelParticipantsRecent, "")
	if got := r.GetParticipants(); got[0].GetPredicateName() != mtproto.Predicate_channelParticipantCreator ||
		got[1].GetPredicateName() != mtproto.Predicate_channelParticipantAdmin {
		t.Errorf("order %s, %s: the creator and admins first", got[0].GetPredicateName(), got[1].GetPredicateName())
	}
	for _, p := range r.GetParticipants() {
		if p.GetUserId() == bob && p.GetPredicateName() != mtproto.Predicate_channelParticipantSelf {
			t.Errorf("Bob's own row is %s", p.GetPredicateName())
		}
	}

	f.exec("UPDATE channels SET broadcast = 1, megagroup = 0 WHERE id = ?", id)
	_, err := get(bob, mtproto.Predicate_channelParticipantsRecent, "")
	wantErr(t, "a channel's subscriber", err, mtproto.ErrChatAdminRequired)
	if _, err = get(admin, mtproto.Predicate_channelParticipantsRecent, ""); err != nil {
		t.Errorf("a channel's admin: %v", err)
	}
	_, err = get(kicked, mtproto.Predicate_channelParticipantsRecent, "")
	wantErr(t, "one kicked", err, mtproto.ErrChannelPrivate)
}
