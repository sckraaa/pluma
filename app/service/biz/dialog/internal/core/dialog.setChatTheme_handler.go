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
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// DialogSetChatTheme
// dialog.setChatTheme user_id:long peer_type:int peer_id:long theme_emoticon:string = Bool;
//
// In a private chat both users' dialog rows change, each in its box
// transaction, by ascending user id; a repeated call gives the same rows.
func (c *DialogCore) DialogSetChatTheme(in *dialog.TLDialogSetChatTheme) (*mtproto.Bool, error) {
	if err := c.setBothDialogs(in.UserId, in.PeerType, in.PeerId, "theme_emoticon", in.ThemeEmoticon); err != nil {
		c.Logger.Errorf("dialog.setChatTheme - error: %v", err)
		return nil, err
	}
	return mtproto.BoolTrue, nil
}

// setBothDialogs sets a column of userId's dialog with peer and, in a
// private chat with another user, of the peer's dialog with userId: two box
// transactions, by ascending user id.
func (c *DialogCore) setBothDialogs(userId int64, peerType int32, peerId int64, column string, value any) error {
	type row struct {
		userId, peerId int64
	}
	rows := []row{{userId, peerId}}
	if peerType == mtproto.PEER_USER && peerId != userId {
		rows = append(rows, row{peerId, userId})
		if peerId < userId {
			rows[0], rows[1] = rows[1], rows[0]
		}
	}
	for _, r := range rows {
		key := dialog.GetDialogCacheKeyByPeer(r.userId, peerType, r.peerId)
		err := c.svcCtx.Dao.RunBox(c.ctx, r.userId, []string{key}, func(b *userbox.Box) error {
			_, err := c.svcCtx.Dao.DialogsDAO.UpdateCustomMapTx(b.Tx(), map[string]any{column: value}, r.userId, peerType, r.peerId)
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}
