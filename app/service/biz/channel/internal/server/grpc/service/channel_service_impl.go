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

// ChannelInviteToChannel
// channel.inviteToChannel self_id:long perm_auth_key_id:long channel_id:long id:Vector<long> = Updates;
func (s *Service) ChannelInviteToChannel(ctx context.Context, request *channel.TLChannelInviteToChannel) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.inviteToChannel - metadata: {%s}", c.MD)

	r, err := c.ChannelInviteToChannel(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelEditTitle
// channel.editTitle self_id:long perm_auth_key_id:long channel_id:long title:string = Updates;
func (s *Service) ChannelEditTitle(ctx context.Context, request *channel.TLChannelEditTitle) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.editTitle - metadata: {%s}", c.MD)

	r, err := c.ChannelEditTitle(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelEditAbout
// channel.editAbout self_id:long perm_auth_key_id:long channel_id:long about:string = Bool;
func (s *Service) ChannelEditAbout(ctx context.Context, request *channel.TLChannelEditAbout) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.editAbout - metadata: {%s}", c.MD)

	r, err := c.ChannelEditAbout(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelCheckUsername
// channel.checkUsername self_id:long channel_id:long username:string = Bool;
func (s *Service) ChannelCheckUsername(ctx context.Context, request *channel.TLChannelCheckUsername) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.checkUsername - metadata: {%s}", c.MD)

	r, err := c.ChannelCheckUsername(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelUpdateUsername
// channel.updateUsername self_id:long perm_auth_key_id:long channel_id:long username:string = Bool;
func (s *Service) ChannelUpdateUsername(ctx context.Context, request *channel.TLChannelUpdateUsername) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.updateUsername - metadata: {%s}", c.MD)

	r, err := c.ChannelUpdateUsername(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelGetAdminedPublicChannels
// channel.getAdminedPublicChannels self_id:long = Vector<long>;
func (s *Service) ChannelGetAdminedPublicChannels(ctx context.Context, request *channel.TLChannelGetAdminedPublicChannels) (*channel.Vector_Long, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.getAdminedPublicChannels - metadata: {%s}", c.MD)

	r, err := c.ChannelGetAdminedPublicChannels(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelDeleteChannel
// channel.deleteChannel self_id:long perm_auth_key_id:long channel_id:long = Updates;
func (s *Service) ChannelDeleteChannel(ctx context.Context, request *channel.TLChannelDeleteChannel) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.deleteChannel - metadata: {%s}", c.MD)

	r, err := c.ChannelDeleteChannel(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelEditAdmin
// channel.editAdmin self_id:long perm_auth_key_id:long channel_id:long user_id:long admin_rights:int rank:string keep_rank:Bool = Updates;
func (s *Service) ChannelEditAdmin(ctx context.Context, request *channel.TLChannelEditAdmin) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.editAdmin - metadata: {%s}", c.MD)

	r, err := c.ChannelEditAdmin(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelEditBanned
// channel.editBanned self_id:long perm_auth_key_id:long channel_id:long user_id:long banned_rights:int until_date:int = Updates;
func (s *Service) ChannelEditBanned(ctx context.Context, request *channel.TLChannelEditBanned) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.editBanned - metadata: {%s}", c.MD)

	r, err := c.ChannelEditBanned(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelEditDefaultBannedRights
// channel.editDefaultBannedRights self_id:long perm_auth_key_id:long channel_id:long banned_rights:int = Updates;
func (s *Service) ChannelEditDefaultBannedRights(ctx context.Context, request *channel.TLChannelEditDefaultBannedRights) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.editDefaultBannedRights - metadata: {%s}", c.MD)

	r, err := c.ChannelEditDefaultBannedRights(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelEditCreator
// channel.editCreator self_id:long perm_auth_key_id:long channel_id:long user_id:long = Updates;
func (s *Service) ChannelEditCreator(ctx context.Context, request *channel.TLChannelEditCreator) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.editCreator - metadata: {%s}", c.MD)

	r, err := c.ChannelEditCreator(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelSearch
// channel.search self_id:long channel_id:long q:string from_id:long media_type:int min_date:int max_date:int offset_id:int add_offset:int limit:int max_id:int min_id:int = messages.Messages;
func (s *Service) ChannelSearch(ctx context.Context, request *channel.TLChannelSearch) (*mtproto.Messages_Messages, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.search - metadata: {%s}", c.MD)

	r, err := c.ChannelSearch(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelReadMessageContents
// channel.readMessageContents self_id:long perm_auth_key_id:long channel_id:long id:Vector<int> = Bool;
func (s *Service) ChannelReadMessageContents(ctx context.Context, request *channel.TLChannelReadMessageContents) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.readMessageContents - metadata: {%s}", c.MD)

	r, err := c.ChannelReadMessageContents(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelReadMentions
// channel.readMentions self_id:long perm_auth_key_id:long channel_id:long = messages.AffectedHistory;
func (s *Service) ChannelReadMentions(ctx context.Context, request *channel.TLChannelReadMentions) (*mtproto.Messages_AffectedHistory, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.readMentions - metadata: {%s}", c.MD)

	r, err := c.ChannelReadMentions(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelGetUnreadMentions
// channel.getUnreadMentions self_id:long channel_id:long offset_id:int add_offset:int limit:int max_id:int min_id:int = messages.Messages;
func (s *Service) ChannelGetUnreadMentions(ctx context.Context, request *channel.TLChannelGetUnreadMentions) (*mtproto.Messages_Messages, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.getUnreadMentions - metadata: {%s}", c.MD)

	r, err := c.ChannelGetUnreadMentions(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelEditPhoto
// channel.editPhoto self_id:long perm_auth_key_id:long channel_id:long photo_id:long = Updates;
func (s *Service) ChannelEditPhoto(ctx context.Context, request *channel.TLChannelEditPhoto) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.editPhoto - metadata: {%s}", c.MD)

	r, err := c.ChannelEditPhoto(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelSetDiscussionGroup
// channel.setDiscussionGroup self_id:long perm_auth_key_id:long channel_id:long group_id:long = Bool;
func (s *Service) ChannelSetDiscussionGroup(ctx context.Context, request *channel.TLChannelSetDiscussionGroup) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.setDiscussionGroup - metadata: {%s}", c.MD)

	r, err := c.ChannelSetDiscussionGroup(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelGetGroupsForDiscussion
// channel.getGroupsForDiscussion self_id:long = Vector<long>;
func (s *Service) ChannelGetGroupsForDiscussion(ctx context.Context, request *channel.TLChannelGetGroupsForDiscussion) (*channel.Vector_Long, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.getGroupsForDiscussion - metadata: {%s}", c.MD)

	r, err := c.ChannelGetGroupsForDiscussion(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelGetDiscussionMessage
// channel.getDiscussionMessage self_id:long channel_id:long msg_id:int = messages.DiscussionMessage;
func (s *Service) ChannelGetDiscussionMessage(ctx context.Context, request *channel.TLChannelGetDiscussionMessage) (*mtproto.Messages_DiscussionMessage, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.getDiscussionMessage - metadata: {%s}", c.MD)

	r, err := c.ChannelGetDiscussionMessage(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelGetReplies
// channel.getReplies self_id:long channel_id:long msg_id:int offset_id:int offset_date:int add_offset:int limit:int max_id:int min_id:int = messages.Messages;
func (s *Service) ChannelGetReplies(ctx context.Context, request *channel.TLChannelGetReplies) (*mtproto.Messages_Messages, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.getReplies - metadata: {%s}", c.MD)

	r, err := c.ChannelGetReplies(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelReadDiscussion
// channel.readDiscussion self_id:long perm_auth_key_id:long channel_id:long msg_id:int read_max_id:int = Bool;
func (s *Service) ChannelReadDiscussion(ctx context.Context, request *channel.TLChannelReadDiscussion) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.readDiscussion - metadata: {%s}", c.MD)

	r, err := c.ChannelReadDiscussion(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelToggleSetting
// channel.toggleSetting self_id:long perm_auth_key_id:long channel_id:long setting:int enabled:Bool = Updates;
func (s *Service) ChannelToggleSetting(ctx context.Context, request *channel.TLChannelToggleSetting) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.toggleSetting - metadata: {%s}", c.MD)

	r, err := c.ChannelToggleSetting(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelSetAvailableReactions
// channel.setAvailableReactions self_id:long perm_auth_key_id:long channel_id:long reactions_type:int reactions:Vector<string> = Updates;
func (s *Service) ChannelSetAvailableReactions(ctx context.Context, request *channel.TLChannelSetAvailableReactions) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.setAvailableReactions - metadata: {%s}", c.MD)

	r, err := c.ChannelSetAvailableReactions(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelIncrementMessageViews
// channel.incrementMessageViews self_id:long channel_id:long id:Vector<int> = Bool;
func (s *Service) ChannelIncrementMessageViews(ctx context.Context, request *channel.TLChannelIncrementMessageViews) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.incrementMessageViews - metadata: {%s}", c.MD)

	r, err := c.ChannelIncrementMessageViews(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelAddMessageForwards
// channel.addMessageForwards self_id:long channel_id:long id:Vector<int> random_id:Vector<long> = Bool;
func (s *Service) ChannelAddMessageForwards(ctx context.Context, request *channel.TLChannelAddMessageForwards) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.addMessageForwards - metadata: {%s}", c.MD)

	r, err := c.ChannelAddMessageForwards(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelDeleteHistory
// channel.deleteHistory self_id:long perm_auth_key_id:long channel_id:long for_everyone:Bool max_id:int = Updates;
func (s *Service) ChannelDeleteHistory(ctx context.Context, request *channel.TLChannelDeleteHistory) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.deleteHistory - metadata: {%s}", c.MD)

	r, err := c.ChannelDeleteHistory(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelDeleteParticipantHistory
// channel.deleteParticipantHistory self_id:long perm_auth_key_id:long channel_id:long participant_id:long = messages.AffectedHistory;
func (s *Service) ChannelDeleteParticipantHistory(ctx context.Context, request *channel.TLChannelDeleteParticipantHistory) (*mtproto.Messages_AffectedHistory, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.deleteParticipantHistory - metadata: {%s}", c.MD)

	r, err := c.ChannelDeleteParticipantHistory(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelToggleSignatures
// channel.toggleSignatures self_id:long perm_auth_key_id:long channel_id:long enabled:Bool profiles:Bool = Updates;
func (s *Service) ChannelToggleSignatures(ctx context.Context, request *channel.TLChannelToggleSignatures) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.toggleSignatures - metadata: {%s}", c.MD)

	r, err := c.ChannelToggleSignatures(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// ChannelToggleSlowMode
// channel.toggleSlowMode self_id:long perm_auth_key_id:long channel_id:long seconds:int = Updates;
func (s *Service) ChannelToggleSlowMode(ctx context.Context, request *channel.TLChannelToggleSlowMode) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channel.toggleSlowMode - metadata: {%s}", c.MD)

	r, err := c.ChannelToggleSlowMode(request)
	if err != nil {
		return nil, err
	}

	return r, err
}
