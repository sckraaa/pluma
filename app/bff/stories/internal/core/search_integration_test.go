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
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/stories/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/stories"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// postTagged stores a photo story of owner with caption for the rules of
// audience at the date at.
func postTagged(t *testing.T, s *stories.Store, owner int64, caption, audience string, at int32) int32 {
	t.Helper()
	st, created, err := s.Create(context.Background(), &stories.Story{
		Peer: stories.UserPeer(owner), RandomId: itId(), SenderId: owner, Date: at, ExpireDate: at + stories.DefaultPeriod,
		Caption: caption,
		Privacy: []*mtproto.PrivacyRule{{PredicateName: audience}},
		Media: mtproto.MakeTLMessageMediaPhoto(&mtproto.MessageMedia{Photo_FLAGPHOTO: mtproto.MakeTLPhoto(&mtproto.Photo{
			Id: itId(), AccessHash: 1, FileReference: []byte{}, Sizes: []*mtproto.PhotoSize{}, DcId: 1,
		}).To_Photo()}).To_MessageMedia(),
	})
	if err != nil || !created {
		t.Fatalf("story: %v, created %v", err, created)
	}
	return st.Id
}

func foundIds(r *mtproto.Stories_FoundStories) []int32 {
	out := make([]int32, 0, len(r.GetStories()))
	for _, s := range r.GetStories() {
		out = append(out, s.GetStory().GetId())
	}
	return out
}

// stories.searchPosts on the index: the public stories only, for a
// stranger, a contact and the owner; peer narrows; pages of one follow
// next_offset with the same count; the stories come as their finder sees
// them, with their owner in users.
func TestSearchPostsOfUsers(t *testing.T) {
	db := itDB()
	owner, contact, stranger := itId(), itId(), itId()
	d := &dao.Dao{Store: stories.NewStore(db), DB: db, UserClient: people{owner: owner, contact: contact}}
	tag := fmt.Sprintf("tag%d", itId())
	at := now()
	older := postTagged(t, d.Store, owner, "first #"+tag, mtproto.Predicate_privacyValueAllowAll, at-10)
	postTagged(t, d.Store, owner, "for contacts #"+tag, mtproto.Predicate_privacyValueAllowContacts, at-5)
	newer := postTagged(t, d.Store, owner, "second #"+tag, mtproto.Predicate_privacyValueAllowAll, at)

	for name, caller := range map[string]int64{"a stranger": stranger, "a contact": contact, "the owner": owner} {
		r, err := itCore(d, caller).StoriesSearchPosts(&mtproto.TLStoriesSearchPosts{Hashtag: wrapperspb.String("#" + tag), Limit: 10})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if ids := foundIds(r); !slices.Equal(ids, []int32{newer, older}) || r.GetCount() != 2 || r.GetNextOffset() != nil {
			t.Errorf("%s finds %v (count %d, next_offset %v), want [%d %d]", name, ids, r.GetCount(), r.GetNextOffset(), newer, older)
		}
		for _, s := range r.GetStories() {
			if s.GetPeer().GetUserId() != owner || s.GetStory().GetOut() != (caller == owner) {
				t.Errorf("%s: the found story %v", name, s)
			}
		}
		if !slices.ContainsFunc(r.GetUsers(), func(u *mtproto.User) bool { return u.GetId() == owner }) {
			t.Errorf("%s: the owner is not in users: %v", name, r.GetUsers())
		}
	}

	c := itCore(d, stranger)
	ownerPeer := mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: owner, AccessHash: 1}).To_InputPeer()
	first, err := c.StoriesSearchPosts(&mtproto.TLStoriesSearchPosts{Hashtag: wrapperspb.String(tag), Peer: ownerPeer, Limit: 1})
	if err != nil || !slices.Equal(foundIds(first), []int32{newer}) || first.GetCount() != 2 || first.GetNextOffset().GetValue() == "" {
		t.Fatalf("the first page: %v, %v", first, err)
	}
	second, err := c.StoriesSearchPosts(&mtproto.TLStoriesSearchPosts{Hashtag: wrapperspb.String(tag), Peer: ownerPeer, Limit: 1, Offset: first.GetNextOffset().GetValue()})
	if err != nil || !slices.Equal(foundIds(second), []int32{older}) || second.GetCount() != 2 || second.GetNextOffset() != nil {
		t.Errorf("the second page: %v, %v", second, err)
	}
	mine, err := c.StoriesSearchPosts(&mtproto.TLStoriesSearchPosts{Hashtag: wrapperspb.String(tag), Peer: selfInputPeer(), Limit: 10})
	if err != nil || len(mine.GetStories()) != 0 || mine.GetCount() != 0 {
		t.Errorf("of the searcher, who has none: %v, %v", mine, err)
	}

	bot := itCore(d, stranger)
	bot.MD.IsBot = true
	if _, err = bot.StoriesSearchPosts(&mtproto.TLStoriesSearchPosts{Hashtag: wrapperspb.String(tag), Limit: 10}); err != mtproto.ErrBotMethodInvalid {
		t.Errorf("a bot: %v, want BOT_METHOD_INVALID", err)
	}
}
