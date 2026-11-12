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
	"github.com/teamgram/proto/mtproto"
	syncpb "github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/stories"
)

// The updates of stories carry no pts: a device that missed one reads the
// feeds again by their state (stories.getAllStories), so they are pushed
// to the devices that are online and kept nowhere.

// updateStory is updateStory of a story of peer.
func updateStory(peer stories.Peer, item *mtproto.StoryItem) *mtproto.Update {
	u := &mtproto.Update{Peer_PEER: peer.ToPeer(), Story: item}
	if peer.IsUser() {
		u.UserId = peer.Id
	}
	return mtproto.MakeTLUpdateStory(u).To_Update()
}

// updateReadStories is updateReadStories of peer up to maxId.
func updateReadStories(peer stories.Peer, maxId int32) *mtproto.Update {
	u := &mtproto.Update{Peer_PEER: peer.ToPeer(), MaxId: maxId}
	if peer.IsUser() {
		u.UserId = peer.Id
	}
	return mtproto.MakeTLUpdateReadStories(u).To_Update()
}

// updatesOf is the container of updates without pts or seq.
func updatesOf(list []*mtproto.Update, users []*mtproto.User, chats []*mtproto.Chat) *mtproto.Updates {
	if users == nil {
		users = []*mtproto.User{}
	}
	if chats == nil {
		chats = []*mtproto.Chat{}
	}
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: list,
		Users:   users,
		Chats:   chats,
		Date:    now(),
		Seq:     0,
	}).To_Updates()
}

// pushOthers sends updates to the other devices of the caller.
func (c *StoriesCore) pushOthers(updates *mtproto.Updates) {
	if _, err := c.svcCtx.Dao.SyncClient.SyncUpdatesNotMe(c.ctx, &syncpb.TLSyncUpdatesNotMe{
		UserId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		Updates:       updates,
	}); err != nil {
		c.Logger.Errorf("stories: push to the devices of user %d: %v", c.MD.UserId, err)
	}
}

// pushTo sends updates to every device of the user userId.
func (c *StoriesCore) pushTo(userId int64, updates *mtproto.Updates) {
	if _, err := c.svcCtx.Dao.SyncClient.SyncPushUpdates(c.ctx, &syncpb.TLSyncPushUpdates{
		UserId:  userId,
		Updates: updates,
	}); err != nil {
		c.Logger.Errorf("stories: push to user %d: %v", userId, err)
	}
}

// announce tells the users who see the peer of the story st about it,
// each as it sees the story: updateStory with the story to those it is
// for, with storyItemDeleted to those it is deleted for (gone: the story
// was deleted) or who lost it with an edit of its rules (before is the
// story as it was). The caller's own devices are told by the method
// itself. A failure is logged: the story is stored, and the feeds show it
// at their next read.
func (c *StoriesCore) announce(st, before *stories.Story, gone bool) {
	var err error
	if st.Peer.IsUser() {
		err = c.announceUser(st, before, gone)
	} else {
		err = c.announceChannel(st, gone)
	}
	if err != nil {
		c.Logger.Errorf("stories: announce of story %d of peer %d/%d: %v", st.Id, st.Peer.Type, st.Peer.Id, err)
	}
}

// sentBy reads the reactions the viewers of the story left, by viewer.
func (c *StoriesCore) sentBy(st *stories.Story) (map[int64]string, error) {
	views, err := c.svcCtx.Dao.Store.Views(c.ctx, st.Peer, st.Id)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]string, len(views))
	for _, v := range views {
		if v.Reaction != "" {
			out[v.ViewerId] = v.Reaction
		}
	}
	return out, nil
}

// announceUser: the story of a user goes to those who see the user (its
// contacts' owners, the users it talks to, the members of its groups: the
// watchers of its status, ADR 0030) and whom its rules let in, but not to
// those the user blocked or hides its stories from.
func (c *StoriesCore) announceUser(st, before *stories.Story, gone bool) error {
	owner := st.Peer.Id
	watchers, err := c.svcCtx.Dao.UserGetStatusWatchers(c.ctx, &userpb.TLUserGetStatusWatchers{UserId: owner})
	if err != nil {
		return err
	}
	var ids []int64
	for _, id := range watchers.GetDatas() {
		if id != owner {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	blocked, err := c.svcCtx.Dao.UserCheckBlockUserList(c.ctx, &userpb.TLUserCheckBlockUserList{UserId: owner, Id: ids})
	if err != nil {
		return err
	}
	hiddenFrom, err := stories.BlockedOf(c.ctx, c.svcCtx.Dao.DB, owner, ids)
	if err != nil {
		return err
	}
	all := append([]int64{owner}, ids...)
	users, err := c.svcCtx.Dao.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: all, To: all})
	if err != nil {
		return err
	}
	ownerUser, ok := users.GetImmutableUser(owner)
	if !ok {
		return nil
	}
	rules, err := c.resolveChats(st.Privacy, ids)
	if err != nil {
		return err
	}
	var rulesBefore []*mtproto.PrivacyRule
	if before != nil {
		if rulesBefore, err = c.resolveChats(before.Privacy, ids); err != nil {
			return err
		}
	}
	var sent map[int64]string
	if !gone {
		if sent, err = c.sentBy(st); err != nil {
			return err
		}
	}
	for _, id := range ids {
		u, ok := users.GetImmutableUser(id)
		if !ok || u.Deleted() || u.IsBot() || containsId(blocked.GetDatas(), id) || hiddenFrom[id] {
			continue
		}
		viewer := ownerUser.StoryViewerOf(id, false)
		sees := mtproto.EvaluateStoryPrivacy(rules, viewer)
		saw := before != nil && mtproto.EvaluateStoryPrivacy(rulesBefore, viewer)
		var item *mtproto.StoryItem
		switch {
		case sees && !gone:
			item = stories.Item(st, stories.Seen{SentReaction: sent[id]})
		case sees, saw:
			item = stories.Deleted(st.Id)
		default:
			continue
		}
		var list []*mtproto.User
		if shown, err := users.GetUnsafeUser(id, owner); err == nil {
			list = append(list, shown)
		}
		c.pushTo(id, updatesOf([]*mtproto.Update{updateStory(st.Peer, item)}, list, nil))
	}
	return nil
}

// announceChannel: the story of a channel goes to its members, the caller
// aside.
func (c *StoriesCore) announceChannel(st *stories.Story, gone bool) error {
	members, err := c.svcCtx.Dao.ChannelGetMemberIdList(c.ctx, &channelpb.TLChannelGetMemberIdList{ChannelId: st.Peer.Id})
	if err != nil {
		return err
	}
	var ids []int64
	for _, id := range members.GetDatas() {
		if id != c.MD.UserId {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	mc, err := c.svcCtx.Dao.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: st.Peer.Id, Id: ids})
	if err != nil {
		return err
	}
	var (
		sent  map[int64]string
		stats *stories.Stats
	)
	if !gone {
		if sent, err = c.sentBy(st); err != nil {
			return err
		}
		all, err := c.svcCtx.Dao.Store.StatsOf(c.ctx, st.Peer, []int32{st.Id}, 0)
		if err != nil {
			return err
		}
		if stats = all[st.Id]; stats == nil {
			stats = &stories.Stats{}
		}
	}
	at := now()
	for _, id := range ids {
		chat := mc.ToChat(id, at)
		if chat.GetPredicateName() != mtproto.Predicate_channel {
			continue
		}
		item := stories.Deleted(st.Id)
		if !gone {
			item = stories.Item(st, stories.Seen{Out: st.SenderId == id, Stats: stats, SentReaction: sent[id]})
		}
		c.pushTo(id, updatesOf([]*mtproto.Update{updateStory(st.Peer, item)}, nil, []*mtproto.Chat{chat}))
	}
	return nil
}

func containsId(ids []int64, id int64) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}
