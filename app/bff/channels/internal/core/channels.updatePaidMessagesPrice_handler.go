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

// Errors of channels.updatePaidMessagesPrice the schema's list has no entry
// for (core.telegram.org/method/channels.updatePaidMessagesPrice, 400).
var (
	errStarsAmountInvalid          = status.Error(mtproto.ErrBadRequest, "STARS_AMOUNT_INVALID")
	errChannelMonoforumUnsupported = status.Error(mtproto.ErrBadRequest, "CHANNEL_MONOFORUM_UNSUPPORTED")
)

// ChannelsUpdatePaidMessagesPrice
// channels.updatePaidMessagesPrice#4b12327b flags:# broadcast_messages_allowed:flags.0?true channel:InputChannel send_paid_messages_stars:long = Updates;
//
// The direct messages of a broadcast channel (task 084, ADR 0055;
// core.telegram.org/api/monoforum): broadcast_messages_allowed switches
// them on, its absence off; the first time makes the channel's monoforum.
// The answer holds the service message of the channel and both sides of the
// pair, which the clients apply (tdesktop edit_peer_info_box.cpp:330-340,
// Android PostSuggestionsEditActivity.java:221-222).
//
// Messages are free here: there are no stars to pay with, so switching the
// direct messages on with a price above 0 is STARS_AMOUNT_INVALID ("The
// specified amount in stars is invalid", the method's page). The clients
// start the price from stars_paid_messages_channel_amount_default of the
// app config (Android PostSuggestionsEditActivity.java:77, tdesktop
// edit_privacy_box.cpp:1344, iOS PostSuggestionsSettingsScreen.swift:264),
// which is 0 here. Switching them off takes any amount: tdesktop sends -1
// with it (edit_peer_info_box.cpp:331-337, 1079), Android 0
// (PostSuggestionsEditActivity.java:216). In a supergroup the method sets
// that price alone, and with the only price there is nothing changes:
// CHAT_NOT_MODIFIED, which tdesktop takes for done
// (edit_peer_info_box.cpp:341-351). CHANNEL_MONOFORUM_UNSUPPORTED for a
// monoforum, CHAT_NOT_MODIFIED when the direct messages are as asked,
// CHAT_ADMIN_REQUIRED for one who may not change the channel's info.
func (c *ChannelsCore) ChannelsUpdatePaidMessagesPrice(in *mtproto.TLChannelsUpdatePaidMessagesPrice) (*mtproto.Updates, error) {
	mc, err := c.mutableChannel(in.GetChannel())
	if err != nil {
		return nil, err
	}
	ch := mc.GetChannel()
	stars := in.GetSendPaidMessagesStars()
	switch {
	case ch.GetMonoforum():
		return nil, errChannelMonoforumUnsupported
	case !ch.GetBroadcast():
		if err = readable(mc, c.MD.UserId); err != nil {
			return nil, err
		}
		if stars != 0 {
			return nil, errStarsAmountInvalid
		}
		return nil, mtproto.ErrChatNotModified
	case in.GetBroadcastMessagesAllowed() && stars != 0:
		return nil, errStarsAmountInvalid
	}
	r, err := c.svcCtx.Channel.ChannelUpdateDirectMessages(c.ctx, &channelpb.TLChannelUpdateDirectMessages{
		SelfId:        c.MD.UserId,
		PermAuthKeyId: c.MD.PermAuthKeyId,
		ChannelId:     ch.GetId(),
		Enabled:       mtproto.ToBool(in.GetBroadcastMessagesAllowed()),
	})
	if err != nil {
		c.Logger.Errorf("channels.updatePaidMessagesPrice - channel %d: %v", ch.GetId(), err)
		return nil, err
	}
	return r, nil
}
