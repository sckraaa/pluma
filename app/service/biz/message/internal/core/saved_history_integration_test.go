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

//go:build integration

package core

import (
	"context"
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
)

// TestSavedHistoryOfChannel: the sub-chat of Saved Messages with a channel
// holds the posts saved from it, newest first, and nothing of "My notes" or
// of another channel; the count is its own.
func TestSavedHistoryOfChannel(t *testing.T) {
	const user, channel, other = int64(3001), int64(3101), int64(3102)
	did := mtproto.MakeDialogId(user, mtproto.PEER_USER, user)
	saved := []struct {
		peerType int32
		peerId   int64
	}{
		{mtproto.PEER_CHANNEL, channel},
		{mtproto.PEER_USER, user},
		{mtproto.PEER_CHANNEL, other},
		{mtproto.PEER_CHANNEL, channel},
		{mtproto.PEER_CHANNEL, channel},
	}
	for i, s := range saved {
		if _, err := itRaw.Exec("INSERT INTO messages (user_id, user_message_box_id, dialog_id1, dialog_id2, dialog_message_id, sender_user_id, peer_type, peer_id, message_data, saved_peer_type, saved_peer_id, deleted) VALUES (?, ?, ?, ?, ?, ?, ?, ?, '{}', ?, ?, ?)",
			user, i+1, did.A, did.B, user*100+int64(i), user, mtproto.PEER_USER, user, s.peerType, s.peerId, i == 4); err != nil {
			t.Fatal(err)
		}
	}

	r, err := New(context.Background(), itSvc).MessageGetSavedHistoryMessages(&message.TLMessageGetSavedHistoryMessages{
		UserId: user, PeerType: mtproto.PEER_CHANNEL, PeerId: channel, Limit: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	var ids []int32
	for _, b := range r.GetBoxList() {
		ids = append(ids, b.GetMessageId())
	}
	if want := []int32{4, 1}; !slices.Equal(ids, want) {
		t.Errorf("saved history of the channel %v, want %v", ids, want)
	}
	if r.GetCount() != 2 {
		t.Errorf("count %d, want 2", r.GetCount())
	}
}
