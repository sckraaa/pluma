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
)

// ChannelsGetInactiveChannels
// channels.getInactiveChannels#11e831ee = messages.InactiveChats;
//
// The account's channels and supergroups, the least active first, with the
// date of its last activity in each (biz channel.getInactiveChannels). The
// clients ask it when a join or a new group meets CHANNELS_TOO_MUCH, to
// let the user leave some (Android TooManyCommunitiesActivity.java:335,
// LimitReachedBottomSheet.java:2684; tdesktop premium_limits_box.cpp:223;
// iOS InactiveChannels.swift:23, which retries an error without end). In
// Pluma everyone has the Premium limit (ADR 0041, premium.ChannelsLimit),
// so the list comes up only at that limit; the method answers at any time.
func (c *ChannelsCore) ChannelsGetInactiveChannels(in *mtproto.TLChannelsGetInactiveChannels) (*mtproto.Messages_InactiveChats, error) {
	r, err := c.svcCtx.Channel.ChannelGetInactiveChannels(c.ctx, &channelpb.TLChannelGetInactiveChannels{SelfId: c.MD.UserId})
	if err != nil {
		c.Logger.Errorf("channels.getInactiveChannels - error: %v", err)
		return nil, err
	}
	if r.Users == nil {
		r.Users = []*mtproto.User{}
	}
	return r, nil
}
