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

// InboxChannelDeletedToInbox
// inbox.channelDeletedToInbox user_id:long from_id:long channel_id:long = Void;
//
// from_id deleted a channel the recipient was in (task 055): the
// creator's transaction marked it deleted and took everyone out; the
// recipient's drops the dialog and journals updateChannel with seq and the
// channel as channelForbidden, on which every client drops it (Android
// MessagesController.java:20146-20155; iOS UpdatePeers.swift:378-389;
// tdesktop data_channel.cpp:215-226; Web K dialogs.ts:178-183). A device
// offline now gets it from updates.getDifference.
func (c *InboxCore) InboxChannelDeletedToInbox(in *inbox.TLInboxChannelDeletedToInbox) error {
	peer := &mtproto.PeerUtil{PeerType: mtproto.PEER_CHANNEL, PeerId: in.ChannelId}
	mc, err := c.svcCtx.Dao.ChannelClient.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: in.ChannelId, Id: []int64{in.UserId}})
	if errors.Is(err, mtproto.ErrChannelInvalid) {
		return c.apply(in.UserId, peer, func(*userbox.Box) (bool, error) { return true, nil })
	}
	if err != nil {
		return err
	}
	if !mc.GetChannel().GetDeleted() {
		// Not deleted: the event is not this channel's state.
		return c.apply(in.UserId, peer, func(*userbox.Box) (bool, error) { return true, nil })
	}
	view := mc.ToChat(in.UserId, int32(time.Now().Unix()))
	return c.apply(in.UserId, peer, func(b *userbox.Box) (bool, error) {
		if _, err := b.Tx().Exec("UPDATE dialogs SET deleted = 1 WHERE user_id = ? AND peer_type = ? AND peer_id = ?",
			b.UserId(), mtproto.PEER_CHANNEL, in.ChannelId); err != nil {
			return true, err
		}
		u := mtproto.MakeTLUpdateChannel(&mtproto.Update{ChannelId: peer.PeerId}).To_Update()
		return true, c.svcCtx.Dao.EmitSeqUpdate(b, u, nil, []*mtproto.Chat{view})
	})
}
