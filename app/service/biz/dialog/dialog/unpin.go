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

package dialog

import (
	"github.com/teamgram/proto/mtproto"
)

// PinOfDeletedQuery reads, by user_id, peer_type and peer_id, what a
// deletion of a dialogs row needs to know of its pin: its folder, the pin
// columns and whether the row was a dialog at all. It locks the row, so the
// pin read is the pin the deletion removes.
const PinOfDeletedQuery = "SELECT folder_id, pinned, folder_pinned, deleted FROM dialogs WHERE user_id = ? AND peer_type = ? AND peer_id = ? FOR UPDATE"

// PinOfDeleted is a dialogs row as PinOfDeletedQuery reads it.
type PinOfDeleted struct {
	FolderId     int32 `db:"folder_id"`
	Pinned       int64 `db:"pinned"`
	FolderPinned int64 `db:"folder_pinned"`
	Deleted      bool  `db:"deleted"`
}

// Unpin is the update with seq that tells every device of the account its
// dialog with peerType/peerId, whose row goes, is not pinned any more
// (ADR 0020): updateDialogPinned with pinned false in the dialog's folder.
// It is nil when the row did not show a pinned dialog. A dialog is pinned
// by the column of its folder, as dialogs are read: pinned in the main list
// (folder 0), folder_pinned in the archive (folder 1); the other column is
// a pin kept from before the dialog moved and is not shown.
//
// A client keeps a pinned dialog in its list after its messages go, so
// without the update a dialog deleted on one device would stay pinned on
// the others.
func (p PinOfDeleted) Unpin(peerType int32, peerId int64) *mtproto.Update {
	pin := p.Pinned
	if p.FolderId != 0 {
		pin = p.FolderPinned
	}
	if p.Deleted || pin <= 0 {
		return nil
	}
	return mtproto.MakeTLUpdateDialogPinned(&mtproto.Update{
		Pinned:   false,
		FolderId: mtproto.MakeFlagsInt32(p.FolderId),
		Peer_DIALOGPEER: mtproto.MakeTLDialogPeer(&mtproto.DialogPeer{
			Peer: mtproto.MakePeer(peerType, peerId),
		}).To_DialogPeer(),
	}).To_Update()
}
