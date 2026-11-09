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
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// UserSetTopicNotifySettings
// user.setTopicNotifySettings user_id:long channel_id:long top_msg_id:int settings:PeerNotifySettings = Bool;
func (c *UserCore) UserSetTopicNotifySettings(in *user.TLUserSetTopicNotifySettings) (*mtproto.Bool, error) {
	if err := c.svcCtx.Dao.SetTopicNotifySettings(c.ctx, in.UserId, in.ChannelId, in.TopMsgId, in.Settings); err != nil {
		c.Logger.Errorf("user.setTopicNotifySettings - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

// UserGetTopicNotifySettings
// user.getTopicNotifySettings user_id:long channel_id:long top_msg_id:int = PeerNotifySettings;
func (c *UserCore) UserGetTopicNotifySettings(in *user.TLUserGetTopicNotifySettings) (*mtproto.PeerNotifySettings, error) {
	s, err := c.svcCtx.Dao.GetTopicNotifySettings(c.ctx, in.UserId, in.ChannelId, in.TopMsgId)
	if err != nil {
		c.Logger.Errorf("user.getTopicNotifySettings - error: %v", err)
		return nil, err
	}
	return s, nil
}

// UserGetTopicNotifyExceptions
// user.getTopicNotifyExceptions user_id:long channel_id:long = Updates;
func (c *UserCore) UserGetTopicNotifyExceptions(in *user.TLUserGetTopicNotifyExceptions) (*mtproto.Updates, error) {
	us, err := c.svcCtx.Dao.TopicNotifyExceptions(c.ctx, in.UserId, in.ChannelId)
	if err != nil {
		c.Logger.Errorf("user.getTopicNotifyExceptions - error: %v", err)
		return nil, err
	}
	return mtproto.MakeUpdatesByUpdates(us...), nil
}
