//go:build integration

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
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
)

func (f *diffFixture) delete(self, channelId int64, ids ...int32) (*mtproto.Messages_AffectedMessages, error) {
	return f.c.ChannelDeleteMessages(&channel.TLChannelDeleteMessages{SelfId: self, PermAuthKeyId: 1, ChannelId: channelId, Id: ids})
}

// TestDelete: a member deletes its own messages, not another's; the
// creator deletes any; the deletion is one journal row of pts_count ids,
// the top message moves back, and a repeat deletes nothing.
func TestDelete(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob := f.user("Creator"), f.user("Bob")
	id := f.create(creator)
	f.public(id)
	if _, err := f.join(bob, id, ""); err != nil {
		t.Fatal(err)
	}
	f.send(id, bob, 1)
	f.send(id, creator, 2)
	var top int32
	if err := f.box.QueryRow("SELECT top_message FROM channels WHERE id = ?", id).Scan(&top); err != nil {
		t.Fatal(err)
	}
	bobs, creators := top-2, []int32{top - 1, top}

	_, err := f.delete(bob, id, creators[0])
	wantErr(t, "another's message", err, mtproto.ErrMessageDeleteForbidden)
	r, err := f.delete(bob, id, bobs)
	if err != nil {
		t.Fatal(err)
	}
	if r.GetPtsCount() != 1 {
		t.Errorf("own message: pts_count %d", r.GetPtsCount())
	}
	r, err = f.delete(creator, id, creators[1], creators[0], creators[1], 999)
	if err != nil {
		t.Fatal(err)
	}
	if r.GetPtsCount() != 2 {
		t.Fatalf("the creator: pts_count %d, want 2", r.GetPtsCount())
	}
	d := f.diff(creator, id, r.GetPts()-2, 10)
	if len(d.GetOtherUpdates()) != 1 || !slices.Equal(d.GetOtherUpdates()[0].GetMessages(), creators) {
		t.Errorf("the journal row: %v", d.GetOtherUpdates())
	}
	if err = f.box.QueryRow("SELECT top_message FROM channels WHERE id = ?", id).Scan(&top); err != nil {
		t.Fatal(err)
	}
	if top >= bobs {
		t.Errorf("top message %d, want below %d", top, bobs)
	}
	again, err := f.delete(creator, id, creators...)
	if err != nil || again.GetPtsCount() != 0 || again.GetPts() != r.GetPts() {
		t.Errorf("a repeat: %v, %v (want count 0 at pts %d)", again, err, r.GetPts())
	}
}
