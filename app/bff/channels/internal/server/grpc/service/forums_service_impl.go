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

package service

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/channels/internal/core"
)

// The forums and topics of supergroups (RPCForums, task 060).

var _ mtproto.RPCForumsServer = (*Service)(nil)

// MessagesGetForumTopics
// messages.getForumTopics#3ba47bff flags:# peer:InputPeer q:flags.0?string offset_date:int offset_id:int offset_topic:int limit:int = messages.ForumTopics;
func (s *Service) MessagesGetForumTopics(ctx context.Context, request *mtproto.TLMessagesGetForumTopics) (*mtproto.Messages_ForumTopics, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getForumTopics - metadata: {%s}", c.MD)
	return c.MessagesGetForumTopics(request)
}

// MessagesGetForumTopicsByID
// messages.getForumTopicsByID#af0a4a08 peer:InputPeer topics:Vector<int> = messages.ForumTopics;
func (s *Service) MessagesGetForumTopicsByID(ctx context.Context, request *mtproto.TLMessagesGetForumTopicsByID) (*mtproto.Messages_ForumTopics, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getForumTopicsByID - metadata: {%s}", c.MD)
	return c.MessagesGetForumTopicsByID(request)
}

// MessagesEditForumTopic
// messages.editForumTopic#cecc1134 flags:# peer:InputPeer topic_id:int title:flags.0?string icon_emoji_id:flags.1?long closed:flags.2?Bool hidden:flags.3?Bool = Updates;
func (s *Service) MessagesEditForumTopic(ctx context.Context, request *mtproto.TLMessagesEditForumTopic) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.editForumTopic - metadata: {%s}", c.MD)
	return c.MessagesEditForumTopic(request)
}

// MessagesUpdatePinnedForumTopic
// messages.updatePinnedForumTopic#175df251 peer:InputPeer topic_id:int pinned:Bool = Updates;
func (s *Service) MessagesUpdatePinnedForumTopic(ctx context.Context, request *mtproto.TLMessagesUpdatePinnedForumTopic) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.updatePinnedForumTopic - metadata: {%s}", c.MD)
	return c.MessagesUpdatePinnedForumTopic(request)
}

// MessagesReorderPinnedForumTopics
// messages.reorderPinnedForumTopics#e7841f0 flags:# force:flags.0?true peer:InputPeer order:Vector<int> = Updates;
func (s *Service) MessagesReorderPinnedForumTopics(ctx context.Context, request *mtproto.TLMessagesReorderPinnedForumTopics) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.reorderPinnedForumTopics - metadata: {%s}", c.MD)
	return c.MessagesReorderPinnedForumTopics(request)
}

// MessagesCreateForumTopic
// messages.createForumTopic#2f98c3d5 flags:# title_missing:flags.4?true peer:InputPeer title:string icon_color:flags.0?int icon_emoji_id:flags.3?long random_id:long send_as:flags.2?InputPeer = Updates;
func (s *Service) MessagesCreateForumTopic(ctx context.Context, request *mtproto.TLMessagesCreateForumTopic) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.createForumTopic - metadata: {%s}", c.MD)
	return c.MessagesCreateForumTopic(request)
}

// MessagesDeleteTopicHistory
// messages.deleteTopicHistory#d2816f10 peer:InputPeer top_msg_id:int = messages.AffectedHistory;
func (s *Service) MessagesDeleteTopicHistory(ctx context.Context, request *mtproto.TLMessagesDeleteTopicHistory) (*mtproto.Messages_AffectedHistory, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.deleteTopicHistory - metadata: {%s}", c.MD)
	return c.MessagesDeleteTopicHistory(request)
}

// ChannelsToggleForum
// channels.toggleForum#3ff75734 channel:InputChannel enabled:Bool tabs:Bool = Updates;
func (s *Service) ChannelsToggleForum(ctx context.Context, request *mtproto.TLChannelsToggleForum) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.toggleForum - metadata: {%s}", c.MD)
	return c.ChannelsToggleForum(request)
}

// ChannelsToggleViewForumAsMessages
// channels.toggleViewForumAsMessages#9738bb15 channel:InputChannel enabled:Bool = Updates;
func (s *Service) ChannelsToggleViewForumAsMessages(ctx context.Context, request *mtproto.TLChannelsToggleViewForumAsMessages) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.toggleViewForumAsMessages - metadata: {%s}", c.MD)
	return c.ChannelsToggleViewForumAsMessages(request)
}

// ChannelsCreateForumTopic
// channels.createForumTopic#f40c0224 flags:# channel:InputChannel title:string icon_color:flags.0?int icon_emoji_id:flags.3?long random_id:long send_as:flags.2?InputPeer = Updates;
func (s *Service) ChannelsCreateForumTopic(ctx context.Context, request *mtproto.TLChannelsCreateForumTopic) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.createForumTopic - metadata: {%s}", c.MD)
	return c.ChannelsCreateForumTopic(request)
}

// ChannelsGetForumTopics
// channels.getForumTopics#de560d1 flags:# channel:InputChannel q:flags.0?string offset_date:int offset_id:int offset_topic:int limit:int = messages.ForumTopics;
func (s *Service) ChannelsGetForumTopics(ctx context.Context, request *mtproto.TLChannelsGetForumTopics) (*mtproto.Messages_ForumTopics, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.getForumTopics - metadata: {%s}", c.MD)
	return c.ChannelsGetForumTopics(request)
}

// ChannelsGetForumTopicsByID
// channels.getForumTopicsByID#b0831eb9 channel:InputChannel topics:Vector<int> = messages.ForumTopics;
func (s *Service) ChannelsGetForumTopicsByID(ctx context.Context, request *mtproto.TLChannelsGetForumTopicsByID) (*mtproto.Messages_ForumTopics, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.getForumTopicsByID - metadata: {%s}", c.MD)
	return c.ChannelsGetForumTopicsByID(request)
}

// ChannelsEditForumTopic
// channels.editForumTopic#f4dfa185 flags:# channel:InputChannel topic_id:int title:flags.0?string icon_emoji_id:flags.1?long closed:flags.2?Bool hidden:flags.3?Bool = Updates;
func (s *Service) ChannelsEditForumTopic(ctx context.Context, request *mtproto.TLChannelsEditForumTopic) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.editForumTopic - metadata: {%s}", c.MD)
	return c.ChannelsEditForumTopic(request)
}

// ChannelsUpdatePinnedForumTopic
// channels.updatePinnedForumTopic#6c2d9026 channel:InputChannel topic_id:int pinned:Bool = Updates;
func (s *Service) ChannelsUpdatePinnedForumTopic(ctx context.Context, request *mtproto.TLChannelsUpdatePinnedForumTopic) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.updatePinnedForumTopic - metadata: {%s}", c.MD)
	return c.ChannelsUpdatePinnedForumTopic(request)
}

// ChannelsDeleteTopicHistory
// channels.deleteTopicHistory#34435f2d channel:InputChannel top_msg_id:int = messages.AffectedHistory;
func (s *Service) ChannelsDeleteTopicHistory(ctx context.Context, request *mtproto.TLChannelsDeleteTopicHistory) (*mtproto.Messages_AffectedHistory, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.deleteTopicHistory - metadata: {%s}", c.MD)
	return c.ChannelsDeleteTopicHistory(request)
}

// ChannelsReorderPinnedForumTopics
// channels.reorderPinnedForumTopics#2950a18f flags:# force:flags.0?true channel:InputChannel order:Vector<int> = Updates;
func (s *Service) ChannelsReorderPinnedForumTopics(ctx context.Context, request *mtproto.TLChannelsReorderPinnedForumTopics) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.reorderPinnedForumTopics - metadata: {%s}", c.MD)
	return c.ChannelsReorderPinnedForumTopics(request)
}
