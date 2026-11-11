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
	"reflect"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/pkg/channelcontents"
)

func TestChannelViewUnreadReactions(t *testing.T) {
	in := &sync.TLSyncPushChannelUpdates{
		ChannelId: 5, FromId: 1, Mentioned: []int64{2},
		Updates: mtproto.MakeUpdatesByUpdates(mtproto.MakeTLUpdateMessageReactions(&mtproto.Update{
			Reactions_MESSAGEREACTIONS: &mtproto.MessageReactions{RecentReactions: []*mtproto.MessagePeerReaction{{Unread: true}}},
		}).To_Update()),
	}
	unread := func(member int64) bool {
		u := (&SyncCore{}).channelView(in, member, nil, nil, nil)
		return u.GetUpdates()[0].GetReactions_MESSAGEREACTIONS().GetRecentReactions()[0].GetUnread()
	}
	if !unread(2) {
		t.Error("the author lost the unread mark")
	}
	if unread(3) {
		t.Error("another member sees the unread mark")
	}
}

// TestChannelViewSentAs: a message written as a channel is out for its
// author, the member who sent it, and for nobody else.
func TestChannelViewSentAs(t *testing.T) {
	in := &sync.TLSyncPushChannelUpdates{
		ChannelId: 5, FromId: 1,
		Updates: mtproto.MakeUpdatesByUpdates(mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{
			Message_MESSAGE: mtproto.MakeTLMessage(&mtproto.Message{Id: 9, PeerId: mtproto.MakePeerChannel(5), FromId: mtproto.MakePeerChannel(6)}).To_Message(),
		}).To_Update()),
	}
	out := func(member int64) bool {
		u := (&SyncCore{}).channelView(in, member, nil, nil, nil)
		return u.GetUpdates()[0].GetMessage_MESSAGE().GetOut()
	}
	if !out(1) {
		t.Error("the author's devices see the message as another's")
	}
	if out(2) {
		t.Error("another member sees the message as its own")
	}
}

// TestChannelViewSentAsChangedByAnother: a message written as a channel that
// another member changes, by ticking an item of its checklist, stays out
// for its author (author_id) and is not out for the member who changed it.
func TestChannelViewSentAsChangedByAnother(t *testing.T) {
	const author, ticker = int64(1), int64(2)
	in := &sync.TLSyncPushChannelUpdates{
		ChannelId: 5, FromId: ticker, AuthorId: author,
		Updates: mtproto.MakeUpdatesByUpdates(mtproto.MakeTLUpdateEditChannelMessage(&mtproto.Update{
			Message_MESSAGE: mtproto.MakeTLMessage(&mtproto.Message{Id: 9, PeerId: mtproto.MakePeerChannel(5), FromId: mtproto.MakePeerChannel(6), Out: true}).To_Message(),
		}).To_Update()),
	}
	out := func(member int64) bool {
		u := (&SyncCore{}).channelView(in, member, nil, nil, nil)
		return u.GetUpdates()[0].GetMessage_MESSAGE().GetOut()
	}
	if !out(author) {
		t.Error("the author's devices see the message as another's")
	}
	if out(ticker) {
		t.Error("the member who ticked sees the message as its own")
	}
}

// TestChannelViewVoicePost: a new voice post is unread for every member but
// its author, though the copy pushed has no flag (task 080).
func TestChannelViewVoicePost(t *testing.T) {
	const author = int64(1)
	doc := mtproto.MakeTLDocument(&mtproto.Document{
		Id: 1, MimeType: "audio/ogg",
		Attributes: []*mtproto.DocumentAttribute{mtproto.MakeTLDocumentAttributeAudio(&mtproto.DocumentAttribute{Voice: true}).To_DocumentAttribute()},
	}).To_Document()
	in := &sync.TLSyncPushChannelUpdates{
		ChannelId: 5, FromId: author,
		Updates: mtproto.MakeUpdatesByUpdates(mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{
			Message_MESSAGE: mtproto.MakeTLMessage(&mtproto.Message{
				Id: 9, Post: true, PeerId: mtproto.MakePeerChannel(5),
				Media: mtproto.MakeTLMessageMediaDocument(&mtproto.MessageMedia{Document: doc}).To_MessageMedia(),
			}).To_Message(),
		}).To_Update()),
	}
	unread := func(member int64) bool {
		u := (&SyncCore{}).channelView(in, member, nil, nil, nil)
		return u.GetUpdates()[0].GetMessage_MESSAGE().GetMediaUnread()
	}
	if unread(author) {
		t.Error("the author's devices see its post unread")
	}
	if !unread(2) {
		t.Error("a subscriber sees the new voice post played")
	}
	if in.GetUpdates().GetUpdates()[0].GetMessage_MESSAGE().GetMediaUnread() {
		t.Error("the view changed the update pushed")
	}
}

// authorsQuerier answers the look-up of the authors of channel messages
// with authors (id to sender_user_id) and every other query with no rows.
type authorsQuerier map[int32]int64

func (q authorsQuerier) QueryRowsPartial(_ context.Context, v interface{}, query string, _ ...interface{}) error {
	if !strings.Contains(query, "FROM channel_messages") {
		return nil
	}
	rows := reflect.ValueOf(v).Elem()
	for id, sender := range q {
		r := reflect.New(rows.Type().Elem()).Elem()
		r.FieldByName("Id").SetInt(int64(id))
		r.FieldByName("SenderUserId").SetInt(sender)
		rows.Set(reflect.Append(rows, r))
	}
	return nil
}

// TestChannelViewVoicePostChangedByAnother: a push of a voice post from
// another admin, without author_id (its comments go with its copy in the
// discussion group), keeps it played for its author and unread for the
// admin, by the author stored (task 080).
func TestChannelViewVoicePostChangedByAnother(t *testing.T) {
	const author, admin = int64(1), int64(3)
	doc := mtproto.MakeTLDocument(&mtproto.Document{
		Id: 1, MimeType: "audio/ogg",
		Attributes: []*mtproto.DocumentAttribute{mtproto.MakeTLDocumentAttributeAudio(&mtproto.DocumentAttribute{Voice: true}).To_DocumentAttribute()},
	}).To_Document()
	m := mtproto.MakeTLMessage(&mtproto.Message{
		Id: 9, Post: true, PeerId: mtproto.MakePeerChannel(5),
		Media: mtproto.MakeTLMessageMediaDocument(&mtproto.MessageMedia{Document: doc}).To_MessageMedia(),
	}).To_Message()
	in := &sync.TLSyncPushChannelUpdates{
		ChannelId: 5, FromId: admin,
		Updates: mtproto.MakeUpdatesByUpdates(mtproto.MakeTLUpdateEditChannelMessage(&mtproto.Update{Message_MESSAGE: m}).To_Update()),
	}
	contents, err := channelcontents.LoadAll(context.Background(), authorsQuerier{9: author}, 5, []*mtproto.Message{m})
	if err != nil {
		t.Fatal(err)
	}
	unread := func(member int64) bool {
		u := (&SyncCore{}).channelView(in, member, contents, nil, nil)
		return u.GetUpdates()[0].GetMessage_MESSAGE().GetMediaUnread()
	}
	if unread(author) {
		t.Error("the author sees its post unread after another admin's change")
	}
	if !unread(admin) {
		t.Error("the admin who changed the post sees it played")
	}
}
