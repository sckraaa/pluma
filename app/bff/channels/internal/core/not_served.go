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
)

// The methods of channels and supergroups not served yet (ADR 0034, tasks
// 038-049): each answers ERR_ENTERPRISE_IS_BLOCKED (400), which the clients
// do not retry, never an internal error (500), which Android's and Web K's
// transports retry without end. A method moves to its own handler file when
// its task serves it.

// MessagesGetPersonalChannelHistory is not served yet: messages.getPersonalChannelHistory#55fb0996.
func (c *ChannelsCore) MessagesGetPersonalChannelHistory(in *mtproto.TLMessagesGetPersonalChannelHistory) (*mtproto.Messages_Messages, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsReadHistory is not served yet: channels.readHistory#cc104937.
func (c *ChannelsCore) ChannelsReadHistory(in *mtproto.TLChannelsReadHistory) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsDeleteMessages is not served yet: channels.deleteMessages#84c1fd4e.
func (c *ChannelsCore) ChannelsDeleteMessages(in *mtproto.TLChannelsDeleteMessages) (*mtproto.Messages_AffectedMessages, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsGetMessages is not served yet: channels.getMessages#ad8c9a23 (also #93d7b347).
func (c *ChannelsCore) ChannelsGetMessages(in *mtproto.TLChannelsGetMessages) (*mtproto.Messages_Messages, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsGetParticipants is not served yet: channels.getParticipants#77ced9d0.
func (c *ChannelsCore) ChannelsGetParticipants(in *mtproto.TLChannelsGetParticipants) (*mtproto.Channels_ChannelParticipants, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsEditAdmin is not served yet: channels.editAdmin#9a98ad68 (also #d33c8902).
func (c *ChannelsCore) ChannelsEditAdmin(in *mtproto.TLChannelsEditAdmin) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsEditTitle is not served yet: channels.editTitle#566decd0.
func (c *ChannelsCore) ChannelsEditTitle(in *mtproto.TLChannelsEditTitle) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsEditPhoto is not served yet: channels.editPhoto#f12e57c9.
func (c *ChannelsCore) ChannelsEditPhoto(in *mtproto.TLChannelsEditPhoto) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsJoinChannel7F6A1E22 is not served yet: channels.joinChannel7F6A1E22#7f6a1e22.
func (c *ChannelsCore) ChannelsJoinChannel7F6A1E22(in *mtproto.TLChannelsJoinChannel7F6A1E22) (*mtproto.Messages_ChatInviteJoinResult, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsLeaveChannel is not served yet: channels.leaveChannel#f836aa95.
func (c *ChannelsCore) ChannelsLeaveChannel(in *mtproto.TLChannelsLeaveChannel) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsInviteToChannelC9E33D54 is not served yet: channels.inviteToChannelC9E33D54#c9e33d54.
func (c *ChannelsCore) ChannelsInviteToChannelC9E33D54(in *mtproto.TLChannelsInviteToChannelC9E33D54) (*mtproto.Messages_InvitedUsers, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsDeleteChannel is not served yet: channels.deleteChannel#c0111fe3.
func (c *ChannelsCore) ChannelsDeleteChannel(in *mtproto.TLChannelsDeleteChannel) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsExportMessageLink is not served yet: channels.exportMessageLink#e63fadeb.
func (c *ChannelsCore) ChannelsExportMessageLink(in *mtproto.TLChannelsExportMessageLink) (*mtproto.ExportedMessageLink, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsToggleSignatures is not served yet: channels.toggleSignatures#418d549c (also #1f69b606).
func (c *ChannelsCore) ChannelsToggleSignatures(in *mtproto.TLChannelsToggleSignatures) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsGetAdminedPublicChannels is not served yet: channels.getAdminedPublicChannels#f8b036af.
func (c *ChannelsCore) ChannelsGetAdminedPublicChannels(in *mtproto.TLChannelsGetAdminedPublicChannels) (*mtproto.Messages_Chats, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsEditBanned is not served yet: channels.editBanned#96e6cd81.
func (c *ChannelsCore) ChannelsEditBanned(in *mtproto.TLChannelsEditBanned) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsGetAdminLog is not served yet: channels.getAdminLog#33ddf480.
func (c *ChannelsCore) ChannelsGetAdminLog(in *mtproto.TLChannelsGetAdminLog) (*mtproto.Channels_AdminLogResults, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsSetStickers is not served yet: channels.setStickers#ea8ca4f9.
func (c *ChannelsCore) ChannelsSetStickers(in *mtproto.TLChannelsSetStickers) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsReadMessageContents is not served yet: channels.readMessageContents#eab5dc38.
func (c *ChannelsCore) ChannelsReadMessageContents(in *mtproto.TLChannelsReadMessageContents) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsDeleteHistory9BAA9647 is not served yet: channels.deleteHistory9BAA9647#9baa9647.
func (c *ChannelsCore) ChannelsDeleteHistory9BAA9647(in *mtproto.TLChannelsDeleteHistory9BAA9647) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsTogglePreHistoryHidden is not served yet: channels.togglePreHistoryHidden#eabbb94c.
func (c *ChannelsCore) ChannelsTogglePreHistoryHidden(in *mtproto.TLChannelsTogglePreHistoryHidden) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsGetGroupsForDiscussion is not served yet: channels.getGroupsForDiscussion#f5dad378.
func (c *ChannelsCore) ChannelsGetGroupsForDiscussion(in *mtproto.TLChannelsGetGroupsForDiscussion) (*mtproto.Messages_Chats, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsSetDiscussionGroup is not served yet: channels.setDiscussionGroup#40582bb2.
func (c *ChannelsCore) ChannelsSetDiscussionGroup(in *mtproto.TLChannelsSetDiscussionGroup) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsEditLocation is not served yet: channels.editLocation#58e63f6d.
func (c *ChannelsCore) ChannelsEditLocation(in *mtproto.TLChannelsEditLocation) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsToggleSlowMode is not served yet: channels.toggleSlowMode#edd49ef0.
func (c *ChannelsCore) ChannelsToggleSlowMode(in *mtproto.TLChannelsToggleSlowMode) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsGetInactiveChannels is not served yet: channels.getInactiveChannels#11e831ee.
func (c *ChannelsCore) ChannelsGetInactiveChannels(in *mtproto.TLChannelsGetInactiveChannels) (*mtproto.Messages_InactiveChats, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsDeleteParticipantHistory is not served yet: channels.deleteParticipantHistory#367544db.
func (c *ChannelsCore) ChannelsDeleteParticipantHistory(in *mtproto.TLChannelsDeleteParticipantHistory) (*mtproto.Messages_AffectedHistory, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsToggleParticipantsHidden is not served yet: channels.toggleParticipantsHidden#6a6e7854.
func (c *ChannelsCore) ChannelsToggleParticipantsHidden(in *mtproto.TLChannelsToggleParticipantsHidden) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsJoinChannel24B524C5 is not served yet: channels.joinChannel24B524C5#24b524c5.
func (c *ChannelsCore) ChannelsJoinChannel24B524C5(in *mtproto.TLChannelsJoinChannel24B524C5) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsEditCreator is not served yet: channels.editCreator#8f38cd1f.
func (c *ChannelsCore) ChannelsEditCreator(in *mtproto.TLChannelsEditCreator) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsGetFutureCreatorAfterLeave is not served yet: channels.getFutureCreatorAfterLeave#a00918af.
func (c *ChannelsCore) ChannelsGetFutureCreatorAfterLeave(in *mtproto.TLChannelsGetFutureCreatorAfterLeave) (*mtproto.User, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsInviteToChannel199F3A6C is not served yet: channels.inviteToChannel199F3A6C#199f3a6c.
func (c *ChannelsCore) ChannelsInviteToChannel199F3A6C(in *mtproto.TLChannelsInviteToChannel199F3A6C) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsDeleteHistoryAF369D42 is not served yet: channels.deleteHistoryAF369D42#af369d42.
func (c *ChannelsCore) ChannelsDeleteHistoryAF369D42(in *mtproto.TLChannelsDeleteHistoryAF369D42) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}
