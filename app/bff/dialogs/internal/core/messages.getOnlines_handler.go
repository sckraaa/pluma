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
	"slices"
	"time"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// MessagesGetOnlines
// messages.getOnlines#6e2be050 peer:InputPeer = ChatOnlines;
//
// How many members of a group or a channel are online now, for a member of
// it (Android MessagesController.java:10788 polls it for the header). The
// support ghost (ADR 0048) is counted by itself alone: to the others it is
// no member.
func (c *DialogsCore) MessagesGetOnlines(in *mtproto.TLMessagesGetOnlines) (*mtproto.ChatOnlines, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.GetPeer())
	var (
		members []int64
		denied  error
	)
	switch {
	case peer.IsChat():
		r, err := c.svcCtx.Dao.ChatClient.ChatGetChatParticipantIdList(c.ctx, &chatpb.TLChatGetChatParticipantIdList{ChatId: peer.PeerId})
		if err != nil {
			c.Logger.Errorf("messages.getOnlines - error: %v", err)
			return nil, err
		}
		members, denied = r.GetDatas(), mtproto.ErrChatIdInvalid
	case peer.IsChannel():
		r, err := c.svcCtx.Dao.Channel.ChannelGetMemberIdList(c.ctx, &channelpb.TLChannelGetMemberIdList{ChannelId: peer.PeerId})
		if err != nil {
			c.Logger.Errorf("messages.getOnlines - error: %v", err)
			return nil, err
		}
		members, denied = r.GetDatas(), mtproto.ErrChannelPrivate
	default:
		return nil, mtproto.ErrPeerIdInvalid
	}
	if !slices.Contains(members, c.MD.UserId) {
		return nil, denied
	}
	members, err := c.shownMembers(peer, members)
	if err != nil {
		c.Logger.Errorf("messages.getOnlines - error: %v", err)
		return nil, err
	}
	seen, err := c.svcCtx.Dao.UserClient.UserGetLastSeens(c.ctx, &userpb.TLUserGetLastSeens{Id: members})
	if err != nil {
		c.Logger.Errorf("messages.getOnlines - error: %v", err)
		return nil, err
	}
	return mtproto.MakeTLChatOnlines(&mtproto.ChatOnlines{
		Onlines: countOnline(seen.GetDatas(), time.Now().Unix()),
	}).To_ChatOnlines(), nil
}

// shownMembers are members without the hidden ones but the viewer itself
// (ADR 0048), and for a subscriber of the direct messages of a channel the
// subscriber alone.
func (c *DialogsCore) shownMembers(peer *mtproto.PeerUtil, members []int64) ([]int64, error) {
	hidden := map[int64]bool{}
	if peer.IsChannel() {
		mc, err := c.svcCtx.Dao.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: peer.PeerId, Id: members})
		if err != nil {
			return nil, err
		}
		if me, _ := mc.GetParticipant(c.MD.UserId); mc.GetChannel().GetMonoforum() && !channelpb.SublistAdmin(me) {
			// A subscriber of the direct messages of a channel is
			// alone in its chat with the channel (ADR 0055): who
			// else writes to it, and who answers, is not its to
			// count. Android does not poll a monoforum
			// (MessagesController.java:16588).
			return []int64{c.MD.UserId}, nil
		}
		for _, p := range mc.GetParticipants() {
			if p.GetHidden() {
				hidden[p.GetUserId()] = true
			}
		}
	} else {
		chat, err := c.svcCtx.Dao.ChatClient.ChatGetMutableChat(c.ctx, &chatpb.TLChatGetMutableChat{ChatId: peer.PeerId})
		if err != nil {
			return nil, err
		}
		for _, p := range chat.GetChatParticipants() {
			if p.GetHidden() {
				hidden[p.GetUserId()] = true
			}
		}
	}
	out := make([]int64, 0, len(members))
	for _, id := range members {
		if !hidden[id] || id == c.MD.UserId {
			out = append(out, id)
		}
	}
	return out, nil
}

// countOnline counts those still within their online window (lastSeenAt is
// its end while it lasts, user.MakeUserStatus).
func countOnline(seen []*userpb.LastSeenData, now int64) int32 {
	var n int32
	for _, s := range seen {
		if s.GetLastSeenAt() > now {
			n++
		}
	}
	return n
}
