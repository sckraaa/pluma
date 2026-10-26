package dao

import (
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// applyEdit returns the stored message with the editable parts of edit:
// text, entities, media, reply markup, action and the edit date. Its id,
// direction, peers and reply stay those of the box.
func applyEdit(stored, edit *mtproto.Message) *mtproto.Message {
	stored.Message = edit.Message
	stored.Entities = edit.Entities
	stored.Media = edit.Media
	stored.ReplyMarkup = edit.ReplyMarkup
	stored.Action = edit.Action
	stored.EditDate = edit.EditDate
	stored.EditHide = edit.EditHide
	return stored
}

// storedMessage decodes the message of a messages row.
func storedMessage(row *dataobject.MessagesDO) (*mtproto.Message, error) {
	var m *mtproto.Message
	if err := jsonx.UnmarshalFromString(row.MessageData, &m); err != nil {
		return nil, err
	}
	return m.FixData(), nil
}

func boxOfRow(row *dataobject.MessagesDO, message *mtproto.Message) *mtproto.MessageBox {
	return &mtproto.MessageBox{
		UserId:            row.UserId,
		MessageId:         row.UserMessageBoxId,
		SenderUserId:      row.SenderUserId,
		PeerType:          row.PeerType,
		PeerId:            row.PeerId,
		RandomId:          row.RandomId,
		DialogId1:         row.DialogId1,
		DialogId2:         row.DialogId2,
		DialogMessageId:   row.DialogMessageId,
		MessageFilterType: row.MessageFilterType,
		Message:           message,
		Mentioned:         row.Mentioned,
		MediaUnread:       row.MediaUnread,
		Pinned:            row.Pinned,
	}
}

// editRow writes edit into the message of a row (applyEdit).
func (d *Dao) editRow(b *userbox.Box, row *dataobject.MessagesDO, edit *mtproto.Message) (*mtproto.MessageBox, *mtproto.Update, error) {
	return d.editRowWith(b, row, func(stored *mtproto.Message) *mtproto.Message { return applyEdit(stored, edit) })
}

// editRowWith writes the message change makes of a row's stored message:
// the messages row, its hashtags and the journal row updateEditMessage. It
// returns the box with its pts and the update.
//
// The journal row holds the whole message after the change: getDifference
// gives a message new in the difference in the state of its last edit
// there (ADR 0019), so an edit journaled in part would spoil it.
func (d *Dao) editRowWith(b *userbox.Box, row *dataobject.MessagesDO, change func(stored *mtproto.Message) *mtproto.Message) (*mtproto.MessageBox, *mtproto.Update, error) {
	stored, err := storedMessage(row)
	if err != nil {
		return nil, nil, err
	}
	message := change(stored)
	data, err := jsonx.Marshal(message)
	if err != nil {
		return nil, nil, err
	}
	if _, err = d.MessagesDAO.UpdateEditMessageTx(b.Tx(), string(data), message.GetMessage(), b.UserId(), row.UserMessageBoxId); err != nil {
		return nil, nil, err
	}
	if err = d.replaceHashTags(b, boxPeer{PeerType: row.PeerType, PeerId: row.PeerId}, row.UserMessageBoxId, message, true); err != nil {
		return nil, nil, err
	}
	update := mtproto.MakeTLUpdateEditMessage(&mtproto.Update{Message_MESSAGE: message}).To_Update()
	b.AddPtsUpdate(update, 1)
	box := boxOfRow(row, message)
	box.Pts, box.PtsCount = update.Pts_INT32, update.PtsCount
	return box, update, nil
}

// EditOutgoing applies an edit by b's owner to its message id in the dialog
// with peer. The message is read in the transaction: it must exist, not be
// deleted, be in that dialog and be the owner's. It returns the edited box
// and the journaled updateEditMessage.
func (d *Dao) EditOutgoing(b *userbox.Box, peerType int32, peerId int64, id int32, edit *mtproto.Message) (*mtproto.MessageBox, *mtproto.Update, error) {
	row, err := messageById(b, id)
	if err != nil {
		return nil, nil, err
	}
	did := mtproto.MakeDialogId(b.UserId(), peerType, peerId)
	if row == nil || row.DialogId1 != did.A || row.DialogId2 != did.B {
		return nil, nil, mtproto.ErrMessageIdInvalid
	}
	if row.SenderUserId != b.UserId() {
		return nil, nil, mtproto.ErrMessageAuthorRequired
	}
	return d.editRow(b, row, edit)
}

// EditIncoming applies the sender's edit of message dialogMessageId to b's
// owner's copy. It returns nil when the owner has no copy (deleted, or
// never received).
func (d *Dao) EditIncoming(b *userbox.Box, dialogMessageId int64, edit *mtproto.Message) (*mtproto.Update, error) {
	rows, err := messagesByDialogMessageIds(b, []int64{dialogMessageId})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	_, update, err := d.editRow(b, &rows[0], edit)
	return update, err
}
