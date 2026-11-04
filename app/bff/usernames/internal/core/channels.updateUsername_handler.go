// Copyright 2022 Teamgram Authors
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

// ChannelsUpdateUsername
// channels.updateUsername#3514b3de channel:InputChannel username:string = Bool;
//
// The channel's public link (task 044); "" makes the channel private. The
// format is the one of accounts; biz channel checks the rights, the
// owner's limit of public channels and that nobody else has the username.
func (c *UsernamesCore) ChannelsUpdateUsername(in *mtproto.TLChannelsUpdateUsername) (*mtproto.Bool, error) {
	if in.GetChannel().GetPredicateName() != mtproto.Predicate_inputChannel {
		return nil, mtproto.ErrChannelInvalid
	}
	channelId, err := c.channelOf(in.Channel)
	if err != nil {
		return nil, err
	}
	if err = checkAccountUsername(in.Username); err != nil {
		return nil, err
	}
	return c.svcCtx.Dao.Channel.ChannelUpdateUsername(c.ctx, &channelpb.TLChannelUpdateUsername{
		SelfId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		ChannelId:     channelId,
		Username:      in.Username,
	})
}
