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
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/monoforum"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// The direct messages of a channel (task 084, ADR 0055) on MySQL.

// directChannel makes a broadcast channel of creator, with its dialog.
func (f *diffFixture) directChannel(creator int64) int64 {
	f.t.Helper()
	id := f.create(creator)
	f.broadcast(id)
	return id
}

// subscriber puts userId into the channel with role and admin rights.
func (f *diffFixture) subscriber(channelId, userId int64, role, rights int32) {
	f.t.Helper()
	f.exec("INSERT INTO channel_participants (channel_id, user_id, role, admin_rights, `date`) VALUES (?, ?, ?, ?, 1)", channelId, userId, role, rights)
}

func (f *diffFixture) direct(self, channelId int64, on bool) (*mtproto.Updates, error) {
	return f.c.ChannelUpdateDirectMessages(&channel.TLChannelUpdateDirectMessages{SelfId: self, PermAuthKeyId: 1, ChannelId: channelId, Enabled: mtproto.ToBool(on)})
}

// monoforumOf switches the direct messages of the channel on and returns
// its monoforum.
func (f *diffFixture) monoforumOf(creator, channelId int64) int64 {
	f.t.Helper()
	if _, err := f.direct(creator, channelId, true); err != nil {
		f.t.Fatal(err)
	}
	var id int64
	if err := f.box.QueryRow("SELECT linked_monoforum_id FROM channels WHERE id = ?", channelId).Scan(&id); err != nil || id == 0 {
		f.t.Fatalf("the monoforum of %d: %d, %v", channelId, id, err)
	}
	return id
}

// inSublist writes a message of from to the sublist peer of the monoforum
// as msg does: from the subscriber itself, or from the channel when an
// admin answers (broadcastId above 0). The subscriber gets its dialog.
func (f *diffFixture) inSublist(monoforumId, from, peer, broadcastId int64) int32 {
	f.t.Helper()
	var id int32
	_, err := userbox.Run(context.Background(), f.box, from, func(b *userbox.Box) error {
		ch, err := channelbox.Open(b, monoforumId)
		if err != nil {
			return err
		}
		m := mtproto.MakeTLMessage(&mtproto.Message{
			Id:          ch.NextMessageId(),
			FromId:      mtproto.MakePeerUser(from),
			PeerId:      mtproto.MakePeerChannel(monoforumId),
			SavedPeerId: mtproto.MakePeerUser(peer),
			Date:        int32(b.Date()),
			Message:     "text",
		}).To_Message()
		if broadcastId != 0 {
			m.FromId = mtproto.MakePeerChannel(broadcastId)
		}
		id = m.Id
		ch.AddPtsUpdate(mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: m}).To_Update(), 1)
		data, err := jsonx.MarshalToString(m)
		if err != nil {
			return err
		}
		if _, err = b.Tx().Exec("INSERT INTO channel_messages (channel_id, id, sender_user_id, send_as_id, random_id, pts, message_data, message, saved_peer_id, date2) VALUES (?, ?, ?, ?, ?, ?, ?, 'text', ?, ?)",
			monoforumId, m.Id, from, broadcastId, 1000+int64(m.Id), ch.Pts(), data, peer, b.Date()); err != nil {
			return err
		}
		if _, err = b.Tx().Exec("UPDATE channels SET top_message = ? WHERE id = ?", m.Id, monoforumId); err != nil {
			return err
		}
		if err = monoforum.Moved(b.Tx(), monoforumId, peer, m.Id, false, broadcastId != 0, b.Date()); err != nil {
			return err
		}
		if broadcastId != 0 {
			return nil
		}
		_, err = b.Tx().Exec("INSERT INTO dialogs (user_id, peer_type, peer_id, peer_dialog_id, top_message, read_inbox_max_id, draft_message_data, date2) VALUES (?, ?, ?, ?, ?, ?, 'null', ?)"+
			" ON DUPLICATE KEY UPDATE top_message = VALUES(top_message), read_inbox_max_id = VALUES(read_inbox_max_id), deleted = 0",
			from, mtproto.PEER_CHANNEL, monoforumId, mtproto.MakePeerDialogId(mtproto.PEER_CHANNEL, monoforumId), m.Id, m.Id, b.Date())
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

// showDialog gives userId a dialog with the channel, as the inbox does for
// an admin of the direct messages.
func (f *diffFixture) showDialog(userId, channelId int64) {
	f.t.Helper()
	f.exec("INSERT INTO dialogs (user_id, peer_type, peer_id, peer_dialog_id, draft_message_data, date2) VALUES (?, ?, ?, ?, 'null', 1) ON DUPLICATE KEY UPDATE deleted = 0",
		userId, mtproto.PEER_CHANNEL, channelId, mtproto.MakePeerDialogId(mtproto.PEER_CHANNEL, channelId))
}

func (f *diffFixture) historyIds(self, channelId int64) ([]int32, error) {
	h, err := f.c.ChannelGetHistory(&channel.TLChannelGetHistory{SelfId: self, ChannelId: channelId, Limit: 100})
	if err != nil {
		return nil, err
	}
	out := ids(h.GetMessages())
	slices.Sort(out)
	return out, nil
}

func (f *diffFixture) viewOf(self, channelId int64) *mtproto.Chat {
	f.t.Helper()
	mc, err := f.c.svcCtx.Dao.GetMutableChannel(context.Background(), channelId, []int64{self})
	if err != nil {
		f.t.Fatal(err)
	}
	return mc.ToChat(self, 1_800_000_000)
}

// pushesTo counts the pushes to recipient that carry an update of the kind.
func (f *diffFixture) pushesTo(recipient int64, predicate string) int {
	f.t.Helper()
	rows, err := f.box.Query("SELECT payload FROM mq_outbox WHERE recipient_id = ? AND method IN ('sync.TL_sync_pushUpdates', 'sync.TL_sync_updatesNotMe')", recipient)
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	n := 0
	for rows.Next() {
		var data []byte
		if err = rows.Scan(&data); err != nil {
			f.t.Fatal(err)
		}
		var in struct {
			Updates *mtproto.Updates `json:"updates"`
		}
		if err = jsonx.Unmarshal(data, &in); err != nil {
			f.t.Fatal(err)
		}
		for _, u := range in.Updates.GetUpdates() {
			if u.GetPredicateName() == predicate {
				n++
			}
		}
	}
	if err = rows.Err(); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// TestDirectMessagesSwitch: one who may change the channel's info switches
// its direct messages on, which makes the monoforum, linked both ways and
// named only while they are on; off and on again keeps the same monoforum.
func TestDirectMessagesSwitch(t *testing.T) {
	f := newDiffFixture(t)
	creator, editor, poster, reader := f.user("Creator"), f.user("Editor"), f.user("Poster"), f.user("Reader")
	id := f.directChannel(creator)
	f.subscriber(id, editor, channel.RoleAdmin, mtproto.ADMIN_CHANGE_INFO)
	f.subscriber(id, poster, channel.RoleAdmin, mtproto.ADMIN_POST_MESSAGES)
	f.subscriber(id, reader, channel.RoleMember, 0)

	_, err := f.direct(poster, id, true)
	wantErr(t, "an admin without change_info", err, mtproto.ErrChatAdminRequired)
	_, err = f.direct(reader, id, true)
	wantErr(t, "a subscriber", err, mtproto.ErrChatAdminRequired)
	_, err = f.direct(creator, id, false)
	wantErr(t, "switching off what is off", err, mtproto.ErrChatNotModified)

	r, err := f.direct(editor, id, true)
	if err != nil {
		t.Fatal(err)
	}
	var mono int64
	var isMono bool
	var back int64
	if err = f.box.QueryRow("SELECT linked_monoforum_id FROM channels WHERE id = ?", id).Scan(&mono); err != nil || mono <= id {
		t.Fatalf("the monoforum %d of %d: %v", mono, id, err)
	}
	if err = f.box.QueryRow("SELECT monoforum, linked_monoforum_id FROM channels WHERE id = ?", mono).Scan(&isMono, &back); err != nil || !isMono || back != id {
		t.Fatalf("the monoforum row: monoforum %v, channel %d, %v", isMono, back, err)
	}
	var service *mtproto.Message
	for _, u := range r.GetUpdates() {
		if m := u.GetMessage_MESSAGE(); m != nil {
			service = m
		}
	}
	if a := service.GetAction(); a.GetPredicateName() != mtproto.Predicate_messageActionPaidMessagesPrice || !a.GetBroadcastMessagesAllowed() || a.GetStars_INT64() != 0 {
		t.Errorf("the service message: %v", service)
	}
	if len(r.GetChats()) != 2 {
		t.Fatalf("the answer has %d chats, want the channel and its monoforum", len(r.GetChats()))
	}
	for _, c := range r.GetChats() {
		switch c.GetId() {
		case id:
			if !c.GetBroadcastMessagesAllowed() || c.GetLinkedMonoforumId().GetValue() != mono {
				t.Errorf("the channel: %v", c)
			}
		case mono:
			if !c.GetMonoforum() || !c.GetMegagroup() || c.GetLinkedMonoforumId().GetValue() != id || c.GetTitle() != "t" || c.GetLeft() {
				t.Errorf("the monoforum: %v", c)
			}
		default:
			t.Errorf("an unexpected chat %d", c.GetId())
		}
	}
	_, err = f.direct(creator, id, true)
	wantErr(t, "switching on what is on", err, mtproto.ErrChatNotModified)
	_, err = f.direct(creator, mono, true)
	wantErr(t, "the direct messages of a monoforum", err, dao.ErrChannelMonoforumUnsupported)
	group := f.create(creator)
	_, err = f.direct(creator, group, true)
	wantErr(t, "the direct messages of a supergroup", err, mtproto.ErrChannelInvalid)

	if _, err = f.direct(creator, id, false); err != nil {
		t.Fatal(err)
	}
	if c := f.viewOf(creator, id); c.GetBroadcastMessagesAllowed() || c.GetLinkedMonoforumId() != nil {
		t.Errorf("the channel with direct messages off: %v", c)
	}
	if c := f.viewOf(creator, mono); !c.GetMonoforum() || c.GetLinkedMonoforumId() != nil {
		t.Errorf("the monoforum switched off: %v", c)
	}
	if _, err = f.direct(creator, id, true); err != nil {
		t.Fatal(err)
	}
	var again int64
	if err = f.box.QueryRow("SELECT linked_monoforum_id FROM channels WHERE id = ?", id).Scan(&again); err != nil || again != mono {
		t.Errorf("switched on again the monoforum is %d, want the same %d (%v)", again, mono, err)
	}
}

// TestMonoforumViewers: what a user is in a monoforum follows from the
// channel: its creator and the admins with manage_direct_messages manage
// it, the others who see the channel are its subscribers, and one who does
// not see the channel does not see the monoforum; a change of rights in the
// channel is a change here at once.
func TestMonoforumViewers(t *testing.T) {
	f := newDiffFixture(t)
	creator, manager, poster, reader, outsider := f.user("Creator"), f.user("Manager"), f.user("Poster"), f.user("Reader"), f.user("Out")
	id := f.directChannel(creator)
	f.subscriber(id, manager, channel.RoleAdmin, mtproto.ADMIN_MANAGE_DIRECT_MESSAGES|mtproto.ADMIN_DELETE_MESSAGES)
	f.subscriber(id, poster, channel.RoleAdmin, mtproto.ADMIN_POST_MESSAGES)
	f.subscriber(id, reader, channel.RoleMember, 0)
	mono := f.monoforumOf(creator, id)

	if c := f.viewOf(creator, mono); c.GetPredicateName() != mtproto.Predicate_channel || !c.GetAdminRights().GetManageDirectMessages() {
		t.Errorf("the creator's monoforum: %v", c)
	}
	if c := f.viewOf(manager, mono); !c.GetAdminRights().GetManageDirectMessages() || !c.GetAdminRights().GetDeleteMessages() || c.GetAdminRights().GetPostMessages() {
		t.Errorf("the manager's monoforum: %v", c)
	}
	for name, user := range map[string]int64{"an admin without the right": poster, "a subscriber": reader} {
		if c := f.viewOf(user, mono); c.GetPredicateName() != mtproto.Predicate_channel || c.GetAdminRights() != nil || c.GetLeft() {
			t.Errorf("%s sees the monoforum as %v", name, c)
		}
	}
	if c := f.viewOf(outsider, mono); c.GetPredicateName() != mtproto.Predicate_channelForbidden {
		t.Errorf("one out of a private channel sees its monoforum: %v", c)
	}
	_, err := f.historyIds(outsider, mono)
	wantErr(t, "the history for one out of the channel", err, mtproto.ErrChannelPrivate)

	f.public(id)
	if c := f.viewOf(outsider, mono); c.GetPredicateName() != mtproto.Predicate_channel {
		t.Errorf("one out of a public channel does not see its monoforum: %v", c)
	}
	f.exec("UPDATE channel_participants SET state = 2, banned_rights = 1 WHERE channel_id = ? AND user_id = ?", id, reader)
	if c := f.viewOf(reader, mono); c.GetPredicateName() != mtproto.Predicate_channelForbidden {
		t.Errorf("one banned from the channel sees its monoforum: %v", c)
	}
	f.exec("UPDATE channel_participants SET admin_rights = ? WHERE channel_id = ? AND user_id = ?", mtproto.ADMIN_POST_MESSAGES, id, manager)
	if c := f.viewOf(manager, mono); c.GetAdminRights() != nil {
		t.Errorf("an admin whose right was taken still manages: %v", c)
	}

	list, err := f.c.svcCtx.Dao.GetChannelListByIdList(context.Background(), creator, []int64{mono})
	if err != nil || len(list) != 2 || list[0].GetChannel().GetId() != mono || list[1].GetChannel().GetId() != id {
		t.Errorf("a monoforum comes with its channel: %v, %v", list, err)
	}
}

// TestSublistsApart: each subscriber sees its own sublist and nothing of
// another's, by the history, by ids, in the difference, in the dialog and
// its top message; the admins see them all. The updates go to the admins
// and to those who have a sublist.
func TestSublistsApart(t *testing.T) {
	f := newDiffFixture(t)
	creator, alice, bob, carol := f.user("Creator"), f.user("Alice"), f.user("Bob"), f.user("Carol")
	id := f.directChannel(creator)
	f.public(id)
	mono := f.monoforumOf(creator, id)
	f.showDialog(creator, mono)

	a1 := f.inSublist(mono, alice, alice, 0)
	b1 := f.inSublist(mono, bob, bob, 0)
	a2 := f.inSublist(mono, creator, alice, id)
	b2 := f.inSublist(mono, bob, bob, 0)

	for name, c := range map[string]struct {
		user int64
		want []int32
	}{
		"alice":   {alice, []int32{a1, a2}},
		"bob":     {bob, []int32{b1, b2}},
		"carol":   {carol, []int32{}},
		"creator": {creator, []int32{a1, b1, a2, b2}},
	} {
		got, err := f.historyIds(c.user, mono)
		if err != nil || !slices.Equal(got, c.want) {
			t.Errorf("the history of %s: %v, %v; want %v", name, got, err, c.want)
		}
	}
	r, err := f.c.ChannelGetMessages(&channel.TLChannelGetMessages{SelfId: alice, ChannelId: mono, Id: []int32{a1, b1, a2, b2}})
	if err != nil || !slices.Equal(ids(r.GetDatas()), []int32{a1, a2}) {
		t.Errorf("alice asks for every id and gets %v, %v", ids(r.GetDatas()), err)
	}
	if !r.GetDatas()[0].GetOut() || r.GetDatas()[1].GetOut() || r.GetDatas()[1].GetFromId().GetChannelId() != id {
		t.Errorf("alice's messages: %v", r.GetDatas())
	}
	s, err := f.c.ChannelSearch(&channel.TLChannelSearch{SelfId: alice, ChannelId: mono, Q: "text", MediaType: mtproto.MEDIA_EMPTY, Limit: 100})
	if err != nil || len(s.GetMessages()) != 2 {
		t.Errorf("alice's search finds %v, %v", ids(s.GetMessages()), err)
	}
	// An admin searches one chat (the search and the shared media of a
	// sublist); a subscriber naming another's chat finds nothing.
	s, err = f.c.ChannelSearch(&channel.TLChannelSearch{SelfId: creator, ChannelId: mono, Q: "text", MediaType: mtproto.MEDIA_EMPTY, Limit: 100, SavedPeerId: bob})
	if err != nil || !slices.Equal(ids(s.GetMessages()), []int32{b2, b1}) {
		t.Errorf("the creator's search in bob's chat finds %v, %v", ids(s.GetMessages()), err)
	}
	for _, c := range []struct {
		self, sublist int64
		want          int32
	}{{creator, alice, 2}, {creator, bob, 2}, {alice, bob, 0}, {alice, alice, 2}} {
		n, err := f.c.ChannelSearchCount(&channel.TLChannelSearchCount{SelfId: c.self, ChannelId: mono, MediaType: mtproto.MEDIA_EMPTY, SavedPeerId: c.sublist})
		if err != nil || n.GetV() != c.want {
			t.Errorf("%d counts %v, %v in the chat of %d; want %d", c.self, n.GetV(), err, c.sublist, c.want)
		}
	}

	d := f.diff(alice, mono, channelbox.BasePts, 100)
	if got := ids(d.GetNewMessages()); !slices.Equal(got, []int32{a1, a2}) || !d.GetFinal() {
		t.Errorf("alice's difference: %v, final %v", got, d.GetFinal())
	}
	var pts int32
	if err = f.box.QueryRow("SELECT pts FROM channels WHERE id = ?", mono).Scan(&pts); err != nil || d.GetPts() != pts {
		t.Errorf("alice's difference ends at %d, the monoforum is at %d (%v)", d.GetPts(), pts, err)
	}
	if got := ids(f.diff(creator, mono, channelbox.BasePts, 100).GetNewMessages()); len(got) != 4 {
		t.Errorf("the creator's difference: %v", got)
	}

	if dl := f.dialog(alice, mono); dl.GetTopMessage() != a2 || dl.GetUnreadCount() != 1 || dl.GetReadInboxMaxId() != a1 {
		t.Errorf("alice's dialog: %v", dl)
	}
	if dl := f.dialog(bob, mono); dl.GetTopMessage() != b2 || dl.GetUnreadCount() != 0 {
		t.Errorf("bob's dialog: %v", dl)
	}
	if dl := f.dialog(creator, mono); dl.GetTopMessage() != b2 || dl.GetUnreadCount() != 2 || dl.GetReadInboxMaxId() != 0 {
		t.Errorf("the creator's dialog: %v, want both of bob's unread from the start", dl)
	}
	top, err := f.c.ChannelGetTopMessages(&channel.TLChannelGetTopMessages{SelfId: alice, IdList: []int64{mono}})
	if err != nil || len(top.GetDatas()) != 1 || top.GetDatas()[0].GetId() != a2 {
		t.Errorf("alice's top message: %v, %v", top.GetDatas(), err)
	}
	if top, err = f.c.ChannelGetTopMessages(&channel.TLChannelGetTopMessages{SelfId: carol, IdList: []int64{mono}}); err != nil || len(top.GetDatas()) != 0 {
		t.Errorf("the top message of one who never wrote: %v, %v", top.GetDatas(), err)
	}

	members, err := f.c.ChannelGetMemberIdList(&channel.TLChannelGetMemberIdList{ChannelId: mono})
	want := []int64{creator, alice, bob}
	slices.Sort(want)
	if err != nil || !slices.Equal(members.GetDatas(), want) {
		t.Errorf("the recipients of the monoforum: %v, %v; want %v", members.GetDatas(), err, want)
	}
	peers, err := f.c.ChannelGetMessageSublists(&channel.TLChannelGetMessageSublists{ChannelId: mono, Id: []int32{b1, a2, 999}})
	if err != nil || !slices.Equal(peers.GetDatas(), []int64{bob, alice, 0}) {
		t.Errorf("the sublists of messages: %v, %v", peers.GetDatas(), err)
	}
	_, err = f.c.ChannelGetDiscussionMessage(&channel.TLChannelGetDiscussionMessage{SelfId: alice, ChannelId: mono, MsgId: b1})
	wantErr(t, "a thread of a monoforum", err, mtproto.ErrMsgIdInvalid)
}

// TestSublistList: the admins list the sublists, the one written to last
// first, each with its last message and what the subscriber wrote that
// they have not read; a subscriber does not.
func TestSublistList(t *testing.T) {
	f := newDiffFixture(t)
	creator, alice, bob := f.user("Creator"), f.user("Alice"), f.user("Bob")
	id := f.directChannel(creator)
	f.public(id)
	mono := f.monoforumOf(creator, id)
	a1 := f.inSublist(mono, alice, alice, 0)
	b1 := f.inSublist(mono, bob, bob, 0)
	a2 := f.inSublist(mono, alice, alice, 0)
	b2 := f.inSublist(mono, creator, bob, id)
	_ = a1

	list := func(in *channel.TLChannelGetSublists) *mtproto.Messages_SavedDialogs {
		t.Helper()
		in.SelfId, in.ChannelId = creator, mono
		r, err := f.c.ChannelGetSublists(in)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	r := list(&channel.TLChannelGetSublists{Limit: 20})
	if r.GetPredicateName() != mtproto.Predicate_messages_savedDialogs || len(r.GetDialogs()) != 2 || len(r.GetMessages()) != 2 {
		t.Fatalf("the list: %v", r)
	}
	first, second := r.GetDialogs()[0], r.GetDialogs()[1]
	if first.GetPredicateName() != mtproto.Predicate_monoForumDialog || first.GetPeer().GetUserId() != bob || first.GetTopMessage() != b2 ||
		first.GetUnreadCount() != 0 || first.GetReadInboxMaxId() != b2 || first.GetReadOutboxMaxId() != b1 {
		t.Errorf("bob's sublist: %v", first)
	}
	if second.GetPeer().GetUserId() != alice || second.GetTopMessage() != a2 || second.GetUnreadCount() != 2 || second.GetReadInboxMaxId() != 0 || second.GetReadOutboxMaxId() != a2 {
		t.Errorf("alice's sublist: %v", second)
	}
	if r = list(&channel.TLChannelGetSublists{Limit: 1}); r.GetPredicateName() != mtproto.Predicate_messages_savedDialogsSlice || r.GetCount() != 2 || len(r.GetDialogs()) != 1 {
		t.Errorf("the first page: %v", r)
	}
	if r = list(&channel.TLChannelGetSublists{Limit: 1, OffsetId: b2}); len(r.GetDialogs()) != 1 || r.GetDialogs()[0].GetPeer().GetUserId() != alice || r.GetPredicateName() != mtproto.Predicate_messages_savedDialogs {
		t.Errorf("the second page: %v", r)
	}
	if r = list(&channel.TLChannelGetSublists{Ids: []int64{alice, 12345}}); len(r.GetDialogs()) != 1 || r.GetDialogs()[0].GetPeer().GetUserId() != alice {
		t.Errorf("the sublists by id: %v", r)
	}
	_, err := f.c.ChannelGetSublists(&channel.TLChannelGetSublists{SelfId: alice, ChannelId: mono, Limit: 20})
	wantErr(t, "a subscriber lists the sublists", err, mtproto.ErrChatAdminRequired)
	_, err = f.c.ChannelGetSublists(&channel.TLChannelGetSublists{SelfId: creator, ChannelId: id, Limit: 20})
	wantErr(t, "the sublists of a channel that is no monoforum", err, dao.ErrParentPeerInvalid)

	h, err := f.c.ChannelGetSublistHistory(&channel.TLChannelGetSublistHistory{SelfId: creator, ChannelId: mono, PeerId: bob, Limit: 50})
	if err != nil || !slices.Equal(ids(h.GetMessages()), []int32{b2, b1}) || h.GetCount() != 2 {
		t.Errorf("the history of bob's sublist: %v, %v", ids(h.GetMessages()), err)
	}
	_, err = f.c.ChannelGetSublistHistory(&channel.TLChannelGetSublistHistory{SelfId: bob, ChannelId: mono, PeerId: alice, Limit: 50})
	wantErr(t, "a subscriber reads a sublist as an admin", err, mtproto.ErrChatAdminRequired)

	author, err := f.c.ChannelGetMessageAuthor(&channel.TLChannelGetMessageAuthor{SelfId: creator, ChannelId: mono, Id: b2})
	if err != nil || author.GetV() != creator {
		t.Errorf("the author of the channel's answer: %v, %v", author, err)
	}
	_, err = f.c.ChannelGetMessageAuthor(&channel.TLChannelGetMessageAuthor{SelfId: creator, ChannelId: mono, Id: b1})
	wantErr(t, "the author of a subscriber's message", err, mtproto.ErrMsgIdInvalid)
	_, err = f.c.ChannelGetMessageAuthor(&channel.TLChannelGetMessageAuthor{SelfId: bob, ChannelId: mono, Id: b2})
	wantErr(t, "a subscriber asks who answered", err, mtproto.ErrChatAdminRequired)
	_, err = f.c.ChannelGetMessageAuthor(&channel.TLChannelGetMessageAuthor{SelfId: creator, ChannelId: id, Id: 1})
	wantErr(t, "the author of a post of a channel", err, mtproto.ErrChannelInvalid)
}

// TestSublistRead: an admin's read of a sublist is the admins' one mark,
// told to the other admins and, when its messages were read, to the
// subscriber; a subscriber's read is its own mark with seq, told to the
// admins as the outbox of the sublist.
func TestSublistRead(t *testing.T) {
	f := newDiffFixture(t)
	creator, manager, alice, bob := f.user("Creator"), f.user("Manager"), f.user("Alice"), f.user("Bob")
	id := f.directChannel(creator)
	f.public(id)
	f.subscriber(id, manager, channel.RoleAdmin, mtproto.ADMIN_MANAGE_DIRECT_MESSAGES)
	mono := f.monoforumOf(creator, id)
	f.showDialog(creator, mono)
	a1 := f.inSublist(mono, alice, alice, 0)
	a2 := f.inSublist(mono, alice, alice, 0)
	b1 := f.inSublist(mono, bob, bob, 0)

	readSublist := func(self, channelId, peer int64, maxId int32) error {
		_, err := f.c.ChannelReadSublist(&channel.TLChannelReadSublist{SelfId: self, PermAuthKeyId: 1, ChannelId: channelId, PeerId: peer, MaxId: maxId})
		return err
	}
	wantErr(t, "a subscriber reads a sublist", readSublist(bob, mono, alice, a1), mtproto.ErrChatAdminRequired)
	wantErr(t, "a sublist of a channel that is no monoforum", readSublist(creator, id, alice, a1), dao.ErrParentPeerInvalid)

	if err := readSublist(creator, mono, alice, a1); err != nil {
		t.Fatal(err)
	}
	row, _, err := monoforum.Sublist(monoforum.OnDB(context.Background(), f.c.svcCtx.Dao.Mysql.DB), mono, alice)
	if err != nil || row.ReadInboxMaxId != a1 {
		t.Fatalf("the admins' mark of alice's sublist: %v, %v; want %d", row, err, a1)
	}
	if n := f.pushesTo(manager, mtproto.Predicate_updateReadMonoForumInbox); n != 1 {
		t.Errorf("the other admin got %d updateReadMonoForumInbox, want 1", n)
	}
	if n := f.pushesTo(creator, mtproto.Predicate_updateReadMonoForumInbox); n != 1 {
		t.Errorf("the reader's other devices got %d updateReadMonoForumInbox, want 1", n)
	}
	if n := f.pushesTo(alice, mtproto.Predicate_updateReadChannelOutbox); n != 1 {
		t.Errorf("alice got %d updateReadChannelOutbox, want 1", n)
	}
	if n := f.pushesTo(bob, mtproto.Predicate_updateReadChannelOutbox) + f.pushesTo(bob, mtproto.Predicate_updateReadMonoForumInbox); n != 0 {
		t.Errorf("bob was told of alice's sublist %d times", n)
	}
	if err = readSublist(manager, mono, alice, a1); err != nil {
		t.Fatal(err)
	}
	if n := f.pushesTo(creator, mtproto.Predicate_updateReadMonoForumInbox); n != 1 {
		t.Errorf("a read behind the mark was told: %d", n)
	}
	if dl := f.dialog(creator, mono); dl.GetUnreadCount() != 2 || dl.GetReadInboxMaxId() != 0 {
		t.Errorf("the creator's dialog after one read: %v", dl)
	}

	// An admin who reads the whole monoforum reads every sublist.
	if err = f.read(manager, mono, 0); err != nil {
		t.Fatal(err)
	}
	f.showDialog(manager, mono)
	if dl := f.dialog(manager, mono); dl.GetUnreadCount() != 0 || dl.GetReadInboxMaxId() != b1 {
		t.Errorf("the manager's dialog after reading all: %v", dl)
	}
	if row, _, err = monoforum.Sublist(monoforum.OnDB(context.Background(), f.c.svcCtx.Dao.Mysql.DB), mono, bob); err != nil || row.ReadInboxMaxId != b1 {
		t.Errorf("the admins' mark of bob's sublist: %v, %v", row, err)
	}
	_ = a2

	// The channel answers alice; she reads it.
	c1 := f.inSublist(mono, creator, alice, id)
	before := f.pushesTo(manager, mtproto.Predicate_updateReadMonoForumOutbox)
	if err = f.read(alice, mono, 0); err != nil {
		t.Fatal(err)
	}
	us := f.readUpdates(alice)
	if len(us) != 1 || us[0].GetMaxId() != c1 || us[0].GetStillUnreadCount() != 0 || us[0].GetChannelId() != mono {
		t.Errorf("alice's updateReadChannelInbox: %v", us)
	}
	if row, _, err = monoforum.Sublist(monoforum.OnDB(context.Background(), f.c.svcCtx.Dao.Mysql.DB), mono, alice); err != nil || row.ReadOutboxMaxId != c1 {
		t.Errorf("how far alice read: %v, %v; want %d", row, err, c1)
	}
	if n := f.pushesTo(manager, mtproto.Predicate_updateReadMonoForumOutbox); n != before+1 {
		t.Errorf("the admins got %d updateReadMonoForumOutbox, want one more than %d", n, before)
	}
	if dl := f.dialog(alice, mono); dl.GetUnreadCount() != 0 || dl.GetReadOutboxMaxId() != c1 {
		t.Errorf("alice's dialog: %v", dl)
	}

	marked, err := f.c.ChannelMarkSublistUnread(&channel.TLChannelMarkSublistUnread{SelfId: creator, PermAuthKeyId: 1, ChannelId: mono, PeerId: bob, Unread: mtproto.BoolTrue})
	if err != nil || !mtproto.FromBool(marked) {
		t.Fatalf("marking bob's sublist unread: %v, %v", marked, err)
	}
	marks, err := f.c.ChannelGetSublistUnreadMarks(&channel.TLChannelGetSublistUnreadMarks{SelfId: manager, ChannelId: mono})
	if err != nil || !slices.Equal(marks.GetDatas(), []int64{bob}) {
		t.Errorf("the unread marks: %v, %v", marks.GetDatas(), err)
	}
	if n := f.pushesTo(manager, mtproto.Predicate_updateDialogUnreadMark); n != 1 {
		t.Errorf("the other admin got %d updateDialogUnreadMark, want 1", n)
	}
	if err = readSublist(manager, mono, bob, 0); err != nil {
		t.Fatal(err)
	}
	if marks, err = f.c.ChannelGetSublistUnreadMarks(&channel.TLChannelGetSublistUnreadMarks{SelfId: manager, ChannelId: mono}); err != nil || len(marks.GetDatas()) != 0 {
		t.Errorf("the unread marks after a read: %v, %v", marks.GetDatas(), err)
	}
	_, err = f.c.ChannelMarkSublistUnread(&channel.TLChannelMarkSublistUnread{SelfId: alice, PermAuthKeyId: 1, ChannelId: mono, PeerId: bob, Unread: mtproto.BoolTrue})
	wantErr(t, "a subscriber marks a sublist", err, mtproto.ErrChatAdminRequired)
}

// TestSublistDelete: a subscriber deletes its own messages of its sublist,
// not the channel's answers, and nothing of another's; an admin deletes
// any, and a whole sublist a portion at a time. The sublist's last message
// moves back and an emptied sublist leaves the list.
func TestSublistDelete(t *testing.T) {
	f := newDiffFixture(t)
	creator, alice, bob := f.user("Creator"), f.user("Alice"), f.user("Bob")
	id := f.directChannel(creator)
	f.public(id)
	mono := f.monoforumOf(creator, id)
	a1 := f.inSublist(mono, alice, alice, 0)
	b1 := f.inSublist(mono, bob, bob, 0)
	c1 := f.inSublist(mono, creator, alice, id)
	a2 := f.inSublist(mono, alice, alice, 0)

	r, err := f.delete(alice, mono, b1)
	if err != nil || r.GetPtsCount() != 0 || f.isDeleted(mono, b1) {
		t.Errorf("alice deletes bob's message: %v, %v", r, err)
	}
	_, err = f.delete(alice, mono, c1)
	wantErr(t, "alice deletes the channel's answer", err, mtproto.ErrMessageDeleteForbidden)
	if r, err = f.delete(alice, mono, a2); err != nil || r.GetPtsCount() != 1 || !f.isDeleted(mono, a2) {
		t.Fatalf("alice deletes her own message: %v, %v", r, err)
	}
	q := monoforum.OnDB(context.Background(), f.c.svcCtx.Dao.Mysql.DB)
	if row, _, err := monoforum.Sublist(q, mono, alice); err != nil || row.TopMessage != c1 {
		t.Errorf("the last message of alice's sublist: %v, %v; want %d", row, err, c1)
	}
	if got, _ := f.historyIds(alice, mono); !slices.Equal(got, []int32{a1, c1}) {
		t.Errorf("alice's history after the deletion: %v", got)
	}
	d := f.diff(bob, mono, channelbox.BasePts, 100)
	for _, u := range d.GetOtherUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateDeleteChannelMessages {
			t.Errorf("bob is told of a deletion in alice's sublist: %v", u)
		}
	}

	if r, err = f.delete(creator, mono, b1); err != nil || r.GetPtsCount() != 1 {
		t.Fatalf("the creator deletes bob's message: %v, %v", r, err)
	}
	if row, _, err := monoforum.Sublist(q, mono, bob); err != nil || row.TopMessage != 0 {
		t.Errorf("bob's emptied sublist: %v, %v", row, err)
	}
	list, err := f.c.ChannelGetSublists(&channel.TLChannelGetSublists{SelfId: creator, ChannelId: mono, Limit: 20})
	if err != nil || len(list.GetDialogs()) != 1 || list.GetDialogs()[0].GetPeer().GetUserId() != alice {
		t.Errorf("the list after bob's sublist emptied: %v, %v", list, err)
	}

	_, err = f.c.ChannelDeleteSublistHistory(&channel.TLChannelDeleteSublistHistory{SelfId: alice, PermAuthKeyId: 1, ChannelId: mono, PeerId: alice})
	wantErr(t, "a subscriber deletes a sublist", err, mtproto.ErrChatAdminRequired)
	h, err := f.c.ChannelDeleteSublistHistory(&channel.TLChannelDeleteSublistHistory{SelfId: creator, PermAuthKeyId: 1, ChannelId: mono, PeerId: alice, MaxId: a1})
	if err != nil || h.GetPtsCount() != 1 || h.GetOffset() != 0 || !f.isDeleted(mono, a1) || f.isDeleted(mono, c1) {
		t.Errorf("deleting alice's sublist up to %d: %v, %v", a1, h, err)
	}
	if h, err = f.c.ChannelDeleteSublistHistory(&channel.TLChannelDeleteSublistHistory{SelfId: creator, PermAuthKeyId: 1, ChannelId: mono, PeerId: alice}); err != nil || h.GetPtsCount() != 1 || !f.isDeleted(mono, c1) {
		t.Errorf("deleting the rest of alice's sublist: %v, %v", h, err)
	}
	if h, err = f.c.ChannelDeleteSublistHistory(&channel.TLChannelDeleteSublistHistory{SelfId: creator, PermAuthKeyId: 1, ChannelId: mono, PeerId: alice}); err != nil || h.GetPtsCount() != 0 {
		t.Errorf("deleting an empty sublist: %v, %v", h, err)
	}
	rep, err := channelbox.CheckJournal(context.Background(), f.box, mono, channelbox.BasePts)
	if err != nil || !rep.OK() {
		t.Errorf("the journal of the monoforum: %+v, %v", rep, err)
	}
}

// TestMonoforumLeave: channels.leaveChannel of a monoforum takes the chat
// with the channel out of the dialogs of the one who leaves, and nothing
// else: it is still what it was in the monoforum, and its messages stay.
// One who may not open the monoforum is not in it.
func TestMonoforumLeave(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob, eve := f.user("Creator"), f.user("Bob"), f.user("Eve")
	id := f.directChannel(creator)
	f.subscriber(id, bob, channel.RoleMember, 0)
	mono := f.monoforumOf(creator, id)
	b1 := f.inSublist(mono, bob, bob, 0)

	r, err := f.c.ChannelLeaveChannel(&channel.TLChannelLeaveChannel{SelfId: bob, PermAuthKeyId: 1, ChannelId: mono})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.GetUpdates()) != 0 || len(r.GetChats()) != 1 || r.GetChats()[0].GetLeft() || r.GetChats()[0].GetPredicateName() != mtproto.Predicate_channel {
		t.Errorf("the answer to the leave: %v", r)
	}
	var deleted bool
	if err = f.box.QueryRow("SELECT deleted FROM dialogs WHERE user_id = ? AND peer_type = ? AND peer_id = ?", bob, mtproto.PEER_CHANNEL, mono).Scan(&deleted); err != nil || !deleted {
		t.Errorf("bob's dialog with the monoforum after the leave: deleted %v, %v", deleted, err)
	}
	if got, err := f.historyIds(bob, mono); err != nil || !slices.Equal(got, []int32{b1}) {
		t.Errorf("bob's history after the leave: %v, %v", got, err)
	}
	if c := f.viewOf(bob, mono); c.GetLeft() || c.GetPredicateName() != mtproto.Predicate_channel {
		t.Errorf("the monoforum to bob after the leave: %v", c)
	}
	_, err = f.c.ChannelLeaveChannel(&channel.TLChannelLeaveChannel{SelfId: eve, PermAuthKeyId: 1, ChannelId: mono})
	wantErr(t, "one who may not open the monoforum leaves it", err, mtproto.ErrUserNotParticipant)
}

// TestMonoforumDeletedWithChannel: deleting the channel deletes its
// monoforum, and it leaves the dialogs of everyone who has it: the
// creator's at once, with updateChannel for its devices, a subscriber's
// who is no member by the inbox event.
func TestMonoforumDeletedWithChannel(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob := f.user("Creator"), f.user("Bob")
	id := f.directChannel(creator)
	f.public(id)
	mono := f.monoforumOf(creator, id)
	f.showDialog(creator, mono)
	f.inSublist(mono, bob, bob, 0)

	if _, err := f.deleteChannel(creator, id); err != nil {
		t.Fatal(err)
	}
	var deleted bool
	if err := f.box.QueryRow("SELECT deleted FROM channels WHERE id = ?", mono).Scan(&deleted); err != nil || !deleted {
		t.Errorf("the monoforum after the channel's deletion: deleted %v, %v", deleted, err)
	}
	if n := f.deletedEvents(bob); n != 1 {
		t.Errorf("bob: %d deletion events, want the monoforum's", n)
	}
	var dialogs int
	if err := f.box.QueryRow("SELECT COUNT(*) FROM dialogs WHERE user_id = ? AND peer_type = ? AND peer_id = ? AND deleted = 0", creator, mtproto.PEER_CHANNEL, mono).Scan(&dialogs); err != nil || dialogs != 0 {
		t.Errorf("the creator's dialog with the monoforum: %d, %v", dialogs, err)
	}
	if n := f.seqChannelUpdates(creator, mono); n == 0 {
		t.Error("no updateChannel of the monoforum for the creator's devices")
	}
}
