package dao

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/internal/dal/dataobject"
	"github.com/teamgram/teamgram-server/app/service/biz/dialog/dialog"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// Statements of box transactions. Each runs on b.Tx(), reads or changes only
// the rows of the box owner and changes rows by their key (ADR 0011, p. 3):
// the set of rows is read first, then changed by the list of keys. msg
// serves one messages table (MessageSharding 1, checked at start).

// maxPortion is the most messages one transaction deletes or unpins:
// operations on a whole history go in portions, as in Telegram.
const maxPortion = 1000

const (
	messageColumns = "user_id, user_message_box_id, dialog_id1, dialog_id2, dialog_message_id, sender_user_id, peer_type, peer_id, random_id, message_filter_type, message_data, message, mentioned, media_unread, pinned, has_reaction, reaction, reaction_date, reaction_unread, saved_peer_type, saved_peer_id, date2, ttl_period"

	insertMessageQuery = "INSERT INTO messages (user_id, user_message_box_id, dialog_id1, dialog_id2, dialog_message_id, sender_user_id, peer_type, peer_id, random_id, message_filter_type, message_data, message, mentioned, media_unread, pinned, saved_peer_type, saved_peer_id, date2, ttl_period, pts) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"

	dialogColumns = "user_id, peer_type, peer_id, peer_dialog_id, pinned, top_message, pinned_msg_id, read_inbox_max_id, read_outbox_max_id, unread_count, unread_mentions_count, unread_reactions_count, unread_mark, folder_id, date2, deleted"

	// Upserts of the dialog row: the row may be new (rows affected 1) or
	// changed (2). top_message only grows here.
	upsertOutgoingDialogQuery = "INSERT INTO dialogs (user_id, peer_type, peer_id, peer_dialog_id, top_message, draft_message_data, date2) VALUES (?, ?, ?, ?, ?, 'null', ?) AS new" +
		" ON DUPLICATE KEY UPDATE top_message = GREATEST(dialogs.top_message, new.top_message), date2 = new.date2, deleted = 0, unread_mark = 0, draft_type = 0, draft_message_data = 'null'"
	upsertIncomingDialogQuery = "INSERT INTO dialogs (user_id, peer_type, peer_id, peer_dialog_id, top_message, unread_count, unread_mentions_count, draft_message_data, date2) VALUES (?, ?, ?, ?, ?, ?, ?, 'null', ?) AS new" +
		" ON DUPLICATE KEY UPDATE top_message = GREATEST(dialogs.top_message, new.top_message), unread_count = dialogs.unread_count + new.unread_count," +
		" unread_mentions_count = dialogs.unread_mentions_count + new.unread_mentions_count, date2 = new.date2, deleted = 0"
)

// boxPeer is a dialog of the box owner.
type boxPeer struct {
	PeerType int32
	PeerId   int64
}

func (p boxPeer) dialogId(userId int64) mtproto.DialogID {
	return mtproto.MakeDialogId(userId, p.PeerType, p.PeerId)
}

func (p boxPeer) toPeer() *mtproto.Peer {
	return mtproto.MakePeer(p.PeerType, p.PeerId)
}

// peerOfDialog is the peer of a dialog id as userId sees it.
func peerOfDialog(userId int64, did mtproto.DialogID) boxPeer {
	if did.A < 0 {
		return boxPeer{PeerType: int32(-did.A), PeerId: did.B}
	}
	return boxPeer{PeerType: mtproto.PEER_USER, PeerId: mtproto.GetPeerIdByDialogId(userId, did)}
}

func inList(n int) string {
	if n == 0 {
		return "NULL"
	}
	b := make([]byte, 0, 2*n)
	for i := range n {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '?')
	}
	return string(b)
}

// insertMessage writes a message row of the box: pts is the pts of the
// journal row that adds it to the box (a repeated send answers with it).
// Every copy carries the id of its message in every box,
// dialog_message_id, which is unique in a box: a copy written twice fails.
func insertMessage(b *userbox.Box, do *dataobject.MessagesDO, pts int32) error {
	if do.UserId != b.UserId() {
		return fmt.Errorf("msg: message of user %d in the box of %d", do.UserId, b.UserId())
	}
	if do.DialogMessageId <= 0 {
		return fmt.Errorf("msg: message %d of user %d without a dialog_message_id", do.UserMessageBoxId, do.UserId)
	}
	_, err := b.Tx().Exec(insertMessageQuery,
		do.UserId, do.UserMessageBoxId, do.DialogId1, do.DialogId2, do.DialogMessageId, do.SenderUserId,
		do.PeerType, do.PeerId, do.RandomId, do.MessageFilterType, do.MessageData, do.Message,
		do.Mentioned, do.MediaUnread, do.Pinned, do.SavedPeerType, do.SavedPeerId, do.Date2, do.TtlPeriod, pts)
	return err
}

// messagesByIds returns the box's messages with these ids that are not
// deleted, by ascending id.
func messagesByIds(b *userbox.Box, ids []int32) ([]dataobject.MessagesDO, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := make([]any, 0, len(ids)+1)
	args = append(args, b.UserId())
	for _, id := range ids {
		args = append(args, id)
	}
	var rows []dataobject.MessagesDO
	err := b.Tx().QueryRowsPartial(&rows,
		"SELECT "+messageColumns+" FROM messages WHERE user_id = ? AND user_message_box_id IN ("+inList(len(ids))+") AND deleted = 0 ORDER BY user_message_box_id",
		args...)
	return rows, err
}

// messageById returns a message of the box that is not deleted, or nil.
func messageById(b *userbox.Box, id int32) (*dataobject.MessagesDO, error) {
	rows, err := messagesByIds(b, []int32{id})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// messagesByDialogMessageIds returns the box's copies of these messages that
// are not deleted, by ascending id.
func messagesByDialogMessageIds(b *userbox.Box, dialogMessageIds []int64) ([]dataobject.MessagesDO, error) {
	if len(dialogMessageIds) == 0 {
		return nil, nil
	}
	args := make([]any, 0, len(dialogMessageIds)+1)
	args = append(args, b.UserId())
	for _, id := range dialogMessageIds {
		args = append(args, id)
	}
	var rows []dataobject.MessagesDO
	err := b.Tx().QueryRowsPartial(&rows,
		"SELECT "+messageColumns+" FROM messages WHERE user_id = ? AND dialog_message_id IN ("+inList(len(dialogMessageIds))+") AND deleted = 0 ORDER BY user_message_box_id",
		args...)
	return rows, err
}

// replyCopy returns the box's copy of fromId's message
// fromMessageId, or nil. It reads one committed row of fromId's box without
// locking it.
func replyCopy(b *userbox.Box, fromId int64, fromMessageId int32) (*dataobject.MessagesDO, error) {
	var rows []dataobject.MessagesDO
	err := b.Tx().QueryRowsPartial(&rows,
		"SELECT "+messageColumns+" FROM messages WHERE user_id = ? AND deleted = 0 AND dialog_message_id ="+
			" (SELECT dialog_message_id FROM messages WHERE user_id = ? AND user_message_box_id = ? AND deleted = 0 LIMIT 1) LIMIT 1",
		b.UserId(), fromId, fromMessageId)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// dialogMessageIds returns up to limit ids of the dialog's messages that are
// not deleted, newest first.
func dialogMessageIds(b *userbox.Box, peer boxPeer, limit int) ([]int32, error) {
	did := peer.dialogId(b.UserId())
	var ids []int32
	err := b.Tx().QueryRowsPartial(&ids,
		"SELECT user_message_box_id FROM messages WHERE user_id = ? AND dialog_id1 = ? AND dialog_id2 = ? AND deleted = 0 ORDER BY user_message_box_id DESC LIMIT ?",
		b.UserId(), did.A, did.B, limit)
	return ids, err
}

// pinnedMessageIds returns up to limit ids of the dialog's pinned messages,
// newest first.
func pinnedMessageIds(b *userbox.Box, peer boxPeer, limit int) ([]int32, error) {
	did := peer.dialogId(b.UserId())
	var ids []int32
	err := b.Tx().QueryRowsPartial(&ids,
		"SELECT user_message_box_id FROM messages WHERE user_id = ? AND dialog_id1 = ? AND dialog_id2 = ? AND pinned = 1 AND deleted = 0 ORDER BY user_message_box_id DESC LIMIT ?",
		b.UserId(), did.A, did.B, limit)
	return ids, err
}

// phoneCallMessages returns up to limit of the box's phone call messages
// that are not deleted, newest first.
func phoneCallMessages(b *userbox.Box, limit int) ([]dataobject.MessagesDO, error) {
	var rows []dataobject.MessagesDO
	err := b.Tx().QueryRowsPartial(&rows,
		"SELECT "+messageColumns+" FROM messages WHERE user_id = ? AND message_filter_type = ? AND deleted = 0 ORDER BY user_message_box_id DESC LIMIT ?",
		b.UserId(), mtproto.MEDIA_PHONE_CALL, limit)
	return rows, err
}

// deleteMessages marks messages of the box deleted; ids must be ones read in
// the transaction.
func (d *Dao) deleteMessages(b *userbox.Box, ids []int32) error {
	if len(ids) == 0 {
		return nil
	}
	n, err := d.MessagesDAO.DeleteMessagesByMessageIdListTx(b.Tx(), b.UserId(), ids)
	if err != nil {
		return err
	}
	if n != int64(len(ids)) {
		return fmt.Errorf("msg: deleted %d of %d messages of user %d", n, len(ids), b.UserId())
	}
	return nil
}

// dialogRow returns the box's dialog with peer, or nil.
func dialogRow(b *userbox.Box, peer boxPeer) (*dataobject.DialogsDO, error) {
	do := &dataobject.DialogsDO{}
	err := b.Tx().QueryRowPartial(do,
		"SELECT "+dialogColumns+" FROM dialogs WHERE user_id = ? AND peer_type = ? AND peer_id = ?",
		b.UserId(), peer.PeerType, peer.PeerId)
	if errors.Is(err, sqlx.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return do, nil
}

// upsertDialog runs a dialog upsert and adds the cache keys it invalidates:
// the row's key, and the list keys when the row is new.
func upsertDialog(b *userbox.Box, peer boxPeer, query string, args ...any) error {
	r, err := b.Tx().Exec(query, args...)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	b.InvalidateCache(dialog.UpsertCacheKeys(b.UserId(), peer.PeerType, peer.PeerId, n)...)
	return nil
}

// upsertOutgoingDialog records the sender's new top message of the dialog.
func upsertOutgoingDialog(b *userbox.Box, peer boxPeer, topMessage int32, date int64) error {
	return upsertDialog(b, peer, upsertOutgoingDialogQuery,
		b.UserId(), peer.PeerType, peer.PeerId, mtproto.MakePeerDialogId(peer.PeerType, peer.PeerId), topMessage, date)
}

// upsertIncomingDialog records received messages: the new top message and
// the unread and unread mention counts they add.
func upsertIncomingDialog(b *userbox.Box, peer boxPeer, topMessage, unread, mentions int32, date int64) error {
	return upsertDialog(b, peer, upsertIncomingDialogQuery,
		b.UserId(), peer.PeerType, peer.PeerId, mtproto.MakePeerDialogId(peer.PeerType, peer.PeerId), topMessage, unread, mentions, date)
}

// updateDialog sets columns of the dialog row by its key.
func updateDialog(b *userbox.Box, peer boxPeer, set string, args ...any) error {
	args = append(args, b.UserId(), peer.PeerType, peer.PeerId)
	if _, err := b.Tx().Exec("UPDATE dialogs SET "+set+" WHERE user_id = ? AND peer_type = ? AND peer_id = ?", args...); err != nil {
		return err
	}
	b.InvalidateCache(dialog.GetDialogCacheKeyByPeer(b.UserId(), peer.PeerType, peer.PeerId))
	return nil
}

// deleteDialogRow deletes the box's dialog row with peer, if there is one,
// and adds the cache keys the deletion invalidates. The row holds columns of
// the box (top_message, read_inbox_max_id, the unread counters): it goes in
// the transaction that empties the dialog, so a message that arrives later
// creates a new row instead of being left without one.
func deleteDialogRow(b *userbox.Box, peer boxPeer) (bool, error) {
	r, err := b.Tx().Exec("DELETE FROM dialogs WHERE user_id = ? AND peer_type = ? AND peer_id = ?",
		b.UserId(), peer.PeerType, peer.PeerId)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	if err != nil || n == 0 {
		return false, err
	}
	b.InvalidateCache(dialog.DeleteCacheKeys(b.UserId(), peer.PeerType, peer.PeerId)...)
	return true, nil
}

// DeleteDialogRow deletes b's owner's dialog row with peer, if there is one.
func (d *Dao) DeleteDialogRow(b *userbox.Box, peerType int32, peerId int64) (bool, error) {
	return deleteDialogRow(b, boxPeer{PeerType: peerType, PeerId: peerId})
}

// The counters of a dialog are kept without reading the whole dialog, which
// in a long group chat would hold the box lock for the time of a scan of
// every message on each read and deletion. The messages index
// (user_id, dialog_id1, dialog_id2, deleted, user_message_box_id) gives the
// newest message and the unread range directly; the unread mention and
// reaction counters change by the flags of the rows a transaction changes.

// topMessage returns the id of the dialog's newest message that is not
// deleted, or 0: one lookup in the messages index.
func topMessage(b *userbox.Box, peer boxPeer) (int32, error) {
	did := peer.dialogId(b.UserId())
	var top int32
	err := b.Tx().QueryRow(&top,
		"SELECT COALESCE(MAX(user_message_box_id), 0) FROM messages WHERE user_id = ? AND dialog_id1 = ? AND dialog_id2 = ? AND deleted = 0",
		b.UserId(), did.A, did.B)
	return top, err
}

// unreadAfter counts the dialog's incoming messages after maxId that are not
// deleted: it reads the unread range of the messages index only.
func unreadAfter(b *userbox.Box, peer boxPeer, maxId int32) (int32, error) {
	did := peer.dialogId(b.UserId())
	var n int32
	err := b.Tx().QueryRow(&n,
		"SELECT COUNT(*) FROM messages WHERE user_id = ? AND dialog_id1 = ? AND dialog_id2 = ? AND deleted = 0 AND user_message_box_id > ? AND sender_user_id <> ?",
		b.UserId(), did.A, did.B, maxId, b.UserId())
	return n, err
}

// changedRows are rows of one dialog a transaction deleted or whose unread
// flags it cleared, as they were before the change.
type changedRows struct {
	peer boxPeer
	rows []dataobject.MessagesDO
}

// byDialog groups rows of b's owner's messages by dialog, in peer_dialog_id
// order (rows of one table change in key order, ADR 0011, p. 3).
func byDialog(userId int64, rows []dataobject.MessagesDO) []changedRows {
	var out []changedRows
	for _, r := range rows {
		p := peerOfDialog(userId, mtproto.DialogID{A: r.DialogId1, B: r.DialogId2})
		i := slices.IndexFunc(out, func(c changedRows) bool { return c.peer == p })
		if i < 0 {
			out = append(out, changedRows{peer: p})
			i = len(out) - 1
		}
		out[i].rows = append(out[i].rows, r)
	}
	slices.SortFunc(out, func(a, b changedRows) int { return comparePeers(a.peer, b.peer) })
	return out
}

// recountDialog brings the counters of the dialog row up to date after the
// transaction changed rows of the dialog: deleted them (deleted) or cleared
// their unread flags. The unread mention and reaction counters go down by
// the flags the rows had; after a deletion top_message is read from the
// messages index and unread_count goes down by the unread incoming messages
// deleted. A dialog without a row is left alone.
func recountDialog(b *userbox.Box, c changedRows, deleted bool) error {
	dlg, err := dialogRow(b, c.peer)
	if err != nil || dlg == nil {
		return err
	}
	top, unread := dlg.TopMessage, dlg.UnreadCount
	mentions, reactions := dlg.UnreadMentionsCount, dlg.UnreadReactionsCount
	for _, r := range c.rows {
		incoming := r.SenderUserId != b.UserId()
		if incoming && r.Mentioned {
			mentions--
		}
		if r.ReactionUnread {
			reactions--
		}
		if deleted && incoming && r.UserMessageBoxId > dlg.ReadInboxMaxId {
			unread--
		}
	}
	if deleted {
		if top, err = topMessage(b, c.peer); err != nil {
			return err
		}
	}
	unread, mentions, reactions = max(unread, 0), max(mentions, 0), max(reactions, 0)
	if top == dlg.TopMessage && unread == dlg.UnreadCount && mentions == dlg.UnreadMentionsCount && reactions == dlg.UnreadReactionsCount {
		return nil
	}
	return updateDialog(b, c.peer, "top_message = ?, unread_count = ?, unread_mentions_count = ?, unread_reactions_count = ?",
		top, unread, mentions, reactions)
}

// replaceHashTags writes the hashtags of a message of the box, dropping
// those of its previous version on edit.
func (d *Dao) replaceHashTags(b *userbox.Box, peer boxPeer, id int32, message *mtproto.Message, edit bool) error {
	if edit {
		if _, err := d.HashTagsDAO.DeleteHashTagMessageIdTx(b.Tx(), b.UserId(), id); err != nil {
			return err
		}
	}
	for _, entity := range message.GetEntities() {
		if entity.GetPredicateName() != mtproto.Predicate_messageEntityHashtag || entity.GetUrl() == "" {
			continue
		}
		if _, _, err := d.HashTagsDAO.InsertOrUpdateTx(b.Tx(), &dataobject.HashTagsDO{
			UserId:           b.UserId(),
			PeerType:         peer.PeerType,
			PeerId:           peer.PeerId,
			HashTag:          entity.GetUrl(),
			HashTagMessageId: id,
		}); err != nil {
			return err
		}
	}
	return nil
}
