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

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// voicePost writes a voice post of from to the broadcast channel, as msg
// does (no media_unread in the one copy), and returns its id and pts.
func (f *diffFixture) voicePost(channelId, from int64) (id, pts int32) {
	f.t.Helper()
	_, err := userbox.Run(context.Background(), f.box, from, func(b *userbox.Box) error {
		ch, err := channelbox.Open(b, channelId)
		if err != nil {
			return err
		}
		doc := mtproto.MakeTLDocument(&mtproto.Document{
			Id: 1, MimeType: "audio/ogg",
			Attributes: []*mtproto.DocumentAttribute{mtproto.MakeTLDocumentAttributeAudio(&mtproto.DocumentAttribute{Voice: true, Duration: 3}).To_DocumentAttribute()},
		}).To_Document()
		m := mtproto.MakeTLMessage(&mtproto.Message{
			Id: ch.NextMessageId(), Post: true, PeerId: mtproto.MakePeerChannel(channelId), Date: int32(b.Date()),
			Media: mtproto.MakeTLMessageMediaDocument(&mtproto.MessageMedia{Document: doc}).To_MessageMedia(),
			Views: wrapperspb.Int32(1), Forwards: wrapperspb.Int32(0),
		}).To_Message()
		ch.AddPtsUpdate(mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: m}).To_Update(), 1)
		data, err := jsonx.MarshalToString(m)
		if err != nil {
			return err
		}
		id, pts = m.Id, ch.Pts()
		if _, err = b.Tx().Exec("INSERT INTO channel_messages (channel_id, id, sender_user_id, pts, message_filter_type, message_data, date2) VALUES (?, ?, ?, ?, ?, ?, ?)",
			channelId, m.Id, from, pts, mtproto.GetMediaType(m), data, b.Date()); err != nil {
			return err
		}
		_, err = b.Tx().Exec("UPDATE channels SET top_message = ? WHERE id = ?", m.Id, channelId)
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return id, pts
}

// unreadIn tells whether message id is media_unread in the history of the
// channel self reads, and in its answer to channel.getMessages.
func (f *diffFixture) unreadIn(self, channelId int64, id int32) (history, got bool) {
	f.t.Helper()
	h, err := f.c.ChannelGetHistory(&channel.TLChannelGetHistory{SelfId: self, ChannelId: channelId, Limit: 100})
	if err != nil {
		f.t.Fatal(err)
	}
	found := false
	for _, m := range h.GetMessages() {
		if m.GetId() == id {
			history, found = m.GetMediaUnread(), true
		}
	}
	if !found {
		f.t.Fatalf("message %d is not in the history of %d", id, self)
	}
	r, err := f.c.ChannelGetMessages(&channel.TLChannelGetMessages{SelfId: self, ChannelId: channelId, Id: []int32{id}})
	if err != nil || len(r.GetDatas()) != 1 {
		f.t.Fatalf("getMessages: %v, %v", r.GetDatas(), err)
	}
	return history, r.GetDatas()[0].GetMediaUnread()
}

func (f *diffFixture) topUnread(self, channelId int64) bool {
	f.t.Helper()
	r, err := f.c.ChannelGetTopMessages(&channel.TLChannelGetTopMessages{SelfId: self, IdList: []int64{channelId}})
	if err != nil || len(r.GetDatas()) != 1 {
		f.t.Fatalf("top message: %v, %v", r.GetDatas(), err)
	}
	return r.GetDatas()[0].GetMediaUnread()
}

func (f *diffFixture) readContents(self, channelId int64, ids ...int32) error {
	_, err := f.c.ChannelReadMessageContents(&channel.TLChannelReadMessageContents{SelfId: self, PermAuthKeyId: 1, ChannelId: channelId, Id: ids})
	return err
}

// TestContentsPerSubscriber: the voice posts a subscriber who joins later
// finds are unread for it, and for each subscriber until it plays them;
// they are never unread for their author, nor for one who only looks at
// the public channel. A play is the reader's alone, its devices are told,
// and the plays of a deleted post go with it.
func TestContentsPerSubscriber(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob, carol, outsider := f.user("Creator"), f.user("Bob"), f.user("Carol"), f.user("Out")
	id := f.create(creator)
	f.broadcast(id)
	f.public(id)
	f.post(id, creator)
	voice, _ := f.voicePost(id, creator)
	for _, u := range []int64{bob, carol} {
		if _, err := f.join(u, id, ""); err != nil {
			t.Fatal(err)
		}
	}

	for _, u := range []int64{bob, carol} {
		if h, g := f.unreadIn(u, id, voice); !h || !g {
			t.Errorf("a newcomer: unread in the history %v, in getMessages %v", h, g)
		}
		if !f.topUnread(u, id) {
			t.Error("a newcomer: the top message is read")
		}
	}
	if h, g := f.unreadIn(creator, id, voice); h || g || f.topUnread(creator, id) {
		t.Error("the author sees its own post unread")
	}
	if h, g := f.unreadIn(outsider, id, voice); h || g {
		t.Error("one outside the channel sees the post unread")
	}
	if h, _ := f.unreadIn(bob, id, voice-1); h {
		t.Error("a text post is unread")
	}

	if err := f.readContents(bob, id, voice); err != nil {
		t.Fatal(err)
	}
	if h, g := f.unreadIn(bob, id, voice); h || g || f.topUnread(bob, id) {
		t.Error("bob played the post and it is still unread")
	}
	if h, _ := f.unreadIn(carol, id, voice); !h {
		t.Error("bob's play made the post read for carol")
	}
	us := f.seqUpdates(bob, mtproto.Predicate_updateChannelReadMessagesContents)
	if len(us) != 1 || us[0].GetChannelId() != id || len(us[0].GetMessages()) != 1 || us[0].GetMessages()[0] != voice {
		t.Errorf("bob's devices got %v", us)
	}
	// A second play changes nothing; the author's play writes nothing.
	if err := f.readContents(bob, id, voice); err != nil {
		t.Fatal(err)
	}
	if err := f.readContents(creator, id, voice); err != nil {
		t.Fatal(err)
	}
	var plays int
	if err := f.box.QueryRow("SELECT COUNT(*) FROM channel_contents_read WHERE channel_id = ?", id).Scan(&plays); err != nil || plays != 1 {
		t.Errorf("plays %d, %v; want bob's one", plays, err)
	}

	if _, err := f.delete(creator, id, voice); err != nil {
		t.Fatal(err)
	}
	if err := f.box.QueryRow("SELECT COUNT(*) FROM channel_contents_read WHERE channel_id = ?", id).Scan(&plays); err != nil || plays != 0 {
		t.Errorf("plays of a deleted post %d, %v", plays, err)
	}
}

// TestContentsInDifference: a voice post that comes in the difference is
// unread for a subscriber, read once it played it, and never for its
// author.
func TestContentsInDifference(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob := f.user("Creator"), f.user("Bob")
	id := f.create(creator)
	f.broadcast(id)
	f.public(id)
	if _, err := f.join(bob, id, ""); err != nil {
		t.Fatal(err)
	}
	from := f.send(id, creator, 1)
	voice, _ := f.voicePost(id, creator)
	unread := func(self int64) bool {
		d := f.diff(self, id, from, 100)
		for _, m := range append(d.GetNewMessages(), d.GetMessages()...) {
			if m.GetId() == voice {
				return m.GetMediaUnread()
			}
		}
		t.Fatalf("%s: no post %d in %v", d.GetPredicateName(), voice, ids(d.GetNewMessages()))
		return false
	}
	if !unread(bob) {
		t.Error("the new voice post is read for a subscriber")
	}
	if unread(creator) {
		t.Error("the author's post is unread for it")
	}
	if err := f.readContents(bob, id, voice); err != nil {
		t.Fatal(err)
	}
	if unread(bob) {
		t.Error("the played post is unread in the difference")
	}
}

// TestContentsMentions: in a supergroup an unread mention is media_unread
// and mentioned, as the clients count unread mentions (tdesktop
// HistoryItem::isUnreadMention); read, it stays mentioned only.
func TestContentsMentions(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob := f.user("Creator"), f.user("Bob")
	id := f.create(creator)
	f.public(id)
	if _, err := f.join(bob, id, ""); err != nil {
		t.Fatal(err)
	}
	f.send(id, creator, 2)
	last := f.top(id)
	f.exec("INSERT INTO channel_message_mentions (channel_id, user_id, message_id) VALUES (?, ?, ?)", id, bob, last)
	view := func() *mtproto.Message {
		r, err := f.c.ChannelGetMessages(&channel.TLChannelGetMessages{SelfId: bob, ChannelId: id, Id: []int32{last}})
		if err != nil || len(r.GetDatas()) != 1 {
			t.Fatalf("getMessages: %v, %v", r.GetDatas(), err)
		}
		return r.GetDatas()[0]
	}
	if m := view(); !m.GetMentioned() || !m.GetMediaUnread() {
		t.Errorf("unread mention: mentioned %v, media_unread %v", m.GetMentioned(), m.GetMediaUnread())
	}
	r, err := f.c.ChannelGetUnreadMentions(&channel.TLChannelGetUnreadMentions{SelfId: bob, ChannelId: id, Limit: 10})
	if err != nil || len(r.GetMessages()) != 1 || !r.GetMessages()[0].GetMediaUnread() {
		t.Errorf("getUnreadMentions: %v, %v", r.GetMessages(), err)
	}
	if err = f.readContents(bob, id, last); err != nil {
		t.Fatal(err)
	}
	if m := view(); !m.GetMentioned() || m.GetMediaUnread() {
		t.Errorf("read mention: mentioned %v, media_unread %v", m.GetMentioned(), m.GetMediaUnread())
	}
}
