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

// ghost writes an account that is a ghost (ADR 0048).
func (f *diffFixture) ghost(name string) int64 {
	f.t.Helper()
	id := f.user(name)
	f.exec("UPDATE users SET ghost = 1 WHERE id = ?", id)
	return id
}

// traces are what the others could see of a member: the channel's
// messages, its counters and its recent actions.
type traces struct {
	messages, log        int
	participants, admins int32
}

func (f *diffFixture) traces(channelId int64) traces {
	f.t.Helper()
	var t traces
	if err := f.box.QueryRow("SELECT COUNT(*) FROM channel_messages WHERE channel_id = ?", channelId).Scan(&t.messages); err != nil {
		f.t.Fatal(err)
	}
	if err := f.box.QueryRow("SELECT COUNT(*) FROM channel_admin_log WHERE channel_id = ?", channelId).Scan(&t.log); err != nil {
		f.t.Fatal(err)
	}
	t.participants, t.admins = f.counts(channelId)
	return t
}

// hiddenRow is the ghost's row: state and hidden.
func (f *diffFixture) hiddenRow(channelId, userId int64) (state int32, hidden bool) {
	f.t.Helper()
	if err := f.box.QueryRow("SELECT state, hidden FROM channel_participants WHERE channel_id = ? AND user_id = ?", channelId, userId).Scan(&state, &hidden); err != nil {
		f.t.Fatal(err)
	}
	return
}

// listed is the members self is shown (channelParticipantsRecent) and the
// count of them.
func (f *diffFixture) listed(self, channelId int64, filter, q string) ([]int64, int32) {
	f.t.Helper()
	r, err := f.c.ChannelGetParticipants(&channel.TLChannelGetParticipants{SelfId: self, ChannelId: channelId, Filter: filter, Q: q, Limit: 100})
	if err != nil {
		f.t.Fatal(err)
	}
	var ids []int64
	for _, p := range r.GetParticipants() {
		id := p.GetUserId()
		if id == 0 {
			// channelParticipantBanned names its peer.
			id = p.GetPeer().GetUserId()
		}
		ids = append(ids, id)
	}
	return ids, r.GetCount()
}

func contains(ids []int64, id int64) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

// TestGhostJoinsUnseen: a ghost joining a public supergroup by itself
// writes its hidden row, its dialog and updateChannel for its devices, and
// nothing anyone else sees: no service message, no recent action, the
// counters as they were, out of every list but its own. Its leave is as
// quiet.
func TestGhostJoinsUnseen(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob, support := f.user("Creator"), f.user("Bob"), f.ghost("Support")
	id := f.create(creator)
	f.exec("UPDATE channels SET participants_count = 1, admins_count = 1 WHERE id = ?", id)
	f.public(id)
	if _, err := f.join(bob, id, ""); err != nil {
		t.Fatal(err)
	}
	before := f.traces(id)

	r, err := f.join(support, id, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range r.GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateNewChannelMessage {
			t.Errorf("a service message in the answer: %v", u)
		}
	}
	if len(r.GetChats()) != 1 || r.GetChats()[0].GetLeft() {
		t.Errorf("the ghost is shown the channel as %v, want a member", r.GetChats())
	}
	if after := f.traces(id); after != before {
		t.Errorf("traces after the ghost joined %+v, want %+v", after, before)
	}
	if state, hidden := f.hiddenRow(id, support); state != channel.StateIn || !hidden {
		t.Errorf("row state %d hidden %v, want in and hidden", state, hidden)
	}
	if n := f.seqChannelUpdates(support, id); n != 1 {
		t.Errorf("updateChannel journaled %d times for the ghost, want once", n)
	}
	var dialogs int
	if err = f.box.QueryRow("SELECT COUNT(*) FROM dialogs WHERE user_id = ? AND peer_type = ? AND peer_id = ? AND deleted = 0",
		support, mtproto.PEER_CHANNEL, id).Scan(&dialogs); err != nil || dialogs != 1 {
		t.Errorf("the ghost's dialog: %d, %v", dialogs, err)
	}
	_, err = f.join(support, id, "")
	wantErr(t, "joining again", err, mtproto.ErrUserAlreadyParticipant)

	for _, filter := range []string{mtproto.Predicate_channelParticipantsRecent, mtproto.Predicate_channelParticipantsSearch, mtproto.Predicate_channelParticipantsMentions} {
		ids, count := f.listed(bob, id, filter, "")
		if contains(ids, support) || count != 2 {
			t.Errorf("%s to Bob: %v of %d, want the creator and Bob", filter, ids, count)
		}
	}
	if ids, _ := f.listed(bob, id, mtproto.Predicate_channelParticipantsSearch, "Supp"); len(ids) != 0 {
		t.Errorf("Bob finds %v searching for the ghost", ids)
	}
	if ids, count := f.listed(support, id, mtproto.Predicate_channelParticipantsRecent, ""); !contains(ids, support) || count != 3 {
		t.Errorf("the ghost sees %v of %d, want itself among 3", ids, count)
	}

	if _, err = f.leave(support, id); err != nil {
		t.Fatal(err)
	}
	if after := f.traces(id); after != before {
		t.Errorf("traces after the ghost left %+v, want %+v", after, before)
	}
	if state, _ := f.hiddenRow(id, support); state != channel.StateLeft {
		t.Errorf("row state %d after the leave, want left", state)
	}
}

// TestGhostJoinHidden: pluma-admin brings the ghost into a private channel
// without a link; once more changes nothing; a ban keeps it out and its
// kick is as quiet as its join; an account that is no ghost is refused.
func TestGhostJoinHidden(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob, support := f.user("Creator"), f.user("Bob"), f.ghost("Support")
	id := f.create(creator)
	f.exec("UPDATE channels SET participants_count = 1, admins_count = 1 WHERE id = ?", id)
	before := f.traces(id)

	joinHidden := func(userId int64) (bool, error) {
		r, err := f.c.ChannelJoinHidden(&channel.TLChannelJoinHidden{UserId: userId, ChannelId: id})
		return mtproto.FromBool(r), err
	}
	if joined, err := joinHidden(support); err != nil || !joined {
		t.Fatalf("join: %v, %v", joined, err)
	}
	if joined, err := joinHidden(support); err != nil || joined {
		t.Errorf("again: %v, %v, want false", joined, err)
	}
	_, err := joinHidden(bob)
	wantErr(t, "an account that is no ghost", err, mtproto.ErrUserIdInvalid)
	if after := f.traces(id); after != before {
		t.Errorf("traces %+v, want %+v", after, before)
	}

	// The creator bans the ghost, from the menu of its message: the ban
	// is the creator's own act and is logged; the ghost was never shown,
	// so no service message says it was removed, and nothing is counted.
	if _, err = f.editBanned(creator, id, support, mtproto.BANNED_VIEW_MESSAGES, 0); err != nil {
		t.Fatal(err)
	}
	after := f.traces(id)
	if after.messages != before.messages || after.participants != before.participants || after.log != before.log+1 {
		t.Errorf("traces after the ban %+v, want the messages and counts of %+v and one recent action more", after, before)
	}
	if f.kickedEvents(support) != 1 {
		t.Error("the ghost's box is not told of the ban")
	}
	_, err = joinHidden(support)
	wantErr(t, "banned", err, mtproto.ErrChannelPrivate)
	if ids, _ := f.listed(creator, id, mtproto.Predicate_channelParticipantsKicked, ""); !contains(ids, support) {
		t.Errorf("the creator's list of the removed %v lacks the ghost it banned", ids)
	}

	if _, err = f.editBanned(creator, id, support, 0, 0); err != nil {
		t.Fatal(err)
	}
	// What inbox.channelKickedToInbox did to the ghost's box.
	f.exec("UPDATE dialogs SET deleted = 1 WHERE user_id = ? AND peer_type = ? AND peer_id = ?", support, mtproto.PEER_CHANNEL, id)
	if joined, err := joinHidden(support); err != nil || !joined {
		t.Fatalf("after the ban was lifted: %v, %v", joined, err)
	}
	left, err := f.c.ChannelLeaveHidden(&channel.TLChannelLeaveHidden{UserId: support, ChannelId: id})
	if err != nil || !mtproto.FromBool(left) {
		t.Fatalf("leave: %v, %v", left, err)
	}
	if state, _ := f.hiddenRow(id, support); state != channel.StateLeft {
		t.Errorf("row state %d, want left", state)
	}
}

// TestGhostRefused: nobody adds the ghost, promotes it or hands it the
// channel: each would show it.
func TestGhostRefused(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob, support := f.user("Creator"), f.user("Bob"), f.ghost("Support")
	id := f.create(creator)
	f.exec("UPDATE channels SET participants_count = 1, admins_count = 1 WHERE id = ?", id)
	f.public(id)
	before := f.traces(id)

	_, err := f.invite(creator, id, support)
	wantErr(t, "invite", err, mtproto.ErrUserPrivacyRestricted)
	if f.invitedEvents(support) != 0 {
		t.Error("the ghost was invited")
	}
	_, err = f.editAdmin(creator, id, support, mtproto.ADMIN_INVITE_USERS, "")
	wantErr(t, "promote", err, mtproto.ErrUserPrivacyRestricted)
	if after := f.traces(id); after != before {
		t.Errorf("traces %+v, want %+v", after, before)
	}

	// With another user the ghost is left out, the other added.
	if _, err = f.invite(creator, id, support, bob); err != nil {
		t.Fatal(err)
	}
	if f.invitedEvents(bob) != 1 || f.invitedEvents(support) != 0 {
		t.Error("Bob is to be added, the ghost not")
	}

	if _, err = f.join(support, id, ""); err != nil {
		t.Fatal(err)
	}
	_, err = f.c.ChannelEditCreator(&channel.TLChannelEditCreator{SelfId: creator, PermAuthKeyId: 1, ChannelId: id, UserId: support})
	wantErr(t, "the channel handed to the hidden ghost", err, mtproto.ErrUserIdInvalid)
	if next, err := f.futureCreator(creator, id); err != nil || next != bob {
		t.Errorf("the next owner %d, %v, want Bob, never the ghost", next, err)
	}
}

// TestGhostSetUserHidden: an account made a ghost has its member rows
// hidden and the counters of their channels recounted; admin rows stay
// shown; once more changes nothing; undone, the rows are shown again.
func TestGhostSetUserHidden(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob := f.user("Creator"), f.user("Bob")
	a, b := f.create(creator), f.create(creator)
	f.exec("UPDATE channels SET participants_count = 1, admins_count = 1 WHERE id IN (?, ?)", a, b)
	f.public(a)
	f.public(b)
	for _, id := range []int64{a, b} {
		if _, err := f.join(bob, id, ""); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.editAdmin(creator, b, bob, mtproto.ADMIN_INVITE_USERS, ""); err != nil {
		t.Fatal(err)
	}
	f.exec("UPDATE users SET ghost = 1 WHERE id = ?", bob)

	set := func(hidden bool) int32 {
		t.Helper()
		r, err := f.c.ChannelSetUserHidden(&channel.TLChannelSetUserHidden{UserId: bob, Hidden: mtproto.ToBool(hidden)})
		if err != nil {
			t.Fatal(err)
		}
		return r.GetV()
	}
	if n := set(true); n != 1 {
		t.Errorf("%d channels changed, want the one Bob is a member of", n)
	}
	if p, _ := f.counts(a); p != 1 {
		t.Errorf("participants_count %d, want 1 without Bob", p)
	}
	if p, ad := f.counts(b); p != 2 || ad != 2 {
		t.Errorf("counts of the channel Bob administers %d, %d, want 2 and 2", p, ad)
	}
	if n := set(true); n != 0 {
		t.Errorf("again: %d channels changed, want none", n)
	}
	if ids, _ := f.listed(creator, a, mtproto.Predicate_channelParticipantsRecent, ""); contains(ids, bob) {
		t.Errorf("the creator still sees Bob: %v", ids)
	}
	if n := set(false); n != 1 {
		t.Errorf("undone: %d channels changed, want 1", n)
	}
	if p, _ := f.counts(a); p != 2 {
		t.Errorf("participants_count %d, want 2 with Bob again", p)
	}
}

// TestGhostReadsUnseen: the ghost's reads write no "read by" marks and
// push nobody updateReadChannelOutbox, and the outbox of the others is not
// read by them.
func TestGhostReadsUnseen(t *testing.T) {
	f := newDiffFixture(t)
	creator, support := f.user("Creator"), f.ghost("Support")
	id := f.create(creator)
	f.public(id)
	f.send(id, creator, 2)
	last := f.top(id)
	if _, err := f.join(support, id, ""); err != nil {
		t.Fatal(err)
	}
	f.exec("UPDATE dialogs SET read_inbox_max_id = 0 WHERE user_id = ? AND peer_type = ? AND peer_id = ?", support, mtproto.PEER_CHANNEL, id)
	if err := f.read(support, id, last); err != nil {
		t.Fatal(err)
	}
	var marks int
	if err := f.box.QueryRow("SELECT COUNT(*) FROM channel_read_marks WHERE channel_id = ? AND user_id = ?", id, support).Scan(&marks); err != nil || marks != 0 {
		t.Errorf("read marks of the ghost: %d, %v", marks, err)
	}
	if n := f.outboxPushes(creator); n != 0 {
		t.Errorf("the creator was told of the ghost's read %d times", n)
	}
	d := f.dialog(creator, id)
	if d.GetReadOutboxMaxId() >= last {
		t.Errorf("the creator's outbox read up to %d by the ghost", d.GetReadOutboxMaxId())
	}
}

// TestGhostReadsPublicDiscussion: the ghost reads the comments of a public
// channel it is not in, as a subscriber does; a private channel's are
// closed to it.
func TestGhostReadsPublicDiscussion(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob, support := f.user("Creator"), f.user("Bob"), f.ghost("Support")
	p := f.newDiscussionPost(creator, bob)
	// The group itself is private: only its channel opens it.
	f.exec("DELETE FROM username WHERE peer_type = ? AND peer_id = ?", mtproto.PEER_CHANNEL, p.groupId)
	reader := func(groupId int64) int32 {
		t.Helper()
		mc, err := f.c.ChannelGetMutableChannel(&channel.TLChannelGetMutableChannel{ChannelId: groupId, Id: []int64{support}})
		if err != nil {
			t.Fatal(err)
		}
		row, ok := mc.GetParticipant(support)
		if !ok {
			return -1
		}
		return row.GetState()
	}
	if state := reader(p.groupId); state != -1 {
		t.Errorf("the ghost reads the comments of a private channel: state %d", state)
	}
	f.public(p.channelId)
	if state := reader(p.groupId); state != channel.StateLinked {
		t.Errorf("state %d in the comments of a public channel, want linked", state)
	}
}

// TestGhostReadsCommentsUnseen: the ghost reading the comments of a public
// channel tells the authors nothing, and their replies do not show read by
// it when they open the thread.
func TestGhostReadsCommentsUnseen(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob, support := f.user("Creator"), f.user("Bob"), f.ghost("Support")
	p := f.newDiscussionPost(creator, bob)
	f.public(p.channelId)
	pushes := f.outboxPushes(bob)

	if _, err := f.c.ChannelReadDiscussion(&channel.TLChannelReadDiscussion{SelfId: support, PermAuthKeyId: 1, ChannelId: p.groupId, MsgId: p.copyId, ReadMaxId: p.comment}); err != nil {
		t.Fatal(err)
	}
	if n := f.outboxPushes(bob); n != pushes {
		t.Errorf("Bob was told of the ghost's read: %d pushes, want %d", n, pushes)
	}
	dm, err := f.discussionMessage(bob, p.groupId, p.comment)
	if err != nil {
		t.Fatal(err)
	}
	if got := dm.GetReadOutboxMaxId().GetValue(); got != 0 {
		t.Errorf("Bob's reply read up to %d by the ghost, want unread", got)
	}
}

// TestGhostShownOnce: an admin who restricted the ghost finds it in the
// restricted list, as banned_count counts it; asking to lift rights it does
// not have tells nothing of it; an account that stopped being a ghost and
// joins again is an ordinary member, listed and counted.
func TestGhostShownOnce(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob, support := f.user("Creator"), f.user("Bob"), f.ghost("Support")
	id := f.create(creator)
	f.exec("UPDATE channels SET participants_count = 1, admins_count = 1 WHERE id = ?", id)
	f.public(id)
	if _, err := f.join(support, id, ""); err != nil {
		t.Fatal(err)
	}
	before := f.traces(id)

	_, err := f.editAdmin(creator, id, support, 0, "")
	wantErr(t, "taking admin rights from the hidden ghost", err, mtproto.ErrUserNotParticipant)
	if _, err = f.editBanned(creator, id, support, 0, 0); err != nil {
		t.Fatal(err)
	}
	if after := f.traces(id); after != before {
		t.Errorf("traces after lifting nothing %+v, want %+v", after, before)
	}

	if _, err = f.editBanned(creator, id, support, mtproto.BANNED_SEND_MESSAGES, 0); err != nil {
		t.Fatal(err)
	}
	if ids, count := f.listed(creator, id, mtproto.Predicate_channelParticipantsBanned, ""); !contains(ids, support) || count != 1 {
		t.Errorf("the restricted list %v of %d, want the ghost the creator restricted", ids, count)
	}
	if ids, _ := f.listed(bob, id, mtproto.Predicate_channelParticipantsRecent, ""); contains(ids, support) {
		t.Errorf("the restricted ghost is among the members: %v", ids)
	}

	if _, err = f.leave(support, id); err != nil {
		t.Fatal(err)
	}
	f.exec("UPDATE users SET ghost = 0 WHERE id = ?", support)
	if _, err = f.join(support, id, ""); err != nil {
		t.Fatal(err)
	}
	if state, hidden := f.hiddenRow(id, support); state != channel.StateIn || hidden {
		t.Errorf("row state %d hidden %v of the former ghost, want in and shown", state, hidden)
	}
	if ids, count := f.listed(bob, id, mtproto.Predicate_channelParticipantsRecent, ""); !contains(ids, support) || count != 2 {
		t.Errorf("the members %v of %d, want the former ghost among 2", ids, count)
	}
}
