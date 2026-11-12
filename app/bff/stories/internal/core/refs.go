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

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/stories"
)

// refs are the users and the channels an answer refers to.
type refs struct {
	users    []int64
	channels []int64
}

func (r *refs) user(ids ...int64) {
	for _, id := range ids {
		if id != 0 && !slices.Contains(r.users, id) {
			r.users = append(r.users, id)
		}
	}
}

func (r *refs) channel(ids ...int64) {
	for _, id := range ids {
		if id != 0 && !slices.Contains(r.channels, id) {
			r.channels = append(r.channels, id)
		}
	}
}

func (r *refs) peer(p stories.Peer) {
	if p.IsUser() {
		r.user(p.Id)
	} else if p.Id != 0 {
		r.channel(p.Id)
	}
}

func (r *refs) tlPeer(p *mtproto.Peer) {
	switch p.GetPredicateName() {
	case mtproto.Predicate_peerUser:
		r.user(p.GetUserId())
	case mtproto.Predicate_peerChannel:
		r.channel(p.GetChannelId())
	}
}

// story adds what the story refers to: its peer, the author of a repost,
// the channel of a post it shows, and for its owner the users its rules
// name.
func (r *refs) story(st *stories.Story, owner bool) {
	r.peer(st.Peer)
	r.tlPeer(st.FwdFrom.GetFrom())
	for _, a := range st.MediaAreas {
		if a.GetPredicateName() == mtproto.Predicate_mediaAreaChannelPost {
			r.channel(a.GetChannelId())
		}
	}
	if owner {
		for _, rule := range st.Privacy {
			r.user(rule.GetUsers()...)
		}
	}
}

// resolve returns the users and the channels of r as the caller sees them.
// One the caller may not see (a deleted account, a private channel) comes
// as the services give it: a deleted user, channelForbidden.
func (c *StoriesCore) resolve(r *refs) ([]*mtproto.User, []*mtproto.Chat, error) {
	users := []*mtproto.User{}
	chats := []*mtproto.Chat{}
	if len(r.users) > 0 {
		ids := r.users
		if !slices.Contains(ids, c.MD.UserId) {
			ids = append(slices.Clone(ids), c.MD.UserId)
		}
		list, err := c.svcCtx.Dao.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: ids, To: []int64{c.MD.UserId}})
		if err != nil {
			return nil, nil, err
		}
		users = list.GetUserListByIdList(c.MD.UserId, r.users...)
	}
	if len(r.channels) > 0 {
		list, err := c.svcCtx.Dao.ChannelGetChannelListByIdList(c.ctx, &channelpb.TLChannelGetChannelListByIdList{
			SelfId: c.MD.UserId,
			IdList: r.channels,
		})
		if err != nil {
			return nil, nil, err
		}
		at := now()
		for _, mc := range list.GetDatas() {
			chats = append(chats, mc.ToChat(c.MD.UserId, at))
		}
	}
	return users, chats, nil
}
