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

	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// DeleteDialog deletes userId's dialog row with a peer, if there is one.
// The row holds columns of the user's box (top_message, read_inbox_max_id,
// the unread counters): it is deleted in a box transaction (ADR 0011,
// p. 2), ordered with the box's other writes. A pinned dialog is unpinned
// on every device of the account by updateDialogPinned with seq in the same
// transaction (dialog.PinOfDeleted, ADR 0020); the push carries no users or
// chats, as msg's deletion of a dialog row does: no client needs them for
// the update.
func (d *Dao) DeleteDialog(ctx context.Context, userId int64, peerType int32, peerId int64) error {
	return d.RunBox(ctx, userId, []string{dialog.GetDialogCacheKeyByPeer(userId, peerType, peerId)}, func(b *userbox.Box) error {
		var pins []dialog.PinOfDeleted
		if err := b.Tx().QueryRowsPartial(&pins, dialog.PinOfDeletedQuery, userId, peerType, peerId); err != nil {
			return err
		}
		if len(pins) == 0 {
			return nil
		}
		n, err := d.DialogsDAO.DeleteTx(b.Tx(), userId, peerType, peerId)
		if err != nil || n == 0 {
			return err
		}
		b.InvalidateCache(dialog.DeleteCacheKeys(userId, peerType, peerId)...)
		if u := pins[0].Unpin(peerType, peerId); u != nil {
			return d.EmitSeqUpdate(b, u, nil, nil)
		}
		return nil
	})
}
