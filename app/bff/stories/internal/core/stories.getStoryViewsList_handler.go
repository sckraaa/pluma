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
	"sort"
	"strconv"
	"strings"

	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/reactions"
	"github.com/teamgram/teamgram-server/pkg/stories"
)

// StoriesGetStoryViewsList
// stories.getStoryViewsList#7ed23c57 flags:# just_contacts:flags.0?true reactions_first:flags.2?true forwards_first:flags.3?true peer:InputPeer q:flags.1?string id:int offset:string limit:int = stories.StoryViewsList;
//
// Who opened a story of the account, with the reactions they left: newest
// first, or with reactions_first those who reacted first. just_contacts
// keeps the caller's contacts, q those whose name or username has it. The
// counters are of the whole story, count of the list asked for. offset is
// the place the page before ended at, "" for the first; next_offset is
// absent at the end, which is how the clients know it (Android
// SelfStoryViewsPage.java:1366-1373). The viewers are kept as long as the
// story (how long Telegram keeps them was not read for this task; every
// Premium limit is lifted here, ADR 0041). The reposts and forwards of a
// story are not listed
// (storyViewPublicForward, storyViewPublicRepost): forwards_first changes
// nothing.
func (c *StoriesCore) StoriesGetStoryViewsList(in *mtproto.TLStoriesGetStoryViewsList) (*mtproto.Stories_StoryViewsList, error) {
	v, err := c.view(in.Peer)
	if err != nil {
		c.Logger.Errorf("stories.getStoryViewsList - error: %v", err)
		return nil, err
	}
	if !v.own {
		// The viewers of a channel's story are shown to nobody; its
		// reactions are stories.getStoryReactionsList.
		return nil, mtproto.ErrPeerIdInvalid
	}
	st, err := c.svcCtx.Dao.Store.One(c.ctx, v.peer, in.Id)
	if err != nil {
		c.Logger.Errorf("stories.getStoryViewsList - error: %v", err)
		return nil, err
	}
	if st == nil {
		return nil, errStoryIdInvalid
	}
	offset, err := pageOffset(in.Offset)
	if err != nil {
		return nil, err
	}
	views, err := c.svcCtx.Dao.Store.Views(c.ctx, v.peer, in.Id)
	if err != nil {
		c.Logger.Errorf("stories.getStoryViewsList - error: %v", err)
		return nil, err
	}
	total := int32(len(views))
	var reacted int32
	ids := make([]int64, 0, len(views))
	for _, view := range views {
		ids = append(ids, view.ViewerId)
		if view.Reaction != "" {
			reacted++
		}
	}

	var (
		users     *userpb.Vector_ImmutableUser
		blocked   []int64
		hidden    map[int64]bool
		contacts  []int64
		selfUser  *mtproto.ImmutableUser
		needUsers = len(ids) > 0
	)
	if needUsers {
		all := append([]int64{c.MD.UserId}, ids...)
		// To everyone of them: the caller's rows of its contacts carry the
		// names it searches by.
		if users, err = c.svcCtx.Dao.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: all, To: all}); err != nil {
			c.Logger.Errorf("stories.getStoryViewsList - error: %v", err)
			return nil, err
		}
		selfUser, _ = users.GetImmutableUser(c.MD.UserId)
		list, err := c.svcCtx.Dao.UserCheckBlockUserList(c.ctx, &userpb.TLUserCheckBlockUserList{UserId: c.MD.UserId, Id: ids})
		if err != nil {
			c.Logger.Errorf("stories.getStoryViewsList - error: %v", err)
			return nil, err
		}
		blocked = list.GetDatas()
		if hidden, err = stories.BlockedOf(c.ctx, c.svcCtx.Dao.DB, c.MD.UserId, ids); err != nil {
			c.Logger.Errorf("stories.getStoryViewsList - error: %v", err)
			return nil, err
		}
		if in.JustContacts {
			list, err := c.svcCtx.Dao.UserGetContactIdList(c.ctx, &userpb.TLUserGetContactIdList{UserId: c.MD.UserId})
			if err != nil {
				c.Logger.Errorf("stories.getStoryViewsList - error: %v", err)
				return nil, err
			}
			contacts = list.GetDatas()
		}
	}

	q := strings.ToLower(strings.TrimSpace(in.GetQ().GetValue()))
	kept := views[:0:0]
	for _, view := range views {
		u, ok := users.GetImmutableUser(view.ViewerId)
		if !ok || u.Deleted() {
			continue
		}
		if in.JustContacts && !containsId(contacts, view.ViewerId) {
			continue
		}
		if q != "" && !nameHas(u, selfUser, q) {
			continue
		}
		kept = append(kept, view)
	}
	if in.ReactionsFirst {
		sort.SliceStable(kept, func(i, j int) bool {
			a, b := kept[i], kept[j]
			if (a.Reaction != "") != (b.Reaction != "") {
				return a.Reaction != ""
			}
			if a.Reaction != "" && a.ReactionDate != b.ReactionDate {
				return a.ReactionDate > b.ReactionDate
			}
			return false
		})
	}

	limit := int(clampLimit(in.Limit, 20, 100))
	page, next := pageOf(len(kept), offset, limit)
	answer := &mtproto.Stories_StoryViewsList{
		Count:          int32(len(kept)),
		ViewsCount:     total,
		ForwardsCount:  st.ForwardsCount,
		ReactionsCount: reacted,
		Views:          make([]*mtproto.StoryView, 0, limit),
		Chats:          []*mtproto.Chat{},
		Users:          []*mtproto.User{},
	}
	r := &refs{}
	for _, view := range kept[page[0]:page[1]] {
		sv := &mtproto.StoryView{
			UserId:  view.ViewerId,
			Date:    view.Date,
			Blocked: containsId(blocked, view.ViewerId),
			// The viewers list is where the clients hide the stories
			// from a viewer and read it back (Android
			// StoriesController.java:4446-4477).
			BlockedMyStoriesFrom: hidden[view.ViewerId],
		}
		if view.Reaction != "" {
			sv.Reaction = reactions.FromKey(view.Reaction)
		}
		answer.Views = append(answer.Views, mtproto.MakeTLStoryView(sv).To_StoryView())
		r.user(view.ViewerId)
	}
	if next != "" {
		answer.NextOffset = mtproto.MakeFlagsString(next)
	}
	if len(r.users) > 0 {
		answer.Users = users.GetUserListByIdList(c.MD.UserId, r.users...)
	}
	return mtproto.MakeTLStoriesStoryViewsList(answer).To_Stories_StoryViewsList(), nil
}

// nameHas tells whether the name the caller knows the user by, or its
// username, has q (lower case).
func nameHas(u, self *mtproto.ImmutableUser, q string) bool {
	first, last := u.FirstName(), u.LastName()
	if self == nil {
		return strings.Contains(strings.ToLower(first+" "+last), q) || strings.Contains(strings.ToLower(u.Username()), q)
	}
	if row := self.GetContactData(u.Id()); row != nil {
		first, last = row.GetFirstName().GetValue(), row.GetLastName().GetValue()
	}
	return strings.Contains(strings.ToLower(first+" "+last), q) || strings.Contains(strings.ToLower(u.Username()), q)
}

// pageOffset reads the offset of a page of viewers or reactions: the
// number of rows before it. Anything else is OFFSET_INVALID.
func pageOffset(offset string) (int, error) {
	if offset == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(offset)
	if err != nil || n < 0 {
		return 0, mtproto.ErrOffsetInvalid
	}
	return n, nil
}

// pageOf is the page of limit rows from offset of n rows, and the offset
// of the page after it, "" when there is none.
func pageOf(n, offset, limit int) ([2]int, string) {
	if offset > n {
		offset = n
	}
	end := offset + limit
	if end >= n {
		return [2]int{offset, n}, ""
	}
	return [2]int{offset, end}, strconv.Itoa(end)
}
