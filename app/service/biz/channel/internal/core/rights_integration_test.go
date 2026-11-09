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
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
)

func (f *diffFixture) editAdmin(self, channelId, userId int64, rights int32, rank string) (*mtproto.Updates, error) {
	return f.c.ChannelEditAdmin(&channel.TLChannelEditAdmin{SelfId: self, PermAuthKeyId: 1, ChannelId: channelId, UserId: userId,
		AdminRights: rights, Rank: rank, KeepRank: mtproto.BoolFalse})
}

func (f *diffFixture) editBanned(self, channelId, userId int64, rights, until int32) (*mtproto.Updates, error) {
	return f.c.ChannelEditBanned(&channel.TLChannelEditBanned{SelfId: self, PermAuthKeyId: 1, ChannelId: channelId, UserId: userId,
		BannedRights: rights, UntilDate: until})
}

// row is a participant's row: role, state, admin_rights, banned_rights,
// until_date.
func (f *diffFixture) row(channelId, userId int64) (role, state, admin, banned, until int32) {
	f.t.Helper()
	if err := f.box.QueryRow("SELECT role, state, admin_rights, banned_rights, until_date FROM channel_participants WHERE channel_id = ? AND user_id = ?",
		channelId, userId).Scan(&role, &state, &admin, &banned, &until); err != nil {
		f.t.Fatal(err)
	}
	return
}

func (f *diffFixture) kickedEvents(recipient int64) int {
	f.t.Helper()
	var n int
	if err := f.box.QueryRow("SELECT COUNT(*) FROM mq_outbox WHERE recipient_id = ? AND topic = ? AND method = ?",
		recipient, itInboxTopic, "inbox.TL_inbox_channelKickedToInbox").Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// TestEditAdmin: the creator promotes a member with any rights and a rank;
// that admin promotes others with at most its own rights and edits only
// those it promoted; nobody edits the creator, who sets only its own
// anonymity; a user out of the channel is not promoted; demotion counts
// the admin out.
func TestEditAdmin(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob, carol, dave, eve := f.user("Creator"), f.user("Bob"), f.user("Carol"), f.user("Dave"), f.user("Eve")
	id := f.create(creator)
	// The creator counts among the admins, as channel.createChannel writes it.
	f.exec("UPDATE channels SET admins_count = 1 WHERE id = ?", id)
	f.public(id)
	for _, u := range []int64{bob, carol, dave} {
		if _, err := f.join(u, id, ""); err != nil {
			t.Fatal(err)
		}
	}
	bobRights := mtproto.ADMIN_ADD_ADMINS | mtproto.ADMIN_PIN_MESSAGES
	r, err := f.editAdmin(creator, id, bob, bobRights, "mod")
	if err != nil {
		t.Fatal(err)
	}
	if u := r.GetUpdates(); len(u) != 1 || u[0].GetPredicateName() != mtproto.Predicate_updateChannel {
		t.Errorf("answer %v", u)
	}
	if role, _, admin, _, _ := f.row(id, bob); role != channel.RoleAdmin || admin != bobRights {
		t.Errorf("bob: role %d rights %b", role, admin)
	}
	// The creator is one of them.
	if _, admins := f.counts(id); admins != 2 {
		t.Errorf("admins %d", admins)
	}

	_, err = f.editAdmin(bob, id, carol, mtproto.ADMIN_BAN_USERS, "")
	wantErr(t, "more than its own", err, mtproto.ErrRightForbidden)
	if _, err = f.editAdmin(bob, id, carol, mtproto.ADMIN_PIN_MESSAGES, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = f.editAdmin(creator, id, dave, mtproto.ADMIN_PIN_MESSAGES, ""); err != nil {
		t.Fatal(err)
	}
	_, err = f.editAdmin(bob, id, dave, 0, "")
	wantErr(t, "an admin another promoted", err, mtproto.ErrChatAdminRequired)
	_, err = f.editAdmin(bob, id, creator, mtproto.ADMIN_PIN_MESSAGES, "")
	wantErr(t, "the creator", err, mtproto.ErrUserCreator)
	_, err = f.editAdmin(carol, id, eve, mtproto.ADMIN_PIN_MESSAGES, "")
	wantErr(t, "without add_admins", err, mtproto.ErrChatAdminRequired)
	_, err = f.editAdmin(creator, id, eve, mtproto.ADMIN_PIN_MESSAGES, "")
	wantErr(t, "out of the channel", err, mtproto.ErrUserNotParticipant)
	_, err = f.editAdmin(creator, id, carol, mtproto.ADMIN_PIN_MESSAGES, "a very long admin title")
	wantErr(t, "a long rank", err, mtproto.ErrAdminRankInvalid)

	if _, err = f.editAdmin(creator, id, creator, mtproto.ADMIN_ANONYMOUS|mtproto.ADMIN_BAN_USERS, "boss"); err != nil {
		t.Fatal(err)
	}
	if role, _, admin, _, _ := f.row(id, creator); role != channel.RoleCreator || admin != mtproto.ADMIN_ANONYMOUS {
		t.Errorf("creator: role %d rights %b", role, admin)
	}

	if _, err = f.editAdmin(bob, id, carol, 0, ""); err != nil {
		t.Fatal(err)
	}
	if role, _, admin, _, _ := f.row(id, carol); role != channel.RoleMember || admin != 0 {
		t.Errorf("carol demoted: role %d rights %b", role, admin)
	}
	if _, admins := f.counts(id); admins != 3 {
		t.Errorf("admins after a demotion %d", admins)
	}
}

// TestEditBanned: an admin with ban_users restricts a member, who stays in
// the channel; kicks one, who leaves the count, gets an inbox event and a
// service message tells the others; frees both: the kicked may join
// again. Admins it may not edit and the creator are out of reach; a
// member without ban_users bans nobody.
func TestEditBanned(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob, carol, dave, eve := f.user("Creator"), f.user("Bob"), f.user("Carol"), f.user("Dave"), f.user("Eve")
	id := f.create(creator)
	f.public(id)
	for _, u := range []int64{bob, carol, dave} {
		if _, err := f.join(u, id, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.editAdmin(creator, id, bob, mtproto.ADMIN_BAN_USERS, ""); err != nil {
		t.Fatal(err)
	}
	participants, _ := f.counts(id)

	until := int32(time.Now().Add(time.Hour).Unix())
	if _, err := f.editBanned(bob, id, carol, mtproto.BANNED_SEND_MEDIA, until); err != nil {
		t.Fatal(err)
	}
	if _, state, _, banned, u := f.row(id, carol); state != channel.StateIn || banned != mtproto.BANNED_SEND_MEDIA || u != until {
		t.Errorf("restricted: state %d banned %b until %d", state, banned, u)
	}

	r, err := f.editBanned(bob, id, dave, mtproto.BANNED_VIEW_MESSAGES, 0)
	if err != nil {
		t.Fatal(err)
	}
	m := r.GetUpdates()[0].GetMessage_MESSAGE()
	if m.GetAction().GetPredicateName() != mtproto.Predicate_messageActionChatDeleteUser || m.GetAction().GetUserId() != dave {
		t.Errorf("the service message of a kick: %v", m)
	}
	if _, state, _, _, u := f.row(id, dave); state != channel.StateKicked || u != math.MaxInt32 {
		t.Errorf("kicked: state %d until %d", state, u)
	}
	if n, _ := f.counts(id); n != participants-1 {
		t.Errorf("participants %d, want %d", n, participants-1)
	}
	if f.kickedEvents(dave) != 1 {
		t.Errorf("no inbox event for the kicked")
	}
	_, err = f.join(dave, id, "")
	wantErr(t, "the kicked joins", err, mtproto.ErrChannelPrivate)

	// A user never in the channel may be kicked beforehand.
	if _, err = f.editBanned(bob, id, eve, mtproto.BANNED_VIEW_MESSAGES, 0); err != nil {
		t.Fatal(err)
	}
	_, err = f.editBanned(bob, id, eve, mtproto.BANNED_SEND_MEDIA, 0)
	wantErr(t, "restricting one out", err, mtproto.ErrUserNotParticipant)

	_, err = f.editBanned(bob, id, creator, mtproto.BANNED_VIEW_MESSAGES, 0)
	wantErr(t, "the creator", err, mtproto.ErrUserAdminInvalid)
	_, err = f.editBanned(carol, id, bob, mtproto.BANNED_VIEW_MESSAGES, 0)
	wantErr(t, "without ban_users", err, mtproto.ErrChatAdminRequired)
	_, err = f.editBanned(bob, id, bob, mtproto.BANNED_VIEW_MESSAGES, 0)
	wantErr(t, "oneself", err, mtproto.ErrParticipantIdInvalid)
	_, err = f.editBanned(bob, id, carol, 1<<30, 0)
	wantErr(t, "unknown flags", err, mtproto.ErrBannedRightsInvalid)

	var kicked, banned int32
	if err = f.box.QueryRow("SELECT COUNT(*) FROM channel_participants WHERE channel_id = ? AND state = ?", id, channel.StateKicked).Scan(&kicked); err != nil || kicked != 2 {
		t.Errorf("kicked rows %d, %v", kicked, err)
	}
	mc, err := f.c.svcCtx.Dao.GetMutableChannel(f.c.ctx, id, nil)
	if err != nil {
		t.Fatal(err)
	}
	if kicked, banned = mc.GetChannel().GetKickedCount(), mc.GetChannel().GetBannedCount(); kicked != 2 || banned != 1 {
		t.Errorf("counted: kicked %d banned %d", kicked, banned)
	}

	for _, u := range []int64{carol, dave} {
		if _, err = f.editBanned(bob, id, u, 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	if _, state, _, b, _ := f.row(id, carol); state != channel.StateIn || b != 0 {
		t.Errorf("carol freed: state %d banned %b", state, b)
	}
	if _, state, _, _, _ := f.row(id, dave); state != channel.StateLeft {
		t.Errorf("dave freed: state %d", state)
	}
	if _, err = f.join(dave, id, ""); err != nil {
		t.Errorf("the freed joins again: %v", err)
	}
}

// TestEditDefaultBannedRights: an admin with ban_users sets the defaults of
// a supergroup, which then hold for its members; the same again is not
// modified; view_messages is no default; a channel has none.
func TestEditDefaultBannedRights(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob := f.user("Creator"), f.user("Bob")
	id := f.create(creator)
	f.public(id)
	if _, err := f.join(bob, id, ""); err != nil {
		t.Fatal(err)
	}
	edit := func(self int64, rights int32) error {
		_, err := f.c.ChannelEditDefaultBannedRights(&channel.TLChannelEditDefaultBannedRights{SelfId: self, PermAuthKeyId: 1, ChannelId: id, BannedRights: rights})
		return err
	}
	if err := edit(creator, mtproto.BANNED_CHANGE_INFO); err != nil {
		t.Fatal(err)
	}
	_, err := f.title(bob, id, "bob's")
	wantErr(t, "a member under the default", err, mtproto.ErrChatAdminRequired)
	wantErr(t, "the same", edit(creator, mtproto.BANNED_CHANGE_INFO), mtproto.ErrChatNotModified)
	wantErr(t, "view_messages", edit(creator, mtproto.BANNED_VIEW_MESSAGES), mtproto.ErrBannedRightsInvalid)
	wantErr(t, "a member", edit(bob, 0), mtproto.ErrChatAdminRequired)
	f.exec("UPDATE channels SET broadcast = 1, megagroup = 0 WHERE id = ?", id)
	wantErr(t, "a channel", edit(creator, 0), mtproto.ErrChannelInvalid)
}

// TestEditCreator: the creator hands the channel to a member, who becomes
// the creator, while the old one stays an admin with every right that the
// new one may edit; only the creator hands it, only to a member.
func TestEditCreator(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob, carol, eve := f.user("Creator"), f.user("Bob"), f.user("Carol"), f.user("Eve")
	id := f.create(creator)
	// The creator counts among the admins, as channel.createChannel writes it.
	f.exec("UPDATE channels SET admins_count = 1 WHERE id = ?", id)
	f.public(id)
	for _, u := range []int64{bob, carol} {
		if _, err := f.join(u, id, ""); err != nil {
			t.Fatal(err)
		}
	}
	give := func(self, to int64) error {
		_, err := f.c.ChannelEditCreator(&channel.TLChannelEditCreator{SelfId: self, PermAuthKeyId: 1, ChannelId: id, UserId: to})
		return err
	}
	wantErr(t, "a member hands it", give(bob, carol), mtproto.ErrChatAdminRequired)
	wantErr(t, "to oneself", give(creator, creator), mtproto.ErrChatNotModified)
	wantErr(t, "to one out", give(creator, eve), mtproto.ErrUserIdInvalid)
	if err := give(creator, bob); err != nil {
		t.Fatal(err)
	}
	if role, _, _, _, _ := f.row(id, bob); role != channel.RoleCreator {
		t.Errorf("bob: role %d", role)
	}
	if role, _, admin, _, _ := f.row(id, creator); role != channel.RoleAdmin || admin != channelperm.CreatorRights {
		t.Errorf("the old creator: role %d rights %b", role, admin)
	}
	var owner int64
	if err := f.box.QueryRow("SELECT creator_user_id FROM channels WHERE id = ?", id).Scan(&owner); err != nil || owner != bob {
		t.Errorf("creator_user_id %d, %v", owner, err)
	}
	if _, admins := f.counts(id); admins != 2 {
		t.Errorf("admins %d", admins)
	}
	if _, err := f.editAdmin(bob, id, creator, 0, ""); err != nil {
		t.Errorf("the new creator demotes the old one: %v", err)
	}
}

// bot makes userId a bot; nochats: it cannot join groups.
func (f *diffFixture) bot(userId int64, nochats bool) {
	f.t.Helper()
	f.exec("INSERT INTO bots (bot_id, bot_nochats, token) VALUES (?, ?, ?)", userId, nochats, fmt.Sprint("token", userId))
}

// TestEditAdminAddsBot: a bot out of a channel or a supergroup joins by being
// made an admin, as the clients expect; nobody else does, and the admin still
// gives no more than its own rights.
func TestEditAdminAddsBot(t *testing.T) {
	f := newDiffFixture(t)
	creator, user, bob := f.user("Creator"), f.user("User"), f.user("Bob")
	id := f.create(creator)
	f.exec("UPDATE channels SET admins_count = 1 WHERE id = ?", id)
	f.public(id)
	if _, err := f.join(bob, id, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := f.editAdmin(creator, id, bob, mtproto.ADMIN_ADD_ADMINS|mtproto.ADMIN_PIN_MESSAGES, ""); err != nil {
		t.Fatal(err)
	}

	bot, nochats, greedy := f.user("Bot"), f.user("NoChats"), f.user("Greedy")
	f.bot(bot, false)
	f.bot(nochats, true)
	f.bot(greedy, false)

	participants, _ := f.counts(id)
	r, err := f.editAdmin(creator, id, bot, mtproto.ADMIN_PIN_MESSAGES, "")
	if err != nil {
		t.Fatal(err)
	}
	if role, state, admin, _, _ := f.row(id, bot); role != channel.RoleAdmin || state != channelperm.StateIn || admin != mtproto.ADMIN_PIN_MESSAGES {
		t.Errorf("bot: role %d state %d rights %b", role, state, admin)
	}
	if now, admins := f.counts(id); now != participants+1 || admins != 3 {
		t.Errorf("participants %d (was %d), admins %d", now, participants, admins)
	}
	if n := f.invitedEvents(bot); n != 1 {
		t.Errorf("inbox events of the bot: %d", n)
	}
	// A supergroup tells of the new member, as an invitation does.
	var added bool
	for _, u := range r.GetUpdates() {
		if a := u.GetMessage_MESSAGE().GetAction(); a.GetPredicateName() == mtproto.Predicate_messageActionChatAddUser && len(a.GetUsers()) == 1 && a.GetUsers()[0] == bot {
			added = true
		}
	}
	if !added {
		t.Errorf("answer %v", r.GetUpdates())
	}

	_, err = f.editAdmin(creator, id, user, mtproto.ADMIN_PIN_MESSAGES, "")
	wantErr(t, "a user out of the channel", err, mtproto.ErrUserNotParticipant)
	_, err = f.editAdmin(creator, id, greedy, 0, "")
	wantErr(t, "a bot with no rights", err, mtproto.ErrUserNotParticipant)
	_, err = f.editAdmin(bob, id, greedy, mtproto.ADMIN_BAN_USERS, "")
	wantErr(t, "more than its own", err, mtproto.ErrRightForbidden)
	_, err = f.editAdmin(creator, id, nochats, mtproto.ADMIN_PIN_MESSAGES, "")
	wantErr(t, "a bot that cannot join groups", err, mtproto.ErrBotGroupsBlocked)

	// A broadcast channel: no service message, and bot_nochats is of groups.
	post := f.create(creator)
	f.broadcast(post)
	r, err = f.editAdmin(creator, post, nochats, mtproto.ADMIN_POST_MESSAGES, "")
	if err != nil {
		t.Fatal(err)
	}
	if u := r.GetUpdates(); len(u) != 1 || u[0].GetPredicateName() != mtproto.Predicate_updateChannel {
		t.Errorf("answer in a broadcast channel %v", u)
	}
	if role, _, _, _, _ := f.row(post, nochats); role != channel.RoleAdmin {
		t.Errorf("bot in a broadcast channel: role %d", role)
	}
}
