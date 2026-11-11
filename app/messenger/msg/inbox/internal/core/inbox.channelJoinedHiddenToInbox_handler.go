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
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// InboxChannelJoinedHiddenToInbox
// inbox.channelJoinedHiddenToInbox user_id:long from_id:long channel_id:long read_max_id:int = Void;
//
// The ghost user_id joined the channel hidden (task 080, ADR 0048, p. 2):
// from_id mentioned it, or it wrote or reacted there itself (from_id 0).
// The row was written by that transaction; the ghost's box gets the dialog,
// read up to read_max_id so that the mention stays unread, and updateChannel
// with seq with the channel as the ghost sees it now, a member. The chat is
// muted for the ghost first: in a chat it was called into, only the
// mentions of it and the replies to it notify, which the push service
// sends through a mute (push/internal/core/core.go, n.Mention). A ghost no
// longer in the channel when the event comes gets nothing; nor does a
// channel that is gone.
func (c *InboxCore) InboxChannelJoinedHiddenToInbox(in *inbox.TLInboxChannelJoinedHiddenToInbox) error {
	peer := &mtproto.PeerUtil{PeerType: mtproto.PEER_CHANNEL, PeerId: in.ChannelId}
	mc, err := c.svcCtx.Dao.ChannelClient.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: in.ChannelId, Id: []int64{in.UserId}})
	if errors.Is(err, mtproto.ErrChannelInvalid) {
		return c.apply(in.UserId, peer, func(*userbox.Box) (bool, error) { return true, nil })
	}
	if err != nil {
		return err
	}
	if p, ok := mc.GetParticipant(in.UserId); !ok || !p.Member() {
		return c.apply(in.UserId, peer, func(*userbox.Box) (bool, error) { return true, nil })
	}
	view := mc.ToChat(in.UserId, int32(time.Now().Unix()))
	// Muting again on a repeated event changes nothing.
	if _, err = c.svcCtx.Dao.UserClient.UserSetNotifySettings(c.ctx, &userpb.TLUserSetNotifySettings{
		UserId:   in.UserId,
		PeerType: mtproto.PEER_CHANNEL,
		PeerId:   in.ChannelId,
		Settings: mtproto.MakeTLPeerNotifySettings(&mtproto.PeerNotifySettings{
			MuteUntil: &wrapperspb.Int32Value{Value: muteForever},
		}).To_PeerNotifySettings(),
		Chats: []*mtproto.Chat{view},
	}); err != nil {
		return err
	}
	return c.apply(in.UserId, peer, func(b *userbox.Box) (bool, error) {
		member, err := c.svcCtx.Dao.JoinedHidden(b, in.ChannelId, in.ReadMaxId)
		if err != nil || !member {
			return true, err
		}
		u := mtproto.MakeTLUpdateChannel(&mtproto.Update{ChannelId: peer.PeerId}).To_Update()
		return true, c.svcCtx.Dao.EmitSeqUpdate(b, u, nil, []*mtproto.Chat{view})
	})
}
