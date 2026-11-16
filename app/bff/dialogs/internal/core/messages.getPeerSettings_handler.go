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
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/env2"
)

// MessagesGetPeerSettings
// messages.getPeerSettings#efd9a6a2 peer:InputPeer = messages.PeerSettings;
//
// Only users ask it (core.telegram.org/method/messages.getPeerSettings:
// "Only users can use this method"); a bot gets BOT_METHOD_INVALID.
func (c *DialogsCore) MessagesGetPeerSettings(in *mtproto.TLMessagesGetPeerSettings) (*mtproto.Messages_PeerSettings, error) {
	if c.MD.IsBot {
		err := mtproto.ErrBotMethodInvalid
		c.Logger.Errorf("messages.getPeerSettings - error: %v", err)
		return nil, err
	}
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.Peer)

	peerSettings, err := c.svcCtx.UserClient.UserGetPeerSettings(c.ctx, &userpb.TLUserGetPeerSettings{
		UserId:   c.MD.UserId,
		PeerType: peer.PeerType,
		PeerId:   peer.PeerId,
	})

	if err != nil {
		c.Logger.Errorf("messages.getPeerSettings - error: %v", err)

		// TODO(@benqi): handle error
		peerSettings = mtproto.MakeTLPeerSettings(&mtproto.PeerSettings{
			ReportSpam:             false,
			AddContact:             false,
			BlockContact:           false,
			ShareContact:           false,
			NeedContactsException:  false,
			ReportGeo:              false,
			Autoarchived:           false,
			InviteMembers:          false,
			RequestChatBroadcast:   false,
			BusinessBotPaused:      false,
			BusinessBotCanReply:    false,
			GeoDistance:            nil,
			RequestChatTitle:       nil,
			RequestChatDate:        nil,
			BusinessBotId:          nil,
			BusinessBotManageUrl:   nil,
			ChargePaidMessageStars: nil,
			RegistrationMonth:      nil,
			PhoneCountry:           nil,
			NameChangeDate:         nil,
			PhotoChangeDate:        nil,
		}).To_PeerSettings()
	}

	users := []*mtproto.User{}
	// The bar of the connected bot of Telegram Business in the private
	// chat (task 105): business_bot_id, business_bot_manage_url,
	// business_bot_paused and business_bot_can_reply
	// (core.telegram.org/constructor/peerSettings). tdesktop reads it here
	// when it opens the chat (history/view/history_view_contact_status.cpp,
	// BusinessBotStatus::setupState); the bot is in users, so that the
	// client knows the user the bar shows.
	if peer.PeerType == mtproto.PEER_USER {
		bar, err := c.svcCtx.Business.PeerBar(c.ctx, c.svcCtx.UserClient, env2.TDotMe, c.MD.UserId, peer.PeerId)
		if err != nil {
			c.Logger.Errorf("messages.getPeerSettings - the connected bot: %v", err)
			return nil, err
		}
		if bar != nil {
			peerSettings = bar.Apply(peerSettings)
			bots, err := c.svcCtx.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{
				Id:      []int64{c.MD.UserId, bar.BotID},
				Privacy: true,
				HasTo:   true,
				To:      []int64{c.MD.UserId},
			})
			if err != nil {
				c.Logger.Errorf("messages.getPeerSettings - the connected bot: %v", err)
				return nil, err
			}
			users = bots.GetUserListByIdList(c.MD.UserId, bar.BotID)
		}
	}

	return mtproto.MakeTLMessagesPeerSettings(&mtproto.Messages_PeerSettings{
		Settings: peerSettings,
		Chats:    []*mtproto.Chat{},
		Users:    users,
	}).To_Messages_PeerSettings(), nil
}
