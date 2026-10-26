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
	"cmp"
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// Pins of dialogs and of Saved Messages (ADR 0020). A change of the pins and
// its update with seq for every device of the account, the one that made it
// included, are one box transaction of the user; the update is built from
// what the transaction stores. A request that leaves the pins as they are
// writes and pushes nothing.
//
// A pin is a value of the dialog's pin column, the higher the closer to the
// top; 0 is not pinned. Its high 32 bits are the unix time of the pin, its
// low bits order pins made in the same second.

// pinColumn returns the pin column of the dialogs of a folder and the
// condition that selects them: pinned for the main list (folder 0),
// folder_pinned for the archive (folder 1).
func pinColumn(folderId int32) (column, folder string) {
	if folderId != 0 {
		return "folder_pinned", "folder_id = 1"
	}
	return "pinned", "folder_id = 0"
}

// pinListKeys are the cache keys of the lists of a folder's dialogs, pinned
// and not, which a change of a pin there moves a dialog between.
func pinListKeys(userId int64, folderId int32) []string {
	if folderId != 0 {
		return []string{dialog.GetFolderPinnedDialogIdListCacheKey(userId), dialog.GetFolderNotPinnedDialogIdListCacheKey(userId)}
	}
	return []string{dialog.GetPinnedDialogIdListCacheKey(userId), dialog.GetNotPinnedDialogIdListCacheKey(userId)}
}

// topPin is the lowest pin above top, the highest pin there is, made by the
// transaction: a pin made in the same second as another is still above it.
func topPin(b *userbox.Box, top int64) int64 {
	return max(b.Date()<<32, top+1)
}

// pinnedPeer is a dialog, or a dialog of Saved Messages, with its pin.
type pinnedPeer struct {
	PeerType int32 `db:"peer_type"`
	PeerId   int64 `db:"peer_id"`
	Pin      int64 `db:"pin"`
}

func (p pinnedPeer) dialogId() int64 { return mtproto.MakePeerDialogId(p.PeerType, p.PeerId) }

// pinOrder returns the peers that are pinned with the pins given, from the
// top: by pin, and by peer among equal pins, as nothing else orders them.
func pinOrder(peers []pinnedPeer, pin func(pinnedPeer) int64) []pinnedPeer {
	order := make([]pinnedPeer, 0, len(peers))
	for _, p := range peers {
		if v := pin(p); v > 0 {
			p.Pin = v
			order = append(order, p)
		}
	}
	slices.SortFunc(order, func(a, b pinnedPeer) int {
		return cmp.Or(cmp.Compare(b.Pin, a.Pin), cmp.Compare(a.PeerType, b.PeerType), cmp.Compare(a.PeerId, b.PeerId))
	})
	return order
}

func samePins(a, b []pinnedPeer) bool {
	return slices.EqualFunc(a, b, func(x, y pinnedPeer) bool { return x.PeerType == y.PeerType && x.PeerId == y.PeerId })
}

// pinDialogPeers is an order of pins as the updates carry it. An empty one
// reaches the client without the order (protobuf and JSON drop an empty
// list), and the client then fetches the pinned dialogs: none.
func pinDialogPeers(order []pinnedPeer) []*mtproto.DialogPeer {
	peers := make([]*mtproto.DialogPeer, 0, len(order))
	for _, p := range order {
		peers = append(peers, pinDialogPeer(p.PeerType, p.PeerId))
	}
	return peers
}

func pinDialogPeer(peerType int32, peerId int64) *mtproto.DialogPeer {
	return mtproto.MakeTLDialogPeer(&mtproto.DialogPeer{Peer: mtproto.MakePeer(peerType, peerId)}).To_DialogPeer()
}

// newPins returns the pins a reorder gives. The peers of order go on top,
// in that order, above every pin that stays (topPin); a peer named twice
// keeps its first place. With force every other pinned peer of current
// (the pins there now) is unpinned, without it stays where it is.
func newPins[K comparable](b *userbox.Box, force bool, current map[K]int64, order []K) map[K]int64 {
	var (
		pins  = make(map[K]int64, len(current)+len(order))
		named = make([]K, 0, len(order))
		seen  = make(map[K]bool, len(order))
	)
	for _, k := range order {
		if !seen[k] {
			seen[k] = true
			named = append(named, k)
		}
	}
	var top int64
	for k, pin := range current {
		switch {
		case seen[k]:
		case force:
			pins[k] = 0
		default:
			top = max(top, pin)
		}
	}
	base := topPin(b, top)
	for i, k := range named {
		pins[k] = base + int64(len(named)-1-i)
	}
	return pins
}

// ToggleDialogPin pins or unpins userId's dialog with peer in the folder the
// dialog is in and pushes updateDialogPinned with seq; users and chats are
// the peer as userId sees it, for the push. A pin goes on top of the
// folder's pins. A dialog already pinned, or not pinned, as asked is left as
// it is. It returns the folder of the dialog, and mtproto.ErrPeerIdInvalid
// when userId has no dialog with peer.
func (d *Dao) ToggleDialogPin(ctx context.Context, userId int64, peerType int32, peerId int64, pinned bool, users []*mtproto.User, chats []*mtproto.Chat) (int32, error) {
	var (
		peerDialogId = mtproto.MakePeerDialogId(peerType, peerId)
		folderId     int32
	)
	err := d.RunBox(ctx, userId, []string{dialog.GetDialogCacheKey(userId, peerDialogId)}, func(b *userbox.Box) error {
		var row struct {
			PeerType     int32 `db:"peer_type"`
			PeerId       int64 `db:"peer_id"`
			FolderId     int32 `db:"folder_id"`
			Pinned       int64 `db:"pinned"`
			FolderPinned int64 `db:"folder_pinned"`
		}
		err := b.Tx().QueryRowPartial(&row,
			"SELECT peer_type, peer_id, folder_id, pinned, folder_pinned FROM dialogs WHERE user_id = ? AND peer_dialog_id = ? AND deleted = 0 FOR UPDATE",
			userId, peerDialogId)
		if errors.Is(err, sql.ErrNoRows) {
			return mtproto.ErrPeerIdInvalid
		} else if err != nil {
			return err
		}
		folderId = row.FolderId
		column, folder := pinColumn(folderId)
		current := row.Pinned
		if folderId != 0 {
			current = row.FolderPinned
		}
		if (current > 0) == pinned {
			return nil
		}

		var pin int64
		if pinned {
			var top int64
			if err = b.Tx().QueryRow(&top, "SELECT COALESCE(MAX("+column+"), 0) FROM dialogs WHERE user_id = ? AND "+folder, userId); err != nil {
				return err
			}
			pin = topPin(b, top)
		}
		if _, err = b.Tx().Exec("UPDATE dialogs SET "+column+" = ? WHERE user_id = ? AND peer_dialog_id = ?", pin, userId, peerDialogId); err != nil {
			return err
		}
		b.InvalidateCache(pinListKeys(userId, folderId)...)
		return d.EmitSeqUpdate(b, mtproto.MakeTLUpdateDialogPinned(&mtproto.Update{
			Pinned:          pinned,
			FolderId:        mtproto.MakeFlagsInt32(folderId),
			Peer_DIALOGPEER: pinDialogPeer(row.PeerType, row.PeerId),
		}).To_Update(), users, chats)
	})
	return folderId, err
}

// ReorderPinnedDialogs pins userId's dialogs of idList (peer dialog ids) in
// a folder, in that order from the top; with force the other dialogs of the
// folder are unpinned. A dialog of idList that is not in the folder is left
// out. It pushes updatePinnedDialogs with seq with the order of the folder's
// pins as stored after the change; users and chats are the peers of idList
// as userId sees them, for the push. An order that leaves the stored one as
// it is changes nothing.
func (d *Dao) ReorderPinnedDialogs(ctx context.Context, userId int64, force bool, folderId int32, idList []int64, users []*mtproto.User, chats []*mtproto.Chat) error {
	if folderId != 0 {
		folderId = 1
	}
	column, folder := pinColumn(folderId)
	keys := make([]string, 0, len(idList))
	for _, id := range idList {
		keys = append(keys, dialog.GetDialogCacheKey(userId, id))
	}
	return d.RunBox(ctx, userId, keys, func(b *userbox.Box) error {
		named := ""
		if len(idList) > 0 {
			named = " OR peer_dialog_id IN (" + sqlx.InInt64List(idList) + ")"
		}
		var rows []pinnedPeer
		if err := b.Tx().QueryRowsPartial(&rows,
			"SELECT peer_type, peer_id, "+column+" AS pin FROM dialogs WHERE user_id = ? AND "+folder+" AND deleted = 0 AND ("+column+" > 0"+named+") FOR UPDATE",
			userId); err != nil {
			return err
		}
		current := make(map[int64]int64, len(rows))
		for _, r := range rows {
			if r.Pin > 0 {
				current[r.dialogId()] = r.Pin
			}
		}
		pins := newPins(b, force, current, idList)
		pinOf := func(p pinnedPeer) int64 {
			if v, ok := pins[p.dialogId()]; ok {
				return v
			}
			return p.Pin
		}
		before := pinOrder(rows, func(p pinnedPeer) int64 { return p.Pin })
		after := pinOrder(rows, pinOf)
		if samePins(before, after) {
			return nil
		}

		// Each row changes by its key, in key order.
		slices.SortFunc(rows, func(a, b pinnedPeer) int { return cmp.Compare(a.dialogId(), b.dialogId()) })
		for _, r := range rows {
			if pinOf(r) == r.Pin {
				continue
			}
			if _, err := b.Tx().Exec("UPDATE dialogs SET "+column+" = ? WHERE user_id = ? AND "+folder+" AND peer_dialog_id = ?",
				pinOf(r), userId, r.dialogId()); err != nil {
				return err
			}
			b.InvalidateCache(dialog.GetDialogCacheKey(userId, r.dialogId()))
		}
		b.InvalidateCache(pinListKeys(userId, folderId)...)
		return d.EmitSeqUpdate(b, mtproto.MakeTLUpdatePinnedDialogs(&mtproto.Update{
			FolderId:                   mtproto.MakeFlagsInt32(folderId),
			Order_FLAGVECTORDIALOGPEER: pinDialogPeers(after),
		}).To_Update(), users, chats)
	})
}

// savedPinKey is the key of a dialog of Saved Messages: the owner's own notes
// are kept as the user, as msg writes them.
type savedPinKey struct {
	PeerType int32
	PeerId   int64
}

func savedPinKeyOf(userId int64, p *mtproto.PeerUtil) savedPinKey {
	if p.GetPeerType() == mtproto.PEER_SELF {
		return savedPinKey{PeerType: mtproto.PEER_USER, PeerId: userId}
	}
	return savedPinKey{PeerType: p.GetPeerType(), PeerId: p.GetPeerId()}
}

// ToggleSavedDialogPin pins or unpins userId's dialog of Saved Messages with
// peer and pushes updateSavedDialogPinned with seq; users and chats are the
// peer as userId sees it, for the push. A pin goes on top of the pins. A
// dialog that is not there, or is already pinned, or not pinned, as asked,
// is left as it is.
func (d *Dao) ToggleSavedDialogPin(ctx context.Context, userId int64, peer *mtproto.PeerUtil, pinned bool, users []*mtproto.User, chats []*mtproto.Chat) error {
	k := savedPinKeyOf(userId, peer)
	return d.RunBox(ctx, userId, nil, func(b *userbox.Box) error {
		var current int64
		err := b.Tx().QueryRow(&current,
			"SELECT pinned FROM saved_dialogs WHERE user_id = ? AND peer_type = ? AND peer_id = ? AND deleted = 0 FOR UPDATE",
			userId, k.PeerType, k.PeerId)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		} else if err != nil {
			return err
		}
		if (current > 0) == pinned {
			return nil
		}

		var pin int64
		if pinned {
			var top int64
			if err = b.Tx().QueryRow(&top, "SELECT COALESCE(MAX(pinned), 0) FROM saved_dialogs WHERE user_id = ?", userId); err != nil {
				return err
			}
			pin = topPin(b, top)
		}
		if _, err = d.SavedDialogsDAO.UpdateUserPeerPinnedTx(b.Tx(), pin, userId, k.PeerType, k.PeerId); err != nil {
			return err
		}
		return d.EmitSeqUpdate(b, mtproto.MakeTLUpdateSavedDialogPinned(&mtproto.Update{
			Pinned:          pinned,
			Peer_DIALOGPEER: pinDialogPeer(k.PeerType, k.PeerId),
		}).To_Update(), users, chats)
	})
}

// ReorderPinnedSavedDialogs pins userId's dialogs of Saved Messages of order
// in that order from the top; with force the others are unpinned. A peer
// without a dialog there is left out. It pushes updatePinnedSavedDialogs
// with seq with the order of the pins as stored after the change; users and
// chats are the peers of order as userId sees them, for the push. An order
// that leaves the stored one as it is changes nothing.
func (d *Dao) ReorderPinnedSavedDialogs(ctx context.Context, userId int64, force bool, order []*mtproto.PeerUtil, users []*mtproto.User, chats []*mtproto.Chat) error {
	keys := make([]savedPinKey, 0, len(order))
	for _, p := range order {
		keys = append(keys, savedPinKeyOf(userId, p))
	}
	return d.RunBox(ctx, userId, nil, func(b *userbox.Box) error {
		args := make([]any, 0, 1+2*len(keys))
		args = append(args, userId)
		for _, k := range keys {
			args = append(args, k.PeerType, k.PeerId)
		}
		named := strings.Repeat(" OR (peer_type = ? AND peer_id = ?)", len(keys))
		var rows []pinnedPeer
		if err := b.Tx().QueryRowsPartial(&rows,
			"SELECT peer_type, peer_id, pinned AS pin FROM saved_dialogs WHERE user_id = ? AND deleted = 0 AND (pinned > 0"+named+") FOR UPDATE",
			args...); err != nil {
			return err
		}
		key := func(p pinnedPeer) savedPinKey { return savedPinKey{PeerType: p.PeerType, PeerId: p.PeerId} }
		current := make(map[savedPinKey]int64, len(rows))
		for _, r := range rows {
			if r.Pin > 0 {
				current[key(r)] = r.Pin
			}
		}
		pins := newPins(b, force, current, keys)
		pinOf := func(p pinnedPeer) int64 {
			if v, ok := pins[key(p)]; ok {
				return v
			}
			return p.Pin
		}
		before := pinOrder(rows, func(p pinnedPeer) int64 { return p.Pin })
		after := pinOrder(rows, pinOf)
		if samePins(before, after) {
			return nil
		}

		// Each row changes by its key, in key order.
		slices.SortFunc(rows, func(a, b pinnedPeer) int {
			return cmp.Or(cmp.Compare(a.PeerType, b.PeerType), cmp.Compare(a.PeerId, b.PeerId))
		})
		for _, r := range rows {
			if pinOf(r) == r.Pin {
				continue
			}
			if _, err := d.SavedDialogsDAO.UpdateUserPeerPinnedTx(b.Tx(), pinOf(r), userId, r.PeerType, r.PeerId); err != nil {
				return err
			}
		}
		return d.EmitSeqUpdate(b, mtproto.MakeTLUpdatePinnedSavedDialogs(&mtproto.Update{
			Order_FLAGVECTORDIALOGPEER: pinDialogPeers(after),
		}).To_Update(), users, chats)
	})
}
