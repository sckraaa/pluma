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

const (
	foldChannel = int64(1000000001)
	foldViewer  = int64(7)
	foldOther   = int64(8)
)

func foldMessage(id int32, from int64, text string) *mtproto.Message {
	return mtproto.MakeTLMessage(&mtproto.Message{
		Id:      id,
		FromId:  mtproto.MakePeerUser(from),
		PeerId:  mtproto.MakePeerChannel(foldChannel),
		Message: text,
	}).To_Message()
}

func foldNew(id int32, from int64, text string, pts int32) *mtproto.Update {
	return mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: foldMessage(id, from, text), Pts_INT32: pts, PtsCount: 1}).To_Update()
}

func foldEdit(id int32, from int64, text string, pts int32) *mtproto.Update {
	return mtproto.MakeTLUpdateEditChannelMessage(&mtproto.Update{Message_MESSAGE: foldMessage(id, from, text), Pts_INT32: pts, PtsCount: 1}).To_Update()
}

func foldDelete(pts int32, ids ...int32) *mtproto.Update {
	return mtproto.MakeTLUpdateDeleteChannelMessages(&mtproto.Update{ChannelId: foldChannel, Messages: ids, Pts_INT32: pts, PtsCount: int32(len(ids))}).To_Update()
}

func foldPinned(pts int32, ids ...int32) *mtproto.Update {
	return mtproto.MakeTLUpdatePinnedChannelMessages(&mtproto.Update{ChannelId: foldChannel, Messages: ids, Pts_INT32: pts, PtsCount: 1}).To_Update()
}

func ids(messages []*mtproto.Message) []int32 {
	out := make([]int32, 0, len(messages))
	for _, m := range messages {
		out = append(out, m.GetId())
	}
	return out
}

func names(updates []*mtproto.Update) []string {
	out := make([]string, 0, len(updates))
	for _, u := range updates {
		out = append(out, u.PredicateName)
	}
	return out
}

// TestFoldNewMessages: new messages come once, in their last edit, in
// ascending id order, without those deleted later in the range, as the
// viewer sees them.
func TestFoldNewMessages(t *testing.T) {
	f := newChannelFold(foldViewer, 0, 20)
	f.add(foldNew(12, foldOther, "b", 11))
	f.add(foldNew(11, foldViewer, "a", 12))
	f.add(foldNew(13, foldOther, "c", 13))
	f.add(foldEdit(11, foldViewer, "a2", 14))
	f.add(foldDelete(15, 13))

	got := f.messages()
	if !slices.Equal(ids(got), []int32{11, 12}) {
		t.Fatalf("new_messages %v, want [11 12]", ids(got))
	}
	if got[0].GetMessage() != "a2" {
		t.Errorf("message 11 is %q, want its last edit", got[0].GetMessage())
	}
	if !got[0].GetOut() || got[1].GetOut() {
		t.Errorf("out of 11, 12: %v, %v; want the viewer's own only", got[0].GetOut(), got[1].GetOut())
	}
	// The deletion of a message new in the range still goes: a client that
	// had it from a push drops it.
	other := f.otherUpdates()
	if !slices.Equal(names(other), []string{mtproto.Predicate_updateDeleteChannelMessages}) {
		t.Fatalf("other_updates %v", names(other))
	}
}

// TestFoldOlderMessages: an edit of an older message comes once, in its
// last state, and not at all when it is deleted afterwards; deletions merge
// into one where the last was; every update has the final pts and no
// pts_count.
func TestFoldOlderMessages(t *testing.T) {
	f := newChannelFold(foldViewer, 0, 30)
	f.add(foldEdit(3, foldOther, "x1", 21))
	f.add(foldEdit(4, foldOther, "y", 22))
	f.add(foldDelete(23, 1))
	f.add(foldPinned(24, 2))
	f.add(foldEdit(3, foldOther, "x2", 25))
	f.add(foldDelete(27, 4, 5))

	other := f.otherUpdates()
	want := []string{
		mtproto.Predicate_updatePinnedChannelMessages,
		mtproto.Predicate_updateEditChannelMessage,
		mtproto.Predicate_updateDeleteChannelMessages,
	}
	if !slices.Equal(names(other), want) {
		t.Fatalf("other_updates %v, want %v", names(other), want)
	}
	if m := other[1].GetMessage_MESSAGE(); m.GetId() != 3 || m.GetMessage() != "x2" {
		t.Errorf("edit %d %q, want 3 in its last state", m.GetId(), m.GetMessage())
	}
	if got := other[2].GetMessages(); !slices.Equal(got, []int32{1, 4, 5}) {
		t.Errorf("deleted %v, want [1 4 5]", got)
	}
	for _, u := range other {
		if u.GetPts_INT32() != 30 || u.GetPtsCount() != 0 {
			t.Errorf("%s has pts %d count %d, want 30 and 0", u.PredicateName, u.GetPts_INT32(), u.GetPtsCount())
		}
	}
	if len(f.messages()) != 0 {
		t.Errorf("new_messages %v, want none", ids(f.messages()))
	}
}

// TestFoldAvailableMinId: nothing of a message below the viewer's
// available_min_id comes, not even its edits, deletions or pins.
func TestFoldAvailableMinId(t *testing.T) {
	f := newChannelFold(foldViewer, 10, 40)
	f.add(foldNew(9, foldOther, "hidden", 31))
	f.add(foldNew(10, foldOther, "seen", 32))
	f.add(foldEdit(8, foldOther, "hidden", 33))
	f.add(foldDelete(35, 7, 8))
	f.add(foldPinned(36, 9))
	f.add(foldDelete(37, 6, 11))

	if got := ids(f.messages()); !slices.Equal(got, []int32{10}) {
		t.Fatalf("new_messages %v, want [10]", got)
	}
	other := f.otherUpdates()
	if !slices.Equal(names(other), []string{mtproto.Predicate_updateDeleteChannelMessages}) {
		t.Fatalf("other_updates %v", names(other))
	}
	if got := other[0].GetMessages(); !slices.Equal(got, []int32{11}) {
		t.Errorf("deleted %v, want [11]", got)
	}
}

// TestFoldMessageIds: updateMessageID of the viewer's own new messages
// comes first, in id order, for those with a random_id.
func TestFoldMessageIds(t *testing.T) {
	f := newChannelFold(foldViewer, 0, 50)
	f.add(foldDelete(41, 1))
	f.add(foldNew(6, foldViewer, "b", 42))
	f.add(foldNew(5, foldViewer, "a", 43))
	f.add(foldNew(7, foldOther, "c", 44))
	f.setOwn(map[int32]int64{5: 55, 6: 66})

	other := f.otherUpdates()
	want := []string{mtproto.Predicate_updateMessageID, mtproto.Predicate_updateMessageID, mtproto.Predicate_updateDeleteChannelMessages}
	if !slices.Equal(names(other), want) {
		t.Fatalf("other_updates %v, want %v", names(other), want)
	}
	if other[0].GetId_INT32() != 5 || other[0].GetRandomId() != 55 || other[1].GetId_INT32() != 6 || other[1].GetRandomId() != 66 {
		t.Errorf("message ids %v", other[:2])
	}
}

// TestFoldSentAs: a message written as a channel (send_as) is out for its
// author alone, new or edited, and for nobody without setOwn.
func TestFoldSentAs(t *testing.T) {
	sentAs := func(u *mtproto.Update) *mtproto.Update {
		u.GetMessage_MESSAGE().FromId = mtproto.MakePeerChannel(foldChannel + 1)
		return u
	}
	f := newChannelFold(foldViewer, 0, 30)
	f.add(foldEdit(3, foldViewer, "old", 21))
	f.add(sentAs(foldEdit(4, foldViewer, "old as", 22)))
	f.add(sentAs(foldNew(25, foldViewer, "mine", 23)))
	f.add(sentAs(foldNew(26, foldOther, "theirs", 24)))
	if ids := f.viewedIds(); !slices.Equal(slices.Sorted(slices.Values(ids)), []int32{3, 4, 25, 26}) {
		t.Fatalf("viewed ids %v, want [3 4 25 26]", ids)
	}
	f.setOwn(map[int32]int64{4: 0, 25: 0})

	got := f.messages()
	if !got[0].GetOut() || got[1].GetOut() {
		t.Errorf("out of 25, 26: %v, %v; want the viewer's own only", got[0].GetOut(), got[1].GetOut())
	}
	other := f.otherUpdates()
	if len(other) != 2 {
		t.Fatalf("other_updates %v: no updateMessageID without a random_id", names(other))
	}
	// Message 3 comes from the viewer itself, 4 as a channel.
	if !other[0].GetMessage_MESSAGE().GetOut() || !other[1].GetMessage_MESSAGE().GetOut() {
		t.Errorf("out of the edits of 3, 4: %v, %v", other[0].GetMessage_MESSAGE().GetOut(), other[1].GetMessage_MESSAGE().GetOut())
	}

	f = newChannelFold(foldViewer, 0, 30)
	f.add(sentAs(foldNew(25, foldViewer, "mine", 23)))
	f.setOwn(map[int32]int64{})
	if f.messages()[0].GetOut() {
		t.Error("a message written as a channel is out for one who did not write it")
	}
}
