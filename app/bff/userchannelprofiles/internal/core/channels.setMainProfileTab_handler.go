// Copyright 2025 Teamgram Authors
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
//
// Author: teamgramio (teamgram.io@gmail.com)
//

package core

import (
	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
)

// ChannelsSetMainProfileTab
// channels.setMainProfileTab#3583fcb1 channel:InputChannel tab:ProfileTab = Bool;
//
// The tab the channel's profile opens on (channelFull.main_tab), for those
// who may change its info (biz channel.setMainProfileTab). The clients set
// it in their cache first and drop the answer (tdesktop
// TabsHost::setMainTab, iOS ProfileTabs.swift:21, Android
// SharedMediaLayout.java:4277); the other members load the full channel
// again on updateChannel. CHANNEL_INVALID for a channel of another access
// hash (core.telegram.org/method/channels.setMainProfileTab).
func (c *UserChannelProfilesCore) ChannelsSetMainProfileTab(in *mtproto.TLChannelsSetMainProfileTab) (*mtproto.Bool, error) {
	ch := in.GetChannel()
	if ch.GetPredicateName() != mtproto.Predicate_inputChannel || ch.GetChannelId() == 0 {
		return nil, mtproto.ErrChannelInvalid
	}
	mc, err := c.svcCtx.Dao.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: ch.GetChannelId(), Id: []int64{c.MD.UserId}})
	if err != nil {
		c.Logger.Errorf("channels.setMainProfileTab - error: %v", err)
		return nil, err
	}
	if mc.GetChannel().GetAccessHash() != ch.GetAccessHash() || mc.GetChannel().GetDeleted() {
		return nil, mtproto.ErrChannelInvalid
	}
	return c.svcCtx.Dao.Channel.ChannelSetMainProfileTab(c.ctx, &channelpb.TLChannelSetMainProfileTab{
		SelfId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		ChannelId:     ch.GetChannelId(),
		Tab:           in.GetTab(),
	})
}
