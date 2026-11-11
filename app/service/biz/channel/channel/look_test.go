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
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// A color and a pattern each leave their flag out when none, and neither
// is no peerColor at all; a status without an end has no until.
func TestMakePeerColorAndStatus(t *testing.T) {
	if c := MakePeerColor(NoColor, 0); c != nil {
		t.Errorf("none: %v", c)
	}
	if c := MakePeerColor(0, 0); c.GetColor() == nil || c.GetColor().GetValue() != 0 || c.GetBackgroundEmojiId_FLAGINT64() != nil {
		t.Errorf("color 0, the red one, is a color: %v", c)
	}
	if c := MakePeerColor(NoColor, 9); c.GetColor() != nil || c.GetBackgroundEmojiId_FLAGINT64().GetValue() != 9 {
		t.Errorf("a pattern alone: %v", c)
	}
	if s := MakeEmojiStatus(0, 5); s != nil {
		t.Errorf("no emoji: %v", s)
	}
	if s := MakeEmojiStatus(4, 0); s.GetPredicateName() != mtproto.Predicate_emojiStatus || s.GetDocumentId() != 4 || s.GetUntil_FLAGINT32() != nil {
		t.Errorf("for good: %v", s)
	}
	if s := MakeEmojiStatus(4, 10); s.GetUntil_FLAGINT32().GetValue() != 10 {
		t.Errorf("until 10: %v", s)
	}
}

// The look goes to members and to a min channel; the usernames are listed
// only when there are more than the editable one, which stays username.
func TestChatLook(t *testing.T) {
	ch := &ImmutableChannel{
		Id: 5, AccessHash: 6, Title: "c", Broadcast: true, Username: "main", Date: 1,
		Color:        MakePeerColor(2, 0),
		ProfileColor: MakePeerColor(NoColor, 8),
		EmojiStatus:  MakeEmojiStatus(11, 0),
		Usernames: []*mtproto.Username{
			mtproto.MakeTLUsername(&mtproto.Username{Editable: true, Active: true, Username: "main"}).To_Username(),
		},
	}
	member := &ImmutableChannelParticipant{ChannelId: 5, UserId: 1, Role: RoleMember, State: StateIn, Date: 1}
	for name, c := range map[string]*mtproto.Chat{"member": ch.ToChat(member, 2), "min": ch.ToMinChat()} {
		if c.GetColor_FLAGPEERCOLOR().GetColor().GetValue() != 2 || c.GetProfileColor().GetBackgroundEmojiId_FLAGINT64().GetValue() != 8 ||
			c.GetEmojiStatus().GetDocumentId() != 11 {
			t.Errorf("%s: color %v, profile %v, status %v", name, c.GetColor_FLAGPEERCOLOR(), c.GetProfileColor(), c.GetEmojiStatus())
		}
		if c.GetUsername().GetValue() != "main" || c.GetUsernames() != nil {
			t.Errorf("%s with one username: %v, %v", name, c.GetUsername(), c.GetUsernames())
		}
	}
	ch.Usernames = append(ch.Usernames, mtproto.MakeTLUsername(&mtproto.Username{Username: "extra"}).To_Username())
	if c := ch.ToChat(member, 2); c.GetUsername().GetValue() != "main" || len(c.GetUsernames()) != 2 || c.GetUsernames()[1].GetActive() {
		t.Errorf("with two: %v, %v", c.GetUsername(), c.GetUsernames())
	}
}
