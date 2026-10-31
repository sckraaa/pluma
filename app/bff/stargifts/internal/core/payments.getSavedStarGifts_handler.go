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
	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"google.golang.org/grpc/status"
)

// No gift collection exists, so any collection_id is one the server does
// not know (core.telegram.org/method/payments.getSavedStarGifts).
var errCollectionIdInvalid = status.Error(mtproto.ErrBadRequest, "COLLECTION_ID_INVALID")

// PaymentsGetSavedStarGifts
// payments.getSavedStarGifts#a319e569 flags:# exclude_unsaved:flags.0?true exclude_saved:flags.1?true exclude_unlimited:flags.2?true exclude_unique:flags.4?true sort_by_value:flags.5?true exclude_upgradable:flags.7?true exclude_unupgradable:flags.8?true peer_color_available:flags.9?true exclude_hosted:flags.10?true peer:InputPeer collection_id:flags.6?int offset:string limit:int = payments.SavedStarGifts;
//
// The gifts of a user: none, since Pluma has no payments (ADR 0016). The
// answer is Telegram's for a user without gifts (payments.savedStarGifts
// with count 0 and no flags, read from Telegram for an account without
// gifts). Clients ask for it on every profile: Web K opens the profile of
// Saved Messages, the list of its sub-chats, only after this answer
// (appSearchSuper.loadFirstTime awaits appGiftsManager.getPinnedGifts).
// Users and channels have gifts; channels are not served. The method's
// page names no basic groups among its peers, so a basic group is
// PEER_ID_INVALID (not checked against Telegram: the reference account has
// no basic group).
func (c *StarGiftsCore) PaymentsGetSavedStarGifts(in *mtproto.TLPaymentsGetSavedStarGifts) (*mtproto.Payments_SavedStarGifts, error) {
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.GetPeer())
	switch peer.PeerType {
	case mtproto.PEER_SELF:
	case mtproto.PEER_USER:
		users, err := c.svcCtx.Dao.UserClient.UserGetMutableUsersV2(c.ctx, &userpb.TLUserGetMutableUsersV2{Id: []int64{peer.PeerId}})
		if err != nil {
			c.Logger.Errorf("payments.getSavedStarGifts - error: %v", err)
			return nil, err
		}
		if !users.CheckExistUser(peer.PeerId) {
			err = mtproto.ErrPeerIdInvalid
			c.Logger.Errorf("payments.getSavedStarGifts - user %d: %v", peer.PeerId, err)
			return nil, err
		}
	case mtproto.PEER_CHANNEL:
		c.Logger.Errorf("payments.getSavedStarGifts blocked, License key from https://teamgram.net required to unlock enterprise features.")
		return nil, mtproto.ErrEnterpriseIsBlocked
	default:
		err := mtproto.ErrPeerIdInvalid
		c.Logger.Errorf("payments.getSavedStarGifts - error: %v", err)
		return nil, err
	}
	if in.GetCollectionId() != nil {
		err := errCollectionIdInvalid
		c.Logger.Errorf("payments.getSavedStarGifts - error: %v", err)
		return nil, err
	}

	return mtproto.MakeTLPaymentsSavedStarGifts(&mtproto.Payments_SavedStarGifts{
		Count: 0,
		Gifts: []*mtproto.SavedStarGift{},
		Chats: []*mtproto.Chat{},
		Users: []*mtproto.User{},
	}).To_Payments_SavedStarGifts(), nil
}
