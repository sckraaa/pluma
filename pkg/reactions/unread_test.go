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

package reactions

import "testing"

func TestWithUnreadOf(t *testing.T) {
	rows := []Row{
		{UserId: 2, Reaction: thumbsUp, Unread: true, Date: 2},
		{UserId: 1, Reaction: thumbsUp, Unread: true, Date: 1},
	}
	view := WithUnreadOf(ViewOrEmpty(rows, 0, 1, Group), rows, 1)
	for _, p := range view.GetRecentReactions() {
		if want := p.GetPeerId().GetUserId() == 2; p.GetUnread() != want {
			t.Errorf("reaction of %d: unread %v, want %v", p.GetPeerId().GetUserId(), p.GetUnread(), want)
		}
	}
	none := WithUnreadOf(ViewOrEmpty(rows, 0, 1, Group), rows, 0)
	for _, p := range none.GetRecentReactions() {
		if p.GetUnread() {
			t.Error("unread with no author")
		}
	}
}

const thumbsUp = "e\xf0\x9f\x91\x8d"
