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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/dao"
)

func (f *diffFixture) firstMessageId(channelId, from int64) int32 {
	f.t.Helper()
	var first int32
	if err := f.box.QueryRow("SELECT MIN(id) FROM channel_messages WHERE channel_id = ? AND sender_user_id = ?", channelId, from).Scan(&first); err != nil {
		f.t.Fatal(err)
	}
	return first
}

// TestSearchAcross: text over the channels of a member, newest first with a
// paging cursor, folders and types; the search of public channels finds
// posts of a channel the reader is not in, and not one it was kicked from;
// an unsigned post names nobody as its author.
func TestSearchAcross(t *testing.T) {
	f := newDiffFixture(t)
	me, other := f.user("Me"), f.user("Other")
	ch1, ch2 := f.create(me), f.create(me)
	f.send(ch1, me, 2)
	f.send(ch2, me, 2)
	a, b := f.firstMessageId(ch1, me), f.firstMessageId(ch2, me)
	f.exec("UPDATE channel_messages SET message = 'Кот на крыше', date2 = 100, message_data = JSON_SET(message_data, '$.date', 100) WHERE channel_id = ? AND id = ?", ch1, a)
	f.exec("UPDATE channel_messages SET message = 'коты в доме', date2 = 200, message_data = JSON_SET(message_data, '$.date', 200) WHERE channel_id = ? AND id = ?", ch2, b)

	search := func(q dao.AcrossQuery) *mtproto.Messages_Messages {
		t.Helper()
		r, err := f.c.ChannelSearchAcross(&channel.TLChannelSearchAcross{
			SelfId: q.SelfId, PublicOnly: mtproto.ToBool(q.PublicOnly), BroadcastsOnly: mtproto.ToBool(q.BroadcastsOnly),
			GroupsOnly: mtproto.ToBool(q.GroupsOnly), FolderMode: q.FolderMode, FolderChannels: q.FolderChannels,
			Q: q.Q, MediaType: mtproto.MEDIA_EMPTY, OffsetRate: q.OffsetRate, OffsetChannelId: q.OffsetChannelId,
			OffsetId: q.OffsetId, Limit: q.Limit,
		})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	ids := func(r *mtproto.Messages_Messages) string {
		var out []string
		for _, m := range r.GetMessages() {
			out = append(out, fmt.Sprintf("%d:%d", m.GetPeerId().GetChannelId(), m.GetId()))
		}
		return fmt.Sprint(out)
	}
	want := func(pairs ...any) string { return fmt.Sprint(formatPairs(pairs...)) }

	r := search(dao.AcrossQuery{SelfId: me, Q: "кота", Limit: 1})
	if ids(r) != want(ch2, b) || r.GetCount() != 2 || r.GetNextRate().GetValue() != 200 {
		t.Fatalf("first page: %s count %d rate %v", ids(r), r.GetCount(), r.GetNextRate())
	}
	r = search(dao.AcrossQuery{SelfId: me, Q: "кота", Limit: 1, OffsetRate: 200, OffsetChannelId: ch2, OffsetId: b})
	if ids(r) != want(ch1, a) {
		t.Fatalf("next page: %s", ids(r))
	}
	if r := search(dao.AcrossQuery{SelfId: me, Q: "кота", FolderMode: dao.FolderKeep, FolderChannels: []int64{ch1}, Limit: 10}); ids(r) != want(ch1, a) {
		t.Errorf("only a folder: %s", ids(r))
	}
	if r := search(dao.AcrossQuery{SelfId: me, Q: "кота", FolderMode: dao.FolderDrop, FolderChannels: []int64{ch1}, Limit: 10}); ids(r) != want(ch2, b) {
		t.Errorf("not the folder: %s", ids(r))
	}
	if r := search(dao.AcrossQuery{SelfId: me, Q: "кота", BroadcastsOnly: true, Limit: 10}); len(r.GetMessages()) != 0 {
		t.Errorf("broadcasts only of two groups: %s", ids(r))
	}
	if r := search(dao.AcrossQuery{SelfId: other, Q: "кота", Limit: 10}); len(r.GetMessages()) != 0 {
		t.Errorf("no channels: %s", ids(r))
	}

	// Public posts: a broadcast channel with a username.
	f.exec("UPDATE channels SET broadcast = 1, megagroup = 0 WHERE id = ?", ch1)
	f.public(ch1)
	r = search(dao.AcrossQuery{SelfId: other, PublicOnly: true, BroadcastsOnly: true, Q: "кот", Limit: 10})
	if ids(r) != want(ch1, a) {
		t.Fatalf("public posts: %s", ids(r))
	}
	f.exec("INSERT INTO channel_participants (channel_id, user_id, state, banned_rights) VALUES (?, ?, 2, 1)", ch1, other)
	if r := search(dao.AcrossQuery{SelfId: other, PublicOnly: true, BroadcastsOnly: true, Q: "кот", Limit: 10}); len(r.GetMessages()) != 0 {
		t.Errorf("public posts of a channel it is kicked from: %s", ids(r))
	}
}

func formatPairs(pairs ...any) []string {
	var out []string
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, fmt.Sprintf("%v:%v", pairs[i], pairs[i+1]))
	}
	return out
}

// An anonymous post (no from_id) does not show under its writer's id, a
// signed one does; a post copy shows under the channel; the counters and
// mention filter.
func TestSearchAuthors(t *testing.T) {
	f := newDiffFixture(t)
	me := f.user("Me")
	id := f.create(me)
	f.send(id, me, 3)
	first := f.firstMessageId(id, me)
	f.exec("UPDATE channel_messages SET message_data = JSON_REMOVE(message_data, '$.from_id') WHERE channel_id = ? AND id = ?", id, first)
	f.exec("UPDATE channel_messages SET message_data = JSON_SET(message_data, '$.from_id', JSON_OBJECT('predicate_name', ?, 'channel_id', ?)) WHERE channel_id = ? AND id = ?",
		mtproto.Predicate_peerChannel, 99, id, first+1)

	byUser, err := f.c.ChannelSearch(&channel.TLChannelSearch{SelfId: me, ChannelId: id, FromId: me, MediaType: mtproto.MEDIA_EMPTY, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if byUser.GetCount() != 1 || byUser.GetMessages()[0].GetId() != first+2 {
		t.Errorf("under the writer, only its signed message: %d %v", byUser.GetCount(), byUser.GetMessages())
	}
	byChannel, err := f.c.ChannelSearch(&channel.TLChannelSearch{SelfId: me, ChannelId: id, FromId: -99, MediaType: mtproto.MEDIA_EMPTY, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(byChannel.GetMessages()) != 1 || byChannel.GetMessages()[0].GetId() != first+1 {
		t.Errorf("by channel: %v", byChannel.GetMessages())
	}
	n, err := f.c.ChannelSearchCount(&channel.TLChannelSearchCount{SelfId: me, ChannelId: id, MediaType: mtproto.MEDIA_EMPTY})
	if err != nil || n.GetV() < 2 {
		t.Fatalf("count: %v %v", n, err)
	}
}

// The calendar and the positions of the media of a channel.
func TestChannelCalendarAndPositions(t *testing.T) {
	f := newDiffFixture(t)
	me := f.user("Me")
	id := f.create(me)
	f.send(id, me, 3)
	first := f.firstMessageId(id, me)
	const day = 86400
	for i := int32(0); i < 3; i++ {
		f.exec("UPDATE channel_messages SET message_filter_type = ?, date2 = ? WHERE channel_id = ? AND id = ?",
			mtproto.MEDIA_PHOTOS_ONLY, int32(5+i)*day+1, id, first+i)
	}
	cal, err := f.c.ChannelSearchCalendar(&channel.TLChannelSearchCalendar{SelfId: me, ChannelId: id, MediaType: mtproto.MEDIA_PHOTOS_ONLY})
	if err != nil {
		t.Fatal(err)
	}
	if len(cal.GetPeriods()) != 3 || len(cal.GetMessages()) != 3 || cal.GetCount() != 3 || cal.GetMinMsgId() != first {
		t.Fatalf("calendar: %d periods %d msgs count %d min %d", len(cal.GetPeriods()), len(cal.GetMessages()), cal.GetCount(), cal.GetMinMsgId())
	}
	for i, p := range cal.GetPeriods() {
		if cal.GetMessages()[i].GetId() != p.GetMinMsgId() {
			t.Errorf("messages[%d] is %d, period min %d", i, cal.GetMessages()[i].GetId(), p.GetMinMsgId())
		}
	}
	pos, err := f.c.ChannelSearchPositions(&channel.TLChannelSearchPositions{SelfId: me, ChannelId: id, MediaType: mtproto.MEDIA_PHOTOS_ONLY, Limit: 100})
	if err != nil || pos.GetCount() != 3 || len(pos.GetPositions()) != 3 || pos.GetPositions()[0].GetOffset() != 0 {
		t.Fatalf("positions: %v %v", pos, err)
	}
}
