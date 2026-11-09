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

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// The forums and topics of supergroups (task 060, RPCForums). The clients
// of layer 228 send messages.*, whose peer is the forum
// (core.telegram.org/api/forum); the channels.* forms of earlier layers
// are served by the same code, as the schema still has them.

// ChannelsToggleForum
// channels.toggleForum#a2952b54 channel:InputChannel enabled:Bool tabs:Bool sidebar:Bool = Updates;
// channels.toggleForum#3ff75734 channel:InputChannel enabled:Bool tabs:Bool = Updates;
func (c *ChannelsCore) ChannelsToggleForum(in *mtproto.TLChannelsToggleForum) (*mtproto.Updates, error) {
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	return c.svcCtx.Channel.ChannelToggleForum(c.ctx, &channelpb.TLChannelToggleForum{
		SelfId: c.MD.UserId, PermAuthKeyId: c.MD.PermAuthKeyId, ChannelId: mc.GetChannel().GetId(),
		Enabled: in.Enabled, Tabs: mtproto.ToBool(mtproto.FromBool(in.Tabs)), Sidebar: in.Sidebar, // nil in the form of older layers
	})
}

// ChannelsToggleViewForumAsMessages
// channels.toggleViewForumAsMessages#9738bb15 channel:InputChannel enabled:Bool = Updates;
func (c *ChannelsCore) ChannelsToggleViewForumAsMessages(in *mtproto.TLChannelsToggleViewForumAsMessages) (*mtproto.Updates, error) {
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	return c.svcCtx.Channel.ChannelToggleViewForumAsMessages(c.ctx, &channelpb.TLChannelToggleViewForumAsMessages{
		SelfId: c.MD.UserId, PermAuthKeyId: c.MD.PermAuthKeyId, ChannelId: mc.GetChannel().GetId(), Enabled: in.Enabled,
	})
}

// MessagesCreateForumTopic
// messages.createForumTopic#2f98c3d5 flags:# title_missing:flags.4?true peer:InputPeer title:string icon_color:flags.0?int icon_emoji_id:flags.3?long random_id:long send_as:flags.2?InputPeer = Updates;
func (c *ChannelsCore) MessagesCreateForumTopic(in *mtproto.TLMessagesCreateForumTopic) (*mtproto.Updates, error) {
	mc, err := c.threadChannel(in.Peer)
	if err != nil {
		return nil, err
	}
	return c.createTopic(mc, in.Title, in.GetIconColor().GetValue(), in.GetIconEmojiId().GetValue(), in.TitleMissing, in.RandomId)
}

// ChannelsCreateForumTopic
// channels.createForumTopic#f40c0224 flags:# channel:InputChannel title:string icon_color:flags.0?int icon_emoji_id:flags.3?long random_id:long send_as:flags.2?InputPeer = Updates;
func (c *ChannelsCore) ChannelsCreateForumTopic(in *mtproto.TLChannelsCreateForumTopic) (*mtproto.Updates, error) {
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	return c.createTopic(mc, in.Title, in.GetIconColor().GetValue(), in.GetIconEmojiId().GetValue(), false, in.RandomId)
}

func (c *ChannelsCore) createTopic(mc *channelpb.MutableChannel, title string, color int32, emojiId int64, titleMissing bool, randomId int64) (*mtproto.Updates, error) {
	return c.svcCtx.Channel.ChannelCreateForumTopic(c.ctx, &channelpb.TLChannelCreateForumTopic{
		SelfId: c.MD.UserId, PermAuthKeyId: c.MD.PermAuthKeyId, ChannelId: mc.GetChannel().GetId(),
		Title: title, IconColor: color, IconEmojiId: emojiId, TitleMissing: mtproto.ToBool(titleMissing), RandomId: randomId,
	})
}

// MessagesEditForumTopic
// messages.editForumTopic#cecc1134 flags:# peer:InputPeer topic_id:int title:flags.0?string icon_emoji_id:flags.1?long closed:flags.2?Bool hidden:flags.3?Bool = Updates;
func (c *ChannelsCore) MessagesEditForumTopic(in *mtproto.TLMessagesEditForumTopic) (*mtproto.Updates, error) {
	mc, err := c.threadChannel(in.Peer)
	if err != nil {
		return nil, err
	}
	return c.editTopic(mc, in.TopicId, in.Title, in.IconEmojiId, in.Closed, in.Hidden)
}

// ChannelsEditForumTopic
// channels.editForumTopic#f4dfa185 flags:# channel:InputChannel topic_id:int title:flags.0?string icon_emoji_id:flags.1?long closed:flags.2?Bool hidden:flags.3?Bool = Updates;
func (c *ChannelsCore) ChannelsEditForumTopic(in *mtproto.TLChannelsEditForumTopic) (*mtproto.Updates, error) {
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	return c.editTopic(mc, in.TopicId, in.Title, in.IconEmojiId, in.Closed, in.Hidden)
}

// editTopic passes the values the method gives: each is optional, and a
// value of an optional field set says so by fields.
func (c *ChannelsCore) editTopic(mc *channelpb.MutableChannel, topicId int32, title *wrapperspb.StringValue, emoji *wrapperspb.Int64Value, closed, hidden *mtproto.Bool) (*mtproto.Updates, error) {
	req := &channelpb.TLChannelEditForumTopic{
		SelfId: c.MD.UserId, PermAuthKeyId: c.MD.PermAuthKeyId, ChannelId: mc.GetChannel().GetId(), TopicId: topicId,
		Closed: mtproto.ToBool(mtproto.FromBool(closed)), Hidden: mtproto.ToBool(mtproto.FromBool(hidden)),
	}
	if title != nil {
		req.Fields |= channelpb.TopicFieldTitle
		req.Title = title.GetValue()
	}
	if emoji != nil {
		req.Fields |= channelpb.TopicFieldIcon
		req.IconEmojiId = emoji.GetValue()
	}
	if closed != nil {
		req.Fields |= channelpb.TopicFieldClosed
	}
	if hidden != nil {
		req.Fields |= channelpb.TopicFieldHidden
	}
	return c.svcCtx.Channel.ChannelEditForumTopic(c.ctx, req)
}

// MessagesUpdatePinnedForumTopic
// messages.updatePinnedForumTopic#175df251 peer:InputPeer topic_id:int pinned:Bool = Updates;
func (c *ChannelsCore) MessagesUpdatePinnedForumTopic(in *mtproto.TLMessagesUpdatePinnedForumTopic) (*mtproto.Updates, error) {
	mc, err := c.threadChannel(in.Peer)
	if err != nil {
		return nil, err
	}
	return c.pinTopic(mc, in.TopicId, in.Pinned)
}

// ChannelsUpdatePinnedForumTopic
// channels.updatePinnedForumTopic#6c2d9026 channel:InputChannel topic_id:int pinned:Bool = Updates;
func (c *ChannelsCore) ChannelsUpdatePinnedForumTopic(in *mtproto.TLChannelsUpdatePinnedForumTopic) (*mtproto.Updates, error) {
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	return c.pinTopic(mc, in.TopicId, in.Pinned)
}

func (c *ChannelsCore) pinTopic(mc *channelpb.MutableChannel, topicId int32, pinned *mtproto.Bool) (*mtproto.Updates, error) {
	return c.svcCtx.Channel.ChannelUpdatePinnedForumTopic(c.ctx, &channelpb.TLChannelUpdatePinnedForumTopic{
		SelfId: c.MD.UserId, PermAuthKeyId: c.MD.PermAuthKeyId, ChannelId: mc.GetChannel().GetId(), TopicId: topicId, Pinned: pinned,
	})
}

// MessagesReorderPinnedForumTopics
// messages.reorderPinnedForumTopics#e7841f0 flags:# force:flags.0?true peer:InputPeer order:Vector<int> = Updates;
func (c *ChannelsCore) MessagesReorderPinnedForumTopics(in *mtproto.TLMessagesReorderPinnedForumTopics) (*mtproto.Updates, error) {
	mc, err := c.threadChannel(in.Peer)
	if err != nil {
		return nil, err
	}
	return c.reorderTopics(mc, in.Force, in.Order)
}

// ChannelsReorderPinnedForumTopics
// channels.reorderPinnedForumTopics#2950a18f flags:# force:flags.0?true channel:InputChannel order:Vector<int> = Updates;
func (c *ChannelsCore) ChannelsReorderPinnedForumTopics(in *mtproto.TLChannelsReorderPinnedForumTopics) (*mtproto.Updates, error) {
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	return c.reorderTopics(mc, in.Force, in.Order)
}

func (c *ChannelsCore) reorderTopics(mc *channelpb.MutableChannel, force bool, order []int32) (*mtproto.Updates, error) {
	return c.svcCtx.Channel.ChannelReorderPinnedForumTopics(c.ctx, &channelpb.TLChannelReorderPinnedForumTopics{
		SelfId: c.MD.UserId, PermAuthKeyId: c.MD.PermAuthKeyId, ChannelId: mc.GetChannel().GetId(),
		Force: mtproto.ToBool(force), Order: order,
	})
}

// MessagesDeleteTopicHistory
// messages.deleteTopicHistory#d2816f10 peer:InputPeer top_msg_id:int = messages.AffectedHistory;
func (c *ChannelsCore) MessagesDeleteTopicHistory(in *mtproto.TLMessagesDeleteTopicHistory) (*mtproto.Messages_AffectedHistory, error) {
	mc, err := c.threadChannel(in.Peer)
	if err != nil {
		return nil, err
	}
	return c.deleteTopic(mc, in.TopMsgId)
}

// ChannelsDeleteTopicHistory
// channels.deleteTopicHistory#34435f2d channel:InputChannel top_msg_id:int = messages.AffectedHistory;
func (c *ChannelsCore) ChannelsDeleteTopicHistory(in *mtproto.TLChannelsDeleteTopicHistory) (*mtproto.Messages_AffectedHistory, error) {
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	return c.deleteTopic(mc, in.TopMsgId)
}

func (c *ChannelsCore) deleteTopic(mc *channelpb.MutableChannel, topicId int32) (*mtproto.Messages_AffectedHistory, error) {
	return c.svcCtx.Channel.ChannelDeleteTopicHistory(c.ctx, &channelpb.TLChannelDeleteTopicHistory{
		SelfId: c.MD.UserId, PermAuthKeyId: c.MD.PermAuthKeyId, ChannelId: mc.GetChannel().GetId(), TopMsgId: topicId,
	})
}

// MessagesGetForumTopics
// messages.getForumTopics#3ba47bff flags:# peer:InputPeer q:flags.0?string offset_date:int offset_id:int offset_topic:int limit:int = messages.ForumTopics;
func (c *ChannelsCore) MessagesGetForumTopics(in *mtproto.TLMessagesGetForumTopics) (*mtproto.Messages_ForumTopics, error) {
	mc, err := c.threadChannel(in.Peer)
	if err != nil {
		return nil, err
	}
	return c.getTopics(mc, in.GetQ().GetValue(), in.OffsetDate, in.OffsetId, in.OffsetTopic, in.Limit)
}

// ChannelsGetForumTopics
// channels.getForumTopics#de560d1 flags:# channel:InputChannel q:flags.0?string offset_date:int offset_id:int offset_topic:int limit:int = messages.ForumTopics;
func (c *ChannelsCore) ChannelsGetForumTopics(in *mtproto.TLChannelsGetForumTopics) (*mtproto.Messages_ForumTopics, error) {
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	return c.getTopics(mc, in.GetQ().GetValue(), in.OffsetDate, in.OffsetId, in.OffsetTopic, in.Limit)
}

func (c *ChannelsCore) getTopics(mc *channelpb.MutableChannel, q string, offsetDate, offsetId, offsetTopic, limit int32) (*mtproto.Messages_ForumTopics, error) {
	r, err := c.svcCtx.Channel.ChannelGetForumTopics(c.ctx, &channelpb.TLChannelGetForumTopics{
		SelfId: c.MD.UserId, ChannelId: mc.GetChannel().GetId(), Q: q,
		OffsetDate: offsetDate, OffsetId: offsetId, OffsetTopic: offsetTopic, Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	return c.fillTopics(mc, r)
}

// MessagesGetForumTopicsByID
// messages.getForumTopicsByID#af0a4a08 peer:InputPeer topics:Vector<int> = messages.ForumTopics;
func (c *ChannelsCore) MessagesGetForumTopicsByID(in *mtproto.TLMessagesGetForumTopicsByID) (*mtproto.Messages_ForumTopics, error) {
	mc, err := c.threadChannel(in.Peer)
	if err != nil {
		return nil, err
	}
	return c.getTopicsById(mc, in.Topics)
}

// ChannelsGetForumTopicsByID
// channels.getForumTopicsByID#b0831eb9 channel:InputChannel topics:Vector<int> = messages.ForumTopics;
func (c *ChannelsCore) ChannelsGetForumTopicsByID(in *mtproto.TLChannelsGetForumTopicsByID) (*mtproto.Messages_ForumTopics, error) {
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	return c.getTopicsById(mc, in.Topics)
}

func (c *ChannelsCore) getTopicsById(mc *channelpb.MutableChannel, topics []int32) (*mtproto.Messages_ForumTopics, error) {
	r, err := c.svcCtx.Channel.ChannelGetForumTopicsByID(c.ctx, &channelpb.TLChannelGetForumTopicsByID{
		SelfId: c.MD.UserId, ChannelId: mc.GetChannel().GetId(), Topics: topics,
	})
	if err != nil {
		return nil, err
	}
	return c.fillTopics(mc, r)
}

// fillTopics adds the forum and the users of its topics and messages, which
// the clients read the creators of the topics from
// (tdesktop ForumTopic::applyCreator).
func (c *ChannelsCore) fillTopics(mc *channelpb.MutableChannel, r *mtproto.Messages_ForumTopics) (*mtproto.Messages_ForumTopics, error) {
	h := mtproto.NewIDListHelper(c.MD.UserId)
	h.PickByMessages(r.GetMessages()...)
	for _, t := range r.GetTopics() {
		if id := t.GetFromId().GetUserId(); id != 0 {
			h.AppendUsers(id)
		}
	}
	h.AppendChannels(mc.GetChannel().GetId())
	var err error
	if r.Users, r.Chats, err = c.peers(h); err != nil {
		return nil, err
	}
	return r, nil
}
