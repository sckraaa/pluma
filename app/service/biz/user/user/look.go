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

package user

import (
	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// How an account looks (task 084): the color of its name and of its
// profile, each with a pattern of custom emoji, and its emoji status.

// NoColor is the color of an account that chose none (users.color,
// users.profile_color): the clients then take the color of the name from
// the id (Android PeerColorActivity.java:1960-1964, tdesktop
// kUnsetColorIndex in edit_peer_color_box.cpp) and show a plain profile.
const NoColor int32 = -1

// MakePeerColor is peerColor of a color and a pattern of custom emoji, nil
// for neither: color NoColor and emojiId 0 are none and leave their flag
// out (core.telegram.org/constructor/peerColor). Color 0 is a color: the
// flag is set for it.
func MakePeerColor(color int32, emojiId int64) *mtproto.PeerColor {
	if color < 0 && emojiId == 0 {
		return nil
	}
	c := &mtproto.PeerColor{}
	if color >= 0 {
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
