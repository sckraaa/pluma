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

// ChannelsReadHistory
// channels.readHistory#cc104937 channel:InputChannel max_id:int = Bool;
//
// The account read the channel up to max_id (task 043): its dialog, its
// mentions, its other devices and, in a supergroup, the authors of what it
// read (ADR 0034, p. 3). One who reads a public channel it is not in has
// no dialog to move: true, as nothing is to be read there.
func (c *ChannelsCore) ChannelsReadHistory(in *mtproto.TLChannelsReadHistory) (*mtproto.Bool, error) {
	mc, err := c.mutableChannel(in.Channel)
	if err != nil {
		return nil, err
	}
	if err = readable(mc, c.MD.UserId); err != nil {
		return nil, err
	}
	if me, _ := mc.GetParticipant(c.MD.UserId); !me.Member() {
		return mtproto.BoolTrue, nil
	}
	return c.svcCtx.Channel.ChannelReadHistory(c.ctx, &channelpb.TLChannelReadHistory{
		SelfId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		ChannelId:     mc.GetChannel().GetId(),
		MaxId:         in.MaxId,
	})
}
