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
	"time"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
)

// The channels a request names, as the account sees them (ADR 0034).

// mutableChannel loads the channel of in with the account's participant
// row: CHANNEL_INVALID for one that does not exist or a wrong access_hash.
func (c *ChannelsCore) mutableChannel(in *mtproto.InputChannel) (*channelpb.MutableChannel, error) {
	id, hash := in.GetChannelId(), in.GetAccessHash()
	if in.GetPredicateName() != mtproto.Predicate_inputChannel || id == 0 {
		return nil, mtproto.ErrChannelInvalid
	}
	mc, err := c.svcCtx.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: id, Id: []int64{c.MD.UserId}})
	if err != nil {
		return nil, err
	}
	if mc.GetChannel().GetAccessHash() != hash {
		return nil, mtproto.ErrChannelInvalid
	}
	// A deleted channel is no one's to read or change (task 055): its rows
	// stay, every member left. CHANNEL_PRIVATE, on which the clients drop
	// a channel they cannot reach (Android checkChannelError,
	// MessagesController.java:16796-16800 at aac1efcb); which code
	// Telegram gives after a deletion is not verified.
	if mc.GetChannel().GetDeleted() {
		return nil, mtproto.ErrChannelPrivate
	}
	return mc, nil
}

// readable tells whether the account may read the channel: a member, or
// anyone not kicked from a public channel; CHANNEL_PRIVATE otherwise, as
// Telegram answers (ADR 0034, p. 4).
func readable(mc *channelpb.MutableChannel, userId int64) error {
	p, _ := mc.GetParticipant(userId)
	if p.Member() {
		return nil
	}
	// A public channel, or the discussion group of a channel the user is
	// in (task 048): the view is a channel, not channelForbidden.
	if (mc.GetChannel().Public() || p.GetState() == channelpb.StateLinked) && mc.ToChat(userId, int32(time.Now().Unix())).GetPredicateName() == mtproto.Predicate_channel {
		return nil
	}
	return mtproto.ErrChannelPrivate
}
