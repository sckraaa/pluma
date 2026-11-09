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
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// threadChannel is the channel of a thread method's peer: a broadcast
// channel (its posts' comments) or a supergroup; any other peer has no
// threads here (PEER_ID_INVALID).
func (c *ChannelsCore) threadChannel(peer *mtproto.InputPeer) (*channelpb.MutableChannel, error) {
	p := mtproto.FromInputPeer2(c.MD.UserId, peer)
	if p.PeerType != mtproto.PEER_CHANNEL {
		return nil, mtproto.ErrPeerIdInvalid
	}
	return c.mutableChannel(mtproto.MakeTLInputChannel(&mtproto.InputChannel{ChannelId: p.PeerId, AccessHash: p.AccessHash}).To_InputChannel())
}

// MessagesGetDiscussionMessage
// messages.getDiscussionMessage#446972fd peer:InputPeer msg_id:int = messages.DiscussionMessage;
//
// The thread of a post (its copy in the discussion group) or of a message of
// a supergroup (task 048, biz channel.getDiscussionMessage), with the
// channel, the group and the users of the message.
func (c *ChannelsCore) MessagesGetDiscussionMessage(in *mtproto.TLMessagesGetDiscussionMessage) (*mtproto.Messages_DiscussionMessage, error) {
	mc, err := c.threadChannel(in.Peer)
	if err != nil {
		return nil, err
	}
	r, err := c.svcCtx.Channel.ChannelGetDiscussionMessage(c.ctx, &channelpb.TLChannelGetDiscussionMessage{
		SelfId: c.MD.UserId, ChannelId: mc.GetChannel().GetId(), MsgId: in.MsgId,
	})
	if err != nil {
		return nil, err
	}
	h := mtproto.NewIDListHelper(c.MD.UserId)
	h.PickByMessages(r.GetMessages()...)
	h.AppendChannels(mc.GetChannel().GetId())
	if r.Users, r.Chats, err = c.peers(h); err != nil {
		return nil, err
	}
	return r, nil
}

// MessagesGetReplies
// messages.getReplies#22ddd30c peer:InputPeer msg_id:int offset_id:int offset_date:int add_offset:int limit:int max_id:int min_id:int hash:long = messages.Messages;
//
// A page of a thread's replies (task 048, biz channel.getReplies): the
// clients ask by the group and the message the thread starts from
// (Android ChatActivity.java:35661-35672).
func (c *ChannelsCore) MessagesGetReplies(in *mtproto.TLMessagesGetReplies) (*mtproto.Messages_Messages, error) {
	mc, err := c.threadChannel(in.Peer)
	if err != nil {
		return nil, err
	}
	r, err := c.svcCtx.Channel.ChannelGetReplies(c.ctx, &channelpb.TLChannelGetReplies{
		SelfId: c.MD.UserId, ChannelId: mc.GetChannel().GetId(), MsgId: in.MsgId,
		OffsetId: in.OffsetId, OffsetDate: in.OffsetDate, AddOffset: in.AddOffset, Limit: in.Limit, MaxId: in.MaxId, MinId: in.MinId,
	})
	if err != nil {
		return nil, err
	}
	h := mtproto.NewIDListHelper(c.MD.UserId)
	h.PickByMessages(r.GetMessages()...)
	// The page of a topic carries its topic, by whose creator the clients
	// read the owner (tdesktop Data::ForumTopic::applyTopic).
	for _, t := range r.GetTopics() {
		h.PickByPeer(t.GetFromId())
	}
	h.AppendChannels(mc.GetChannel().GetId())
	if r.Users, r.Chats, err = c.peers(h); err != nil {
		return nil, err
	}
	return r, nil
}

// MessagesReadDiscussion
// messages.readDiscussion#f731a9f4 peer:InputPeer msg_id:int read_max_id:int = Bool;
func (c *ChannelsCore) MessagesReadDiscussion(in *mtproto.TLMessagesReadDiscussion) (*mtproto.Bool, error) {
	mc, err := c.threadChannel(in.Peer)
	if err != nil {
		return nil, err
	}
	return c.svcCtx.Channel.ChannelReadDiscussion(c.ctx, &channelpb.TLChannelReadDiscussion{
		SelfId: c.MD.UserId, PermAuthKeyId: c.MD.PermAuthKeyId, ChannelId: mc.GetChannel().GetId(), MsgId: in.MsgId, ReadMaxId: in.ReadMaxId,
	})
}

// peers are the users and the channels of h as the account sees them.
func (c *ChannelsCore) peers(h *mtproto.IDListHelper) ([]*mtproto.User, []*mtproto.Chat, error) {
	users, err := c.svcCtx.User.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
		Id: h.UserIdList, Privacy: true, HasTo: true, To: []int64{c.MD.UserId},
	})
	if err != nil {
		return nil, nil, err
	}
	channels, err := c.svcCtx.Channel.ChannelGetChannelListByIdList(c.ctx, &channelpb.TLChannelGetChannelListByIdList{SelfId: c.MD.UserId, IdList: h.ChannelIdList})
	if err != nil {
		return nil, nil, err
	}
	return users.GetUserListByIdList(c.MD.UserId, h.UserIdList...), channels.GetChannelListByIdList(c.MD.UserId), nil
}
