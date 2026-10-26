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

package dao

import (
	"context"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// MarkDialogUnread sets the unread mark of userId's dialog with a peer
// (PEER_SELF is the dialog with itself, stored as the private one) in one
// box transaction of userId, with updateDialogUnreadMark for every device of
// the account (ADR 0020); users and chats are the peer as userId sees it. A
// dialog the user does not have answers PEER_ID_INVALID; a mark already as
// asked changes nothing.
func (d *Dao) MarkDialogUnread(ctx context.Context, userId int64, peerType int32, peerId int64, unread bool, users []*mtproto.User, chats []*mtproto.Chat) error {
	if peerType == mtproto.PEER_SELF {
		peerType, peerId = mtproto.PEER_USER, userId
	}
	key := dialog.GetDialogCacheKeyByPeer(userId, peerType, peerId)
	return d.RunBox(ctx, userId, []string{key}, func(b *userbox.Box) error {
		var marks []bool
		if err := b.Tx().QueryRowsPartial(&marks,
			"SELECT unread_mark FROM dialogs WHERE user_id = ? AND peer_type = ? AND peer_id = ? AND deleted = 0 FOR UPDATE",
			userId, peerType, peerId); err != nil {
			return err
		}
		switch {
		case len(marks) == 0:
			return mtproto.ErrPeerIdInvalid
		case marks[0] == unread:
			return nil
		}
		if _, err := d.DialogsDAO.UpdateCustomMapTx(b.Tx(), map[string]interface{}{"unread_mark": unread}, userId, peerType, peerId); err != nil {
			return err
		}
		return d.EmitSeqUpdate(b, mtproto.MakeTLUpdateDialogUnreadMark(&mtproto.Update{
			Unread: unread,
			Peer_DIALOGPEER: mtproto.MakeTLDialogPeer(&mtproto.DialogPeer{
				Peer: mtproto.MakePeer(peerType, peerId),
			}).To_DialogPeer(),
		}).To_Update(), users, chats)
	})
}
