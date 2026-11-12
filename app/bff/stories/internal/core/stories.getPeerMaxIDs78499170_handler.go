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
	"time"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/stories"
)

// StoriesGetPeerMaxIDs78499170
// stories.getPeerMaxIDs#78499170 id:Vector<InputPeer> = Vector<RecentStory>;
//
// The newest story in the feeds of each peer, in the order of id: what
// user.stories_max_id and channel.stories_max_id say, for the peers whose
// objects the client has without it. A peer without a story the caller
// may open, or one it may not see, answers recentStory without an id.
func (c *StoriesCore) StoriesGetPeerMaxIDs78499170(in *mtproto.TLStoriesGetPeerMaxIDs78499170) (*mtproto.Vector_RecentStory, error) {
	ids, err := c.peerMaxIds(in.Id)
	if err != nil {
		c.Logger.Errorf("stories.getPeerMaxIDs - error: %v", err)
		return nil, err
	}
	out := make([]*mtproto.RecentStory, 0, len(ids))
	for _, id := range ids {
		out = append(out, mtproto.MakeTLRecentStory(&mtproto.RecentStory{MaxId: mtproto.MakeFlagsInt32(id)}).To_RecentStory())
	}
	return &mtproto.Vector_RecentStory{Datas: out}, nil
}

// peerMaxIds reads the newest story in the feeds of each peer for the
// caller, 0 for none, in the order of the peers.
func (c *StoriesCore) peerMaxIds(peers []*mtproto.InputPeer) ([]int32, error) {
	if len(peers) > stories.MaxIds {
		return nil, mtproto.ErrLimitInvalid
	}
	var (
		me       = c.MD.UserId
		parsed   = make([]*mtproto.PeerUtil, len(peers))
		userIds  = []int64{me}
		channels []int64
	)
	for i, p := range peers {
		if p == nil {
			return nil, mtproto.ErrPeerIdInvalid
		}
		parsed[i] = mtproto.FromInputPeer2(me, p)
		switch parsed[i].PeerType {
		case mtproto.PEER_USER:
			userIds = append(userIds, parsed[i].PeerId)
		case mtproto.PEER_CHANNEL:
			channels = append(channels, parsed[i].PeerId)
		}
	}
	users, err := c.svcCtx.Dao.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: userIds, To: []int64{me}})
	if err != nil {
		return nil, err
	}
	ofChannel := map[int64]int32{}
	if len(channels) > 0 {
		list, err := c.svcCtx.Dao.ChannelGetChannelListByIdList(c.ctx, &channelpb.TLChannelGetChannelListByIdList{SelfId: me, IdList: channels})
		if err != nil {
			return nil, err
		}
		at := now()
		for _, mc := range list.GetDatas() {
			if mc.ToChat(me, at).GetPredicateName() == mtproto.Predicate_channel {
				ofChannel[mc.GetChannel().GetId()] = mc.GetChannel().GetStoriesMaxId()
			}
		}
	}
	out := make([]int32, len(peers))
	for i, p := range parsed {
		switch p.PeerType {
		case mtproto.PEER_SELF:
			if u, ok := users.GetImmutableUser(me); ok {
				out[i] = u.StoriesMaxIdFor(me, c.MD.IsBot, time.Now())
			}
		case mtproto.PEER_USER:
			if u, ok := users.GetImmutableUser(p.PeerId); ok && !u.Deleted() {
				out[i] = u.StoriesMaxIdFor(me, c.MD.IsBot, time.Now())
			}
		case mtproto.PEER_CHANNEL:
			out[i] = ofChannel[p.PeerId]
		}
	}
	return out, nil
}
