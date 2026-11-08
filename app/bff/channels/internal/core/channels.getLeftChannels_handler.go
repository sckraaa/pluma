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

	"google.golang.org/grpc/status"
)

// errTakeoutRequired: channels.getLeftChannels is a method of the takeout
// API, called wrapped in invokeWithTakeout (403, core.telegram.org/method/
// channels.getLeftChannels).
var errTakeoutRequired = status.Error(mtproto.ErrForbidden, "TAKEOUT_REQUIRED")

// ChannelsGetLeftChannels
// channels.getLeftChannels#8341ecc0 offset:int = messages.Chats;
//
// A page of the channels and supergroups the account left (task 041).
func (c *ChannelsCore) ChannelsGetLeftChannels(in *mtproto.TLChannelsGetLeftChannels) (*mtproto.Messages_Chats, error) {
	if c.MD.GetTakeout().GetId() == 0 {
		c.Logger.Errorf("channels.getLeftChannels - error: %v", errTakeoutRequired)
		return nil, errTakeoutRequired
	}
	return c.svcCtx.Channel.ChannelGetLeftChannels(c.ctx, &channelpb.TLChannelGetLeftChannels{SelfId: c.MD.UserId, Offset: in.Offset})
}

// The rest of the takeout API is not served: a client's data export stops
// at account.initTakeoutSession (ERR_ENTERPRISE_IS_BLOCKED, 400).

// AccountInitTakeoutSession is not served: account.initTakeoutSession#8ef3eab0.
func (c *ChannelsCore) AccountInitTakeoutSession(in *mtproto.TLAccountInitTakeoutSession) (*mtproto.Account_Takeout, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// AccountFinishTakeoutSession is not served: account.finishTakeoutSession#1d2652ee.
func (c *ChannelsCore) AccountFinishTakeoutSession(in *mtproto.TLAccountFinishTakeoutSession) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesGetSplitRanges is not served: messages.getSplitRanges#1cff7e08.
func (c *ChannelsCore) MessagesGetSplitRanges(in *mtproto.TLMessagesGetSplitRanges) (*mtproto.Vector_MessageRange, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}
