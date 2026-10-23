package dao

import (
	"slices"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// readInbox marks the dialog read up to maxId: read_inbox_max_id only
// grows, unread_count becomes the number of incoming messages after maxId
// (the range left unread, not the whole dialog), and the journal gets
// updateReadHistoryInbox. It returns the update, or nil when the dialog is
// read that far already or has no row.
func readInbox(b *userbox.Box, peer boxPeer, maxId int32) (*mtproto.Update, error) {
	dlg, err := dialogRow(b, peer)
	if err != nil || dlg == nil || dlg.ReadInboxMaxId >= maxId {
		return nil, err
	}
	unread, err := unreadAfter(b, peer, maxId)
	if err != nil {
		return nil, err
	}
	if err = updateDialog(b, peer, "read_inbox_max_id = ?, unread_count = ?, unread_mark = 0", maxId, unread); err != nil {
		return nil, err
	}
	update := mtproto.MakeTLUpdateReadHistoryInbox(&mtproto.Update{
		Peer_PEER:        peer.toPeer(),
		MaxId:            maxId,
		StillUnreadCount: unread,
	}).To_Update()
	b.AddPtsUpdate(update, 1)
	return update, nil
}

// ReadHistoryResult is what reading a dialog changed.
type ReadHistoryResult struct {
	Update *mtproto.Update
	// Senders of the messages read, each with the dialog_message_id of its
	// newest message read: they learn their messages were read.
	Senders []ReadSender
}

// ReadSender is a sender whose messages a reader read up to DialogMessageId.
type ReadSender struct {
	UserId          int64
	DialogMessageId int64
}

// ReadHistory marks b's owner's dialog with peer read up to maxId (0: up to
// the top message). It returns nil when there is nothing to read; a dialog
// without a row is PEER_ID_INVALID.
func (d *Dao) ReadHistory(b *userbox.Box, peerType int32, peerId int64, maxId int32) (*ReadHistoryResult, error) {
	peer := boxPeer{PeerType: peerType, PeerId: peerId}
	dlg, err := dialogRow(b, peer)
	if err != nil {
		return nil, err
	}
	if dlg == nil {
		return nil, mtproto.ErrPeerIdInvalid
	}
	if maxId <= 0 || maxId > dlg.TopMessage {
		maxId = dlg.TopMessage
	}
	if dlg.ReadInboxMaxId >= maxId {
		return nil, nil
	}

	// The newest message of each sender among the incoming ones read now.
	var senders []struct {
		SenderUserId    int64 `db:"sender_user_id"`
		DialogMessageId int64 `db:"dialog_message_id"`
	}
	did := peer.dialogId(b.UserId())
	if err = b.Tx().QueryRowsPartial(&senders,
		"SELECT m.sender_user_id, m.dialog_message_id FROM messages m JOIN"+
			" (SELECT sender_user_id, MAX(user_message_box_id) AS id FROM messages"+
			" WHERE user_id = ? AND dialog_id1 = ? AND dialog_id2 = ? AND user_message_box_id > ? AND user_message_box_id <= ? AND sender_user_id <> ? AND deleted = 0"+
			" GROUP BY sender_user_id) s ON m.user_id = ? AND m.user_message_box_id = s.id ORDER BY m.sender_user_id",
		b.UserId(), did.A, did.B, dlg.ReadInboxMaxId, maxId, b.UserId(), b.UserId()); err != nil {
		return nil, err
	}

	update, err := readInbox(b, peer, maxId)
	if err != nil || update == nil {
		return nil, err
	}
	res := &ReadHistoryResult{Update: update}
	for _, s := range senders {
		res.Senders = append(res.Senders, ReadSender{UserId: s.SenderUserId, DialogMessageId: s.DialogMessageId})
	}
	return res, nil
}

// ReadOutbox records that readerId read b's owner's messages in the dialog
// with peer (as the owner sees it) up to the owner's copy of
// dialogMessageId: read_outbox_max_id only grows, and the journal gets
// updateReadHistoryOutbox. It returns the update, or nil when there is
// nothing new.
func (d *Dao) ReadOutbox(b *userbox.Box, peerType int32, peerId int64, readerId, dialogMessageId int64) (*mtproto.Update, error) {
	peer := boxPeer{PeerType: peerType, PeerId: peerId}
	copies, err := messagesByDialogMessageIds(b, []int64{dialogMessageId})
	if err != nil || len(copies) == 0 {
		return nil, err
	}
	maxId := copies[0].UserMessageBoxId
	dlg, err := dialogRow(b, peer)
	if err != nil || dlg == nil || dlg.ReadOutboxMaxId >= maxId {
		return nil, err
	}
	if err = updateDialog(b, peer, "read_outbox_max_id = ?", maxId); err != nil {
		return nil, err
	}
	if peerType == mtproto.PEER_USER {
		if _, _, err = d.MessageReadOutboxDAO.InsertOrUpdateTx(b.Tx(), &dataobject.MessageReadOutboxDO{
			UserId:            b.UserId(),
			PeerDialogId:      mtproto.MakePeerDialogId(peerType, peerId),
			ReadUserId:        readerId,
			ReadOutboxMaxId:   maxId,
			ReadOutboxMaxDate: time.Now().Unix(),
		}); err != nil {
			return nil, err
		}
	}
	update := mtproto.MakeTLUpdateReadHistoryOutbox(&mtproto.Update{
		Peer_PEER: peer.toPeer(),
		MaxId:     maxId,
	}).To_Update()
	b.AddPtsUpdate(update, 1)
	return update, nil
}

// ReadContentsResult is what reading message contents changed.
type ReadContentsResult struct {
	Update *mtproto.Update
	// Media are the opened media messages of other senders: their
	// dialog_message_id by sender, which learns they were opened.
	Media []ReadMedia
}

// ReadMedia are messages of SenderId whose media a reader opened.
type ReadMedia struct {
	SenderId         int64
	PeerType         int32 // the dialog as the reader sees it
	PeerId           int64
	DialogMessageIds []int64
}

// ReadContents clears the unread mention, unread media and unread reaction
// flags of b's owner's messages ids where they are set, lowers the dialogs'
// unread mention and reaction counters by the flags cleared and journals one updateReadMessagesContents with the
// ids it changed. It returns nil when nothing changed.
func (d *Dao) ReadContents(b *userbox.Box, ids []int32) (*ReadContentsResult, error) {
	rows, err := messagesByIds(b, ids)
	if err != nil {
		return nil, err
	}
	var (
		changed     []int32
		changedRows []dataobject.MessagesDO
		media       []ReadMedia
	)
	for _, r := range rows {
		if !r.Mentioned && !r.MediaUnread && !r.ReactionUnread {
			continue
		}
		changed = append(changed, r.UserMessageBoxId)
		changedRows = append(changedRows, r)
		peer := peerOfDialog(b.UserId(), mtproto.DialogID{A: r.DialogId1, B: r.DialogId2})
		if r.MediaUnread && !r.Mentioned && r.SenderUserId != b.UserId() {
			i := slices.IndexFunc(media, func(m ReadMedia) bool { return m.SenderId == r.SenderUserId })
			if i < 0 {
				media = append(media, ReadMedia{SenderId: r.SenderUserId, PeerType: peer.PeerType, PeerId: peer.PeerId})
				i = len(media) - 1
			}
			media[i].DialogMessageIds = append(media[i].DialogMessageIds, r.DialogMessageId)
		}
	}
	if len(changed) == 0 {
		return nil, nil
	}
	if err = clearUnreadFlags(b, changed, "mentioned = 0, media_unread = 0, reaction_unread = 0"); err != nil {
		return nil, err
	}
	for _, c := range byDialog(b.UserId(), changedRows) {
		if err = recountDialog(b, c, false); err != nil {
			return nil, err
		}
	}
	slices.SortFunc(media, func(a, b ReadMedia) int { return compareInt64(a.SenderId, b.SenderId) })

	update := mtproto.MakeTLUpdateReadMessagesContents(&mtproto.Update{Messages: changed}).To_Update()
	b.AddPtsUpdate(update, int32(len(changed)))
	return &ReadContentsResult{Update: update, Media: media}, nil
}

// ReadMediaOutgoing records that a reader opened the media of b's owner's
// messages dialogMessageIds: their unread media flag is cleared where set,
// and the journal gets updateReadMessagesContents. It returns the update,
// or nil when nothing changed.
func (d *Dao) ReadMediaOutgoing(b *userbox.Box, dialogMessageIds []int64) (*mtproto.Update, error) {
	rows, err := messagesByDialogMessageIds(b, dialogMessageIds)
	if err != nil {
		return nil, err
	}
	var changed []int32
	for _, r := range rows {
		if r.MediaUnread {
			changed = append(changed, r.UserMessageBoxId)
		}
	}
	if len(changed) == 0 {
		return nil, nil
	}
	if err = clearUnreadFlags(b, changed, "media_unread = 0"); err != nil {
		return nil, err
	}
	update := mtproto.MakeTLUpdateReadMessagesContents(&mtproto.Update{Messages: changed}).To_Update()
	b.AddPtsUpdate(update, int32(len(changed)))
	return update, nil
}

func clearUnreadFlags(b *userbox.Box, ids []int32, set string) error {
	args := make([]any, 0, len(ids)+1)
	args = append(args, b.UserId())
	for _, id := range ids {
		args = append(args, id)
	}
	_, err := b.Tx().Exec("UPDATE messages SET "+set+" WHERE user_id = ? AND user_message_box_id IN ("+inList(len(ids))+")", args...)
	return err
}

func compareInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// comparePeers orders dialogs by peer_dialog_id: several rows of one table
// change in key order (ADR 0011, p. 3).
func comparePeers(a, b boxPeer) int {
	return compareInt64(mtproto.MakePeerDialogId(a.PeerType, a.PeerId), mtproto.MakePeerDialogId(b.PeerType, b.PeerId))
}
