// Copyright 2024 Teamgram Authors
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
	"cmp"
	"slices"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// DialogReorderPinnedSavedDialogs
// dialog.reorderPinnedSavedDialogs user_id:long force:Bool order:Vector<PeerUtil> = Bool;
//
// One box transaction of the user: the pinned saved dialogs are read, then
// each row changes by its key, in key order.
func (c *DialogCore) DialogReorderPinnedSavedDialogs(in *dialog.TLDialogReorderPinnedSavedDialogs) (*mtproto.Bool, error) {
	type peerKey struct {
		PeerType int32 `db:"peer_type"`
		PeerId   int64 `db:"peer_id"`
	}
	userId := in.GetUserId()
	err := c.svcCtx.Dao.RunBox(c.ctx, userId, nil, func(b *userbox.Box) error {
		values := map[peerKey]int64{}
		if mtproto.FromBool(in.GetForce()) {
			var pinned []peerKey
			if err := b.Tx().QueryRowsPartial(&pinned,
				"SELECT peer_type, peer_id FROM saved_dialogs WHERE user_id = ? AND pinned > 0 AND deleted = 0", userId); err != nil {
				return err
			}
			for _, k := range pinned {
				values[k] = 0
			}
		}
		order := time.Now().Unix()
		for _, p := range in.GetOrder() {
			values[peerKey{PeerType: p.GetPeerType(), PeerId: p.GetPeerId()}] = order << 32
			order--
		}
		keys := make([]peerKey, 0, len(values))
		for k := range values {
			keys = append(keys, k)
		}
		slices.SortFunc(keys, func(a, b peerKey) int {
			return cmp.Or(cmp.Compare(a.PeerType, b.PeerType), cmp.Compare(a.PeerId, b.PeerId))
		})
		for _, k := range keys {
			if _, err := c.svcCtx.Dao.SavedDialogsDAO.UpdateUserPeerPinnedTx(b.Tx(), values[k], userId, k.PeerType, k.PeerId); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		c.Logger.Errorf("dialog.reorderPinnedSavedDialogs - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}
