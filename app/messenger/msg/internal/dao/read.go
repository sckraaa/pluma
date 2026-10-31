package dao

import (
	"slices"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/pkg/readmarks"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// readInbox marks the dialog read up to maxId: read_inbox_max_id only
// grows, unread_count becomes the number of incoming messages after maxId
// (the range left unread, not the whole dialog), and the journal gets
// updateReadHistoryInbox. An unread mark goes with the read, and the
// account's devices are told (pushUnreadMarkReset). It returns the update,
// or nil when the dialog is read that far already or has no row.
func (d *Dao) readInbox(b *userbox.Box, peer boxPeer, maxId int32) (*mtproto.Update, error) {
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
	if dlg.UnreadMark {
		if err = d.pushUnreadMarkReset(b, peer); err != nil {
			return nil, err
		}
	}
	return update, nil
}

// ReadHistoryResult is what reading a dialog changed.
type ReadHistoryResult struct {
	Update *mtproto.Update
	// Senders of the messages read, each with the dialog_message_id of its
	// oldest and newest message read: they learn their messages were read.
	Senders []ReadSender
}

// ReadSender is a sender whose messages a reader read, from
// MinDialogMessageId to DialogMessageId.
type ReadSender struct {
	UserId             int64
	MinDialogMessageId int64
	DialogMessageId    int64
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

	// The oldest and the newest message of each sender among the incoming
	// ones read now.
	var senders []struct {
		SenderUserId       int64 `db:"sender_user_id"`
		MinDialogMessageId int64 `db:"min_dialog_message_id"`
		DialogMessageId    int64 `db:"dialog_message_id"`
	}
	did := peer.dialogId(b.UserId())
	if err = b.Tx().QueryRowsPartial(&senders,
		"SELECT s.sender_user_id, oldest.dialog_message_id AS min_dialog_message_id, newest.dialog_message_id FROM"+
			" (SELECT sender_user_id, MIN(user_message_box_id) AS min_id, MAX(user_message_box_id) AS max_id FROM messages"+
			" WHERE user_id = ? AND dialog_id1 = ? AND dialog_id2 = ? AND user_message_box_id > ? AND user_message_box_id <= ? AND sender_user_id <> ? AND deleted = 0"+
			" GROUP BY sender_user_id) s"+
			" JOIN messages oldest ON oldest.user_id = ? AND oldest.user_message_box_id = s.min_id"+
			" JOIN messages newest ON newest.user_id = ? AND newest.user_message_box_id = s.max_id ORDER BY s.sender_user_id",
		b.UserId(), did.A, did.B, dlg.ReadInboxMaxId, maxId, b.UserId(), b.UserId(), b.UserId()); err != nil {
		return nil, err
	}

	update, err := d.readInbox(b, peer, maxId)
	if err != nil || update == nil {
		return nil, err
	}
	res := &ReadHistoryResult{Update: update}
	for _, s := range senders {
		res.Senders = append(res.Senders, ReadSender{UserId: s.SenderUserId, MinDialogMessageId: s.MinDialogMessageId, DialogMessageId: s.DialogMessageId})
	}
	return res, nil
}

// ReadOutbox records that readerId read b's owner's messages in the dialog
// with peer (as the owner sees it) from the owner's copy of
// minDialogMessageId (0: unknown) up to the one of dialogMessageId at
// readAt (unix seconds): the date is kept for every reader
// (recordReadDate), read_outbox_max_id only grows, and the journal gets
// updateReadHistoryOutbox. It returns the update, or nil when there is
// nothing new.
func (d *Dao) ReadOutbox(b *userbox.Box, peerType int32, peerId int64, readerId, minDialogMessageId, dialogMessageId, readAt int64) (*mtproto.Update, error) {
	peer := boxPeer{PeerType: peerType, PeerId: peerId}
	copies, err := messagesByDialogMessageIds(b, []int64{dialogMessageId})
	if err != nil || len(copies) == 0 {
		return nil, err
	}
	maxId := copies[0].UserMessageBoxId
	dlg, err := dialogRow(b, peer)
	if err != nil || dlg == nil {
		return nil, err
	}
	// The oldest message read, deleted since or not; without it the read
	// is known to reach the newest one only.
	minId := maxId
	if minDialogMessageId != 0 && minDialogMessageId != dialogMessageId {
		var ids []int32
		if err = b.Tx().QueryRowsPartial(&ids,
			"SELECT user_message_box_id FROM messages WHERE user_id = ? AND dialog_message_id = ?",
			b.UserId(), minDialogMessageId); err != nil {
			return nil, err
		}
		if len(ids) == 1 && ids[0] < maxId {
			minId = ids[0]
		}
	}
	// In a group the first reader moves read_outbox_max_id; the date of
	// each one is kept all the same.
	if err = recordReadDate(b, peer, readerId, minId, maxId, readAt); err != nil {
		return nil, err
	}
	if dlg.ReadOutboxMaxId >= maxId {
		return nil, nil
	}
	if err = updateDialog(b, peer, "read_outbox_max_id = ?", maxId); err != nil {
		return nil, err
	}
	update := mtproto.MakeTLUpdateReadHistoryOutbox(&mtproto.Update{
		Peer_PEER: peer.toPeer(),
		MaxId:     maxId,
	}).To_Update()
	b.AddPtsUpdate(update, 1)
	return update, nil
}

// recordReadDate keeps the date readerId read b's owner's messages in a
// private chat or a basic group from minId to maxId, the owner's message
// ids, for messages.getOutboxReadDate and
// messages.getMessageReadParticipants: a row per read, whose range holds
// the messages read with it. The reader's dates in the dialog older than
// readmarks.KeepPeriod go, and a read recorded twice keeps its first date.
func recordReadDate(b *userbox.Box, peer boxPeer, readerId int64, minId, maxId int32, readAt int64) error {
	if peer.PeerType != mtproto.PEER_USER && peer.PeerType != mtproto.PEER_CHAT {
		return nil
	}
	peerDialogId := mtproto.MakePeerDialogId(peer.PeerType, peer.PeerId)
	if _, err := b.Tx().Exec("DELETE FROM message_read_outbox"+
		" WHERE user_id = ? AND peer_dialog_id = ? AND read_user_id = ? AND read_outbox_max_date < ?",
		b.UserId(), peerDialogId, readerId, readAt-readmarks.KeepPeriod); err != nil {
		return err
	}
	_, err := b.Tx().Exec("INSERT INTO message_read_outbox"+
		" (user_id, peer_dialog_id, read_user_id, read_outbox_min_id, read_outbox_max_id, read_outbox_max_date) VALUES (?, ?, ?, ?, ?, ?)"+
		" ON DUPLICATE KEY UPDATE read_outbox_max_date = LEAST(read_outbox_max_date, VALUES(read_outbox_max_date))",
		b.UserId(), peerDialogId, readerId, minId, maxId, readAt)
	return err
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

// unreadMention selects the incoming messages that are unread mentions of
// the box owner (the argument): a mention with the unread media flag, as
// clients count them (tdesktop HistoryItem::isUnreadMention).
const unreadMention = "mentioned = 1 AND media_unread = 1 AND sender_user_id <> ?"

// MentionsRead is a portion of reading all unread mentions of a dialog.
type MentionsRead struct {
	Contents *ReadContentsResult // nil when there was nothing to read
	Left     int32               // unread mentions left
}

// ReadMentionsPortion reads up to a portion of the oldest unread mentions of
// b's owner's dialog with peer (as the owner sees it), as ReadContents does,
// and sets the dialog's unread mention counter to the mentions left, which
// also corrects a counter that went astray.
func (d *Dao) ReadMentionsPortion(b *userbox.Box, peerType int32, peerId int64) (*MentionsRead, error) {
	peer := boxPeer{PeerType: peerType, PeerId: peerId}
	did := peer.dialogId(b.UserId())
	var ids []int32
	if err := b.Tx().QueryRowsPartial(&ids,
		"SELECT user_message_box_id FROM messages WHERE user_id = ? AND dialog_id1 = ? AND dialog_id2 = ? AND "+unreadMention+" AND deleted = 0 ORDER BY user_message_box_id LIMIT ?",
		b.UserId(), did.A, did.B, b.UserId(), maxPortion+1); err != nil {
		return nil, err
	}
	more := len(ids) > maxPortion
	if more {
		ids = ids[:maxPortion]
	}
	res := &MentionsRead{}
	var err error
	if len(ids) > 0 {
		if res.Contents, err = d.ReadContents(b, ids); err != nil {
			return nil, err
		}
	}
	if more {
		if res.Left, err = countMessages(b, "dialog_id1 = ? AND dialog_id2 = ? AND "+unreadMention, did.A, did.B, b.UserId()); err != nil {
			return nil, err
		}
	}
	dlg, err := dialogRow(b, peer)
	if err != nil || dlg == nil || dlg.UnreadMentionsCount == res.Left {
		return res, err
	}
	return res, updateDialog(b, peer, "unread_mentions_count = ?", res.Left)
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
