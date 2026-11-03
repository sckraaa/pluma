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

// ChannelGetChannelListByIdList
// channel.getChannelListByIdList self_id:long id_list:Vector<long> = Vector<MutableChannel>;
//
// The channels of id_list that exist, each with the participant row of
// self_id when it has one; an id that does not exist is left out. A failed
// read is an error, never a shorter list.
func (c *ChannelCore) ChannelGetChannelListByIdList(in *channel.TLChannelGetChannelListByIdList) (*channel.Vector_MutableChannel, error) {
	channels, err := c.svcCtx.Dao.GetChannelListByIdList(c.ctx, in.SelfId, in.IdList)
	if err != nil {
		c.Logger.Errorf("channel.getChannelListByIdList - user %d: %v", in.SelfId, err)
		return nil, err
	}
	return &channel.Vector_MutableChannel{Datas: channels}, nil
}
