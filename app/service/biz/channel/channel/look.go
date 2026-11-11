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

package channel

import (
	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// How a channel looks and is addressed (task 078).

// The actions of channel.editUsernames.
const (
	// UsernamesToggle shows or hides one username (channels.toggleUsername).
	UsernamesToggle int32 = 1
	// UsernamesReorder orders the shown usernames (channels.reorderUsernames).
	UsernamesReorder int32 = 2
	// UsernamesDeactivateAll hides every username but the editable one
	// (channels.deactivateAllUsernames).
	UsernamesDeactivateAll int32 = 3
)

// NoColor is the color of a channel that has none: the clients then take
// the color from its id (tdesktop kUnsetColorIndex, edit_peer_color_box.cpp).
const NoColor int32 = -1

// MakePeerColor is peerColor of a color and a pattern of custom emoji, nil
// for neither: color NoColor and emojiId 0 are none, and leave their flag
// out (core.telegram.org/constructor/peerColor).
func MakePeerColor(color int32, emojiId int64) *mtproto.PeerColor {
	if color == NoColor && emojiId == 0 {
		return nil
	}
	c := &mtproto.PeerColor{}
	if color != NoColor {
		c.Color = wrapperspb.Int32(color)
	}
	if emojiId != 0 {
		c.BackgroundEmojiId_FLAGINT64 = wrapperspb.Int64(emojiId)
	}
	return mtproto.MakeTLPeerColor(c).To_PeerColor()
}

// MakeEmojiStatus is emojiStatus of a custom emoji until a date (0: no
// end), nil for documentId 0.
func MakeEmojiStatus(documentId int64, until int32) *mtproto.EmojiStatus {
	if documentId == 0 {
		return nil
	}
	s := &mtproto.EmojiStatus{DocumentId: documentId}
	if until != 0 {
		s.Until_FLAGINT32 = wrapperspb.Int32(until)
		s.Until_INT32 = until
	}
	return mtproto.MakeTLEmojiStatus(s).To_EmojiStatus()
}

// EmptyEmojiStatus is emojiStatusEmpty, the status of a channel that has
// none, as the recent actions show a status set or removed.
func EmptyEmojiStatus() *mtproto.EmojiStatus {
	return mtproto.MakeTLEmojiStatusEmpty(nil).To_EmojiStatus()
}
