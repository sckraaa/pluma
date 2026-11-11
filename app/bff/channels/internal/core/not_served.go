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

// ChannelsSetStickers is not served yet: channels.setStickers#ea8ca4f9.
func (c *ChannelsCore) ChannelsSetStickers(in *mtproto.TLChannelsSetStickers) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsEditLocation is not served yet: channels.editLocation#58e63f6d.
func (c *ChannelsCore) ChannelsEditLocation(in *mtproto.TLChannelsEditLocation) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsGetInactiveChannels is not served yet: channels.getInactiveChannels#11e831ee.
func (c *ChannelsCore) ChannelsGetInactiveChannels(in *mtproto.TLChannelsGetInactiveChannels) (*mtproto.Messages_InactiveChats, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// ChannelsGetFutureCreatorAfterLeave is not served yet: channels.getFutureCreatorAfterLeave#a00918af.
func (c *ChannelsCore) ChannelsGetFutureCreatorAfterLeave(in *mtproto.TLChannelsGetFutureCreatorAfterLeave) (*mtproto.User, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}
