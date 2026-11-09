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
)

// AccountGetNotifySettings
// account.getNotifySettings#12b3ad31 peer:InputNotifyPeer = PeerNotifySettings;
func (c *NotificationCore) AccountGetNotifySettings(in *mtproto.TLAccountGetNotifySettings) (*mtproto.PeerNotifySettings, error) {
	var (
		err      error
		settings *mtproto.PeerNotifySettings
	)

	// The settings of one topic of a forum (tdesktop apiwrap.cpp:1975).
	if in.GetPeer().GetPredicateName() == mtproto.Predicate_inputNotifyForumTopic {
		channelId, err := c.forumOf(in.GetPeer())
		if err != nil {
			c.Logger.Errorf("account.getNotifySettings - error: %v", err)
			return nil, err
		}
		settings, err := c.svcCtx.Dao.UserClient.UserGetTopicNotifySettings(c.ctx, &userpb.TLUserGetTopicNotifySettings{
			UserId:    c.MD.UserId,
			ChannelId: channelId,
			TopMsgId:  in.GetPeer().GetTopMsgId(),
		})
		if err != nil {
			c.Logger.Errorf("account.getNotifySettings - error: %v", err)
			return nil, err
		}
		return settings, nil
	}

	peer := mtproto.FromInputNotifyPeer(c.MD.UserId, in.Peer)
	switch peer.PeerType {
	case mtproto.PEER_USER:
		// TODO(@benqi): check peerUser Exists
	case mtproto.PEER_CHAT:
		// TODO(@benqi): check peerChat exists
	case mtproto.PEER_CHANNEL:
		// TODO(@benqi): check peerChannel exists
	case mtproto.PEER_USERS:
	case mtproto.PEER_CHATS:
	case mtproto.PEER_BROADCASTS:
	default:
		err = mtproto.ErrPeerIdInvalid
		c.Logger.Errorf("account.updateNotifySettings - error: %v", err)
		return nil, err
	}

	settings, err = c.svcCtx.Dao.UserClient.UserGetNotifySettings(c.ctx, &userpb.TLUserGetNotifySettings{
		UserId:   c.MD.UserId,
		PeerType: peer.PeerType,
		PeerId:   peer.PeerId,
	})
	if err != nil {
		c.Logger.Errorf("getNotifySettings error - %v", err)
		return nil, err
	}

	return settings, err
}

// forumOf is the channel of an inputNotifyForumTopic, one the account may
// read: a peer that is not one is PEER_ID_INVALID.
func (c *NotificationCore) forumOf(in *mtproto.InputNotifyPeer) (int64, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.GetPeer())
	if peer.PeerType != mtproto.PEER_CHANNEL || c.svcCtx.Plugin == nil {
		return 0, mtproto.ErrPeerIdInvalid
	}
	channel, err := c.svcCtx.Plugin.GetChannelById(c.ctx, c.MD.UserId, peer.PeerId)
	if err != nil {
		return 0, err
	}
	if channel.GetPredicateName() == mtproto.Predicate_channelForbidden {
		return 0, mtproto.ErrChannelPrivate
	}
	return peer.PeerId, nil
}
