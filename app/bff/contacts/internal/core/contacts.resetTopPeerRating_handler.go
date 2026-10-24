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

// ContactsResetTopPeerRating
// contacts.resetTopPeerRating#1ae373ac category:TopPeerCategory peer:InputPeer = Bool;
//
// The user removes a peer from the suggestions of a category: its rating
// there starts again from zero (user.resetTopPeerRating).
func (c *ContactsCore) ContactsResetTopPeerRating(in *mtproto.TLContactsResetTopPeerRating) (*mtproto.Bool, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.GetPeer())
	switch {
	case in.GetCategory() == nil:
		err := mtproto.ErrInputConstructorInvalid
		c.Logger.Errorf("contacts.resetTopPeerRating - error: %v", err)
		return nil, err
	case peer.PeerType == mtproto.PEER_SELF:
		peer.PeerType = mtproto.PEER_USER
	case peer.PeerType != mtproto.PEER_USER && peer.PeerType != mtproto.PEER_CHAT && peer.PeerType != mtproto.PEER_CHANNEL:
		err := mtproto.ErrPeerIdInvalid
		c.Logger.Errorf("contacts.resetTopPeerRating - error: %v", err)
		return nil, err
	}

	if _, err := c.svcCtx.Dao.UserClient.UserResetTopPeerRating(c.ctx, &userpb.TLUserResetTopPeerRating{
		UserId:   c.MD.UserId,
		Category: in.GetCategory(),
		PeerType: peer.PeerType,
		PeerId:   peer.PeerId,
	}); err != nil {
		c.Logger.Errorf("contacts.resetTopPeerRating - error: %v", err)
		return nil, err
	}

	return mtproto.BoolTrue, nil
}
