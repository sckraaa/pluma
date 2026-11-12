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
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

func sublistNew(id int32, peer int64, pts int32) *mtproto.Update {
	m := foldMessage(id, peer, "text")
	m.SavedPeerId = mtproto.MakePeerUser(peer)
	return mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: m, Pts_INT32: pts, PtsCount: 1}).To_Update()
}

// The difference of a monoforum for a subscriber holds its own sublist
// alone: the messages of the others, their edits, and the ids of theirs in a
// deletion or a pin are left out, and so is an update that names nothing.
func TestFoldSublist(t *testing.T) {
	edited := foldMessage(2, foldOther, "edited")
	edited.SavedPeerId = mtproto.MakePeerUser(foldOther)
	edit := mtproto.MakeTLUpdateEditChannelMessage(&mtproto.Update{Message_MESSAGE: edited, Pts_INT32: 6, PtsCount: 1}).To_Update()
	updates := []*mtproto.Update{
		sublistNew(1, foldViewer, 2),
		sublistNew(2, foldOther, 3),
		sublistNew(3, foldViewer, 4),
		sublistNew(4, foldOther, 5),
		edit,
		foldDelete(8, 3, 4),
		foldPinned(9, 2),
		foldPinned(10, 1),
		mtproto.MakeTLUpdateChannelWebPage(&mtproto.Update{ChannelId: foldChannel, Pts_INT32: 11, PtsCount: 1}).To_Update(),
	}
	var named []int32
	for _, u := range updates {
		named = append(named, idsNamed(u)...)
	}
	if slices.Sort(named); !slices.Equal(named, []int32{1, 2, 3, 4}) {
		t.Fatalf("named ids %v", named)
	}
	f := newChannelFold(foldViewer, 0, 11)
	f.setSublist(foldViewer, map[int32]bool{1: true, 3: true})
	for _, u := range updates {
		f.add(u)
	}
	f.setOwn(map[int32]int64{})
	if got := ids(f.messages()); !slices.Equal(got, []int32{1}) {
		t.Errorf("new messages %v, want 1 alone: 3 was deleted, 2 and 4 are another's", got)
	}
	var kinds []string
	for _, u := range f.otherUpdates() {
		kinds = append(kinds, u.GetPredicateName())
		switch u.GetPredicateName() {
		case mtproto.Predicate_updateDeleteChannelMessages:
			if !slices.Equal(u.GetMessages(), []int32{3}) {
				t.Errorf("the deletion names %v, want 3", u.GetMessages())
			}
		case mtproto.Predicate_updatePinnedChannelMessages:
			if !slices.Equal(u.GetMessages(), []int32{1}) {
				t.Errorf("the pin names %v, want 1", u.GetMessages())
			}
		}
	}
	if want := []string{mtproto.Predicate_updateDeleteChannelMessages, mtproto.Predicate_updatePinnedChannelMessages}; !slices.Equal(kinds, want) {
		t.Errorf("other updates %v, want %v", kinds, want)
	}
}
