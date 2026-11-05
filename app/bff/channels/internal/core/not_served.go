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

// ChannelsEditAdmin is not served yet: channels.editAdmin#9a98ad68 (also #d33c8902).
func (c *ChannelsCore) ChannelsEditAdmin(in *mtproto.TLChannelsEditAdmin) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsEditPhoto is not served yet: channels.editPhoto#f12e57c9.
func (c *ChannelsCore) ChannelsEditPhoto(in *mtproto.TLChannelsEditPhoto) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsToggleSignatures is not served yet: channels.toggleSignatures#418d549c (also #1f69b606).
func (c *ChannelsCore) ChannelsToggleSignatures(in *mtproto.TLChannelsToggleSignatures) (*mtproto.Updates, error) {
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

// ChannelsEditCreator is not served yet: channels.editCreator#8f38cd1f.
func (c *ChannelsCore) ChannelsEditCreator(in *mtproto.TLChannelsEditCreator) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsGetFutureCreatorAfterLeave is not served yet: channels.getFutureCreatorAfterLeave#a00918af.
func (c *ChannelsCore) ChannelsGetFutureCreatorAfterLeave(in *mtproto.TLChannelsGetFutureCreatorAfterLeave) (*mtproto.User, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsDeleteHistoryAF369D42 is not served yet: channels.deleteHistoryAF369D42#af369d42.
func (c *ChannelsCore) ChannelsDeleteHistoryAF369D42(in *mtproto.TLChannelsDeleteHistoryAF369D42) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}
