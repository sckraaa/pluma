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

// InboxChannelInvitedToInbox
// inbox.channelInvitedToInbox user_id:long from_id:long channel_id:long = Void;
//
// from_id added the recipient to a channel or a supergroup (task 041, ADR
// 0034, p. 3): the inviter's transaction wrote the participant row; the
// recipient's writes the dialog, read up to the last message as a join
// does, and journals updateChannel with seq for its devices. One no longer
// in the channel when the event comes (it left, or was removed) gets
// nothing; nor does a channel that is gone.
func (c *InboxCore) InboxChannelInvitedToInbox(in *inbox.TLInboxChannelInvitedToInbox) error {
	peer := &mtproto.PeerUtil{PeerType: mtproto.PEER_CHANNEL, PeerId: in.ChannelId}
	mc, err := c.svcCtx.Dao.ChannelClient.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: in.ChannelId, Id: []int64{in.UserId}})
	if errors.Is(err, mtproto.ErrChannelInvalid) {
		return c.apply(in.UserId, peer, func(*userbox.Box) (bool, error) { return true, nil })
	}
	if err != nil {
		return err
	}
	view := mc.ToChat(in.UserId, int32(time.Now().Unix()))
	return c.apply(in.UserId, peer, func(b *userbox.Box) (bool, error) {
		member, err := c.svcCtx.Dao.InvitedToChannel(b, in.ChannelId)
		if err != nil || !member {
			return true, err
		}
		u := mtproto.MakeTLUpdateChannel(&mtproto.Update{ChannelId: peer.PeerId}).To_Update()
		return true, c.svcCtx.Dao.EmitSeqUpdate(b, u, nil, []*mtproto.Chat{view})
	})
}
