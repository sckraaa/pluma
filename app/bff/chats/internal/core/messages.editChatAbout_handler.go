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
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
)

// channelAboutMax is the longest description of a channel, in characters,
// as the clients enforce it (Android ChannelCreateActivity: 255).
const channelAboutMax = 255

// MessagesEditChatAbout
// messages.editChatAbout#def60797 peer:InputPeer about:string = Bool;
func (c *ChatsCore) MessagesEditChatAbout(in *mtproto.TLMessagesEditChatAbout) (*mtproto.Bool, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.Peer)

	switch peer.PeerType {
	case mtproto.PEER_CHAT:
		_, err := c.svcCtx.Dao.ChatClient.Client().ChatEditChatAbout(c.ctx, &chatpb.TLChatEditChatAbout{
			ChatId:     peer.PeerId,
			EditUserId: c.MD.UserId,
			About:      in.About,
		})
		if err != nil {
			c.Logger.Errorf("messages.editChatAbout - error: %v", err)
			return nil, err
		}
	case mtproto.PEER_CHANNEL:
		// A channel's description (task 044): biz channel checks the
		// rights and tells the members.
		if utf8.RuneCountInString(in.About) > channelAboutMax {
			return nil, mtproto.ErrChatAboutTooLong
		}
		mc, err := c.svcCtx.Dao.Channel.ChannelGetMutableChannel(c.ctx, &channelpb.TLChannelGetMutableChannel{ChannelId: peer.PeerId, Id: []int64{c.MD.UserId}})
		if err != nil {
			return nil, err
		}
		if mc.GetChannel().GetAccessHash() != peer.AccessHash {
			return nil, mtproto.ErrChannelInvalid
		}
		return c.svcCtx.Dao.Channel.ChannelEditAbout(c.ctx, &channelpb.TLChannelEditAbout{
			SelfId:        c.MD.UserId,
			PermAuthKeyId: c.MD.PermAuthKeyId,
			ChannelId:     peer.PeerId,
			About:         in.About,
		})
	default:
		err := mtproto.ErrPeerIdInvalid
		c.Logger.Errorf("invalid peer type: {%v}")
		return nil, err
	}

	return mtproto.BoolTrue, nil
}
