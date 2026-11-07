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
	"fmt"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
)

// TestSearch: a search of a channel finds by text (wildcards taken as
// text), author, kind and pin, leaving out service messages (Bob's join), newest first with the count of all that
// match, pages below offset_id, and keeps to what the reader may see; one
// out of a private channel is CHANNEL_PRIVATE.
func TestSearch(t *testing.T) {
	f := newDiffFixture(t)
	creator, bob, eve := f.user("Creator"), f.user("Bob"), f.user("Eve")
	id := f.create(creator)
	f.public(id)
	if _, err := f.join(bob, id, ""); err != nil {
		t.Fatal(err)
	}
	f.send(id, creator, 3)
	f.send(id, bob, 2)
	ids := func(r *mtproto.Messages_Messages) string {
		var out []int32
		for _, m := range r.GetMessages() {
			out = append(out, m.GetId())
		}
		return fmt.Sprint(out)
	}
	var first int32
	if err := f.box.QueryRow("SELECT MIN(id) FROM channel_messages WHERE channel_id = ? AND sender_user_id = ?", id, creator).Scan(&first); err != nil {
		t.Fatal(err)
	}
	a, b := first+1, first+2
	f.exec("UPDATE channel_messages SET message = '50% off', pinned = 1 WHERE channel_id = ? AND id = ?", id, a)
	f.exec("UPDATE channel_messages SET message = '500 off', message_filter_type = ? WHERE channel_id = ? AND id = ?", mtproto.MEDIA_PHOTOS_ONLY, id, b)

	search := func(q string, from int64, media int32, offset, limit int32) *mtproto.Messages_Messages {
		t.Helper()
		r, err := f.c.ChannelSearch(&channel.TLChannelSearch{SelfId: bob, ChannelId: id, Q: q, FromId: from, MediaType: media, OffsetId: offset, Limit: limit})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := search("50%", 0, mtproto.MEDIA_EMPTY, 0, 10); ids(r) != fmt.Sprint([]int32{a}) || r.GetCount() != 1 {
		t.Errorf("text with a wildcard: %s count %d", ids(r), r.GetCount())
	}
	if r := search("", 0, mtproto.MEDIA_PINNED, 0, 10); ids(r) != fmt.Sprint([]int32{a}) {
		t.Errorf("pinned: %s", ids(r))
	}
	if r := search("", 0, mtproto.MEDIA_PHOTOVIDEO, 0, 10); ids(r) != fmt.Sprint([]int32{b}) {
		t.Errorf("photos and videos: %s", ids(r))
	}
	r := search("", bob, mtproto.MEDIA_EMPTY, 0, 1)
	if len(r.GetMessages()) != 1 || r.GetCount() != 2 || r.GetPredicateName() != mtproto.Predicate_messages_channelMessages {
		t.Fatalf("by author, a page of one: %s count %d %s", ids(r), r.GetCount(), r.GetPredicateName())
	}
	if next := search("", bob, mtproto.MEDIA_EMPTY, r.GetMessages()[0].GetId(), 1); len(next.GetMessages()) != 1 || next.GetMessages()[0].GetId() >= r.GetMessages()[0].GetId() {
		t.Errorf("the next page: %s", ids(next))
	}

	f.exec("DELETE FROM username WHERE peer_type = ? AND peer_id = ?", mtproto.PEER_CHANNEL, id)
	_, err := f.c.ChannelSearch(&channel.TLChannelSearch{SelfId: eve, ChannelId: id, Q: "off", MediaType: mtproto.MEDIA_EMPTY, Limit: 10})
	wantErr(t, "one out of a private channel", err, mtproto.ErrChannelPrivate)
}
