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

//go:build integration

package core

import (
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dao"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// Saved GIFs (task 020): a saved GIF goes to the top, an unsaved one off
// the list, and each change is one updateSavedGifs with seq for every
// device; a change that changes nothing pushes nothing. A GIF saved over the
// limit drops the oldest.
func TestSavedGifs(t *testing.T) {
	c := userCore(t)
	u, _ := newUser(t, "Anna")
	save := func(id int64, unsave bool) {
		t.Helper()
		if r, err := c.UserSaveGif(&user.TLUserSaveGif{UserId: u, Id: id, Unsave: mtproto.ToBool(unsave)}); err != nil || !mtproto.FromBool(r) {
			t.Fatalf("save %d (unsave %v): %v %v", id, unsave, r, err)
		}
	}
	list := func() []int64 {
		t.Helper()
		r, err := c.UserGetSavedGifIdList(&user.TLUserGetSavedGifIdList{UserId: u})
		if err != nil {
			t.Fatal(err)
		}
		return r.GetDatas()
	}
	pushes := func() int {
		t.Helper()
		n := 0
		for _, p := range seqPushes(t, u) {
			if p.update.GetPredicateName() == mtproto.Predicate_updateSavedGifs {
				n++
			}
		}
		return n
	}

	for _, id := range []int64{11, 12, 13} {
		save(id, false)
	}
	save(11, false)
	save(11, false) // already at the top
	save(12, true)
	save(99, true) // not saved
	if got, want := list(), []int64{11, 13}; !slices.Equal(got, want) {
		t.Fatalf("saved GIFs %v, want %v", got, want)
	}
	if n := pushes(); n != 5 {
		t.Fatalf("%d pushes of updateSavedGifs, want 5", n)
	}

	// One more than the limit: the oldest, 13, goes.
	newest := int64(100 + dao.SavedGifsLimit - 2)
	for id := int64(100); id <= newest; id++ {
		save(id, false)
	}
	got := list()
	if len(got) != dao.SavedGifsLimit || got[0] != newest || !slices.Contains(got, 11) || slices.Contains(got, 13) {
		t.Fatalf("%d saved GIFs over the limit, the newest %d, 11 kept %v, 13 kept %v",
			len(got), got[0], slices.Contains(got, 11), slices.Contains(got, 13))
	}
	if _, err := c.UserSaveGif(&user.TLUserSaveGif{UserId: u, Unsave: mtproto.BoolFalse}); err == nil {
		t.Fatal("user.saveGif took the GIF 0")
	}
}
