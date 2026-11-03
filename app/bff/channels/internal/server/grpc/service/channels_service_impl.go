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

// MessagesGetPersonalChannelHistory
// messages.getPersonalChannelHistory#55fb0996
func (s *Service) MessagesGetPersonalChannelHistory(ctx context.Context, request *mtproto.TLMessagesGetPersonalChannelHistory) (*mtproto.Messages_Messages, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getPersonalChannelHistory - metadata: {%s}", c.MD)
	return c.MessagesGetPersonalChannelHistory(request)
}

// ChannelsReadHistory
// channels.readHistory#cc104937
func (s *Service) ChannelsReadHistory(ctx context.Context, request *mtproto.TLChannelsReadHistory) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.readHistory - metadata: {%s}", c.MD)
	return c.ChannelsReadHistory(request)
}

// ChannelsDeleteMessages
// channels.deleteMessages#84c1fd4e
func (s *Service) ChannelsDeleteMessages(ctx context.Context, request *mtproto.TLChannelsDeleteMessages) (*mtproto.Messages_AffectedMessages, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.deleteMessages - metadata: {%s}", c.MD)
	return c.ChannelsDeleteMessages(request)
}

// ChannelsGetMessages
// channels.getMessages#ad8c9a23 (also #93d7b347)
func (s *Service) ChannelsGetMessages(ctx context.Context, request *mtproto.TLChannelsGetMessages) (*mtproto.Messages_Messages, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.getMessages - metadata: {%s}", c.MD)
	return c.ChannelsGetMessages(request)
}

// ChannelsGetParticipants
// channels.getParticipants#77ced9d0
func (s *Service) ChannelsGetParticipants(ctx context.Context, request *mtproto.TLChannelsGetParticipants) (*mtproto.Channels_ChannelParticipants, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.getParticipants - metadata: {%s}", c.MD)
	return c.ChannelsGetParticipants(request)
}

// ChannelsGetParticipant
// channels.getParticipant#a0ab6cc6
func (s *Service) ChannelsGetParticipant(ctx context.Context, request *mtproto.TLChannelsGetParticipant) (*mtproto.Channels_ChannelParticipant, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.getParticipant - metadata: {%s}", c.MD)
	return c.ChannelsGetParticipant(request)
}

// ChannelsGetChannels
// channels.getChannels#a7f6bbb
func (s *Service) ChannelsGetChannels(ctx context.Context, request *mtproto.TLChannelsGetChannels) (*mtproto.Messages_Chats, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.getChannels - metadata: {%s}", c.MD)
	return c.ChannelsGetChannels(request)
}

// ChannelsGetFullChannel
// channels.getFullChannel#8736a09
func (s *Service) ChannelsGetFullChannel(ctx context.Context, request *mtproto.TLChannelsGetFullChannel) (*mtproto.Messages_ChatFull, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.getFullChannel - metadata: {%s}", c.MD)
	return c.ChannelsGetFullChannel(request)
}

// ChannelsCreateChannel
// channels.createChannel#91006707 (also #3d5fb10f)
func (s *Service) ChannelsCreateChannel(ctx context.Context, request *mtproto.TLChannelsCreateChannel) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.createChannel - metadata: {%s}", c.MD)
	return c.ChannelsCreateChannel(request)
}

// ChannelsEditAdmin
// channels.editAdmin#9a98ad68 (also #d33c8902)
func (s *Service) ChannelsEditAdmin(ctx context.Context, request *mtproto.TLChannelsEditAdmin) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.editAdmin - metadata: {%s}", c.MD)
	return c.ChannelsEditAdmin(request)
}

// ChannelsEditTitle
// channels.editTitle#566decd0
func (s *Service) ChannelsEditTitle(ctx context.Context, request *mtproto.TLChannelsEditTitle) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.editTitle - metadata: {%s}", c.MD)
	return c.ChannelsEditTitle(request)
}

// ChannelsEditPhoto
// channels.editPhoto#f12e57c9
func (s *Service) ChannelsEditPhoto(ctx context.Context, request *mtproto.TLChannelsEditPhoto) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.editPhoto - metadata: {%s}", c.MD)
	return c.ChannelsEditPhoto(request)
}

// ChannelsJoinChannel7F6A1E22
// channels.joinChannel7F6A1E22#7f6a1e22
func (s *Service) ChannelsJoinChannel7F6A1E22(ctx context.Context, request *mtproto.TLChannelsJoinChannel7F6A1E22) (*mtproto.Messages_ChatInviteJoinResult, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.joinChannel7F6A1E22 - metadata: {%s}", c.MD)
	return c.ChannelsJoinChannel7F6A1E22(request)
}

// ChannelsLeaveChannel
// channels.leaveChannel#f836aa95
func (s *Service) ChannelsLeaveChannel(ctx context.Context, request *mtproto.TLChannelsLeaveChannel) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.leaveChannel - metadata: {%s}", c.MD)
	return c.ChannelsLeaveChannel(request)
}

// ChannelsInviteToChannelC9E33D54
// channels.inviteToChannelC9E33D54#c9e33d54
func (s *Service) ChannelsInviteToChannelC9E33D54(ctx context.Context, request *mtproto.TLChannelsInviteToChannelC9E33D54) (*mtproto.Messages_InvitedUsers, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.inviteToChannelC9E33D54 - metadata: {%s}", c.MD)
	return c.ChannelsInviteToChannelC9E33D54(request)
}

// ChannelsDeleteChannel
// channels.deleteChannel#c0111fe3
func (s *Service) ChannelsDeleteChannel(ctx context.Context, request *mtproto.TLChannelsDeleteChannel) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.deleteChannel - metadata: {%s}", c.MD)
	return c.ChannelsDeleteChannel(request)
}

// ChannelsExportMessageLink
// channels.exportMessageLink#e63fadeb
func (s *Service) ChannelsExportMessageLink(ctx context.Context, request *mtproto.TLChannelsExportMessageLink) (*mtproto.ExportedMessageLink, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.exportMessageLink - metadata: {%s}", c.MD)
	return c.ChannelsExportMessageLink(request)
}

// ChannelsToggleSignatures
// channels.toggleSignatures#418d549c (also #1f69b606)
func (s *Service) ChannelsToggleSignatures(ctx context.Context, request *mtproto.TLChannelsToggleSignatures) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.toggleSignatures - metadata: {%s}", c.MD)
	return c.ChannelsToggleSignatures(request)
}

// ChannelsGetAdminedPublicChannels
// channels.getAdminedPublicChannels#f8b036af
func (s *Service) ChannelsGetAdminedPublicChannels(ctx context.Context, request *mtproto.TLChannelsGetAdminedPublicChannels) (*mtproto.Messages_Chats, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.getAdminedPublicChannels - metadata: {%s}", c.MD)
	return c.ChannelsGetAdminedPublicChannels(request)
}

// ChannelsEditBanned
// channels.editBanned#96e6cd81
func (s *Service) ChannelsEditBanned(ctx context.Context, request *mtproto.TLChannelsEditBanned) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.editBanned - metadata: {%s}", c.MD)
	return c.ChannelsEditBanned(request)
}

// ChannelsGetAdminLog
// channels.getAdminLog#33ddf480
func (s *Service) ChannelsGetAdminLog(ctx context.Context, request *mtproto.TLChannelsGetAdminLog) (*mtproto.Channels_AdminLogResults, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.getAdminLog - metadata: {%s}", c.MD)
	return c.ChannelsGetAdminLog(request)
}

// ChannelsSetStickers
// channels.setStickers#ea8ca4f9
func (s *Service) ChannelsSetStickers(ctx context.Context, request *mtproto.TLChannelsSetStickers) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.setStickers - metadata: {%s}", c.MD)
	return c.ChannelsSetStickers(request)
}

// ChannelsReadMessageContents
// channels.readMessageContents#eab5dc38
func (s *Service) ChannelsReadMessageContents(ctx context.Context, request *mtproto.TLChannelsReadMessageContents) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.readMessageContents - metadata: {%s}", c.MD)
	return c.ChannelsReadMessageContents(request)
}

// ChannelsDeleteHistory9BAA9647
// channels.deleteHistory9BAA9647#9baa9647
func (s *Service) ChannelsDeleteHistory9BAA9647(ctx context.Context, request *mtproto.TLChannelsDeleteHistory9BAA9647) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.deleteHistory9BAA9647 - metadata: {%s}", c.MD)
	return c.ChannelsDeleteHistory9BAA9647(request)
}

// ChannelsTogglePreHistoryHidden
// channels.togglePreHistoryHidden#eabbb94c
func (s *Service) ChannelsTogglePreHistoryHidden(ctx context.Context, request *mtproto.TLChannelsTogglePreHistoryHidden) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.togglePreHistoryHidden - metadata: {%s}", c.MD)
	return c.ChannelsTogglePreHistoryHidden(request)
}

// ChannelsGetGroupsForDiscussion
// channels.getGroupsForDiscussion#f5dad378
func (s *Service) ChannelsGetGroupsForDiscussion(ctx context.Context, request *mtproto.TLChannelsGetGroupsForDiscussion) (*mtproto.Messages_Chats, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.getGroupsForDiscussion - metadata: {%s}", c.MD)
	return c.ChannelsGetGroupsForDiscussion(request)
}

// ChannelsSetDiscussionGroup
// channels.setDiscussionGroup#40582bb2
func (s *Service) ChannelsSetDiscussionGroup(ctx context.Context, request *mtproto.TLChannelsSetDiscussionGroup) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.setDiscussionGroup - metadata: {%s}", c.MD)
	return c.ChannelsSetDiscussionGroup(request)
}

// ChannelsEditLocation
// channels.editLocation#58e63f6d
func (s *Service) ChannelsEditLocation(ctx context.Context, request *mtproto.TLChannelsEditLocation) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.editLocation - metadata: {%s}", c.MD)
	return c.ChannelsEditLocation(request)
}

// ChannelsToggleSlowMode
// channels.toggleSlowMode#edd49ef0
func (s *Service) ChannelsToggleSlowMode(ctx context.Context, request *mtproto.TLChannelsToggleSlowMode) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.toggleSlowMode - metadata: {%s}", c.MD)
	return c.ChannelsToggleSlowMode(request)
}

// ChannelsGetInactiveChannels
// channels.getInactiveChannels#11e831ee
func (s *Service) ChannelsGetInactiveChannels(ctx context.Context, request *mtproto.TLChannelsGetInactiveChannels) (*mtproto.Messages_InactiveChats, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.getInactiveChannels - metadata: {%s}", c.MD)
	return c.ChannelsGetInactiveChannels(request)
}

// ChannelsDeleteParticipantHistory
// channels.deleteParticipantHistory#367544db
func (s *Service) ChannelsDeleteParticipantHistory(ctx context.Context, request *mtproto.TLChannelsDeleteParticipantHistory) (*mtproto.Messages_AffectedHistory, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.deleteParticipantHistory - metadata: {%s}", c.MD)
	return c.ChannelsDeleteParticipantHistory(request)
}

// ChannelsToggleParticipantsHidden
// channels.toggleParticipantsHidden#6a6e7854
func (s *Service) ChannelsToggleParticipantsHidden(ctx context.Context, request *mtproto.TLChannelsToggleParticipantsHidden) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.toggleParticipantsHidden - metadata: {%s}", c.MD)
	return c.ChannelsToggleParticipantsHidden(request)
}

// ChannelsJoinChannel24B524C5
// channels.joinChannel24B524C5#24b524c5
func (s *Service) ChannelsJoinChannel24B524C5(ctx context.Context, request *mtproto.TLChannelsJoinChannel24B524C5) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.joinChannel24B524C5 - metadata: {%s}", c.MD)
	return c.ChannelsJoinChannel24B524C5(request)
}

// ChannelsEditCreator
// channels.editCreator#8f38cd1f
func (s *Service) ChannelsEditCreator(ctx context.Context, request *mtproto.TLChannelsEditCreator) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.editCreator - metadata: {%s}", c.MD)
	return c.ChannelsEditCreator(request)
}

// ChannelsGetFutureCreatorAfterLeave
// channels.getFutureCreatorAfterLeave#a00918af
func (s *Service) ChannelsGetFutureCreatorAfterLeave(ctx context.Context, request *mtproto.TLChannelsGetFutureCreatorAfterLeave) (*mtproto.User, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.getFutureCreatorAfterLeave - metadata: {%s}", c.MD)
	return c.ChannelsGetFutureCreatorAfterLeave(request)
}

// ChannelsInviteToChannel199F3A6C
// channels.inviteToChannel199F3A6C#199f3a6c
func (s *Service) ChannelsInviteToChannel199F3A6C(ctx context.Context, request *mtproto.TLChannelsInviteToChannel199F3A6C) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.inviteToChannel199F3A6C - metadata: {%s}", c.MD)
	return c.ChannelsInviteToChannel199F3A6C(request)
}

// ChannelsDeleteHistoryAF369D42
// channels.deleteHistoryAF369D42#af369d42
func (s *Service) ChannelsDeleteHistoryAF369D42(ctx context.Context, request *mtproto.TLChannelsDeleteHistoryAF369D42) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("channels.deleteHistoryAF369D42 - metadata: {%s}", c.MD)
	return c.ChannelsDeleteHistoryAF369D42(request)
}
