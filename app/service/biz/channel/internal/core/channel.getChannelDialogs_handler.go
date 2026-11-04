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
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
)

// ChannelGetChannelDialogs
// channel.getChannelDialogs self_id:long id_list:Vector<long> = Vector<Dialog>;
func (c *ChannelCore) ChannelGetChannelDialogs(in *channel.TLChannelGetChannelDialogs) (*channel.Vector_Dialog, error) {
	dialogs, err := c.svcCtx.Dao.ChannelDialogs(c.ctx, in.SelfId, in.IdList)
	if err != nil {
		c.Logger.Errorf("channel.getChannelDialogs - error: %v", err)
		return nil, err
	}
	return &channel.Vector_Dialog{Datas: dialogs}, nil
}

// ChannelGetTopMessages
// channel.getTopMessages self_id:long id_list:Vector<long> = Vector<Message>;
func (c *ChannelCore) ChannelGetTopMessages(in *channel.TLChannelGetTopMessages) (*channel.Vector_Message, error) {
	messages, err := c.svcCtx.Dao.TopMessages(c.ctx, in.SelfId, in.IdList)
	if err != nil {
		c.Logger.Errorf("channel.getTopMessages - error: %v", err)
		return nil, err
	}
	return &channel.Vector_Message{Datas: messages}, nil
}
