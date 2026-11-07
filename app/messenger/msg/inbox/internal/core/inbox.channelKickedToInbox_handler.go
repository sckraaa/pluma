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
	"errors"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// InboxChannelKickedToInbox
// inbox.channelKickedToInbox user_id:long from_id:long channel_id:long = Void;
//
// from_id kicked the recipient from a channel (channels.editBanned with
// view_messages, task 042): its box drops the dialog and journals
// updateChannel with seq and the channel as channelForbidden, on which
// every client drops it, as for a deleted channel. The event applies the
// state the channel has when it is handled: one freed again in the
// meantime keeps the channel.
func (c *InboxCore) InboxChannelKickedToInbox(in *inbox.TLInboxChannelKickedToInbox) error {
	peer := &mtproto.PeerUtil{PeerType: mtproto.PEER_CHANNEL, PeerId: in.ChannelId}
	none := func(*userbox.Box) (bool, error) { return true, nil }
	mc, err := c.svcCtx.Dao.ChannelClient.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: in.ChannelId, Id: []int64{in.UserId}})
	if errors.Is(err, mtproto.ErrChannelInvalid) {
		return c.apply(in.UserId, peer, none)
	}
	if err != nil {
		return err
	}
	now := int32(time.Now().Unix())
	p, _ := mc.GetParticipant(in.UserId)
	if !mc.GetChannel().GetDeleted() && !p.Kicked(now) {
		return c.apply(in.UserId, peer, none)
	}
	return c.dropChannel(in.UserId, peer, mc.ToChat(in.UserId, now))
}
