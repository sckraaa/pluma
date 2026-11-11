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
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
)

func TestChannelViewUnreadReactions(t *testing.T) {
	in := &sync.TLSyncPushChannelUpdates{
		ChannelId: 5, FromId: 1, Mentioned: []int64{2},
		Updates: mtproto.MakeUpdatesByUpdates(mtproto.MakeTLUpdateMessageReactions(&mtproto.Update{
			Reactions_MESSAGEREACTIONS: &mtproto.MessageReactions{RecentReactions: []*mtproto.MessagePeerReaction{{Unread: true}}},
		}).To_Update()),
	}
	unread := func(member int64) bool {
		u := (&SyncCore{}).channelView(in, member, nil, nil)
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
		u := (&SyncCore{}).channelView(in, member, nil, nil)
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
		u := (&SyncCore{}).channelView(in, member, nil, nil)
		return u.GetUpdates()[0].GetMessage_MESSAGE().GetOut()
	}
	if !out(author) {
		t.Error("the author's devices see the message as another's")
	}
	if out(ticker) {
		t.Error("the member who ticked sees the message as its own")
	}
}
