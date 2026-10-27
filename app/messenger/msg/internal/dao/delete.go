package dao

import (
	"slices"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// Deleted is what a deletion removed from a box.
type Deleted struct {
	// Rows are the deleted messages, by ascending id.
	Rows   []dataobject.MessagesDO
	Update *mtproto.Update // updateDeleteMessages
	// Left is the number of messages left when the deletion went in a
	// portion: the operation is to be repeated while it is above 0.
	Left int32
}

// Ids are the deleted message ids.
func (d *Deleted) Ids() []int32 {
	ids := make([]int32, len(d.Rows))
	for i, r := range d.Rows {
		ids[i] = r.UserMessageBoxId
	}
	return ids
}

// ByDialog groups the deleted messages' dialog_message_id by dialog, as the
// box owner sees it, in peer_dialog_id order.
func (d *Deleted) ByDialog(userId int64) []DeletedInDialog {
	var out []DeletedInDialog
	for _, r := range d.Rows {
		p := peerOfDialog(userId, mtproto.DialogID{A: r.DialogId1, B: r.DialogId2})
		i := slices.IndexFunc(out, func(x DeletedInDialog) bool { return x.PeerType == p.PeerType && x.PeerId == p.PeerId })
		if i < 0 {
			out = append(out, DeletedInDialog{PeerType: p.PeerType, PeerId: p.PeerId})
			i = len(out) - 1
		}
		out[i].DialogMessageIds = append(out[i].DialogMessageIds, r.DialogMessageId)
	}
	slices.SortFunc(out, func(a, b DeletedInDialog) int {
		return comparePeers(boxPeer{a.PeerType, a.PeerId}, boxPeer{b.PeerType, b.PeerId})
	})
	return out
}

// DeletedInDialog are the deleted messages of one dialog.
type DeletedInDialog struct {
	PeerType         int32
	PeerId           int64
	DialogMessageIds []int64
}

// deleteRows deletes messages read in the transaction: the rows, then the
// counters of their dialogs, then the journal row updateDeleteMessages with
// pts_count the number of messages. It returns nil when rows is empty.
func (d *Dao) deleteRows(b *userbox.Box, rows []dataobject.MessagesDO) (*Deleted, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	res := &Deleted{Rows: rows}
	ids := res.Ids()
	if err := d.deleteMessages(b, ids); err != nil {
		return nil, err
	}
	for _, c := range byDialog(b.UserId(), rows) {
		if err := recountDialog(b, c, true); err != nil {
			return nil, err
		}
	}
	res.Update = mtproto.MakeTLUpdateDeleteMessages(&mtproto.Update{Messages: ids}).To_Update()
	b.AddPtsUpdate(res.Update, int32(len(ids)))
	return res, nil
}

// DeleteMessages deletes b's owner's messages ids that are not deleted yet
// (read in the transaction). It returns nil when none is left to delete.
func (d *Dao) DeleteMessages(b *userbox.Box, ids []int32) (*Deleted, error) {
	rows, err := messagesByIds(b, ids)
	if err != nil {
		return nil, err
	}
	return d.deleteRows(b, rows)
}

// DeleteCopies deletes b's owner's copies of the messages dialogMessageIds,
// at most a portion. It returns nil when there is none.
func (d *Dao) DeleteCopies(b *userbox.Box, dialogMessageIds []int64) (*Deleted, error) {
	rows, err := messagesByDialogMessageIds(b, dialogMessageIds)
	if err != nil {
		return nil, err
	}
	left := len(rows) - maxPortion
	if left > 0 {
		rows = rows[:maxPortion]
	}
	res, err := d.deleteRows(b, rows)
	if res != nil && left > 0 {
		res.Left = int32(left)
	}
	return res, err
}

// DeleteDialogPortion deletes up to a portion of the newest messages of
// b's owner's dialog with peer. It returns nil when the dialog has no
// messages left.
func (d *Dao) DeleteDialogPortion(b *userbox.Box, peerType int32, peerId int64) (*Deleted, error) {
	peer := boxPeer{PeerType: peerType, PeerId: peerId}
	ids, err := dialogMessageIds(b, peer, maxPortion+1)
	if err != nil {
		return nil, err
	}
	more := len(ids) > maxPortion
	if more {
		ids = ids[:maxPortion]
	}
	res, err := d.DeleteMessages(b, ids)
	if err != nil || res == nil || !more {
		return res, err
	}
	res.Left, err = countDialogMessages(b, peer)
	return res, err
}

// DeletePhoneCallPortion deletes up to a portion of b's owner's newest
// phone call messages. It returns nil when there are none.
func (d *Dao) DeletePhoneCallPortion(b *userbox.Box) (*Deleted, error) {
	rows, err := phoneCallMessages(b, maxPortion+1)
	if err != nil {
		return nil, err
	}
	more := len(rows) > maxPortion
	if more {
		rows = rows[:maxPortion]
	}
	slices.SortFunc(rows, func(a, b dataobject.MessagesDO) int {
		return int(a.UserMessageBoxId - b.UserMessageBoxId)
	})
	res, err := d.deleteRows(b, rows)
	if err != nil || res == nil || !more {
		return res, err
	}
	res.Left, err = countMessages(b, "message_filter_type = ?", mtproto.MEDIA_PHONE_CALL)
	return res, err
}

// ClearHistoryResult is a portion of clearing a dialog's history.
type ClearHistoryResult struct {
	Deleted *Deleted
	// Updates journaled, in pts order: updateDeleteMessages, then
	// updateReadHistoryInbox and updateEditMessage when they changed
	// something.
	Updates []*mtproto.Update
	// Left is the number of messages left to delete.
	Left int32
	// DialogDeleted: the dialog row went with the last portion.
	DialogDeleted bool
	// Unpin unpins the deleted dialog when it was pinned, nil otherwise:
	// the caller journals and pushes it after the push of Updates
	// (UnpinDeleted).
	Unpin *mtproto.Update
}

// ClearHistory deletes a portion of b's owner's dialog with peer, newest
// first. With justClear, the top message stays and becomes the "history
// cleared" service message; without it every message goes, and with the
// last portion the dialog row too, in the same transaction, with the
// update that unpins it when it was pinned (Unpin). The dialog is read up
// to its top message. It returns nil when there is nothing to do.
func (d *Dao) ClearHistory(b *userbox.Box, peerType int32, peerId int64, justClear bool) (*ClearHistoryResult, error) {
	peer := boxPeer{PeerType: peerType, PeerId: peerId}
	ids, err := dialogMessageIds(b, peer, maxPortion+2)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		if justClear {
			return nil, nil
		}
		// No messages left, e.g. the last portion went in an earlier call
		// that failed after commit: the row alone.
		deleted, unpin, err := deleteDialogRow(b, peer)
		if err != nil || !deleted {
			return nil, err
		}
		return &ClearHistoryResult{DialogDeleted: true, Unpin: unpin}, nil
	}
	top := ids[0]
	var topRow *dataobject.MessagesDO
	if justClear {
		if topRow, err = messageById(b, top); err != nil {
			return nil, err
		}
		ids = ids[1:]
		if len(ids) == 0 && isHistoryClear(topRow) {
			return nil, nil
		}
	}
	more := len(ids) > maxPortion
	if more {
		ids = ids[:maxPortion]
	}

	res := &ClearHistoryResult{}
	if res.Deleted, err = d.DeleteMessages(b, ids); err != nil {
		return nil, err
	}
	if more {
		if res.Left, err = countDialogMessages(b, peer); err != nil {
			return nil, err
		}
		if justClear {
			res.Left-- // the top message stays
		}
	}
	if res.Deleted != nil {
		res.Updates = append(res.Updates, res.Deleted.Update)
	}
	read, err := d.readInbox(b, peer, top)
	if err != nil {
		return nil, err
	}
	if read != nil {
		res.Updates = append(res.Updates, read)
	}
	if topRow != nil && !isHistoryClear(topRow) {
		stored, err := storedMessage(topRow)
		if err != nil {
			return nil, err
		}
		cleared := mtproto.MakeTLMessageService(&mtproto.Message{
			Out:    true,
			Id:     topRow.UserMessageBoxId,
			FromId: mtproto.MakePeerUser(b.UserId()),
			PeerId: stored.GetPeerId(),
			Date:   stored.GetDate(),
			Action: mtproto.MakeTLMessageActionHistoryClear(nil).To_MessageAction(),
		}).To_Message()
		_, edited, err := d.editRowWith(b, topRow, func(*mtproto.Message) *mtproto.Message { return cleared })
		if err != nil {
			return nil, err
		}
		res.Updates = append(res.Updates, edited)
	}
	if !justClear && !more {
		if res.DialogDeleted, res.Unpin, err = deleteDialogRow(b, peer); err != nil {
			return nil, err
		}
	}
	if len(res.Updates) == 0 && !res.DialogDeleted {
		return nil, nil
	}
	return res, nil
}

func isHistoryClear(row *dataobject.MessagesDO) bool {
	if row == nil {
		return false
	}
	m, err := storedMessage(row)
	return err == nil && m.GetPredicateName() == mtproto.Predicate_messageService &&
		m.GetAction().GetPredicateName() == mtproto.Predicate_messageActionHistoryClear
}

// countMessages counts the box's messages that are not deleted and match
// where.
func countMessages(b *userbox.Box, where string, args ...any) (int32, error) {
	var n int32
	err := b.Tx().QueryRow(&n, "SELECT COUNT(*) FROM messages WHERE user_id = ? AND deleted = 0 AND "+where, append([]any{b.UserId()}, args...)...)
	return n, err
}

// countDialogMessages counts the messages of b's owner's dialog with peer.
func countDialogMessages(b *userbox.Box, peer boxPeer) (int32, error) {
	did := peer.dialogId(b.UserId())
	return countMessages(b, "dialog_id1 = ? AND dialog_id2 = ?", did.A, did.B)
}
