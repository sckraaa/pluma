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

	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/core"
)

// ChannelGetMutableChannel
// channel.getMutableChannel channel_id:long id:Vector<long> = MutableChannel;
func (s *Service) ChannelGetMutableChannel(ctx context.Context, request *channel.TLChannelGetMutableChannel) (*channel.MutableChannel, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.getMutableChannel - metadata: {%s}", c.MD)

	r, err := c.ChannelGetMutableChannel(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelGetChannelListByIdList
// channel.getChannelListByIdList self_id:long id_list:Vector<long> = Vector<MutableChannel>;
func (s *Service) ChannelGetChannelListByIdList(ctx context.Context, request *channel.TLChannelGetChannelListByIdList) (*channel.Vector_MutableChannel, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.getChannelListByIdList - metadata: {%s}", c.MD)

	r, err := c.ChannelGetChannelListByIdList(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelCreateChannel
// channel.createChannel creator_id:long title:string about:string broadcast:Bool megagroup:Bool = Updates;
func (s *Service) ChannelCreateChannel(ctx context.Context, request *channel.TLChannelCreateChannel) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.createChannel - metadata: {%s}", c.MD)

	r, err := c.ChannelCreateChannel(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelGetChannelDialogs
// channel.getChannelDialogs self_id:long id_list:Vector<long> = Vector<Dialog>;
func (s *Service) ChannelGetChannelDialogs(ctx context.Context, request *channel.TLChannelGetChannelDialogs) (*channel.Vector_Dialog, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.getChannelDialogs - metadata: {%s}", c.MD)

	r, err := c.ChannelGetChannelDialogs(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelGetTopMessages
// channel.getTopMessages self_id:long id_list:Vector<long> = Vector<Message>;
func (s *Service) ChannelGetTopMessages(ctx context.Context, request *channel.TLChannelGetTopMessages) (*channel.Vector_Message, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.getTopMessages - metadata: {%s}", c.MD)

	r, err := c.ChannelGetTopMessages(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelGetMemberIdList
// channel.getMemberIdList channel_id:long = Vector<long>;
func (s *Service) ChannelGetMemberIdList(ctx context.Context, request *channel.TLChannelGetMemberIdList) (*channel.Vector_Long, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.getMemberIdList - metadata: {%s}", c.MD)

	r, err := c.ChannelGetMemberIdList(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelGetHistory
// channel.getHistory self_id:long channel_id:long offset_id:int offset_date:int add_offset:int limit:int max_id:int min_id:int = messages.Messages;
func (s *Service) ChannelGetHistory(ctx context.Context, request *channel.TLChannelGetHistory) (*mtproto.Messages_Messages, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.getHistory - metadata: {%s}", c.MD)

	r, err := c.ChannelGetHistory(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelGetMessages
// channel.getMessages self_id:long channel_id:long id:Vector<int> = Vector<Message>;
func (s *Service) ChannelGetMessages(ctx context.Context, request *channel.TLChannelGetMessages) (*channel.Vector_Message, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.getMessages - metadata: {%s}", c.MD)

	r, err := c.ChannelGetMessages(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelGetChannelDifference
// channel.getChannelDifference self_id:long channel_id:long pts:int limit:int = updates.ChannelDifference;
func (s *Service) ChannelGetChannelDifference(ctx context.Context, request *channel.TLChannelGetChannelDifference) (*mtproto.Updates_ChannelDifference, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.getChannelDifference - metadata: {%s}", c.MD)

	r, err := c.ChannelGetChannelDifference(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelJoinChannel
// channel.joinChannel self_id:long perm_auth_key_id:long channel_id:long link:string = Updates;
func (s *Service) ChannelJoinChannel(ctx context.Context, request *channel.TLChannelJoinChannel) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.joinChannel - metadata: {%s}", c.MD)

	r, err := c.ChannelJoinChannel(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelLeaveChannel
// channel.leaveChannel self_id:long perm_auth_key_id:long channel_id:long = Updates;
func (s *Service) ChannelLeaveChannel(ctx context.Context, request *channel.TLChannelLeaveChannel) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.leaveChannel - metadata: {%s}", c.MD)

	r, err := c.ChannelLeaveChannel(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelCheckChatInvite
// channel.checkChatInvite self_id:long link:string = ChatInvite;
func (s *Service) ChannelCheckChatInvite(ctx context.Context, request *channel.TLChannelCheckChatInvite) (*mtproto.ChatInvite, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.checkChatInvite - metadata: {%s}", c.MD)

	r, err := c.ChannelCheckChatInvite(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelGetParticipants
// channel.getParticipants self_id:long channel_id:long filter:string q:string offset:int limit:int = channels.ChannelParticipants;
func (s *Service) ChannelGetParticipants(ctx context.Context, request *channel.TLChannelGetParticipants) (*mtproto.Channels_ChannelParticipants, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.getParticipants - metadata: {%s}", c.MD)

	r, err := c.ChannelGetParticipants(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelReadHistory
// channel.readHistory self_id:long perm_auth_key_id:long channel_id:long max_id:int = Bool;
func (s *Service) ChannelReadHistory(ctx context.Context, request *channel.TLChannelReadHistory) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.readHistory - metadata: {%s}", c.MD)

	r, err := c.ChannelReadHistory(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelDeleteMessages
// channel.deleteMessages self_id:long perm_auth_key_id:long channel_id:long id:Vector<int> = messages.AffectedMessages;
func (s *Service) ChannelDeleteMessages(ctx context.Context, request *channel.TLChannelDeleteMessages) (*mtproto.Messages_AffectedMessages, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.deleteMessages - metadata: {%s}", c.MD)

	r, err := c.ChannelDeleteMessages(request)
	if err != nil {
		return nil, err
	}

	return r, err
}
