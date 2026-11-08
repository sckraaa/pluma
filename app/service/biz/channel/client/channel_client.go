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

package channel_client

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"

	"github.com/zeromicro/go-zero/zrpc"
)

type ChannelClient interface {
	ChannelGetMutableChannel(ctx context.Context, in *channel.TLChannelGetMutableChannel) (*channel.MutableChannel, error)
	ChannelGetChannelListByIdList(ctx context.Context, in *channel.TLChannelGetChannelListByIdList) (*channel.Vector_MutableChannel, error)
	ChannelCreateChannel(ctx context.Context, in *channel.TLChannelCreateChannel) (*mtproto.Updates, error)
	ChannelGetChannelDialogs(ctx context.Context, in *channel.TLChannelGetChannelDialogs) (*channel.Vector_Dialog, error)
	ChannelGetTopMessages(ctx context.Context, in *channel.TLChannelGetTopMessages) (*channel.Vector_Message, error)
	ChannelGetMemberIdList(ctx context.Context, in *channel.TLChannelGetMemberIdList) (*channel.Vector_Long, error)
	ChannelGetHistory(ctx context.Context, in *channel.TLChannelGetHistory) (*mtproto.Messages_Messages, error)
	ChannelGetMessages(ctx context.Context, in *channel.TLChannelGetMessages) (*channel.Vector_Message, error)
	ChannelGetChannelDifference(ctx context.Context, in *channel.TLChannelGetChannelDifference) (*mtproto.Updates_ChannelDifference, error)
	ChannelJoinChannel(ctx context.Context, in *channel.TLChannelJoinChannel) (*mtproto.Updates, error)
	ChannelLeaveChannel(ctx context.Context, in *channel.TLChannelLeaveChannel) (*mtproto.Updates, error)
	ChannelCheckChatInvite(ctx context.Context, in *channel.TLChannelCheckChatInvite) (*mtproto.ChatInvite, error)
	ChannelGetParticipants(ctx context.Context, in *channel.TLChannelGetParticipants) (*mtproto.Channels_ChannelParticipants, error)
	ChannelReadHistory(ctx context.Context, in *channel.TLChannelReadHistory) (*mtproto.Bool, error)
	ChannelDeleteMessages(ctx context.Context, in *channel.TLChannelDeleteMessages) (*mtproto.Messages_AffectedMessages, error)
	ChannelInviteToChannel(ctx context.Context, in *channel.TLChannelInviteToChannel) (*mtproto.Updates, error)
	ChannelEditTitle(ctx context.Context, in *channel.TLChannelEditTitle) (*mtproto.Updates, error)
	ChannelEditAbout(ctx context.Context, in *channel.TLChannelEditAbout) (*mtproto.Bool, error)
	ChannelCheckUsername(ctx context.Context, in *channel.TLChannelCheckUsername) (*mtproto.Bool, error)
	ChannelUpdateUsername(ctx context.Context, in *channel.TLChannelUpdateUsername) (*mtproto.Bool, error)
	ChannelGetAdminedPublicChannels(ctx context.Context, in *channel.TLChannelGetAdminedPublicChannels) (*channel.Vector_Long, error)
	ChannelDeleteChannel(ctx context.Context, in *channel.TLChannelDeleteChannel) (*mtproto.Updates, error)
	ChannelIncrementMessageViews(ctx context.Context, in *channel.TLChannelIncrementMessageViews) (*mtproto.Bool, error)
	ChannelAddMessageForwards(ctx context.Context, in *channel.TLChannelAddMessageForwards) (*mtproto.Bool, error)
	ChannelDeleteHistory(ctx context.Context, in *channel.TLChannelDeleteHistory) (*mtproto.Updates, error)
	ChannelDeleteParticipantHistory(ctx context.Context, in *channel.TLChannelDeleteParticipantHistory) (*mtproto.Messages_AffectedHistory, error)
	ChannelToggleSignatures(ctx context.Context, in *channel.TLChannelToggleSignatures) (*mtproto.Updates, error)
	ChannelToggleSlowMode(ctx context.Context, in *channel.TLChannelToggleSlowMode) (*mtproto.Updates, error)
	ChannelToggleJoinRequest(ctx context.Context, in *channel.TLChannelToggleJoinRequest) (*mtproto.Updates, error)
	ChannelExportChatInvite(ctx context.Context, in *channel.TLChannelExportChatInvite) (*mtproto.ExportedChatInvite, error)
	ChannelGetExportedChatInvite(ctx context.Context, in *channel.TLChannelGetExportedChatInvite) (*mtproto.ExportedChatInvite, error)
	ChannelGetExportedChatInvites(ctx context.Context, in *channel.TLChannelGetExportedChatInvites) (*mtproto.Messages_ExportedChatInvites, error)
	ChannelEditExportedChatInvite(ctx context.Context, in *channel.TLChannelEditExportedChatInvite) (*mtproto.Messages_ExportedChatInvite, error)
	ChannelDeleteExportedChatInvite(ctx context.Context, in *channel.TLChannelDeleteExportedChatInvite) (*mtproto.Bool, error)
	ChannelDeleteRevokedExportedChatInvites(ctx context.Context, in *channel.TLChannelDeleteRevokedExportedChatInvites) (*mtproto.Bool, error)
	ChannelGetAdminsWithInvites(ctx context.Context, in *channel.TLChannelGetAdminsWithInvites) (*mtproto.Messages_ChatAdminsWithInvites, error)
	ChannelGetChatInviteImporters(ctx context.Context, in *channel.TLChannelGetChatInviteImporters) (*mtproto.Messages_ChatInviteImporters, error)
	ChannelHideChatJoinRequests(ctx context.Context, in *channel.TLChannelHideChatJoinRequests) (*mtproto.Updates, error)
	ChannelGetPendingJoinRequests(ctx context.Context, in *channel.TLChannelGetPendingJoinRequests) (*mtproto.Update, error)
	ChannelGetLeftChannels(ctx context.Context, in *channel.TLChannelGetLeftChannels) (*mtproto.Messages_Chats, error)
	ChannelToggleSetting(ctx context.Context, in *channel.TLChannelToggleSetting) (*mtproto.Updates, error)
	ChannelSetAvailableReactions(ctx context.Context, in *channel.TLChannelSetAvailableReactions) (*mtproto.Updates, error)
	ChannelSetDiscussionGroup(ctx context.Context, in *channel.TLChannelSetDiscussionGroup) (*mtproto.Bool, error)
	ChannelGetGroupsForDiscussion(ctx context.Context, in *channel.TLChannelGetGroupsForDiscussion) (*channel.Vector_Long, error)
	ChannelGetDiscussionMessage(ctx context.Context, in *channel.TLChannelGetDiscussionMessage) (*mtproto.Messages_DiscussionMessage, error)
	ChannelGetReplies(ctx context.Context, in *channel.TLChannelGetReplies) (*mtproto.Messages_Messages, error)
	ChannelReadDiscussion(ctx context.Context, in *channel.TLChannelReadDiscussion) (*mtproto.Bool, error)
	ChannelEditPhoto(ctx context.Context, in *channel.TLChannelEditPhoto) (*mtproto.Updates, error)
	ChannelReadMentions(ctx context.Context, in *channel.TLChannelReadMentions) (*mtproto.Messages_AffectedHistory, error)
	ChannelGetUnreadMentions(ctx context.Context, in *channel.TLChannelGetUnreadMentions) (*mtproto.Messages_Messages, error)
	ChannelReadMessageContents(ctx context.Context, in *channel.TLChannelReadMessageContents) (*mtproto.Bool, error)
	ChannelSearch(ctx context.Context, in *channel.TLChannelSearch) (*mtproto.Messages_Messages, error)
	ChannelEditCreator(ctx context.Context, in *channel.TLChannelEditCreator) (*mtproto.Updates, error)
	ChannelEditAdmin(ctx context.Context, in *channel.TLChannelEditAdmin) (*mtproto.Updates, error)
	ChannelEditBanned(ctx context.Context, in *channel.TLChannelEditBanned) (*mtproto.Updates, error)
	ChannelEditDefaultBannedRights(ctx context.Context, in *channel.TLChannelEditDefaultBannedRights) (*mtproto.Updates, error)
}

type defaultChannelClient struct {
	cli zrpc.Client
}

func NewChannelClient(cli zrpc.Client) ChannelClient {
	return &defaultChannelClient{
		cli: cli,
	}
}

// ChannelGetMutableChannel
// channel.getMutableChannel channel_id:long id:Vector<long> = MutableChannel;
func (m *defaultChannelClient) ChannelGetMutableChannel(ctx context.Context, in *channel.TLChannelGetMutableChannel) (*channel.MutableChannel, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelGetMutableChannel(ctx, in)
}

// ChannelGetChannelListByIdList
// channel.getChannelListByIdList self_id:long id_list:Vector<long> = Vector<MutableChannel>;
func (m *defaultChannelClient) ChannelGetChannelListByIdList(ctx context.Context, in *channel.TLChannelGetChannelListByIdList) (*channel.Vector_MutableChannel, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelGetChannelListByIdList(ctx, in)
}

// ChannelCreateChannel
// channel.createChannel creator_id:long title:string about:string broadcast:Bool megagroup:Bool = Updates;
func (m *defaultChannelClient) ChannelCreateChannel(ctx context.Context, in *channel.TLChannelCreateChannel) (*mtproto.Updates, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelCreateChannel(ctx, in)
}

// ChannelGetChannelDialogs
// channel.getChannelDialogs self_id:long id_list:Vector<long> = Vector<Dialog>;
func (m *defaultChannelClient) ChannelGetChannelDialogs(ctx context.Context, in *channel.TLChannelGetChannelDialogs) (*channel.Vector_Dialog, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelGetChannelDialogs(ctx, in)
}

// ChannelGetTopMessages
// channel.getTopMessages self_id:long id_list:Vector<long> = Vector<Message>;
func (m *defaultChannelClient) ChannelGetTopMessages(ctx context.Context, in *channel.TLChannelGetTopMessages) (*channel.Vector_Message, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelGetTopMessages(ctx, in)
}

// ChannelGetMemberIdList
// channel.getMemberIdList channel_id:long = Vector<long>;
func (m *defaultChannelClient) ChannelGetMemberIdList(ctx context.Context, in *channel.TLChannelGetMemberIdList) (*channel.Vector_Long, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelGetMemberIdList(ctx, in)
}

// ChannelGetHistory
// channel.getHistory self_id:long channel_id:long offset_id:int offset_date:int add_offset:int limit:int max_id:int min_id:int = messages.Messages;
func (m *defaultChannelClient) ChannelGetHistory(ctx context.Context, in *channel.TLChannelGetHistory) (*mtproto.Messages_Messages, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelGetHistory(ctx, in)
}

// ChannelGetMessages
// channel.getMessages self_id:long channel_id:long id:Vector<int> = Vector<Message>;
func (m *defaultChannelClient) ChannelGetMessages(ctx context.Context, in *channel.TLChannelGetMessages) (*channel.Vector_Message, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelGetMessages(ctx, in)
}

// ChannelGetChannelDifference
// channel.getChannelDifference self_id:long channel_id:long pts:int limit:int = updates.ChannelDifference;
func (m *defaultChannelClient) ChannelGetChannelDifference(ctx context.Context, in *channel.TLChannelGetChannelDifference) (*mtproto.Updates_ChannelDifference, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelGetChannelDifference(ctx, in)
}

// ChannelJoinChannel
// channel.joinChannel self_id:long perm_auth_key_id:long channel_id:long link:string = Updates;
func (m *defaultChannelClient) ChannelJoinChannel(ctx context.Context, in *channel.TLChannelJoinChannel) (*mtproto.Updates, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelJoinChannel(ctx, in)
}

// ChannelLeaveChannel
// channel.leaveChannel self_id:long perm_auth_key_id:long channel_id:long = Updates;
func (m *defaultChannelClient) ChannelLeaveChannel(ctx context.Context, in *channel.TLChannelLeaveChannel) (*mtproto.Updates, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelLeaveChannel(ctx, in)
}

// ChannelCheckChatInvite
// channel.checkChatInvite self_id:long link:string = ChatInvite;
func (m *defaultChannelClient) ChannelCheckChatInvite(ctx context.Context, in *channel.TLChannelCheckChatInvite) (*mtproto.ChatInvite, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelCheckChatInvite(ctx, in)
}

// ChannelGetParticipants
// channel.getParticipants self_id:long channel_id:long filter:string q:string offset:int limit:int = channels.ChannelParticipants;
func (m *defaultChannelClient) ChannelGetParticipants(ctx context.Context, in *channel.TLChannelGetParticipants) (*mtproto.Channels_ChannelParticipants, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelGetParticipants(ctx, in)
}

// ChannelReadHistory
// channel.readHistory self_id:long perm_auth_key_id:long channel_id:long max_id:int = Bool;
func (m *defaultChannelClient) ChannelReadHistory(ctx context.Context, in *channel.TLChannelReadHistory) (*mtproto.Bool, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelReadHistory(ctx, in)
}

// ChannelDeleteMessages
// channel.deleteMessages self_id:long perm_auth_key_id:long channel_id:long id:Vector<int> = messages.AffectedMessages;
func (m *defaultChannelClient) ChannelDeleteMessages(ctx context.Context, in *channel.TLChannelDeleteMessages) (*mtproto.Messages_AffectedMessages, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelDeleteMessages(ctx, in)
}

// ChannelInviteToChannel
// channel.inviteToChannel self_id:long perm_auth_key_id:long channel_id:long id:Vector<long> = Updates;
func (m *defaultChannelClient) ChannelInviteToChannel(ctx context.Context, in *channel.TLChannelInviteToChannel) (*mtproto.Updates, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelInviteToChannel(ctx, in)
}

// ChannelEditTitle
// channel.editTitle self_id:long perm_auth_key_id:long channel_id:long title:string = Updates;
func (m *defaultChannelClient) ChannelEditTitle(ctx context.Context, in *channel.TLChannelEditTitle) (*mtproto.Updates, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelEditTitle(ctx, in)
}

// ChannelEditAbout
// channel.editAbout self_id:long perm_auth_key_id:long channel_id:long about:string = Bool;
func (m *defaultChannelClient) ChannelEditAbout(ctx context.Context, in *channel.TLChannelEditAbout) (*mtproto.Bool, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelEditAbout(ctx, in)
}

// ChannelCheckUsername
// channel.checkUsername self_id:long channel_id:long username:string = Bool;
func (m *defaultChannelClient) ChannelCheckUsername(ctx context.Context, in *channel.TLChannelCheckUsername) (*mtproto.Bool, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelCheckUsername(ctx, in)
}

// ChannelUpdateUsername
// channel.updateUsername self_id:long perm_auth_key_id:long channel_id:long username:string = Bool;
func (m *defaultChannelClient) ChannelUpdateUsername(ctx context.Context, in *channel.TLChannelUpdateUsername) (*mtproto.Bool, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelUpdateUsername(ctx, in)
}

// ChannelGetAdminedPublicChannels
// channel.getAdminedPublicChannels self_id:long = Vector<long>;
func (m *defaultChannelClient) ChannelGetAdminedPublicChannels(ctx context.Context, in *channel.TLChannelGetAdminedPublicChannels) (*channel.Vector_Long, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelGetAdminedPublicChannels(ctx, in)
}

// ChannelDeleteChannel
// channel.deleteChannel self_id:long perm_auth_key_id:long channel_id:long = Updates;
func (m *defaultChannelClient) ChannelDeleteChannel(ctx context.Context, in *channel.TLChannelDeleteChannel) (*mtproto.Updates, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelDeleteChannel(ctx, in)
}

// ChannelEditAdmin
// channel.editAdmin self_id:long perm_auth_key_id:long channel_id:long user_id:long admin_rights:int rank:string keep_rank:Bool = Updates;
func (m *defaultChannelClient) ChannelEditAdmin(ctx context.Context, in *channel.TLChannelEditAdmin) (*mtproto.Updates, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelEditAdmin(ctx, in)
}

// ChannelEditBanned
// channel.editBanned self_id:long perm_auth_key_id:long channel_id:long user_id:long banned_rights:int until_date:int = Updates;
func (m *defaultChannelClient) ChannelEditBanned(ctx context.Context, in *channel.TLChannelEditBanned) (*mtproto.Updates, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelEditBanned(ctx, in)
}

// ChannelEditDefaultBannedRights
// channel.editDefaultBannedRights self_id:long perm_auth_key_id:long channel_id:long banned_rights:int = Updates;
func (m *defaultChannelClient) ChannelEditDefaultBannedRights(ctx context.Context, in *channel.TLChannelEditDefaultBannedRights) (*mtproto.Updates, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelEditDefaultBannedRights(ctx, in)
}

// ChannelEditCreator
// channel.editCreator self_id:long perm_auth_key_id:long channel_id:long user_id:long = Updates;
func (m *defaultChannelClient) ChannelEditCreator(ctx context.Context, in *channel.TLChannelEditCreator) (*mtproto.Updates, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelEditCreator(ctx, in)
}

// ChannelSearch
// channel.search self_id:long channel_id:long q:string from_id:long media_type:int min_date:int max_date:int offset_id:int add_offset:int limit:int max_id:int min_id:int = messages.Messages;
func (m *defaultChannelClient) ChannelSearch(ctx context.Context, in *channel.TLChannelSearch) (*mtproto.Messages_Messages, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelSearch(ctx, in)
}

// ChannelReadMessageContents
// channel.readMessageContents self_id:long perm_auth_key_id:long channel_id:long id:Vector<int> = Bool;
func (m *defaultChannelClient) ChannelReadMessageContents(ctx context.Context, in *channel.TLChannelReadMessageContents) (*mtproto.Bool, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelReadMessageContents(ctx, in)
}

// ChannelReadMentions
// channel.readMentions self_id:long perm_auth_key_id:long channel_id:long = messages.AffectedHistory;
func (m *defaultChannelClient) ChannelReadMentions(ctx context.Context, in *channel.TLChannelReadMentions) (*mtproto.Messages_AffectedHistory, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelReadMentions(ctx, in)
}

// ChannelGetUnreadMentions
// channel.getUnreadMentions self_id:long channel_id:long offset_id:int add_offset:int limit:int max_id:int min_id:int = messages.Messages;
func (m *defaultChannelClient) ChannelGetUnreadMentions(ctx context.Context, in *channel.TLChannelGetUnreadMentions) (*mtproto.Messages_Messages, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelGetUnreadMentions(ctx, in)
}

// ChannelEditPhoto
// channel.editPhoto self_id:long perm_auth_key_id:long channel_id:long photo_id:long = Updates;
func (m *defaultChannelClient) ChannelEditPhoto(ctx context.Context, in *channel.TLChannelEditPhoto) (*mtproto.Updates, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelEditPhoto(ctx, in)
}

// ChannelSetDiscussionGroup
// channel.setDiscussionGroup self_id:long perm_auth_key_id:long channel_id:long group_id:long = Bool;
func (m *defaultChannelClient) ChannelSetDiscussionGroup(ctx context.Context, in *channel.TLChannelSetDiscussionGroup) (*mtproto.Bool, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelSetDiscussionGroup(ctx, in)
}

// ChannelGetGroupsForDiscussion
// channel.getGroupsForDiscussion self_id:long = Vector<long>;
func (m *defaultChannelClient) ChannelGetGroupsForDiscussion(ctx context.Context, in *channel.TLChannelGetGroupsForDiscussion) (*channel.Vector_Long, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelGetGroupsForDiscussion(ctx, in)
}

// ChannelGetDiscussionMessage
// channel.getDiscussionMessage self_id:long channel_id:long msg_id:int = messages.DiscussionMessage;
func (m *defaultChannelClient) ChannelGetDiscussionMessage(ctx context.Context, in *channel.TLChannelGetDiscussionMessage) (*mtproto.Messages_DiscussionMessage, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelGetDiscussionMessage(ctx, in)
}

// ChannelGetReplies
// channel.getReplies self_id:long channel_id:long msg_id:int offset_id:int offset_date:int add_offset:int limit:int max_id:int min_id:int = messages.Messages;
func (m *defaultChannelClient) ChannelGetReplies(ctx context.Context, in *channel.TLChannelGetReplies) (*mtproto.Messages_Messages, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelGetReplies(ctx, in)
}

// ChannelReadDiscussion
// channel.readDiscussion self_id:long perm_auth_key_id:long channel_id:long msg_id:int read_max_id:int = Bool;
func (m *defaultChannelClient) ChannelReadDiscussion(ctx context.Context, in *channel.TLChannelReadDiscussion) (*mtproto.Bool, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelReadDiscussion(ctx, in)
}

// ChannelToggleSetting
// channel.toggleSetting self_id:long perm_auth_key_id:long channel_id:long setting:int enabled:Bool = Updates;
func (m *defaultChannelClient) ChannelToggleSetting(ctx context.Context, in *channel.TLChannelToggleSetting) (*mtproto.Updates, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelToggleSetting(ctx, in)
}

// ChannelSetAvailableReactions
// channel.setAvailableReactions self_id:long perm_auth_key_id:long channel_id:long reactions_type:int reactions:Vector<string> = Updates;
func (m *defaultChannelClient) ChannelSetAvailableReactions(ctx context.Context, in *channel.TLChannelSetAvailableReactions) (*mtproto.Updates, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelSetAvailableReactions(ctx, in)
}

// ChannelIncrementMessageViews
// channel.incrementMessageViews self_id:long channel_id:long id:Vector<int> = Bool;
func (m *defaultChannelClient) ChannelIncrementMessageViews(ctx context.Context, in *channel.TLChannelIncrementMessageViews) (*mtproto.Bool, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelIncrementMessageViews(ctx, in)
}

// ChannelAddMessageForwards
// channel.addMessageForwards self_id:long channel_id:long id:Vector<int> random_id:Vector<long> = Bool;
func (m *defaultChannelClient) ChannelAddMessageForwards(ctx context.Context, in *channel.TLChannelAddMessageForwards) (*mtproto.Bool, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelAddMessageForwards(ctx, in)
}

// ChannelDeleteHistory
// channel.deleteHistory self_id:long perm_auth_key_id:long channel_id:long for_everyone:Bool max_id:int = Updates;
func (m *defaultChannelClient) ChannelDeleteHistory(ctx context.Context, in *channel.TLChannelDeleteHistory) (*mtproto.Updates, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelDeleteHistory(ctx, in)
}

// ChannelDeleteParticipantHistory
// channel.deleteParticipantHistory self_id:long perm_auth_key_id:long channel_id:long participant_id:long = messages.AffectedHistory;
func (m *defaultChannelClient) ChannelDeleteParticipantHistory(ctx context.Context, in *channel.TLChannelDeleteParticipantHistory) (*mtproto.Messages_AffectedHistory, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelDeleteParticipantHistory(ctx, in)
}

// ChannelToggleSignatures
// channel.toggleSignatures self_id:long perm_auth_key_id:long channel_id:long enabled:Bool profiles:Bool = Updates;
func (m *defaultChannelClient) ChannelToggleSignatures(ctx context.Context, in *channel.TLChannelToggleSignatures) (*mtproto.Updates, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelToggleSignatures(ctx, in)
}

// ChannelToggleSlowMode
// channel.toggleSlowMode self_id:long perm_auth_key_id:long channel_id:long seconds:int = Updates;
func (m *defaultChannelClient) ChannelToggleSlowMode(ctx context.Context, in *channel.TLChannelToggleSlowMode) (*mtproto.Updates, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelToggleSlowMode(ctx, in)
}

// ChannelToggleJoinRequest
// channel.toggleJoinRequest self_id:long perm_auth_key_id:long channel_id:long enabled:Bool apply_to_invites:Bool = Updates;
func (m *defaultChannelClient) ChannelToggleJoinRequest(ctx context.Context, in *channel.TLChannelToggleJoinRequest) (*mtproto.Updates, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelToggleJoinRequest(ctx, in)
}

// ChannelExportChatInvite
// channel.exportChatInvite self_id:long channel_id:long legacy_revoke_permanent:Bool request_needed:Bool expire_date:int usage_limit:int title:string = ExportedChatInvite;
func (m *defaultChannelClient) ChannelExportChatInvite(ctx context.Context, in *channel.TLChannelExportChatInvite) (*mtproto.ExportedChatInvite, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelExportChatInvite(ctx, in)
}

// ChannelGetExportedChatInvite
// channel.getExportedChatInvite self_id:long channel_id:long link:string = ExportedChatInvite;
func (m *defaultChannelClient) ChannelGetExportedChatInvite(ctx context.Context, in *channel.TLChannelGetExportedChatInvite) (*mtproto.ExportedChatInvite, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelGetExportedChatInvite(ctx, in)
}

// ChannelGetExportedChatInvites
// channel.getExportedChatInvites self_id:long channel_id:long admin_id:long revoked:Bool offset_date:int offset_link:string limit:int = messages.ExportedChatInvites;
func (m *defaultChannelClient) ChannelGetExportedChatInvites(ctx context.Context, in *channel.TLChannelGetExportedChatInvites) (*mtproto.Messages_ExportedChatInvites, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelGetExportedChatInvites(ctx, in)
}

// ChannelEditExportedChatInvite
// channel.editExportedChatInvite self_id:long channel_id:long link:string revoked:Bool fields:int expire_date:int usage_limit:int request_needed:Bool title:string = messages.ExportedChatInvite;
func (m *defaultChannelClient) ChannelEditExportedChatInvite(ctx context.Context, in *channel.TLChannelEditExportedChatInvite) (*mtproto.Messages_ExportedChatInvite, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelEditExportedChatInvite(ctx, in)
}

// ChannelDeleteExportedChatInvite
// channel.deleteExportedChatInvite self_id:long channel_id:long link:string = Bool;
func (m *defaultChannelClient) ChannelDeleteExportedChatInvite(ctx context.Context, in *channel.TLChannelDeleteExportedChatInvite) (*mtproto.Bool, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelDeleteExportedChatInvite(ctx, in)
}

// ChannelDeleteRevokedExportedChatInvites
// channel.deleteRevokedExportedChatInvites self_id:long channel_id:long admin_id:long = Bool;
func (m *defaultChannelClient) ChannelDeleteRevokedExportedChatInvites(ctx context.Context, in *channel.TLChannelDeleteRevokedExportedChatInvites) (*mtproto.Bool, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelDeleteRevokedExportedChatInvites(ctx, in)
}

// ChannelGetAdminsWithInvites
// channel.getAdminsWithInvites self_id:long channel_id:long = messages.ChatAdminsWithInvites;
func (m *defaultChannelClient) ChannelGetAdminsWithInvites(ctx context.Context, in *channel.TLChannelGetAdminsWithInvites) (*mtproto.Messages_ChatAdminsWithInvites, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelGetAdminsWithInvites(ctx, in)
}

// ChannelGetChatInviteImporters
// channel.getChatInviteImporters self_id:long channel_id:long requested:Bool link:string q:string offset_date:int offset_user:long limit:int = messages.ChatInviteImporters;
func (m *defaultChannelClient) ChannelGetChatInviteImporters(ctx context.Context, in *channel.TLChannelGetChatInviteImporters) (*mtproto.Messages_ChatInviteImporters, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelGetChatInviteImporters(ctx, in)
}

// ChannelHideChatJoinRequests
// channel.hideChatJoinRequests self_id:long perm_auth_key_id:long channel_id:long approved:Bool all:Bool link:string user_id:long = Updates;
func (m *defaultChannelClient) ChannelHideChatJoinRequests(ctx context.Context, in *channel.TLChannelHideChatJoinRequests) (*mtproto.Updates, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelHideChatJoinRequests(ctx, in)
}

// ChannelGetPendingJoinRequests
// channel.getPendingJoinRequests self_id:long channel_id:long = Update;
func (m *defaultChannelClient) ChannelGetPendingJoinRequests(ctx context.Context, in *channel.TLChannelGetPendingJoinRequests) (*mtproto.Update, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelGetPendingJoinRequests(ctx, in)
}

// ChannelGetLeftChannels
// channel.getLeftChannels self_id:long offset:int = messages.Chats;
func (m *defaultChannelClient) ChannelGetLeftChannels(ctx context.Context, in *channel.TLChannelGetLeftChannels) (*mtproto.Messages_Chats, error) {
	md := metadata.RpcMetadataFromIncoming(ctx)
	if md != nil {
		ctx, _ = metadata.RpcMetadataToOutgoing(ctx, md)
	}
	client := channel.NewRPCChannelClient(m.cli.Conn())
	return client.ChannelGetLeftChannels(ctx, in)
}
