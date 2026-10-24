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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// UserGetTopPeers
// user.getTopPeers user_id:long categories:Vector<TopPeerCategory> = contacts.TopPeers;
//
// The ratings of the asked categories (top_peers.go), each sorted, without
// the users and chats: contacts.topPeersDisabled while the user turned
// top peers off. A category without peers is answered empty.
func (c *UserCore) UserGetTopPeers(in *user.TLUserGetTopPeers) (*mtproto.Contacts_TopPeers, error) {
	enabled, since, err := c.svcCtx.Dao.TopPeersState(c.ctx, in.UserId)
	if err != nil {
		c.Logger.Errorf("user.getTopPeers - user %d: %v", in.UserId, err)
		return nil, err
	}
	if !enabled {
		return mtproto.MakeTLContactsTopPeersDisabled(nil).To_Contacts_TopPeers(), nil
	}

	var (
		categories []*mtproto.TopPeerCategory
		asked      = make(map[int32]bool)
	)
	for _, cat := range in.Categories {
		id, ok := topPeerCategory(cat)
		if !ok {
			err = mtproto.ErrInputConstructorInvalid
			c.Logger.Errorf("user.getTopPeers - category %s: %v", cat.GetPredicateName(), err)
			return nil, err
		}
		if !asked[id] {
			asked[id] = true
			categories = append(categories, cat)
		}
	}

	now := time.Now().Unix()
	messages, err := c.svcCtx.Dao.SentMessages(c.ctx, in.UserId, since, topPeersScan)
	if err != nil {
		c.Logger.Errorf("user.getTopPeers - user %d: %v", in.UserId, err)
		return nil, err
	}
	resetList, err := c.svcCtx.Dao.TopPeerResets(c.ctx, in.UserId)
	if err != nil {
		c.Logger.Errorf("user.getTopPeers - user %d: %v", in.UserId, err)
		return nil, err
	}
	resets := make(map[topPeerKey]int64, len(resetList))
	for _, r := range resetList {
		resets[topPeerKey{r.Category, r.PeerType, r.PeerId}] = r.ResetAt
	}

	// Which of the users written to are people and which bots.
	var ids []int64
	seen := make(map[int64]bool)
	for _, m := range messages {
		if m.PeerType == mtproto.PEER_USER && m.PeerId != in.UserId && !seen[m.PeerId] {
			seen[m.PeerId] = true
			ids = append(ids, m.PeerId)
		}
	}
	kinds := make(map[int64]peerKind, len(ids))
	if len(ids) > 0 {
		datas := c.svcCtx.Dao.GetCacheUserDataListByIdList(c.ctx, ids)
		if len(datas) != len(ids) {
			err = fmt.Errorf("%d of %d users not loaded", len(ids)-len(datas), len(ids))
			c.Logger.Errorf("user.getTopPeers - user %d: %v", in.UserId, err)
			return nil, err
		}
		for _, d := range datas {
			u := d.GetUserData()
			switch {
			case u.GetDeleted():
			case u.GetUserType() == user.UserTypeRegular:
				kinds[u.GetId()] = peerPerson
			case u.GetUserType() == user.UserTypeBot:
				kinds[u.GetId()] = peerBot
			}
		}
	}
	rated := rateTopPeers(messages, now, func(peerType int32, peerId int64) peerKind {
		switch peerType {
		case mtproto.PEER_USER:
			return kinds[peerId]
		case mtproto.PEER_CHAT:
			return peerGroup
		}
		return peerIgnored
	}, resets)

	result := mtproto.MakeTLContactsTopPeers(&mtproto.Contacts_TopPeers{
		Categories: make([]*mtproto.TopPeerCategoryPeers, 0, len(categories)),
		Chats:      []*mtproto.Chat{},
		Users:      []*mtproto.User{},
	}).To_Contacts_TopPeers()
	for _, cat := range categories {
		id, _ := topPeerCategory(cat)
		peers := rated[id]
		if peers == nil {
			peers = []*mtproto.TopPeer{}
		}
		result.Categories = append(result.Categories, mtproto.MakeTLTopPeerCategoryPeers(&mtproto.TopPeerCategoryPeers{
			Category: cat,
			Count:    int32(len(peers)),
			Peers:    peers,
		}).To_TopPeerCategoryPeers())
	}
	return result, nil
}
