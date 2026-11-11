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
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// ChannelsGetSendAs
// channels.getSendAs#e785a43f flags:# for_paid_reactions:flags.0?true for_live_stories:flags.1?true peer:InputPeer = channels.SendAsPeers;
//
// The peers the account may write to a supergroup as (task 079,
// pkg/sendas), the default first, with their chats and users. Paid
// reactions and live stories are not served: their lists are empty, which
// the clients read as no choice (tdesktop SendAsPeers::request,
// send_as_peers.cpp:182-191). Premium is open to all (ADR 0041), so no
// peer is premium_required.
func (c *MessagesCore) ChannelsGetSendAs(in *mtproto.TLChannelsGetSendAs) (*mtproto.Channels_SendAsPeers, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.GetPeer())
	if !peer.IsChannel() {
		c.Logger.Errorf("channels.getSendAs - invalid peer: %v", in.GetPeer())
		return nil, mtproto.ErrPeerIdInvalid
	}
	r := &mtproto.Channels_SendAsPeers{
		Peers_VECTORPEER:       []*mtproto.Peer{},
		Peers_VECTORSENDASPEER: []*mtproto.SendAsPeer{},
		Chats:                  []*mtproto.Chat{},
		Users:                  []*mtproto.User{},
	}
	if in.GetForPaidReactions() || in.GetForLiveStories() {
		return mtproto.MakeTLChannelsSendAsPeers(r).To_Channels_SendAsPeers(), nil
	}
	peers, err := c.svcCtx.SendAs.Peers(c.ctx, c.MD.UserId, peer.PeerId)
	if err != nil {
		c.Logger.Errorf("channels.getSendAs - error: %v", err)
		return nil, err
	}
	h := mtproto.NewIDListHelper()
	for _, p := range peers {
		h.PickByPeer(p)
		r.Peers_VECTORPEER = append(r.Peers_VECTORPEER, p)
		r.Peers_VECTORSENDASPEER = append(r.Peers_VECTORSENDASPEER, mtproto.MakeTLSendAsPeer(&mtproto.SendAsPeer{Peer: p}).To_SendAsPeer())
	}
	if len(h.UserIdList) > 0 {
		users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsers(c.ctx, &userpb.TLUserGetMutableUsers{Id: h.UserIdList, To: []int64{c.MD.UserId}})
		if err != nil {
			c.Logger.Errorf("channels.getSendAs - error: %v", err)
			return nil, err
		}
		r.Users = users.GetUserListByIdList(c.MD.UserId, h.UserIdList...)
	}
	if len(h.ChannelIdList) > 0 {
		channels, err := c.svcCtx.Dao.Channel.ChannelGetChannelListByIdList(c.ctx, &channelpb.TLChannelGetChannelListByIdList{SelfId: c.MD.UserId, IdList: h.ChannelIdList})
		if err != nil {
			c.Logger.Errorf("channels.getSendAs - error: %v", err)
			return nil, err
		}
		r.Chats = channels.GetChannelListByIdList(c.MD.UserId)
	}
	return mtproto.MakeTLChannelsSendAsPeers(r).To_Channels_SendAsPeers(), nil
}
