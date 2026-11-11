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
	"context"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/dao"
)

func (f *diffFixture) futureCreator(self, channelId int64) (int64, error) {
	r, err := f.c.ChannelGetFutureCreator(&channel.TLChannelGetFutureCreator{SelfId: self, ChannelId: channelId})
	return r.GetV(), err
}

// transfer reads the waiting transfer of the channel: its next owner and
// when it is due; 0 for none.
func (f *diffFixture) transfer(channelId int64) (to, due int64) {
	f.t.Helper()
	rows, err := f.box.Query("SELECT to_user_id, due_at FROM channel_creator_transfers WHERE channel_id = ?", channelId)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		if err = rows.Scan(&to, &due); err != nil {
			f.t.Fatal(err)
		}
	}
	if err = rows.Err(); err != nil {
		f.t.Fatal(err)
	}
	return
}

func (f *diffFixture) owner(channelId int64) int64 {
	f.t.Helper()
	var owner int64
	if err := f.box.QueryRow("SELECT creator_user_id FROM channels WHERE id = ?", channelId).Scan(&owner); err != nil {
		f.t.Fatal(err)
	}
	return owner
}

// TestFutureCreator: the owner's next owner is an admin before a member,
// the longest in the channel first; never a bot, a deleted account, a
// restricted member or one who left; only the owner asks.
func TestFutureCreator(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob, carol, dave, bot, gone := f.user("Creator"), f.user("Bob"), f.user("Carol"), f.user("Dave"), f.user("Bot"), f.user("Gone")
	id := f.create(creator)
	f.exec("UPDATE channels SET admins_count = 1 WHERE id = ?", id)
	f.public(id)
	for _, u := range []int64{bot, gone, bob, carol, dave} {
		if _, err := f.join(u, id, ""); err != nil {
			t.Fatal(err)
		}
	}
	f.exec("UPDATE users SET is_bot = 1 WHERE id = ?", bot)
	f.exec("UPDATE users SET deleted = 1 WHERE id = ?", gone)
	// Bob joined first, then Carol, then Dave.
	now := time.Now().Unix()
	f.exec("UPDATE channel_participants SET `date` = ? WHERE channel_id = ? AND user_id = ?", now-300, id, bob)
	f.exec("UPDATE channel_participants SET `date` = ? WHERE channel_id = ? AND user_id = ?", now-200, id, carol)
	f.exec("UPDATE channel_participants SET `date` = ? WHERE channel_id = ? AND user_id = ?", now-100, id, dave)
	f.exec("UPDATE channel_participants SET `date` = ? WHERE channel_id = ? AND user_id IN (?, ?)", now-1000, id, bot, gone)

	if next, err := f.futureCreator(creator, id); err != nil || next != bob {
		t.Errorf("the oldest member: %d, %v, want %d", next, err, bob)
	}
	if _, err := f.editAdmin(creator, id, dave, mtproto.ADMIN_PIN_MESSAGES, ""); err != nil {
		t.Fatal(err)
	}
	if next, err := f.futureCreator(creator, id); err != nil || next != dave {
		t.Errorf("an admin first: %d, %v, want %d", next, err, dave)
	}
	if _, err := f.editBanned(creator, id, dave, mtproto.BANNED_SEND_MESSAGES, 0); err != nil {
		t.Fatal(err)
	}
	if next, err := f.futureCreator(creator, id); err != nil || next != bob {
		t.Errorf("a restricted member is not: %d, %v, want %d", next, err, bob)
	}
	if _, err := f.leave(bob, id); err != nil {
		t.Fatal(err)
	}
	if next, err := f.futureCreator(creator, id); err != nil || next != carol {
		t.Errorf("one who left is not: %d, %v, want %d", next, err, carol)
	}
	_, err := f.futureCreator(carol, id)
	wantErr(t, "a member asks", err, mtproto.ErrChatAdminRequired)
	_, err = f.futureCreator(creator, itId())
	wantErr(t, "no channel", err, mtproto.ErrChannelInvalid)
}

// TestCreatorLeaves: the owner of a supergroup leaves; its next owner is
// written for seven days later and the members are told by
// messageActionNewCreatorPending; the owner who comes back keeps the
// channel; when it stays away the transfer makes the next owner the
// creator with messageActionChangeCreator, and the old one an ordinary
// left row.
func TestCreatorLeaves(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob, carol := f.user("Creator"), f.user("Bob"), f.user("Carol")
	id := f.create(creator)
	f.exec("UPDATE channels SET participants_count = 1, admins_count = 1 WHERE id = ?", id)
	f.public(id)
	for _, u := range []int64{bob, carol} {
		if _, err := f.join(u, id, ""); err != nil {
			t.Fatal(err)
		}
	}
	before := time.Now().Unix()
	if _, err := f.leave(creator, id); err != nil {
		t.Fatalf("the owner leaves: %v", err)
	}
	if role, state, _, _, _ := f.row(id, creator); role != channel.RoleCreator || state != channel.StateLeft {
		t.Errorf("the owner's row: role %d state %d", role, state)
	}
	if n, admins := f.counts(id); n != 2 || admins != 0 {
		t.Errorf("participants %d admins %d, want 2 and 0", n, admins)
	}
	to, due := f.transfer(id)
	delay := int64(dao.CreatorTransferDelay / time.Second)
	if to != bob || due < before+delay || due > time.Now().Unix()+delay {
		t.Errorf("the transfer: to %d due %d", to, due)
	}
	pending := f.stored(id, f.top(id))
	if pending.GetAction().GetPredicateName() != mtproto.Predicate_messageActionNewCreatorPending ||
		pending.GetAction().GetNewCreatorId() != bob || pending.GetFromId().GetUserId() != creator {
		t.Errorf("the service message: %v", pending)
	}
	if f.owner(id) != creator {
		t.Errorf("the channel changed owner at once")
	}

	// Coming back keeps the channel.
	if _, err := f.join(creator, id, ""); err != nil {
		t.Fatalf("the owner comes back: %v", err)
	}
	if role, state, _, _, _ := f.row(id, creator); role != channel.RoleCreator || state != channel.StateIn {
		t.Errorf("the owner back: role %d state %d", role, state)
	}
	if to, _ := f.transfer(id); to != 0 {
		t.Errorf("the transfer stays after the owner came back: %d", to)
	}
	if _, admins := f.counts(id); admins != 1 {
		t.Errorf("admins %d after the owner came back, want 1", admins)
	}

	// Away for good: due now, Bob gone, Carol takes it.
	if _, err := f.leave(creator, id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.leave(bob, id); err != nil {
		t.Fatal(err)
	}
	if n, err := f.c.svcCtx.Dao.TransferDueCreators(context.Background(), time.Now().Unix()); err != nil || n != 0 {
		t.Errorf("a transfer before it is due: %d, %v", n, err)
	}
	f.exec("UPDATE channel_creator_transfers SET due_at = ? WHERE channel_id = ?", time.Now().Unix()-1, id)
	if n, err := f.c.svcCtx.Dao.TransferDueCreators(context.Background(), time.Now().Unix()); err != nil || n != 1 {
		t.Fatalf("the transfers due: %d, %v", n, err)
	}
	if f.owner(id) != carol {
		t.Errorf("owner %d, want Carol %d", f.owner(id), carol)
	}
	if role, state, _, _, _ := f.row(id, carol); role != channel.RoleCreator || state != channel.StateIn {
		t.Errorf("Carol: role %d state %d", role, state)
	}
	if role, _, _, _, _ := f.row(id, creator); role != channel.RoleMember {
		t.Errorf("the old owner: role %d", role)
	}
	if _, admins := f.counts(id); admins != 1 {
		t.Errorf("admins %d, want 1", admins)
	}
	changed := f.stored(id, f.top(id))
	if changed.GetAction().GetPredicateName() != mtproto.Predicate_messageActionChangeCreator ||
		changed.GetAction().GetNewCreatorId() != carol || changed.GetFromId().GetUserId() != creator {
		t.Errorf("the service message: %v", changed)
	}
	if to, _ := f.transfer(id); to != 0 {
		t.Errorf("the transfer stays after it was done: %d", to)
	}
	// The old owner comes back as anyone does.
	if _, err := f.join(creator, id, ""); err != nil {
		t.Fatal(err)
	}
	if role, _, _, _, _ := f.row(id, creator); role != channel.RoleMember {
		t.Errorf("the old owner back: role %d", role)
	}
}

// TestCreatorLeavesAlone: the owner alone in a private channel leaves with
// nobody to take it; it still sees the channel, left, and comes back to it
// without a link, as its owner.
func TestCreatorLeavesAlone(t *testing.T) {
	f := newDiffFixture(t)
	creator := f.user("Creator")
	id := f.create(creator)
	f.exec("UPDATE channels SET participants_count = 1, admins_count = 1, broadcast = 1, megagroup = 0 WHERE id = ?", id)
	top := f.top(id)
	r, err := f.leave(creator, id)
	if err != nil {
		t.Fatalf("the owner leaves: %v", err)
	}
	if c := r.GetChats()[0]; c.GetPredicateName() != mtproto.Predicate_channel || !c.GetLeft() {
		t.Errorf("the channel to its owner who left: %s left %v", c.GetPredicateName(), c.GetLeft())
	}
	if to, _ := f.transfer(id); to != 0 {
		t.Errorf("a transfer with nobody to take it: %d", to)
	}
	if f.top(id) != top {
		t.Errorf("a channel told of its owner's leave")
	}
	if _, err = f.join(creator, id, ""); err != nil {
		t.Fatalf("the owner comes back to its private channel: %v", err)
	}
	if role, state, _, _, _ := f.row(id, creator); role != channel.RoleCreator || state != channel.StateIn {
		t.Errorf("the owner back: role %d state %d", role, state)
	}
}

// TestCreatorAddedBack: the owner who left and is added back by an admin
// is the owner again, as when it comes back by itself: its transfer goes
// and it counts among the admins. While away it may not change the
// channel's usernames.
func TestCreatorAddedBack(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob, carol := f.user("Creator"), f.user("Bob"), f.user("Carol")
	id := f.create(creator)
	f.exec("UPDATE channels SET participants_count = 1, admins_count = 1 WHERE id = ?", id)
	f.public(id)
	for _, u := range []int64{bob, carol} {
		if _, err := f.join(u, id, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.editAdmin(creator, id, bob, mtproto.ADMIN_INVITE_USERS, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.leave(creator, id); err != nil {
		t.Fatal(err)
	}
	if to, _ := f.transfer(id); to != bob {
		t.Fatalf("the transfer to %d, want the admin %d", to, bob)
	}
	err := f.editUsernames(creator, id, channel.UsernamesDeactivateAll, "", false)
	wantErr(t, "the owner away edits the usernames", err, mtproto.ErrChatAdminRequired)

	if _, err = f.invite(bob, id, creator); err != nil {
		t.Fatalf("the admin adds the owner back: %v", err)
	}
	if role, state, _, _, _ := f.row(id, creator); role != channel.RoleCreator || state != channel.StateIn {
		t.Errorf("the owner added back: role %d state %d", role, state)
	}
	if to, _ := f.transfer(id); to != 0 {
		t.Errorf("the transfer stays after the owner was added back: %d", to)
	}
	if n, admins := f.counts(id); n != 3 || admins != 2 {
		t.Errorf("participants %d admins %d, want 3 and 2", n, admins)
	}
	if f.owner(id) != creator {
		t.Errorf("owner %d, want %d", f.owner(id), creator)
	}
	if err = f.editUsernames(creator, id, channel.UsernamesDeactivateAll, "", false); err != nil {
		t.Errorf("the owner back edits the usernames: %v", err)
	}
}

// TestInactiveChannels: the user's channels, the least active first, each
// dated by its last message or join; deleted and left ones are out.
func TestInactiveChannels(t *testing.T) {
	f := newDiffFixture(t)
	owner, bob := f.user("Owner"), f.user("Bob")
	quiet, busy, left, deleted := f.create(owner), f.create(owner), f.create(owner), f.create(owner)
	for _, id := range []int64{quiet, busy, left, deleted} {
		f.public(id)
		if _, err := f.join(bob, id, ""); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Unix() - 90*86400
	f.exec("UPDATE channel_participants SET `date` = ? WHERE user_id = ?", old, bob)
	f.exec("UPDATE channel_messages SET date2 = ? WHERE sender_user_id = ?", old, bob)
	f.send(busy, bob, 1)
	if _, err := f.leave(bob, left); err != nil {
		t.Fatal(err)
	}
	f.exec("UPDATE channels SET deleted = 1 WHERE id = ?", deleted)

	r, err := f.c.ChannelGetInactiveChannels(&channel.TLChannelGetInactiveChannels{SelfId: bob})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.GetChats()) != 2 || len(r.GetDates()) != 2 {
		t.Fatalf("chats %d dates %d, want 2", len(r.GetChats()), len(r.GetDates()))
	}
	if r.GetChats()[0].GetId() != quiet || int64(r.GetDates()[0]) != old {
		t.Errorf("first %d at %d, want %d at %d", r.GetChats()[0].GetId(), r.GetDates()[0], quiet, old)
	}
	if r.GetChats()[1].GetId() != busy || int64(r.GetDates()[1]) <= old {
		t.Errorf("second %d at %d, want %d after %d", r.GetChats()[1].GetId(), r.GetDates()[1], busy, old)
	}
}

// TestSetMainTab: one who may change the info of a channel sets the main
// tab, which the channel then has; a subscriber may not; an unknown tab is
// refused.
func TestSetMainTab(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob := f.user("Creator"), f.user("Bob")
	id := f.create(creator)
	f.exec("UPDATE channels SET broadcast = 1, megagroup = 0 WHERE id = ?", id)
	f.public(id)
	if _, err := f.join(bob, id, ""); err != nil {
		t.Fatal(err)
	}
	set := func(self int64, tab *mtproto.ProfileTab) error {
		_, err := f.c.ChannelSetMainProfileTab(&channel.TLChannelSetMainProfileTab{SelfId: self, PermAuthKeyId: 1, ChannelId: id, Tab: tab})
		return err
	}
	if err := set(creator, mtproto.MakeTLProfileTabFiles(nil).To_ProfileTab()); err != nil {
		t.Fatal(err)
	}
	mc, err := f.c.svcCtx.Dao.GetMutableChannel(context.Background(), id, nil)
	if err != nil || mc.GetChannel().GetMainTab() != mtproto.ProfileTabTypeFiles {
		t.Errorf("main tab %d, %v", mc.GetChannel().GetMainTab(), err)
	}
	if err = set(creator, mtproto.MakeTLProfileTabFiles(nil).To_ProfileTab()); err != nil {
		t.Errorf("the same tab again: %v", err)
	}
	wantErr(t, "a subscriber", set(bob, mtproto.MakeTLProfileTabMedia(nil).To_ProfileTab()), mtproto.ErrChatAdminRequired)
	wantErr(t, "no tab", set(creator, nil), mtproto.ErrInputConstructorInvalid)
}
