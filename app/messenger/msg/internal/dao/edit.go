package dao

import (
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
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
	before := proto.Clone(stored).(*mtproto.Message)
	message := change(stored)
	data, err := jsonx.Marshal(message)
	if err != nil {
		return nil, nil, err
	}
	// The media type follows the media: an edit may replace it, and the
	// media lists and messages.getRecentLocations select by it.
	filterType := mtproto.GetMediaType(message)
	if _, err = d.MessagesDAO.UpdateEditMessageTx(b.Tx(), string(data), message.GetMessage(), filterType, b.UserId(), row.UserMessageBoxId); err != nil {
		return nil, nil, err
	}
	if err = d.replaceHashTags(b, boxPeer{PeerType: row.PeerType, PeerId: row.PeerId}, row.UserMessageBoxId, message, true); err != nil {
		return nil, nil, err
	}
	// The update carries the reactions too: Desktop clears them from an
	// edited message without the field (history_item.cpp:2240, task 053).
	// The stored message never holds them.
	shown, err := d.withReactions(b, row, message)
	if err != nil {
		return nil, nil, err
	}
	update := widgetDataUpdate(b.UserId(), row, before, message)
	if update == nil {
		update = mtproto.MakeTLUpdateEditMessage(&mtproto.Update{Message_MESSAGE: shown}).To_Update()
	}
	b.AddPtsUpdate(update, 1)
	box := boxOfRow(row, shown)
	box.MessageFilterType = filterType
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

// widgetDataUpdate is the update of an edit that changed only the state of
// the widget of a message (messageMediaWidget: public_data with its hash,
// chat_data), as a bot writes it (messages.editMessageWidgetData, task
// 106): updateMessageWidgetData of the copy of owner, with the fields that
// changed. Telegram sends it so in private chats and basic groups, "as
// updateEditMessage does" in the owner's pts, without the "edited" mark
// (Android jni/twidget/widget-sandbox/docs/tgw-format.md, "The bot's edits
// of the state"); the clients apply it by the chat as the owner sees it and
// the id in its box (tdesktop tgw/tgw_state.cpp ApplyStateUpdate, Android
// MessagesController.getUpdateType). nil for any other edit.
func widgetDataUpdate(owner int64, row *dataobject.MessagesDO, before, after *mtproto.Message) *mtproto.Update {
	old, cur := before.GetMedia(), after.GetMedia()
	if old.GetPredicateName() != mtproto.Predicate_messageMediaWidget || cur.GetPredicateName() != mtproto.Predicate_messageMediaWidget {
		return nil
	}
	// The edit is of the state alone when the message with the old state
	// is the message as it was.
	same := proto.Clone(after).(*mtproto.Message)
	same.Media.PublicData, same.Media.PublicDataHash, same.Media.ChatData = old.GetPublicData(), old.GetPublicDataHash(), old.GetChatData()
	if !proto.Equal(same, before) {
		return nil
	}
	u := &mtproto.Update{MsgId_INT32: row.UserMessageBoxId}
	did := mtproto.DialogID{A: row.DialogId1, B: row.DialogId2}
	if did.A == -int64(mtproto.PEER_CHAT) {
		u.Peer_PEER = mtproto.MakePeerChat(did.B)
	} else {
		u.Peer_PEER = mtproto.MakePeerUser(mtproto.GetPeerIdByDialogId(owner, did))
	}
	changed := false
	if !proto.Equal(old.GetPublicData(), cur.GetPublicData()) || !proto.Equal(old.GetPublicDataHash(), cur.GetPublicDataHash()) {
		// The flag carries both: a state cleared is an empty string.
		u.PublicData = wrapperspb.String(cur.GetPublicData().GetValue())
		u.PublicDataHash = wrapperspb.Int64(cur.GetPublicDataHash().GetValue())
		changed = true
	}
	if !proto.Equal(old.GetChatData(), cur.GetChatData()) {
		u.ChatData = wrapperspb.String(cur.GetChatData().GetValue())
		changed = true
	}
	if !changed {
		return nil
	}
	return mtproto.MakeTLUpdateMessageWidgetData(u).To_Update()
}
