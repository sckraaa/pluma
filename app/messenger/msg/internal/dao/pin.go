package dao

import (
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// Pinned is a pin or unpin applied to a box.
type Pinned struct {
	Row    *dataobject.MessagesDO
	Update *mtproto.Update // updatePinnedMessages
}

// setPinned pins or unpins a message of the box read in the transaction:
// the message's flag, the dialog's pinned_msg_id (its newest pinned
// message) and the journal row updatePinnedMessages. It returns nil when
// the message is in that state already.
func (d *Dao) setPinned(b *userbox.Box, peer boxPeer, row *dataobject.MessagesDO, unpin bool) (*Pinned, error) {
	if row.Pinned == !unpin {
		return nil, nil
	}
	if _, err := d.MessagesDAO.UpdatePinnedTx(b.Tx(), !unpin, b.UserId(), row.UserMessageBoxId); err != nil {
		return nil, err
	}
	if err := updatePinnedMsgId(b, peer); err != nil {
		return nil, err
	}
	update := mtproto.MakeTLUpdatePinnedMessages(&mtproto.Update{
		Pinned:    !unpin,
		Peer_PEER: peer.toPeer(),
		Messages:  []int32{row.UserMessageBoxId},
	}).To_Update()
	b.AddPtsUpdate(update, 1)
	return &Pinned{Row: row, Update: update}, nil
}

// updatePinnedMsgId sets the dialog's pinned_msg_id to its newest pinned
// message, or 0.
func updatePinnedMsgId(b *userbox.Box, peer boxPeer) error {
	ids, err := pinnedMessageIds(b, peer, 1)
	if err != nil {
		return err
	}
	var pinned int32
	if len(ids) > 0 {
		pinned = ids[0]
	}
	return updateDialog(b, peer, "pinned_msg_id = ?", pinned)
}

// Pin pins (or unpins) b's owner's message id in the dialog with peer. The
// message must exist, not be deleted and be in that dialog. It returns nil
// when the message is in that state already.
func (d *Dao) Pin(b *userbox.Box, peerType int32, peerId int64, id int32, unpin bool) (*Pinned, error) {
	row, err := messageById(b, id)
	if err != nil {
		return nil, err
	}
	did := mtproto.MakeDialogId(b.UserId(), peerType, peerId)
	if row == nil || row.DialogId1 != did.A || row.DialogId2 != did.B {
		return nil, mtproto.ErrMsgIdInvalid
	}
	return d.setPinned(b, boxPeer{PeerType: peerType, PeerId: peerId}, row, unpin)
}

// PinCopy pins (or unpins) b's owner's copy of message dialogMessageId in
// the dialog with peer (as the owner sees it). It returns nil when the owner
// has no copy or the copy is in that state already.
func (d *Dao) PinCopy(b *userbox.Box, peerType int32, peerId int64, dialogMessageId int64, unpin bool) (*Pinned, error) {
	rows, err := messagesByDialogMessageIds(b, []int64{dialogMessageId})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return d.setPinned(b, boxPeer{PeerType: peerType, PeerId: peerId}, &rows[0], unpin)
}

// Unpinned is a portion of unpinning all messages of a dialog.
type Unpinned struct {
	Ids    []int32
	Update *mtproto.Update // updatePinnedMessages
	Left   int32           // pinned messages left
}

// UnpinPortion unpins up to a portion of the newest pinned messages of b's
// owner's dialog with peer (as the owner sees it), sets the dialog's
// pinned_msg_id and journals updatePinnedMessages with pts_count the number
// of messages. It returns nil when nothing is pinned.
func (d *Dao) UnpinPortion(b *userbox.Box, peerType int32, peerId int64) (*Unpinned, error) {
	peer := boxPeer{PeerType: peerType, PeerId: peerId}
	ids, err := pinnedMessageIds(b, peer, maxPortion+1)
	if err != nil || len(ids) == 0 {
		return nil, err
	}
	more := len(ids) > maxPortion
	if more {
		ids = ids[:maxPortion]
	}
	res := &Unpinned{}
	// By ascending id, as the rows change.
	res.Ids = make([]int32, len(ids))
	for i, id := range ids {
		res.Ids[len(ids)-1-i] = id
	}
	if _, err = d.MessagesDAO.UpdateUnPinnedByIdListTx(b.Tx(), b.UserId(), res.Ids); err != nil {
		return nil, err
	}
	if err = updatePinnedMsgId(b, peer); err != nil {
		return nil, err
	}
	if more {
		did := peer.dialogId(b.UserId())
		if res.Left, err = countMessages(b, "dialog_id1 = ? AND dialog_id2 = ? AND pinned = 1", did.A, did.B); err != nil {
			return nil, err
		}
	}
	res.Update = mtproto.MakeTLUpdatePinnedMessages(&mtproto.Update{
		Pinned:    false,
		Peer_PEER: peer.toPeer(),
		Messages:  res.Ids,
	}).To_Update()
	b.AddPtsUpdate(res.Update, int32(len(res.Ids)))
	return res, nil
}
