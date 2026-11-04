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
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// TestSearchUsername: users and channels by the start of their username,
// the shortest first; the excluded contacts are users only, so a channel
// with the id of a contact is found; % and _ in the query are literal; an
// inactive or deleted name is not found.
func TestSearchUsername(t *testing.T) {
	c := userCore(t)
	n := itSeq.Add(1)
	pre := fmt.Sprintf("srch%d", n)
	contact := int64(1<<41 + n)
	rows := []struct {
		name            string
		peerType        int32
		peerId          int64
		active, deleted int
	}{
		{pre + "chan", mtproto.PEER_CHANNEL, contact, 1, 0}, // a channel with the contact's id
		{pre + "user", mtproto.PEER_USER, contact, 1, 0},    // the contact
		{pre + "x", mtproto.PEER_USER, contact + 1, 1, 0},
		{pre + "old", mtproto.PEER_USER, contact + 2, 0, 0},
		{pre + "gone", mtproto.PEER_CHANNEL, contact + 3, 1, 1},
		{pre + "a_b", mtproto.PEER_USER, contact + 4, 1, 0},
		{pre + "aXb", mtproto.PEER_USER, contact + 5, 1, 0},
	}
	for i, r := range rows {
		if _, err := itRaw.Exec("INSERT INTO username (id, username, peer_type, peer_id, editable, active, deleted) VALUES (?, ?, ?, ?, 1, ?, ?)",
			n*100+int64(i), r.name, r.peerType, r.peerId, r.active, r.deleted); err != nil {
			t.Fatal(err)
		}
	}
	search := func(q string) []string {
		t.Helper()
		r, err := c.UserSearchUsername(&user.TLUserSearchUsername{Q: q, ExcludedContacts: []int64{contact}, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, d := range r.GetDatas() {
			kind := "user"
			if d.GetPeer().GetPredicateName() == mtproto.Predicate_peerChannel {
				kind = "channel"
			}
			out = append(out, kind+":"+d.GetUsername())
		}
		return out
	}
	if got, want := fmt.Sprint(search(pre)), fmt.Sprint([]string{"user:" + pre + "x", "user:" + pre + "a_b", "user:" + pre + "aXb", "channel:" + pre + "chan"}); got != want {
		t.Errorf("%s: %s, want %s", pre, got, want)
	}
	if got, want := fmt.Sprint(search(pre+"a_")), fmt.Sprint([]string{"user:" + pre + "a_b"}); got != want {
		t.Errorf("a literal _: %s, want %s", got, want)
	}
}
