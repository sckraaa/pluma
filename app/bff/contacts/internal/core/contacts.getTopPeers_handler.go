// Copyright 2022 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package core

import (
	"github.com/teamgram/proto/mtproto"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// maxTopPeersLimit is the most peers of a category one request returns.
const maxTopPeersLimit = 100

// ContactsGetTopPeers
// contacts.getTopPeers#973478b6 flags:# correspondents:flags.0?true bots_pm:flags.1?true bots_inline:flags.2?true phone_calls:flags.3?true forward_users:flags.4?true forward_chats:flags.5?true groups:flags.10?true channels:flags.15?true offset:int limit:int hash:long = contacts.TopPeers;
//
// The frequent peers of the asked categories, rated from the messages the
// user sends (user.getTopPeers, task 011): each category comes back, empty
// if it has no peers, with its peers from offset, at most limit. users
// lists the users in the order of the categories and ratings — iOS takes
// its order as the ranking (RecentPeers). hash is the hash of the peers'
// ids (core.telegram.org/api/offsets), as tdesktop computes it over the
// list it got (TopPeers::countHash): the same list is answered with
// contacts.topPeersNotModified.
func (c *ContactsCore) ContactsGetTopPeers(in *mtproto.TLContactsGetTopPeers) (*mtproto.Contacts_TopPeers, error) {
	if in.GetOffset() < 0 || in.GetLimit() < 0 {
		err := mtproto.ErrLimitInvalid
		c.Logger.Errorf("contacts.getTopPeers - error: %v", err)
		return nil, err
	}
	limit := min(in.GetLimit(), maxTopPeersLimit)

	var categories []*mtproto.TopPeerCategory
	for _, cat := range []struct {
		asked bool
		make  func() *mtproto.TopPeerCategory
	}{
		{in.GetCorrespondents(), func() *mtproto.TopPeerCategory {
			return mtproto.MakeTLTopPeerCategoryCorrespondents(nil).To_TopPeerCategory()
		}},
		{in.GetBotsPm(), func() *mtproto.TopPeerCategory { return mtproto.MakeTLTopPeerCategoryBotsPM(nil).To_TopPeerCategory() }},
		{in.GetBotsInline(), func() *mtproto.TopPeerCategory {
			return mtproto.MakeTLTopPeerCategoryBotsInline(nil).To_TopPeerCategory()
		}},
		{in.GetPhoneCalls(), func() *mtproto.TopPeerCategory {
			return mtproto.MakeTLTopPeerCategoryPhoneCalls(nil).To_TopPeerCategory()
		}},
		{in.GetForwardUsers(), func() *mtproto.TopPeerCategory {
			return mtproto.MakeTLTopPeerCategoryForwardUsers(nil).To_TopPeerCategory()
		}},
		{in.GetForwardChats(), func() *mtproto.TopPeerCategory {
			return mtproto.MakeTLTopPeerCategoryForwardChats(nil).To_TopPeerCategory()
		}},
		{in.GetGroups(), func() *mtproto.TopPeerCategory { return mtproto.MakeTLTopPeerCategoryGroups(nil).To_TopPeerCategory() }},
		{in.GetChannels(), func() *mtproto.TopPeerCategory {
			return mtproto.MakeTLTopPeerCategoryChannels(nil).To_TopPeerCategory()
		}},
		{in.GetBotsApp(), func() *mtproto.TopPeerCategory { return mtproto.MakeTLTopPeerCategoryBotsApp(nil).To_TopPeerCategory() }},
		{in.GetBotsGuestchat(), func() *mtproto.TopPeerCategory {
			return mtproto.MakeTLTopPeerCategoryBotsGuestChat(nil).To_TopPeerCategory()
		}},
	} {
		if cat.asked {
			categories = append(categories, cat.make())
		}
	}

	rated, err := c.svcCtx.Dao.UserClient.UserGetTopPeers(c.ctx, &userpb.TLUserGetTopPeers{
		UserId:     c.MD.UserId,
		Categories: categories,
	})
	if err != nil {
		c.Logger.Errorf("contacts.getTopPeers - error: %v", err)
		return nil, err
	}
	if rated.GetPredicateName() != mtproto.Predicate_contacts_topPeers {
		return rated, nil // disabled
	}

	// The users and the basic groups of the ratings, as the user sees them:
	// gone users and groups the user is no longer in are left out.
	var userIds, chatIds []int64
	for _, cat := range rated.GetCategories() {
		for _, p := range cat.GetPeers() {
			switch p.GetPeer().GetPredicateName() {
			case mtproto.Predicate_peerUser:
				userIds = append(userIds, p.GetPeer().GetUserId())
			case mtproto.Predicate_peerChat:
				chatIds = append(chatIds, p.GetPeer().GetChatId())
			}
		}
	}
	users := make(map[int64]*mtproto.User)
	if len(userIds) > 0 {
		mUsers, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{
			Id: append([]int64{c.MD.UserId}, userIds...),
			To: []int64{c.MD.UserId},
		})
		if err != nil {
			c.Logger.Errorf("contacts.getTopPeers - error: %v", err)
			return nil, err
		}
		for _, id := range userIds {
			if u, ok := mUsers.GetImmutableUser(id); ok && !u.Deleted() {
				if user, err := mUsers.GetUnsafeUser(c.MD.UserId, id); err == nil {
					users[id] = user
				}
			}
		}
	}
	chats := make(map[int64]*mtproto.Chat)
	if len(chatIds) > 0 {
		mChats, err := c.svcCtx.Dao.ChatClient.ChatGetChatListByIdList(c.ctx, &chatpb.TLChatGetChatListByIdList{
			SelfId: c.MD.UserId,
			IdList: chatIds,
		})
		if err != nil {
			c.Logger.Errorf("contacts.getTopPeers - error: %v", err)
			return nil, err
		}
		for _, chat := range mChats.GetDatas() {
			if me, ok := chat.GetImmutableChatParticipant(c.MD.UserId); ok && me.IsChatMemberStateNormal() && !chat.Deactivated() {
				chats[chat.Id()] = chat.ToUnsafeChat(c.MD.UserId)
			}
		}
	}

	result := mtproto.MakeTLContactsTopPeers(&mtproto.Contacts_TopPeers{
		Categories: make([]*mtproto.TopPeerCategoryPeers, 0, len(rated.GetCategories())),
		Chats:      []*mtproto.Chat{},
		Users:      []*mtproto.User{},
	}).To_Contacts_TopPeers()
	var (
		hash      uint64
		usersSeen = make(map[int64]bool)
		chatsSeen = make(map[int64]bool)
	)
	for _, cat := range rated.GetCategories() {
		var peers []*mtproto.TopPeer
		for _, p := range cat.GetPeers() {
			switch p.GetPeer().GetPredicateName() {
			case mtproto.Predicate_peerUser:
				if users[p.GetPeer().GetUserId()] != nil {
					peers = append(peers, p)
				}
			case mtproto.Predicate_peerChat:
				if chats[p.GetPeer().GetChatId()] != nil {
					peers = append(peers, p)
				}
			}
		}
		count := int32(len(peers))
		page := []*mtproto.TopPeer{}
		if start := int(in.GetOffset()); start < len(peers) {
			page = peers[start:min(len(peers), start+int(limit))]
		}
		for _, p := range page {
			switch p.GetPeer().GetPredicateName() {
			case mtproto.Predicate_peerUser:
				id := p.GetPeer().GetUserId()
				hash = hashUpdate(hash, id)
				if !usersSeen[id] {
					usersSeen[id] = true
					result.Users = append(result.Users, users[id])
				}
			case mtproto.Predicate_peerChat:
				id := p.GetPeer().GetChatId()
				hash = hashUpdate(hash, id)
				if !chatsSeen[id] {
					chatsSeen[id] = true
					result.Chats = append(result.Chats, chats[id])
				}
			}
		}
		result.Categories = append(result.Categories, mtproto.MakeTLTopPeerCategoryPeers(&mtproto.TopPeerCategoryPeers{
			Category: cat.GetCategory(),
			Count:    count,
			Peers:    page,
		}).To_TopPeerCategoryPeers())
	}
	if in.GetHash() != 0 && int64(hash) == in.GetHash() {
		return mtproto.MakeTLContactsTopPeersNotModified(nil).To_Contacts_TopPeers(), nil
	}
	return result, nil
}

// hashUpdate adds id to a hash of ids (core.telegram.org/api/offsets#hash-generation).
func hashUpdate(hash uint64, id int64) uint64 {
	hash ^= hash >> 21
	hash ^= hash << 35
	hash ^= hash >> 4
	return hash + uint64(id)
}
