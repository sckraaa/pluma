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
	"errors"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/channelstats"
)

// channelMessagesViews is messages.getMessagesViews in a channel: for each
// id, in order, the messageViews of the message as the account sees it,
// with the post's views, forwards and comments (replies), which the clients
// poll to keep the counts under the posts current (Android
// MessagesController.java:10579-10630, task 035); an empty one for an id it
// may not see. With increment the account's view of the posts counts first,
// so that the answer has it (task 046): a subscriber's view, or another's
// when the account is no member (biz channel IncrementViews).
//
// A forwarded copy of a post of another channel counts its views to the
// post (task 103), from a channel or from a group, as a forward to a
// private chat does (MessagesGetMessagesViews). A copy in a supergroup
// shows the post's counters, as the clients keep them on a forward to
// anything but a broadcast channel; a copy in a broadcast channel is a post
// of its own, with its own counters, which the clients start at 1 (Android
// SendMessagesHelper.java:2635-2646), so its view counts to both.
func (c *MessagesCore) channelMessagesViews(channelId int64, ids []int32, increment bool) (*mtproto.Messages_MessageViews, error) {
	if increment {
		if _, err := c.svcCtx.Dao.Channel.ChannelIncrementMessageViews(c.ctx, &channelpb.TLChannelIncrementMessageViews{
			SelfId: c.MD.UserId, ChannelId: channelId, Id: ids, Source: int32(channelstats.ViewSubscriber),
		}); err != nil {
			c.Logger.Errorf("messages.getMessagesViews - error: %v", err)
			return nil, err
		}
	}
	messages, err := c.svcCtx.Dao.Channel.ChannelGetMessages(c.ctx, &channelpb.TLChannelGetMessages{SelfId: c.MD.UserId, ChannelId: channelId, Id: ids})
	if err != nil {
		c.Logger.Errorf("messages.getMessagesViews - error: %v", err)
		return nil, err
	}
	byId := map[int32]*mtproto.Message{}
	copies := map[channelstats.ViewSource]map[int64][]int32{}
	for _, m := range messages.GetDatas() {
		byId[m.GetId()] = m
		from, post, ok := forwardedCopy(channelId, m)
		if !ok {
			continue
		}
		source := channelstats.ViewGroup
		if m.GetPost() {
			source = channelstats.ViewChannel
		}
		if copies[source] == nil {
			copies[source] = map[int64][]int32{}
		}
		copies[source][from] = append(copies[source][from], post)
	}
	originals := map[int64]map[int32]*mtproto.Message{}
	for source, posts := range copies {
		got, err := c.originalPosts(posts, source, increment)
		if err != nil {
			c.Logger.Errorf("messages.getMessagesViews - error: %v", err)
			return nil, err
		}
		for from, byPost := range got {
			if originals[from] == nil {
				originals[from] = byPost
				continue
			}
			for id, m := range byPost {
				originals[from][id] = m
			}
		}
	}
	out := &mtproto.Messages_MessageViews{Views: make([]*mtproto.MessageViews, 0, len(ids))}
	h := mtproto.NewIDListHelper(c.MD.UserId)
	h.AppendChannels(channelId)
	for _, id := range ids {
		m := byId[id]
		v := &mtproto.MessageViews{Views: m.GetViews(), Forwards: m.GetForwards()}
		if from, post, ok := forwardedCopy(channelId, m); ok && !m.GetPost() {
			// A post the account may not read leaves the copy as it was
			// forwarded.
			if o := originals[from][post]; o != nil {
				v.Views, v.Forwards = o.GetViews(), o.GetForwards()
			}
		}
		if r := m.GetReplies(); r != nil {
			v.Replies = r
			for _, p := range r.GetRecentRepliers() {
				h.AppendUsers(p.GetUserId())
			}
			if g := r.GetChannelId().GetValue(); g != 0 {
				h.AppendChannels(g)
			}
		}
		out.Views = append(out.Views, mtproto.MakeTLMessageViews(v).To_MessageViews())
	}
	users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id: h.UserIdList, Privacy: true, HasTo: true, To: []int64{c.MD.UserId},
	})
	if err != nil {
		return nil, err
	}
	out.Users = users.GetUserListByIdList(c.MD.UserId, h.UserIdList...)
	channels, err := c.svcCtx.Dao.Channel.ChannelGetChannelListByIdList(c.ctx, &channelpb.TLChannelGetChannelListByIdList{SelfId: c.MD.UserId, IdList: h.ChannelIdList})
	if err != nil {
		return nil, err
	}
	out.Chats = channels.GetChannelListByIdList(c.MD.UserId)
	return mtproto.MakeTLMessagesMessageViews(out).To_Messages_MessageViews(), nil
}

// postOrigin is the post of a broadcast channel that m is a forward of:
// fwd_from names the channel and its channel_post, which a forward of a
// post alone has (messages.forwardMessages, forwardMessages_handler.go:
// 276-284; forwardOrigin gives a message of a group no channel_post).
func postOrigin(m *mtproto.Message) (channelId int64, post int32, ok bool) {
	fwd := m.GetFwdFrom()
	channelId, post = fwd.GetFromId().GetChannelId(), fwd.GetChannelPost().GetValue()
	return channelId, post, channelId != 0 && post != 0
}

// forwardedCopy is postOrigin for the message m of the channel channelId,
// a copy of a post of another channel. The automatic copy of a post in its
// discussion group is none: it alone keeps saved_from_peer in a channel
// (msg channel_send.go postCopy; a forward to a channel drops it), and it
// is the post's thread, not a forward of it.
func forwardedCopy(channelId int64, m *mtproto.Message) (int64, int32, bool) {
	if m.GetFwdFrom().GetSavedFromPeer() != nil {
		return 0, 0, false
	}
	from, post, ok := postOrigin(m)
	return from, post, ok && from != channelId
}

// originalPosts reads the posts of channels that copies were forwarded
// from, as the account sees them, by channel and id; with increment it
// first counts the account's view of them from source. A channel the
// account may not read, or that is gone, gives nothing and counts nothing:
// the copy stays as it is.
func (c *MessagesCore) originalPosts(posts map[int64][]int32, source channelstats.ViewSource, increment bool) (map[int64]map[int32]*mtproto.Message, error) {
	out := make(map[int64]map[int32]*mtproto.Message, len(posts))
	for channelId, ids := range posts {
		if increment {
			_, err := c.svcCtx.Dao.Channel.ChannelIncrementMessageViews(c.ctx, &channelpb.TLChannelIncrementMessageViews{
				SelfId: c.MD.UserId, ChannelId: channelId, Id: ids, Source: int32(source),
			})
			if unreadable(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
		}
		messages, err := c.svcCtx.Dao.Channel.ChannelGetMessages(c.ctx, &channelpb.TLChannelGetMessages{SelfId: c.MD.UserId, ChannelId: channelId, Id: ids})
		if unreadable(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		byId := make(map[int32]*mtproto.Message, len(messages.GetDatas()))
		for _, m := range messages.GetDatas() {
			byId[m.GetId()] = m
		}
		out[channelId] = byId
	}
	return out, nil
}

// unreadable tells whether err is a channel the account may not read, or
// one that is gone.
func unreadable(err error) bool {
	return errors.Is(err, mtproto.ErrChannelPrivate) || errors.Is(err, mtproto.ErrChannelInvalid)
}
