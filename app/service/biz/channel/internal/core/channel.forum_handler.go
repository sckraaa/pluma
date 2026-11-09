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
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"google.golang.org/protobuf/proto"
)

// ChannelToggleForum
// channel.toggleForum self_id:long perm_auth_key_id:long channel_id:long enabled:Bool tabs:Bool sidebar:Bool = Updates;
//
// self_id turns the forum on or off (dao.ToggleForum); the members load the
// channel again. The clients process the answer as Updates
// (Android MessagesController.toggleChannelForum).
func (c *ChannelCore) ChannelToggleForum(in *channel.TLChannelToggleForum) (*mtproto.Updates, error) {
	return c.editRights("channel.toggleForum", in.SelfId, in.PermAuthKeyId, in.ChannelId, func(b *userbox.Box) (*dao.ChannelChange, error) {
		return c.svcCtx.Dao.ToggleForum(b, in.ChannelId, mtproto.FromBool(in.Enabled), mtproto.FromBool(in.Tabs), in.Sidebar)
	})
}

// ChannelToggleViewForumAsMessages
// channel.toggleViewForumAsMessages self_id:long perm_auth_key_id:long channel_id:long enabled:Bool = Updates;
//
// self_id's own choice (dao.ToggleViewForumAsMessages); the answer holds the
// update, which Android applies from it (TopicsController.java:854-862).
func (c *ChannelCore) ChannelToggleViewForumAsMessages(in *channel.TLChannelToggleViewForumAsMessages) (*mtproto.Updates, error) {
	var u *mtproto.Update
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		var err error
		u, err = c.svcCtx.Dao.ToggleViewForumAsMessages(b, in.ChannelId, mtproto.FromBool(in.Enabled))
		return err
	}); err != nil {
		return nil, c.membershipError("channel.toggleViewForumAsMessages", err)
	}
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: []*mtproto.Update{u},
		Users:   []*mtproto.User{},
		Chats:   []*mtproto.Chat{},
		Date:    int32(time.Now().Unix()),
	}).To_Updates(), nil
}

// ChannelCreateForumTopic
// channel.createForumTopic self_id:long perm_auth_key_id:long channel_id:long title:string icon_color:int icon_emoji_id:long title_missing:Bool random_id:long = Updates;
//
// The answer holds updateMessageID and updateNewChannelMessage of the
// topic's service message: tdesktop applies it by the random_id
// (data_histories.cpp:1079-1095), iOS takes the topic's id from it
// (ForumChannels.swift:229-262).
func (c *ChannelCore) ChannelCreateForumTopic(in *channel.TLChannelCreateForumTopic) (*mtproto.Updates, error) {
	return c.topicChange("channel.createForumTopic", in.SelfId, in.PermAuthKeyId, in.ChannelId, func(b *userbox.Box) (*dao.TopicChange, error) {
		return c.svcCtx.Dao.CreateForumTopic(b, in.ChannelId, in.Title, in.IconColor, in.IconEmojiId, mtproto.FromBool(in.TitleMissing), in.RandomId)
	})
}

// ChannelEditForumTopic
// channel.editForumTopic self_id:long perm_auth_key_id:long channel_id:long topic_id:int fields:int title:string icon_emoji_id:long closed:Bool hidden:Bool = Updates;
func (c *ChannelCore) ChannelEditForumTopic(in *channel.TLChannelEditForumTopic) (*mtproto.Updates, error) {
	return c.topicChange("channel.editForumTopic", in.SelfId, in.PermAuthKeyId, in.ChannelId, func(b *userbox.Box) (*dao.TopicChange, error) {
		return c.svcCtx.Dao.EditForumTopic(b, in.ChannelId, in.TopicId, dao.TopicEdit{
			Fields: in.Fields, Title: in.Title, IconEmojiId: in.IconEmojiId,
			Closed: mtproto.FromBool(in.Closed), Hidden: mtproto.FromBool(in.Hidden),
		})
	})
}

// ChannelUpdatePinnedForumTopic
// channel.updatePinnedForumTopic self_id:long perm_auth_key_id:long channel_id:long topic_id:int pinned:Bool = Updates;
func (c *ChannelCore) ChannelUpdatePinnedForumTopic(in *channel.TLChannelUpdatePinnedForumTopic) (*mtproto.Updates, error) {
	return c.topicChange("channel.updatePinnedForumTopic", in.SelfId, in.PermAuthKeyId, in.ChannelId, func(b *userbox.Box) (*dao.TopicChange, error) {
		return c.svcCtx.Dao.UpdatePinnedForumTopic(b, in.ChannelId, in.TopicId, mtproto.FromBool(in.Pinned))
	})
}

// ChannelReorderPinnedForumTopics
// channel.reorderPinnedForumTopics self_id:long perm_auth_key_id:long channel_id:long force:Bool order:Vector<int> = Updates;
func (c *ChannelCore) ChannelReorderPinnedForumTopics(in *channel.TLChannelReorderPinnedForumTopics) (*mtproto.Updates, error) {
	return c.topicChange("channel.reorderPinnedForumTopics", in.SelfId, in.PermAuthKeyId, in.ChannelId, func(b *userbox.Box) (*dao.TopicChange, error) {
		return c.svcCtx.Dao.ReorderPinnedForumTopics(b, in.ChannelId, in.Order, mtproto.FromBool(in.Force))
	})
}

// topicChange runs a change of a topic of selfId in its box transaction and
// pushes it to the members; the answer holds what was pushed, with the
// channel.
func (c *ChannelCore) topicChange(method string, selfId, permAuthKeyId, channelId int64, fn func(b *userbox.Box) (*dao.TopicChange, error)) (*mtproto.Updates, error) {
	var change *dao.TopicChange
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, selfId, func(b *userbox.Box) error {
		var err error
		if change, err = fn(b); err != nil || change.Repeat {
			return err
		}
		return c.svcCtx.Dao.EmitChannelPush(b, permAuthKeyId, &change.ChannelChange)
	}); err != nil {
		return nil, c.membershipError(method, err)
	}
	mc, err := c.svcCtx.Dao.GetMutableChannel(c.ctx, channelId, []int64{selfId})
	if err != nil {
		return nil, err
	}
	now := int32(time.Now().Unix())
	var updates []*mtproto.Update
	if change.Message != nil {
		shown := proto.Clone(change.Message).(*mtproto.Message)
		shown.Out = true
		if change.RandomId != 0 {
			updates = append(updates, mtproto.MakeTLUpdateMessageID(&mtproto.Update{Id_INT32: shown.Id, RandomId: change.RandomId}).To_Update())
		}
		updates = append(updates, mtproto.MakeTLUpdateNewChannelMessage(&mtproto.Update{Message_MESSAGE: shown, Pts_INT32: change.Pts, PtsCount: 1}).To_Update())
	}
	updates = append(updates, change.Updates...)
	return mtproto.MakeTLUpdates(&mtproto.Updates{
		Updates: updates,
		Users:   []*mtproto.User{},
		Chats:   []*mtproto.Chat{mc.ToChat(selfId, now)},
		Date:    now,
	}).To_Updates(), nil
}

// ChannelDeleteTopicHistory
// channel.deleteTopicHistory self_id:long perm_auth_key_id:long channel_id:long top_msg_id:int = messages.AffectedHistory;
//
// A portion of the topic (dao.DeleteTopicHistory); offset above 0 asks the
// client to call again.
func (c *ChannelCore) ChannelDeleteTopicHistory(in *channel.TLChannelDeleteTopicHistory) (*mtproto.Messages_AffectedHistory, error) {
	var (
		del  *dao.Deleted
		more bool
	)
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		var err error
		if del, more, err = c.svcCtx.Dao.DeleteTopicHistory(b, in.ChannelId, in.TopMsgId); err != nil {
			return err
		}
		return c.svcCtx.Dao.EmitDeletePush(b, in.PermAuthKeyId, del)
	}); err != nil {
		return nil, c.membershipError("channel.deleteTopicHistory", err)
	}
	pts := del.Pts
	if pts == 0 {
		var err error
		if pts, err = c.svcCtx.Dao.ChannelPts(c.ctx, in.ChannelId); err != nil {
			return nil, err
		}
	}
	offset := int32(0)
	if more {
		offset = 1
	}
	return mtproto.MakeTLMessagesAffectedHistory(&mtproto.Messages_AffectedHistory{Pts: pts, PtsCount: del.PtsCount, Offset: offset}).To_Messages_AffectedHistory(), nil
}

// ChannelGetForumTopics
// channel.getForumTopics self_id:long channel_id:long q:string offset_date:int offset_id:int offset_topic:int limit:int = messages.ForumTopics;
func (c *ChannelCore) ChannelGetForumTopics(in *channel.TLChannelGetForumTopics) (*mtproto.Messages_ForumTopics, error) {
	r, err := c.svcCtx.Dao.ForumTopics(c.ctx, in.SelfId, in.ChannelId, in.Q, in.OffsetId, in.OffsetTopic, in.Limit)
	if err != nil {
		return nil, c.membershipError("channel.getForumTopics", err)
	}
	return r, nil
}

// ChannelGetForumTopicsByID
// channel.getForumTopicsByID self_id:long channel_id:long topics:Vector<int> = messages.ForumTopics;
func (c *ChannelCore) ChannelGetForumTopicsByID(in *channel.TLChannelGetForumTopicsByID) (*mtproto.Messages_ForumTopics, error) {
	r, err := c.svcCtx.Dao.ForumTopicsByID(c.ctx, in.SelfId, in.ChannelId, in.Topics)
	if err != nil {
		return nil, c.membershipError("channel.getForumTopicsByID", err)
	}
	return r, nil
}

// ChannelSearchTopic
// channel.searchTopic self_id:long channel_id:long top_msg_id:int q:string from_id:long media_type:int unread_mentions:Bool min_date:int max_date:int offset_id:int add_offset:int limit:int max_id:int min_id:int = messages.Messages;
//
// A search in a topic of a forum, or in a thread of a supergroup that is
// not one.
func (c *ChannelCore) ChannelSearchTopic(in *channel.TLChannelSearchTopic) (*mtproto.Messages_Messages, error) {
	q := dao.SearchQuery{Q: in.Q, FromId: in.FromId, MediaType: in.MediaType, MinDate: in.MinDate, MaxDate: in.MaxDate}
	if mtproto.FromBool(in.UnreadMentions) {
		q.UnreadMentionsOf = in.SelfId
	}
	if err := c.applyTop(in.ChannelId, in.TopMsgId, &q); err != nil {
		return nil, err
	}
	r, err := c.search(in.SelfId, in.ChannelId, q, in.OffsetId, in.AddOffset, in.Limit, in.MaxId, in.MinId)
	if err != nil {
		return nil, c.membershipError("channel.searchTopic", err)
	}
	return r, nil
}

// ChannelReadTopicMentions
// channel.readTopicMentions self_id:long perm_auth_key_id:long channel_id:long top_msg_id:int = messages.AffectedHistory;
func (c *ChannelCore) ChannelReadTopicMentions(in *channel.TLChannelReadTopicMentions) (*mtproto.Messages_AffectedHistory, error) {
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		return c.svcCtx.Dao.ReadTopicMentions(b, in.ChannelId, in.TopMsgId)
	}); err != nil {
		return nil, c.membershipError("channel.readTopicMentions", err)
	}
	pts, err := c.svcCtx.Dao.ChannelPts(c.ctx, in.ChannelId)
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLMessagesAffectedHistory(&mtproto.Messages_AffectedHistory{Pts: pts}).To_Messages_AffectedHistory(), nil
}

// ChannelUnpinAllTopicMessages
// channel.unpinAllTopicMessages self_id:long perm_auth_key_id:long channel_id:long top_msg_id:int = messages.AffectedHistory;
//
// The answer is the pts of the unpin with count 1, or the channel's pts with
// count 0 when nothing was pinned.
func (c *ChannelCore) ChannelUnpinAllTopicMessages(in *channel.TLChannelUnpinAllTopicMessages) (*mtproto.Messages_AffectedHistory, error) {
	var (
		u   *mtproto.Update
		pts int32
	)
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		var err error
		if u, pts, err = c.svcCtx.Dao.UnpinAllTopic(b, in.ChannelId, in.TopMsgId); err != nil || u == nil {
			return err
		}
		return c.svcCtx.Dao.EmitChannelPush(b, in.PermAuthKeyId, &dao.ChannelChange{ChannelId: in.ChannelId, Updates: []*mtproto.Update{u}})
	}); err != nil {
		return nil, c.membershipError("channel.unpinAllTopicMessages", err)
	}
	count := int32(1)
	if u == nil {
		var err error
		if pts, err = c.svcCtx.Dao.ChannelPts(c.ctx, in.ChannelId); err != nil {
			return nil, err
		}
		count = 0
	}
	return mtproto.MakeTLMessagesAffectedHistory(&mtproto.Messages_AffectedHistory{Pts: pts, PtsCount: count}).To_Messages_AffectedHistory(), nil
}

// topicDiscussion answers messages.getDiscussionMessage in a forum: the
// topic's root, the newest message and what self_id has not read.
func (c *ChannelCore) topicDiscussion(in *channel.TLChannelGetDiscussionMessage) (*mtproto.Messages_DiscussionMessage, error) {
	root, maxId, readMax, unread, err := c.svcCtx.Dao.TopicThread(c.ctx, in.SelfId, in.ChannelId, in.MsgId)
	if err != nil {
		return nil, c.membershipError("channel.getDiscussionMessage", err)
	}
	top, err := c.svcCtx.Dao.Messages(c.ctx, in.SelfId, in.ChannelId, []int32{root})
	if err != nil {
		return nil, err
	}
	out := &mtproto.Messages_DiscussionMessage{
		Messages:    top,
		UnreadCount: unread,
		Chats:       []*mtproto.Chat{},
		Users:       []*mtproto.User{},
	}
	if maxId != 0 {
		out.MaxId = mtproto.MakeFlagsInt32(maxId)
	}
	if readMax != 0 {
		out.ReadInboxMaxId = mtproto.MakeFlagsInt32(readMax)
	}
	return mtproto.MakeTLMessagesDiscussionMessage(out).To_Messages_DiscussionMessage(), nil
}

// topicReplies answers messages.getReplies in a forum, the history of a
// topic by its id (Android MessagesController.java:11596, tdesktop
// data_replies_list.cpp:538).
func (c *ChannelCore) topicReplies(in *channel.TLChannelGetReplies) (*mtproto.Messages_Messages, error) {
	messages, count, err := c.svcCtx.Dao.TopicPage(c.ctx, in.SelfId, in.ChannelId, in.MsgId, in.OffsetId, in.OffsetDate, in.AddOffset, in.Limit, in.MaxId, in.MinId)
	if err != nil {
		return nil, c.membershipError("channel.getReplies", err)
	}
	return c.channelMessages(in.SelfId, in.ChannelId, messages, count)
}

// readTopic answers messages.readDiscussion in a forum (dao.ReadTopic).
func (c *ChannelCore) readTopic(in *channel.TLChannelReadDiscussion) (*mtproto.Bool, error) {
	if _, err := c.svcCtx.Dao.RunBox(c.ctx, in.SelfId, func(b *userbox.Box) error {
		return c.svcCtx.Dao.ReadTopic(b, in.ChannelId, in.MsgId, in.ReadMaxId)
	}); err != nil {
		return nil, c.membershipError("channel.readDiscussion", err)
	}
	return mtproto.BoolTrue, nil
}
