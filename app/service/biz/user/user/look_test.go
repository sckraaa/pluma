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
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// Color 0 is a color and keeps its flag; no color and no pattern is no
// peerColor at all; a pattern alone is a peerColor without a color.
func TestMakePeerColor(t *testing.T) {
	if c := MakePeerColor(NoColor, 0); c != nil {
		t.Errorf("no color and no pattern: %v, want nil", c)
	}
	c := MakePeerColor(0, 0)
	if c.GetPredicateName() != mtproto.Predicate_peerColor || c.GetColor() == nil || c.GetColor().GetValue() != 0 || c.GetBackgroundEmojiId_FLAGINT64() != nil {
		t.Errorf("color 0: %v, want peerColor with color 0 and no pattern", c)
	}
	c = MakePeerColor(5, 77)
	if c.GetColor().GetValue() != 5 || c.GetBackgroundEmojiId_FLAGINT64().GetValue() != 77 {
		t.Errorf("color 5 with pattern 77: %v", c)
	}
	c = MakePeerColor(NoColor, 77)
	if c == nil || c.GetColor() != nil || c.GetBackgroundEmojiId_FLAGINT64().GetValue() != 77 {
		t.Errorf("a pattern alone: %v, want peerColor without a color", c)
	}
}

// A status has an end only when one was set, and a user whose status has
// ended has none, wherever it was loaded from.
func TestEmojiStatus(t *testing.T) {
	if s := MakeEmojiStatus(0, 0); s != nil {
		t.Errorf("no status: %v", s)
	}
	s := MakeEmojiStatus(9, 0)
	if s.GetPredicateName() != mtproto.Predicate_emojiStatus || s.GetDocumentId() != 9 || s.GetUntil_FLAGINT32() != nil {
		t.Errorf("a status without an end: %v", s)
	}
	now := int32(time.Now().Unix())
	of := func(until int32) *mtproto.EmojiStatus {
		u := &mtproto.ImmutableUser{User: &mtproto.UserData{Id: 1, EmojiStatus: MakeEmojiStatus(9, until)}}
		return u.EmojiStatus()
	}
	if s = of(0); s.GetDocumentId() != 9 {
		t.Errorf("a status without an end: %v", s)
	}
	if s = of(now + 3600); s.GetDocumentId() != 9 || s.GetUntil_FLAGINT32().GetValue() != now+3600 {
		t.Errorf("a status that lasts: %v", s)
	}
	if s = of(now - 1); s != nil {
		t.Errorf("a status that ended: %v, want none", s)
	}
}
