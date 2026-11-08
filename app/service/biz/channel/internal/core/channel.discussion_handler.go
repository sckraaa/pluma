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
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// ChannelSetDiscussionGroup
// channel.setDiscussionGroup self_id:long perm_auth_key_id:long channel_id:long group_id:long = Bool;
//
// self_id links or unlinks the discussion group (dao.SetDiscussionGroup)
// with the channels locked; the members of every channel whose link
// changed load it again.
func (c *ChannelCore) ChannelSetDiscussionGroup(in *channel.TLChannelSetDiscussionGroup) (*mtproto.Bool, error) {
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		changed, err := c.svcCtx.Dao.SetDiscussionGroup(b, in.ChannelId, in.GroupId)
		if err != nil {
			return err
		}
		for _, id := range changed {
			if err = c.svcCtx.Dao.EmitChannelPush(b, in.PermAuthKeyId, &dao.ChannelChange{ChannelId: id, Reload: true}); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, c.membershipError("channel.setDiscussionGroup", err)
	}
	return mtproto.BoolTrue, nil
}

// ChannelGetGroupsForDiscussion
// channel.getGroupsForDiscussion self_id:long = Vector<long>;
func (c *ChannelCore) ChannelGetGroupsForDiscussion(in *channel.TLChannelGetGroupsForDiscussion) (*channel.Vector_Long, error) {
	ids, err := c.svcCtx.Dao.GroupsForDiscussion(c.ctx, in.SelfId)
	if err != nil {
		c.Logger.Errorf("channel.getGroupsForDiscussion - error: %v", err)
		return nil, err
	}
	return &channel.Vector_Long{Datas: ids}, nil
}

// ChannelGetDiscussionMessage
// channel.getDiscussionMessage self_id:long channel_id:long msg_id:int = messages.DiscussionMessage;
//
// The thread of a post (its copy in the discussion group) or of a message
// of a supergroup, as self_id sees it: the message the thread starts from,
// the newest reply, how far self_id read it and how many replies it has
// not read (core.telegram.org/method/messages.getDiscussionMessage). The
// clients then load the replies by the group and that message
// (Android ChatActivity.java:35598-35680). A group self_id may not read is
// CHANNEL_PRIVATE.
func (c *ChannelCore) ChannelGetDiscussionMessage(in *channel.TLChannelGetDiscussionMessage) (*mtproto.Messages_DiscussionMessage, error) {
	d := c.svcCtx.Dao
	forum, err := d.IsForum(c.ctx, in.ChannelId)
	if err != nil {
		return nil, err
	}
	if forum {
		return c.topicDiscussion(in)
	}
	if _, err = d.Messages(c.ctx, in.SelfId, in.ChannelId, []int32{in.MsgId}); err != nil {
		return nil, err
	}
	t, err := d.ThreadOf(c.ctx, in.ChannelId, in.MsgId)
	if err != nil {
		return nil, err
	}
	top, err := d.Messages(c.ctx, in.SelfId, t.GroupId, []int32{t.TopId})
	if err != nil {
		return nil, err
	}
	if len(top) == 0 {
		return nil, mtproto.ErrMsgIdInvalid
	}
	readMax, unread, err := d.ThreadRead(c.ctx, in.SelfId, t)
	if err != nil {
		return nil, err
	}
	out := &mtproto.Messages_DiscussionMessage{
		Messages:    top,
		UnreadCount: unread,
		Chats:       []*mtproto.Chat{},
		Users:       []*mtproto.User{},
	}
	if t.MaxId != 0 {
		out.MaxId = mtproto.MakeFlagsInt32(t.MaxId)
	}
	if readMax != 0 {
		out.ReadInboxMaxId = mtproto.MakeFlagsInt32(readMax)
	}
	return mtproto.MakeTLMessagesDiscussionMessage(out).To_Messages_DiscussionMessage(), nil
}

// ChannelGetReplies
// channel.getReplies self_id:long channel_id:long msg_id:int offset_id:int offset_date:int add_offset:int limit:int max_id:int min_id:int = messages.Messages;
//
// A page of the replies of the thread of msg_id (a post: its comments),
// newest first, as messages.channelMessages of the group with the count of
// all the replies (dao.Search).
func (c *ChannelCore) ChannelGetReplies(in *channel.TLChannelGetReplies) (*mtproto.Messages_Messages, error) {
	forum, err := c.svcCtx.Dao.IsForum(c.ctx, in.ChannelId)
	if err != nil {
		return nil, err
	}
	if forum {
		return c.topicReplies(in)
	}
	t, err := c.svcCtx.Dao.ThreadOf(c.ctx, in.ChannelId, in.MsgId)
	if err != nil {
		return nil, err
	}
	r, err := c.search(in.SelfId, t.GroupId, dao.SearchQuery{MediaType: mtproto.MEDIA_EMPTY, ThreadTop: t.TopId},
		in.OffsetId, in.AddOffset, in.Limit, in.MaxId, in.MinId)
	if err != nil {
		c.Logger.Errorf("channel.getReplies - error: %v", err)
	}
	return r, err
}

// ChannelReadDiscussion
// channel.readDiscussion self_id:long perm_auth_key_id:long channel_id:long msg_id:int read_max_id:int = Bool;
//
// self_id read the thread of msg_id up to read_max_id (dao.ReadThread).
func (c *ChannelCore) ChannelReadDiscussion(in *channel.TLChannelReadDiscussion) (*mtproto.Bool, error) {
	forum, err := c.svcCtx.Dao.IsForum(c.ctx, in.ChannelId)
	if err != nil {
		return nil, err
	}
	if forum {
		return c.readTopic(in)
	}
	t, err := c.svcCtx.Dao.ThreadOf(c.ctx, in.ChannelId, in.MsgId)
	if err != nil {
		return nil, err
	}
	v, err := c.svcCtx.Dao.ViewerOf(c.ctx, in.SelfId, t.GroupId)
	if err != nil {
		return nil, err
	}
	if !v.CanRead {
		return nil, mtproto.ErrChannelPrivate
	}
	if _, err = c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		return c.svcCtx.Dao.ReadThread(b, t.GroupId, t.TopId, in.ReadMaxId)
	}); err != nil {
		return nil, c.membershipError("channel.readDiscussion", err)
	}
	return mtproto.BoolTrue, nil
}
