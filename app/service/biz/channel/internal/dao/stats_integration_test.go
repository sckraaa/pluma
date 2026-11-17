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

package dao

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/channelstats"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// The events of the statistics (task 103) written by the changes of the
// members and by the admins' actions, in the same box transaction.

// user adds an account; a ghost (ADR 0048) when ghost.
func (f *fixture) user(ghost bool) int64 {
	f.t.Helper()
	id := itId()
	f.exec("INSERT INTO users (id, access_hash, phone, country_code, first_name, ghost) VALUES (?, 1, ?, 'US', 'u', ?)", id, fmt.Sprint(id), ghost)
	return id
}

// in runs fn in userId's box transaction and fails the test on an error.
func (f *fixture) in(userId int64, fn func(b *userbox.Box) error) {
	f.t.Helper()
	if _, err := userbox.Run(context.Background(), f.box, userId, fn); err != nil {
		f.t.Fatal(err)
	}
}

type memberEvent struct {
	Joined    bool
	Source    int8
	InviterId int64
}

// memberEvents are the events of userId in the channel, in order.
func (f *fixture) memberEvents(channelId, userId int64) []memberEvent {
	f.t.Helper()
	rows, err := f.box.Query("SELECT joined, source, inviter_id FROM channel_stats_member_events WHERE channel_id = ? AND user_id = ? ORDER BY id", channelId, userId)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var out []memberEvent
	for rows.Next() {
		var e memberEvent
		if err = rows.Scan(&e.Joined, &e.Source, &e.InviterId); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, e)
	}
	if err = rows.Err(); err != nil {
		f.t.Fatal(err)
	}
	return out
}

func (f *fixture) wantEvents(channelId, userId int64, want ...memberEvent) {
	f.t.Helper()
	got := f.memberEvents(channelId, userId)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		f.t.Fatalf("events of %d in %d: %v, want %v", userId, channelId, got, want)
	}
}

func (f *fixture) invite(channelId, adminId int64, link string) {
	f.exec("INSERT INTO chat_invites (id, chat_id, admin_id, link, date2) VALUES (?, ?, ?, ?, 0)", itId(), channelId, adminId, link)
}

func joined(source channelstats.JoinSource, inviter int64) memberEvent {
	return memberEvent{Joined: true, Source: int8(source), InviterId: inviter}
}

var left = memberEvent{}

// TestMemberEventsSources: each way in writes one join with its source and
// who let the member in; leaving and being removed write one leave each.
func TestMemberEventsSources(t *testing.T) {
	f := viewsFixture(t)
	owner := f.user(false)
	group := f.create(owner, true)
	f.exec("INSERT INTO username (username, peer_type, peer_id, editable, active, order2) VALUES (?, ?, ?, 1, 1, 0)", fmt.Sprintf("stats%d", group), mtproto.PEER_CHANNEL, group)

	public, byLink, byFolder, requester, added := f.user(false), f.user(false), f.user(false), f.user(false), f.user(false)
	link, folder := fmt.Sprintf("l%d", group), fmt.Sprintf("f%d", group)
	f.invite(group, owner, link)
	f.invite(group, owner, folder)
	f.exec("INSERT INTO chatlist_invite_peers (slug, channel_id, link) VALUES (?, ?, ?)", fmt.Sprintf("s%d", group), group, folder)

	join := func(userId int64, link string) {
		f.t.Helper()
		f.in(userId, func(b *userbox.Box) error { _, err := f.d.Join(b, group, link); return err })
	}
	join(public, "")
	join(byLink, link)
	join(byFolder, folder)
	f.wantEvents(group, public, joined(channelstats.JoinPublic, 0))
	f.wantEvents(group, byLink, joined(channelstats.JoinLink, owner))
	f.wantEvents(group, byFolder, joined(channelstats.JoinFolder, owner))

	f.exec("INSERT INTO channel_join_requests (channel_id, user_id, link, date2) VALUES (?, ?, '', 1)", group, requester)
	f.in(owner, func(b *userbox.Box) error {
		_, err := f.d.HideRequests(b, group, true, false, "", requester)
		return err
	})
	f.wantEvents(group, requester, joined(channelstats.JoinRequest, owner))

	f.in(owner, func(b *userbox.Box) error { _, err := f.d.AddMembers(b, group, []int64{added}); return err })
	f.wantEvents(group, added, joined(channelstats.JoinAdded, owner))

	f.in(public, func(b *userbox.Box) error { _, err := f.d.Leave(b, group); return err })
	f.wantEvents(group, public, joined(channelstats.JoinPublic, 0), left)

	// Kicked: a leave and the admin's kick; freed later: nothing more.
	f.in(owner, func(b *userbox.Box) error {
		_, err := f.d.EditBanned(b, group, byLink, mtproto.BANNED_VIEW_MESSAGES, 0)
		return err
	})
	f.in(owner, func(b *userbox.Box) error { _, err := f.d.EditBanned(b, group, byLink, 0, 0); return err })
	f.wantEvents(group, byLink, joined(channelstats.JoinLink, owner), left)

	// Restricted: no leave, the admin's restriction.
	f.in(owner, func(b *userbox.Box) error {
		_, err := f.d.EditBanned(b, group, byFolder, mtproto.BANNED_SEND_MESSAGES, 0)
		return err
	})
	f.wantEvents(group, byFolder, joined(channelstats.JoinFolder, owner))

	var kicked, banned int
	if err := f.box.QueryRow("SELECT kicked, banned FROM channel_stats_admin_actions WHERE channel_id = ? AND admin_id = ?", group, owner).Scan(&kicked, &banned); err != nil {
		t.Fatal(err)
	}
	if kicked != 1 || banned != 1 {
		t.Fatalf("admin actions kicked %d, banned %d; want 1, 1 (freeing is no action)", kicked, banned)
	}
}

// TestMemberEventsBroadcast: a broadcast channel records its subscribers
// as a supergroup its members.
func TestMemberEventsBroadcast(t *testing.T) {
	f := viewsFixture(t)
	owner := f.user(false)
	ch := f.create(owner, false)
	f.exec("INSERT INTO username (username, peer_type, peer_id, editable, active, order2) VALUES (?, ?, ?, 1, 1, 0)", fmt.Sprintf("statsb%d", ch), mtproto.PEER_CHANNEL, ch)
	sub := f.user(false)
	f.in(sub, func(b *userbox.Box) error { _, err := f.d.Join(b, ch, ""); return err })
	f.in(sub, func(b *userbox.Box) error { _, err := f.d.Leave(b, ch); return err })
	f.wantEvents(ch, sub, joined(channelstats.JoinPublic, 0), left)
}

// TestMemberEventsGhost: a ghost comes and goes unseen, and is not added.
func TestMemberEventsGhost(t *testing.T) {
	f := viewsFixture(t)
	owner := f.user(false)
	group := f.create(owner, true)
	f.exec("INSERT INTO username (username, peer_type, peer_id, editable, active, order2) VALUES (?, ?, ?, 1, 1, 0)", fmt.Sprintf("statsg%d", group), mtproto.PEER_CHANNEL, group)
	g := f.user(true)
	f.in(g, func(b *userbox.Box) error { _, err := f.d.Join(b, group, ""); return err })
	f.in(g, func(b *userbox.Box) error { _, err := f.d.Leave(b, group); return err })
	f.wantEvents(group, g)
}

// TestMemberEventsRolledBack: an event is in the transaction of its change:
// a change that does not commit leaves no event, so a transaction run again
// writes its events once.
func TestMemberEventsRolledBack(t *testing.T) {
	f := viewsFixture(t)
	owner := f.user(false)
	group := f.create(owner, true)
	f.exec("INSERT INTO username (username, peer_type, peer_id, editable, active, order2) VALUES (?, ?, ?, 1, 1, 0)", fmt.Sprintf("statsr%d", group), mtproto.PEER_CHANNEL, group)
	u := f.user(false)
	abort := errors.New("abort")
	_, err := userbox.Run(context.Background(), f.box, u, func(b *userbox.Box) error {
		if _, err := f.d.Join(b, group, ""); err != nil {
			return err
		}
		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatalf("run: %v", err)
	}
	f.wantEvents(group, u)
	f.in(u, func(b *userbox.Box) error { _, err := f.d.Join(b, group, ""); return err })
	f.wantEvents(group, u, joined(channelstats.JoinPublic, 0))
}

// storedMessage adds message id of sender to the channel, as msg stores it,
// and makes it the channel's last.
func (f *fixture) storedMessage(channelId, sender int64, id int32) {
	f.t.Helper()
	data, err := jsonx.MarshalToString(mtproto.MakeTLMessage(&mtproto.Message{
		Id: id, PeerId: mtproto.MakePeerChannel(channelId), FromId: mtproto.MakePeerUser(sender), Message: "m",
	}).To_Message())
	if err != nil {
		f.t.Fatal(err)
	}
	f.exec("INSERT INTO channel_messages (channel_id, id, sender_user_id, pts, message_data, date2) VALUES (?, ?, ?, ?, ?, 0)",
		channelId, id, sender, id, data)
	f.exec("UPDATE channels SET top_message = ? WHERE id = ?", id, channelId)
}

// TestAdminDeletedAndReaders: an admin's deletion of others' messages adds
// their number to its day; its own messages do not count. Reading a
// supergroup makes a reader of the day, once.
func TestAdminDeletedAndReaders(t *testing.T) {
	f := viewsFixture(t)
	owner := f.user(false)
	group := f.create(owner, true)
	f.exec("INSERT INTO username (username, peer_type, peer_id, editable, active, order2) VALUES (?, ?, ?, 1, 1, 0)", fmt.Sprintf("statsd%d", group), mtproto.PEER_CHANNEL, group)
	member := f.user(false)
	f.in(member, func(b *userbox.Box) error { _, err := f.d.Join(b, group, ""); return err })

	for id := int32(1001); id <= 1003; id++ {
		f.storedMessage(group, member, id)
	}
	f.storedMessage(group, owner, 1004)
	f.in(owner, func(b *userbox.Box) error {
		_, err := f.d.Delete(b, group, []int32{1001, 1002, 1004}, false)
		return err
	})
	f.in(owner, func(b *userbox.Box) error { _, err := f.d.Delete(b, group, []int32{1003}, false); return err })
	// The member's own deletion is none of an admin's.
	f.storedMessage(group, member, 1005)
	f.in(member, func(b *userbox.Box) error { _, err := f.d.Delete(b, group, []int32{1005}, false); return err })

	var deleted int
	if err := f.box.QueryRow("SELECT SUM(deleted) FROM channel_stats_admin_actions WHERE channel_id = ? AND admin_id = ?", group, owner).Scan(&deleted); err != nil {
		t.Fatal(err)
	}
	if deleted != 3 {
		t.Fatalf("deleted %d, want 3", deleted)
	}
	var others int
	if err := f.box.QueryRow("SELECT COUNT(*) FROM channel_stats_admin_actions WHERE channel_id = ? AND admin_id = ?", group, member).Scan(&others); err != nil {
		t.Fatal(err)
	}
	if others != 0 {
		t.Fatalf("the member's own deletion counted: %d", others)
	}

	f.storedMessage(group, owner, 1006)
	f.in(member, func(b *userbox.Box) error { return f.d.Read(b, group, 0, 0) })
	f.storedMessage(group, owner, 1007)
	f.in(member, func(b *userbox.Box) error { return f.d.Read(b, group, 0, 0) })
	var readers int
	if err := f.box.QueryRow("SELECT COUNT(*) FROM channel_stats_readers WHERE channel_id = ? AND user_id = ?", group, member).Scan(&readers); err != nil {
		t.Fatal(err)
	}
	if readers != 1 {
		t.Fatalf("reader rows %d, want 1 (one a day)", readers)
	}
}

// TestAdminParticipantHistoryAndTopicReaders: deleting a member's history
// adds the messages deleted, its service messages too, to the admin's day,
// as deleting them one by one does; reading a topic of a forum makes a reader of the day.
func TestAdminParticipantHistoryAndTopicReaders(t *testing.T) {
	f := viewsFixture(t)
	owner := f.user(false)
	group := f.create(owner, true)
	f.exec("INSERT INTO username (username, peer_type, peer_id, editable, active, order2) VALUES (?, ?, ?, 1, 1, 0)", fmt.Sprintf("statsh%d", group), mtproto.PEER_CHANNEL, group)
	member, reader := f.user(false), f.user(false)
	f.in(member, func(b *userbox.Box) error { _, err := f.d.Join(b, group, ""); return err })
	f.in(reader, func(b *userbox.Box) error { _, err := f.d.Join(b, group, ""); return err })

	for id := int32(2001); id <= 2002; id++ {
		f.storedMessage(group, member, id)
	}
	// Its messages and the service message of its join.
	var want int
	if err := f.box.QueryRow("SELECT COUNT(*) FROM channel_messages WHERE channel_id = ? AND sender_user_id = ? AND deleted = 0", group, member).Scan(&want); err != nil {
		t.Fatal(err)
	}
	f.in(owner, func(b *userbox.Box) error { _, err := f.d.DeleteParticipantHistory(b, group, member); return err })
	var deleted int
	if err := f.box.QueryRow("SELECT SUM(deleted) FROM channel_stats_admin_actions WHERE channel_id = ? AND admin_id = ?", group, owner).Scan(&deleted); err != nil {
		t.Fatal(err)
	}
	if want < 3 || deleted != want {
		t.Fatalf("deleted %d, want %d", deleted, want)
	}

	f.storedMessage(group, owner, 2003)
	f.exec("UPDATE channels SET forum = 1 WHERE id = ?", group)
	f.in(reader, func(b *userbox.Box) error { return f.d.ReadTopic(b, group, channelbox.GeneralTopicId, 0) })
	var readers int
	if err := f.box.QueryRow("SELECT COUNT(*) FROM channel_stats_readers WHERE channel_id = ? AND user_id = ?", group, reader).Scan(&readers); err != nil {
		t.Fatal(err)
	}
	if readers != 1 {
		t.Fatalf("topic reader rows %d, want 1", readers)
	}
}
