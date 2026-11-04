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

// ChannelsCheckUsername
// channels.checkUsername#10e6bd2c channel:InputChannel username:string = Bool;
//
// Whether the account may give the channel the username (task 044): true
// for a free one, false for one taken, USERNAME_INVALID for a bad one,
// CHANNELS_ADMIN_PUBLIC_TOO_MUCH at the limit of public channels. Android
// shows "already taken" for any answer but true and those errors, and asks
// with inputChannelEmpty and "1" only to learn the limit
// (ChannelCreateActivity.java:170-173, 1317-1347 at aac1efcb): the limit is
// checked before the format.
func (c *UsernamesCore) ChannelsCheckUsername(in *mtproto.TLChannelsCheckUsername) (*mtproto.Bool, error) {
	channelId, err := c.channelOf(in.Channel)
	if err != nil {
		return nil, err
	}
	username := in.Username
	formatErr := checkAccountUsername(username)
	if username == "" || formatErr != nil {
		username = ""
	}
	free, err := c.svcCtx.Dao.Channel.ChannelCheckUsername(c.ctx, &channelpb.TLChannelCheckUsername{
		SelfId:    c.MD.UserId,
		ChannelId: channelId,
		Username:  username,
	})
	switch {
	case err != nil:
		return nil, err
	case in.Username == "" || formatErr != nil:
		return nil, mtproto.ErrUsernameInvalid
	}
	return free, nil
}

// channelOf is the id of the channel of in, 0 for inputChannelEmpty (one
// not made yet); CHANNEL_INVALID for a wrong access_hash.
func (c *UsernamesCore) channelOf(in *mtproto.InputChannel) (int64, error) {
	switch in.GetPredicateName() {
	case mtproto.Predicate_inputChannelEmpty:
		return 0, nil
	case mtproto.Predicate_inputChannel:
	default:
		return 0, mtproto.ErrChannelInvalid
	}
	mc, err := c.svcCtx.Dao.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: in.GetChannelId(), Id: []int64{c.MD.UserId}})
	if err != nil {
		return 0, err
	}
	if mc.GetChannel().GetAccessHash() != in.GetAccessHash() {
		return 0, mtproto.ErrChannelInvalid
	}
	return in.GetChannelId(), nil
}
