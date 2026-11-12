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
	"math/rand/v2"
	"slices"
	"strings"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/monoforum"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// The direct messages of a channel (task 084, ADR 0055, pkg/monoforum): the
// monoforum of a broadcast channel, the sublists in it and what its two
// kinds of users, the admins of the direct messages and the subscribers,
// read and delete.

// Errors of the direct messages the schema's list has no entry for
// (core.telegram.org/method/channels.updatePaidMessagesPrice,
// method/messages.readSavedHistory).
var (
	ErrChannelMonoforumUnsupported = status.Error(mtproto.ErrBadRequest, "CHANNEL_MONOFORUM_UNSUPPORTED")
	ErrParentPeerInvalid           = status.Error(mtproto.ErrBadRequest, "PARENT_PEER_INVALID")
)

type directRow struct {
	Monoforum     bool  `db:"monoforum"`
	LinkedId      int64 `db:"linked_monoforum_id"`
	Allowed       bool  `db:"broadcast_messages_allowed"`
	CreatorUserId int64 `db:"creator_user_id"`
}

// SetDirectMessages switches the direct messages of the broadcast channel
// on or off, by one who may change its info (the creator, an admin with
// change_info: the clients offer it to them, tdesktop
// edit_peer_info_box.cpp:1587-1588, Android ChatEditActivity.java:925). The
// first time makes the channel's monoforum, a supergroup of its own linked
// both ways; switching them off keeps it and its messages, and switching
// them on again brings them back. The channel gets the service message
// messageActionPaidMessagesPrice (core.telegram.org/api/paid-messages) and
// its members load it again. It returns the change and the monoforum.
//
// CHANNEL_MONOFORUM_UNSUPPORTED for a monoforum, CHANNEL_INVALID for a
// supergroup, CHAT_NOT_MODIFIED when they are as asked.
func (d *Dao) SetDirectMessages(b *userbox.Box, channelId int64, enabled bool) (*ChannelChange, int64, error) {
	ch, err := openChannel(b, channelId)
	if err != nil {
		return nil, 0, err
	}
	var rows []directRow
	if err = b.Tx().QueryRowsPartial(&rows,
		"SELECT monoforum, linked_monoforum_id, broadcast_messages_allowed, creator_user_id FROM channels WHERE id = ? AND deleted = 0", channelId); err != nil {
		return nil, 0, err
	}
	if len(rows) == 0 {
		return nil, 0, mtproto.ErrChannelInvalid
	}
	if rows[0].Monoforum {
		return nil, 0, ErrChannelMonoforumUnsupported
	}
	r, err := infoEditor(b, channelId)
	if err != nil {
		return nil, 0, err
	}
	if !r.Broadcast {
		return nil, 0, mtproto.ErrChannelInvalid
	}
	if rows[0].Allowed == enabled {
		return nil, 0, mtproto.ErrChatNotModified
	}
	linked := rows[0].LinkedId
	if enabled && linked == 0 {
		// A new id is above every channel the transaction holds, which
		// keeps the lock order (channelbox.Create).
		mf, err := channelbox.Create(b, channelbox.NewChannel{
			AccessHash:    rand.Int64(), //nolint:gosec // an access hash checks a client's reference, it is no secret of crypto
			CreatorUserId: rows[0].CreatorUserId,
			Title:         r.Title,
			Megagroup:     true,
			Date:          b.Date(),
		})
		if err != nil {
			return nil, 0, err
		}
		linked = mf.Id()
		if _, err = b.Tx().Exec("UPDATE channels SET monoforum = 1, linked_monoforum_id = ? WHERE id = ?", channelId, linked); err != nil {
			return nil, 0, err
		}
	}
	change := &ChannelChange{ChannelId: channelId, Reload: true}
	action := mtproto.MakeTLMessageActionPaidMessagesPrice(&mtproto.MessageAction{BroadcastMessagesAllowed: enabled}).To_MessageAction()
	if change.Message, change.Pts, err = addServiceMessage(b, ch, channelId, true, action); err != nil {
		return nil, 0, err
	}
	if _, err = b.Tx().Exec("UPDATE channels SET broadcast_messages_allowed = ?, linked_monoforum_id = ?, top_message = ?, version = version + 1 WHERE id = ?",
		enabled, linked, change.Message.GetId(), channelId); err != nil {
		return nil, 0, err
	}
	return change, linked, nil
}

// monoforumMember reads what userId is in the monoforum channelId:
// PARENT_PEER_INVALID for a channel that is none, CHANNEL_PRIVATE for one
// who may not open it.
func monoforumMember(q monoforum.Querier, channelId, userId, now int64) (monoforum.Side, monoforum.Member, error) {
	side, err := monoforum.SideOf(q, channelId)
	if err != nil {
		return side, monoforum.Member{}, err
	}
	if !side.Monoforum {
		return side, monoforum.Member{}, ErrParentPeerInvalid
	}
	m, err := monoforum.MemberOf(q, side.LinkedId, userId, now)
	if err != nil {
		return side, m, err
	}
	if !m.Access {
		return side, m, mtproto.ErrChannelPrivate
	}
	return side, m, nil
}

// monoforumAdmin is monoforumMember for what only the admins of the direct
// messages do: CHAT_ADMIN_REQUIRED for anyone else
// (core.telegram.org/method/messages.readSavedHistory).
func monoforumAdmin(q monoforum.Querier, channelId, userId, now int64) (monoforum.Side, error) {
	side, m, err := monoforumMember(q, channelId, userId, now)
	if err != nil {
		return side, err
	}
	if !m.Admin {
		return side, mtproto.ErrChatAdminRequired
	}
	return side, nil
}

// emitUserPush adds a push of updates to every device of userId, without
// seq: what it tells is read again with the dialogs.
func (d *Dao) emitUserPush(b *userbox.Box, userId int64, updates ...*mtproto.Update) error {
	in := &sync.TLSyncPushUpdates{UserId: userId, Updates: mtproto.MakeUpdatesByUpdates(updates...)}
	data, err := jsonx.Marshal(in)
	if err != nil {
		return err
	}
	b.Emit(userbox.Event{Topic: d.Topics.Sync, Recipient: userId, Method: string(proto.MessageName(in)), Data: data})
	return nil
}

// emitOwnPush is emitUserPush to the devices of b's owner other than the
// one of permAuthKeyId, which made the change.
func (d *Dao) emitOwnPush(b *userbox.Box, permAuthKeyId int64, updates ...*mtproto.Update) error {
	if permAuthKeyId == 0 {
		return d.emitUserPush(b, b.UserId(), updates...)
	}
	in := &sync.TLSyncUpdatesNotMe{UserId: b.UserId(), PermAuthKeyId: permAuthKeyId, Updates: mtproto.MakeUpdatesByUpdates(updates...)}
	data, err := jsonx.Marshal(in)
	if err != nil {
		return err
	}
	b.Emit(userbox.Event{Topic: d.Topics.Sync, Recipient: b.UserId(), Method: string(proto.MessageName(in)), Data: data})
	return nil
}

// emitToAdmins tells the admins of the direct messages of broadcastId an
// update of a sublist: every one of them but b's owner by push, the owner's
// other devices as well.
func (d *Dao) emitToAdmins(b *userbox.Box, broadcastId, permAuthKeyId int64, u *mtproto.Update) error {
	admins, err := monoforum.AdminIds(b.Tx(), broadcastId)
	if err != nil {
		return err
	}
	for _, id := range admins {
		if id == b.UserId() {
			err = d.emitOwnPush(b, permAuthKeyId, u)
		} else {
			err = d.emitUserPush(b, id, u)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// sublistLast is the last message of the sublist peerId that is not
// deleted, 0 for none.
func sublistLast(b *userbox.Box, channelId, peerId int64) (int32, error) {
	var last []int32
	err := b.Tx().QueryRowsPartial(&last,
		"SELECT COALESCE(MAX(id), 0) FROM channel_messages WHERE channel_id = ? AND saved_peer_id = ? AND deleted = 0", channelId, peerId)
	return first(last), err
}

// readMonoforum is Read in a monoforum (core.telegram.org/api/monoforum):
//   - a subscriber reads its sublist as the supergroup it sees: its mark is
//     its row of dialogs, its devices get updateReadChannelInbox with seq,
//     and the admins updateReadMonoForumOutbox of the sublist;
//   - an admin of the direct messages who reads the whole monoforum reads
//     every sublist up to maxId (tdesktop reads the sublists so,
//     history.cpp:3749-3790).
func (d *Dao) readMonoforum(b *userbox.Box, channelId int64, side monoforum.Side, permAuthKeyId int64, maxId int32) error {
	m, err := monoforum.MemberOf(b.Tx(), side.LinkedId, b.UserId(), b.Date())
	if err != nil {
		return err
	}
	if !m.Access {
		return mtproto.ErrChannelPrivate
	}
	if m.Admin {
		var peers []int64
		if err = b.Tx().QueryRowsPartial(&peers,
			"SELECT s.peer_id FROM channel_monoforum_sublists s WHERE s.channel_id = ? AND (s.unread_mark = 1 OR EXISTS (SELECT 1 FROM channel_messages m"+
				" WHERE m.channel_id = s.channel_id AND m.saved_peer_id = s.peer_id AND m.id > s.read_inbox_max_id AND m.send_as_id = 0 AND m.deleted = 0))"+
				" ORDER BY s.peer_id", channelId); err != nil {
			return err
		}
		for _, p := range peers {
			if err = d.readSublist(b, channelId, side, p, permAuthKeyId, maxId); err != nil {
				return err
			}
		}
		return nil
	}

	var rows []struct {
		ReadInboxMaxId int32 `db:"read_inbox_max_id"`
		FolderId       int32 `db:"folder_id"`
		Pts            int32 `db:"pts"`
	}
	if err = b.Tx().QueryRowsPartial(&rows,
		"SELECT g.read_inbox_max_id, g.folder_id, c.pts FROM dialogs g JOIN channels c ON c.id = g.peer_id AND c.deleted = 0"+
			" WHERE g.user_id = ? AND g.peer_type = ? AND g.peer_id = ? AND g.deleted = 0 FOR UPDATE OF g",
		b.UserId(), mtproto.PEER_CHANNEL, channelId); err != nil {
		return err
	}
	if len(rows) == 0 {
		// It has not written to the channel yet: nothing to read.
		return nil
	}
	last, err := sublistLast(b, channelId, b.UserId())
	if err != nil {
		return err
	}
	if maxId <= 0 || maxId > last {
		maxId = last
	}
	if maxId <= rows[0].ReadInboxMaxId {
		return nil
	}
	if _, err = b.Tx().Exec("UPDATE dialogs SET read_inbox_max_id = ? WHERE user_id = ? AND peer_type = ? AND peer_id = ?",
		maxId, b.UserId(), mtproto.PEER_CHANNEL, channelId); err != nil {
		return err
	}
	InvalidateDialog(b, channelId)
	var unread []int32
	if err = b.Tx().QueryRowsPartial(&unread, subscriberUnreadQuery, channelId, b.UserId(), maxId); err != nil {
		return err
	}
	if err = d.EmitSeqUpdate(b, mtproto.MakeTLUpdateReadChannelInbox(&mtproto.Update{
		FolderId:         mtproto.MakeFlagsInt32(rows[0].FolderId),
		ChannelId:        channelId,
		MaxId:            maxId,
		StillUnreadCount: first(unread),
		Pts_INT32:        rows[0].Pts,
	}).To_Update(), nil, nil); err != nil {
		return err
	}
	moved, err := monoforum.ReadOutbox(b.Tx(), channelId, b.UserId(), maxId)
	if err != nil || !moved {
		return err
	}
	return d.emitToAdmins(b, side.LinkedId, 0, mtproto.MakeTLUpdateReadMonoForumOutbox(&mtproto.Update{
		ChannelId:   channelId,
		SavedPeerId: mtproto.MakePeerUser(b.UserId()),
		ReadMaxId:   maxId,
	}).To_Update())
}

// subscriberUnreadQuery counts what the subscriber has not read of its
// sublist above its mark: the channel's messages.
const subscriberUnreadQuery = "SELECT COUNT(*) FROM channel_messages WHERE channel_id = ? AND saved_peer_id = ? AND id > ? AND send_as_id <> 0 AND deleted = 0"

// readSublist moves the admins' mark of the sublist peerId up to maxId (0
// or above its last message: up to that) and clears its unread mark. The
// other admins and the reader's other devices get updateReadMonoForumInbox,
// the subscriber updateReadChannelOutbox, as in the supergroup it sees
// (core.telegram.org/api/monoforum). A read that changes nothing writes
// nothing.
func (d *Dao) readSublist(b *userbox.Box, channelId int64, side monoforum.Side, peerId, permAuthKeyId int64, maxId int32) error {
	last, err := sublistLast(b, channelId, peerId)
	if err != nil {
		return err
	}
	if maxId <= 0 || maxId > last {
		maxId = last
	}
	before, moved, err := monoforum.ReadInbox(b.Tx(), channelId, peerId, maxId)
	if err != nil {
		return err
	}
	if !moved {
		// The unread mark alone may have gone: the admins read it with the
		// sublists.
		return nil
	}
	InvalidateDialog(b, channelId)
	if err = d.emitToAdmins(b, side.LinkedId, permAuthKeyId, mtproto.MakeTLUpdateReadMonoForumInbox(&mtproto.Update{
		ChannelId:   channelId,
		SavedPeerId: mtproto.MakePeerUser(peerId),
		ReadMaxId:   maxId,
	}).To_Update()); err != nil {
		return err
	}
	// The subscriber learns that its messages were read when one of them is
	// in the range.
	var own []int32
	if err = b.Tx().QueryRowsPartial(&own,
		"SELECT id FROM channel_messages WHERE channel_id = ? AND saved_peer_id = ? AND id > ? AND id <= ? AND send_as_id = 0 AND deleted = 0 LIMIT 1",
		channelId, peerId, before, maxId); err != nil || len(own) == 0 {
		return err
	}
	return d.emitUserPush(b, peerId, mtproto.MakeTLUpdateReadChannelOutbox(&mtproto.Update{ChannelId: channelId, MaxId: maxId}).To_Update())
}

// ReadSublist is messages.readSavedHistory of a monoforum: an admin of the
// direct messages read the sublist peerId up to maxId. PARENT_PEER_INVALID
// for a channel that is no monoforum, CHAT_ADMIN_REQUIRED for anyone but an
// admin of its direct messages.
func (d *Dao) ReadSublist(b *userbox.Box, channelId, peerId, permAuthKeyId int64, maxId int32) error {
	side, err := monoforumAdmin(b.Tx(), channelId, b.UserId(), b.Date())
	if err != nil {
		return err
	}
	return d.readSublist(b, channelId, side, peerId, permAuthKeyId, maxId)
}

// MarkSublistUnread sets or clears the unread mark of the sublist peerId
// for the admins (messages.markDialogUnread with parent_peer), who get
// updateDialogUnreadMark with the sublist. It tells whether the mark
// changed.
func (d *Dao) MarkSublistUnread(b *userbox.Box, channelId, peerId, permAuthKeyId int64, unread bool) (bool, error) {
	side, err := monoforumAdmin(b.Tx(), channelId, b.UserId(), b.Date())
	if err != nil {
		return false, err
	}
	changed, err := monoforum.SetUnreadMark(b.Tx(), channelId, peerId, unread)
	if err != nil || !changed {
		return false, err
	}
	InvalidateDialog(b, channelId)
	return true, d.emitToAdmins(b, side.LinkedId, permAuthKeyId, mtproto.MakeTLUpdateDialogUnreadMark(&mtproto.Update{
		Unread:          unread,
		Peer_DIALOGPEER: mtproto.MakeTLDialogPeer(&mtproto.DialogPeer{Peer: mtproto.MakePeerChannel(channelId)}).To_DialogPeer(),
		SavedPeerId:     mtproto.MakePeerUser(peerId),
	}).To_Update())
}

// SublistUnreadMarks are the sublists of the monoforum marked unread, for
// an admin of its direct messages.
func (d *Dao) SublistUnreadMarks(ctx context.Context, selfId, channelId, now int64) ([]int64, error) {
	q := monoforum.OnDB(ctx, d.Mysql.DB)
	if _, err := monoforumAdmin(q, channelId, selfId, now); err != nil {
		return nil, err
	}
	return monoforum.UnreadMarks(q, channelId)
}

// Sublists are the sublists of the monoforum as an admin of its direct
// messages lists them (messages.getSavedDialogs and getSavedDialogsByID
// with parent_peer): those of peers when it names any, else a page below
// the sublist whose last message is offsetId. Each is a monoForumDialog
// with its last message, as the admin sees it; count is how many sublists
// there are, and more whether the page is not the last.
func (d *Dao) Sublists(ctx context.Context, selfId, channelId int64, peers []int64, offsetId, limit int32, now int64) (dialogs []*mtproto.SavedDialog, messages []*mtproto.Message, count int32, more bool, err error) {
	q := monoforum.OnDB(ctx, d.Mysql.DB)
	if _, err = monoforumAdmin(q, channelId, selfId, now); err != nil {
		return nil, nil, 0, false, err
	}
	var rows []monoforum.SublistRow
	if len(peers) > 0 {
		if rows, err = monoforum.SublistsByPeers(q, channelId, peers); err != nil {
			return nil, nil, 0, false, err
		}
		rows = slices.DeleteFunc(rows, func(r monoforum.SublistRow) bool { return r.TopMessage == 0 })
		count = int32(len(rows))
	} else {
		limit = min(max(limit, 1), monoforum.ListMax)
		// One more than asked tells whether the page is the last.
		if rows, count, err = monoforum.List(q, channelId, offsetId, limit+1); err != nil {
			return nil, nil, 0, false, err
		}
		if more = len(rows) > int(limit); more {
			rows = rows[:limit]
		}
	}
	ids := make([]int32, 0, len(rows))
	dialogs = make([]*mtproto.SavedDialog, 0, len(rows))
	for _, r := range rows {
		unread, err := monoforum.UnreadOfAdmins(q, channelId, r)
		if err != nil {
			return nil, nil, 0, false, err
		}
		var reactions []int32
		if err = d.Mysql.QueryRowsPartial(ctx, &reactions, unreadReactionsQuery+" AND m.saved_peer_id = ?", channelId, selfId, 0, r.PeerId); err != nil {
			return nil, nil, 0, false, err
		}
		draft, err := monoforum.DraftOf(q, selfId, channelId, r.PeerId)
		if err != nil {
			return nil, nil, 0, false, err
		}
		ids = append(ids, r.TopMessage)
		dialogs = append(dialogs, mtproto.MakeTLMonoForumDialog(&mtproto.SavedDialog{
			UnreadMark:           r.UnreadMark,
			Peer:                 mtproto.MakePeerUser(r.PeerId),
			TopMessage:           r.TopMessage,
			ReadInboxMaxId:       r.ReadInboxMaxId,
			ReadOutboxMaxId:      r.ReadOutboxMaxId,
			UnreadCount:          unread,
			UnreadReactionsCount: first(reactions),
			Draft:                draft,
		}).To_SavedDialog())
	}
	if messages, err = d.Messages(ctx, selfId, channelId, ids); err != nil {
		return nil, nil, 0, false, err
	}
	return dialogs, messages, count, more, nil
}

// SublistHistory is a page of the sublist peerId of the monoforum, for an
// admin of its direct messages (messages.getSavedHistory with parent_peer),
// paged as History.
func (d *Dao) SublistHistory(ctx context.Context, selfId, channelId, peerId int64, offsetId, offsetDate, addOffset, limit, maxId, minId int32, now int64) ([]*mtproto.Message, int32, error) {
	if _, err := monoforumAdmin(monoforum.OnDB(ctx, d.Mysql.DB), channelId, selfId, now); err != nil {
		return nil, 0, err
	}
	return d.page(ctx, selfId, channelId, pageQuery{where: " AND saved_peer_id = ?", args: []any{peerId}}, offsetId, offsetDate, addOffset, limit, maxId, minId)
}

// MessageAuthor is who wrote the message id of the monoforum as the channel
// (channels.getMessageAuthor): for the admins of its direct messages alone,
// and of the channel's messages alone; a subscriber's message names its
// author itself. MSG_ID_INVALID for any other message.
func (d *Dao) MessageAuthor(ctx context.Context, selfId, channelId int64, id int32, now int64) (int64, error) {
	q := monoforum.OnDB(ctx, d.Mysql.DB)
	side, err := monoforum.SideOf(q, channelId)
	if err != nil {
		return 0, err
	}
	if !side.Monoforum {
		return 0, mtproto.ErrChannelInvalid
	}
	m, err := monoforum.MemberOf(q, side.LinkedId, selfId, now)
	if err != nil {
		return 0, err
	}
	if !m.Admin {
		return 0, mtproto.ErrChatAdminRequired
	}
	var authors []int64
	if err = d.Mysql.QueryRowsPartial(ctx, &authors,
		"SELECT sender_user_id FROM channel_messages WHERE channel_id = ? AND id = ? AND deleted = 0 AND send_as_id <> 0", channelId, id); err != nil {
		return 0, err
	}
	if len(authors) == 0 {
		return 0, mtproto.ErrMsgIdInvalid
	}
	return authors[0], nil
}

// deleteMonoforum is Delete in a monoforum: an admin of the direct messages
// deletes any message of it, a subscriber its own in its own sublist
// (MESSAGE_DELETE_FORBIDDEN for the channel's answers; a message of another
// sublist does not exist for it and is left out). The sublists that lost
// their last message move back, an emptied one leaves the list.
func (d *Dao) deleteMonoforum(b *userbox.Box, channelId int64, side monoforum.Side, ids []int32) (*Deleted, error) {
	ch, err := openChannel(b, channelId)
	if err != nil {
		return nil, err
	}
	m, err := monoforum.MemberOf(b.Tx(), side.LinkedId, b.UserId(), b.Date())
	if err != nil {
		return nil, err
	}
	if !m.Access {
		return nil, mtproto.ErrChannelPrivate
	}
	out := &Deleted{ChannelId: channelId}
	if len(ids) == 0 {
		return out, nil
	}
	args := []any{channelId}
	for _, id := range ids {
		args = append(args, id)
	}
	var rows []struct {
		Id          int32 `db:"id"`
		SavedPeerId int64 `db:"saved_peer_id"`
		SendAsId    int64 `db:"send_as_id"`
	}
	if err = b.Tx().QueryRowsPartial(&rows,
		"SELECT id, saved_peer_id, send_as_id FROM channel_messages WHERE channel_id = ? AND deleted = 0 AND id IN (?"+strings.Repeat(", ?", len(ids)-1)+") ORDER BY id FOR UPDATE",
		args...); err != nil {
		return nil, err
	}
	var doomed []int32
	var peers []int64
	for _, r := range rows {
		if !m.Admin {
			if r.SavedPeerId != b.UserId() {
				continue
			}
			if r.SendAsId != 0 {
				return nil, mtproto.ErrMessageDeleteForbidden
			}
		}
		doomed = append(doomed, r.Id)
		if !slices.Contains(peers, r.SavedPeerId) {
			peers = append(peers, r.SavedPeerId)
		}
	}
	return d.deleteSublistRows(b, ch, channelId, doomed, peers)
}

// deleteSublistRows deletes the messages ids of the locked monoforum, which
// exist and are in the sublists peers, and moves these sublists back.
func (d *Dao) deleteSublistRows(b *userbox.Box, ch *channelbox.Channel, channelId int64, ids []int32, peers []int64) (*Deleted, error) {
	out := &Deleted{ChannelId: channelId, Ids: ids}
	if len(ids) == 0 {
		return out, nil
	}
	if err := removeMessages(b, ch, out); err != nil {
		return nil, err
	}
	args := []any{channelId}
	for _, id := range ids {
		args = append(args, id)
	}
	if _, err := b.Tx().Exec("DELETE FROM channel_message_reactions WHERE channel_id = ? AND message_id IN (?"+strings.Repeat(", ?", len(ids)-1)+")", args...); err != nil {
		return nil, err
	}
	InvalidateDialog(b, channelId)
	return out, monoforum.Retop(b.Tx(), channelId, peers)
}

// SublistDeleted is one portion of DeleteSublistHistory.
type SublistDeleted struct {
	*Deleted
	Left int32 // the messages of the range still in the sublist
}

// DeleteSublistHistory deletes for everyone the newest portion of the
// sublist peerId of the monoforum, by an admin of its direct messages
// (messages.deleteSavedHistory with parent_peer): up to maxId when above 0,
// within minDate and maxDate when given. Left tells how many remain.
func (d *Dao) DeleteSublistHistory(b *userbox.Box, channelId, peerId int64, maxId, minDate, maxDate int32) (*SublistDeleted, error) {
	ch, err := openChannel(b, channelId)
	if err != nil {
		return nil, err
	}
	if _, err = monoforumAdmin(b.Tx(), channelId, b.UserId(), b.Date()); err != nil {
		return nil, err
	}
	where := " FROM channel_messages WHERE channel_id = ? AND saved_peer_id = ? AND deleted = 0"
	args := []any{channelId, peerId}
	if maxId > 0 {
		where += " AND id <= ?"
		args = append(args, maxId)
	}
	if minDate > 0 {
		where += " AND date2 >= ?"
		args = append(args, minDate)
	}
	if maxDate > 0 {
		where += " AND date2 <= ?"
		args = append(args, maxDate)
	}
	var ids []int32
	if err = b.Tx().QueryRowsPartial(&ids, "SELECT id"+where+" ORDER BY id DESC LIMIT ? FOR UPDATE", append(slices.Clone(args), DeleteMax)...); err != nil {
		return nil, err
	}
	slices.Sort(ids)
	del, err := d.deleteSublistRows(b, ch, channelId, ids, []int64{peerId})
	if err != nil {
		return nil, err
	}
	var left []int32
	if err = b.Tx().QueryRowsPartial(&left, "SELECT COUNT(*)"+where, args...); err != nil {
		return nil, err
	}
	return &SublistDeleted{Deleted: del, Left: first(left)}, nil
}

// monoforumDialog is the dialog of selfId with the monoforum channelId
// (ChannelDialogs), from its row of dialogs read:
//   - a subscriber's is its sublist: the last message of it, what the
//     channel wrote above its mark, and how far the admins read;
//   - an admin's is all the sublists: unread is what the subscribers wrote
//     that the admins have not read, and the mark is where the first of it
//     begins, so that nothing below it is unread.
func (d *Dao) monoforumDialog(ctx context.Context, selfId, channelId int64, r channelDialogRow, admin bool) (*mtproto.Dialog, error) {
	out := &mtproto.Dialog{
		Peer:      mtproto.MakePeerChannel(channelId),
		Pts:       mtproto.MakeFlagsInt32(r.Pts),
		TtlPeriod: mtproto.MakeFlagsInt32(r.TtlPeriod),
	}
	if !admin {
		var top, unread, reactions []int32
		if err := d.Mysql.QueryRowsPartial(ctx, &top,
			"SELECT COALESCE(MAX(id), 0) FROM channel_messages WHERE channel_id = ? AND saved_peer_id = ? AND deleted = 0", channelId, selfId); err != nil {
			return nil, err
		}
		if err := d.Mysql.QueryRowsPartial(ctx, &unread, subscriberUnreadQuery, channelId, selfId, r.ReadInboxMaxId); err != nil {
			return nil, err
		}
		if err := d.Mysql.QueryRowsPartial(ctx, &reactions, unreadReactionsQuery+" AND m.saved_peer_id = ?", channelId, selfId, 0, selfId); err != nil {
			return nil, err
		}
		s, _, err := monoforum.Sublist(monoforum.OnDB(ctx, d.Mysql.DB), channelId, selfId)
		if err != nil {
			return nil, err
		}
		out.TopMessage = first(top)
		out.ReadInboxMaxId = min(r.ReadInboxMaxId, first(top))
		out.ReadOutboxMaxId = s.ReadInboxMaxId
		out.UnreadCount = first(unread)
		out.UnreadReactionsCount = first(reactions)
		return mtproto.MakeTLDialog(out).To_Dialog(), nil
	}
	var rows []struct {
		Unread     int32 `db:"unread"`
		FirstAbove int32 `db:"first_above"`
		Outbox     int32 `db:"outbox"`
	}
	if err := d.Mysql.QueryRowsPartial(ctx, &rows,
		"SELECT COALESCE(SUM(u.n), 0) AS unread, COALESCE(MIN(IF(u.n > 0, u.read_inbox_max_id, NULL)), -1) AS first_above,"+
			" COALESCE(MAX(u.read_outbox_max_id), 0) AS outbox FROM ("+
			"SELECT s.read_inbox_max_id, s.read_outbox_max_id, (SELECT COUNT(*) FROM channel_messages m"+
			" WHERE m.channel_id = s.channel_id AND m.saved_peer_id = s.peer_id AND m.id > s.read_inbox_max_id AND m.send_as_id = 0 AND m.deleted = 0) AS n"+
			" FROM channel_monoforum_sublists s WHERE s.channel_id = ?) u", channelId); err != nil {
		return nil, err
	}
	var reactions []int32
	if err := d.Mysql.QueryRowsPartial(ctx, &reactions, unreadReactionsQuery, channelId, selfId, 0); err != nil {
		return nil, err
	}
	out.TopMessage = r.TopMessage
	out.ReadInboxMaxId = r.TopMessage
	if len(rows) > 0 {
		out.UnreadCount = rows[0].Unread
		out.ReadOutboxMaxId = rows[0].Outbox
		if rows[0].FirstAbove >= 0 {
			out.ReadInboxMaxId = min(rows[0].FirstAbove, r.TopMessage)
		}
	}
	out.UnreadReactionsCount = first(reactions)
	return mtproto.MakeTLDialog(out).To_Dialog(), nil
}

// monoforumTop is the last message selfId sees of the channel when it is a
// monoforum (TopMessages): of its own sublist for a subscriber, of them all
// for an admin of the direct messages; nil for none, or for one who may not
// open it.
func (d *Dao) monoforumTop(ctx context.Context, selfId, channelId int64) (*mtproto.Message, bool, error) {
	side, err := monoforum.SideOf(monoforum.OnDB(ctx, d.Mysql.DB), channelId)
	if err != nil || !side.Monoforum {
		return nil, false, err
	}
	v, err := d.viewerOf(ctx, selfId, channelId)
	if err != nil || !v.canRead {
		return nil, true, err
	}
	own, args := v.own("saved_peer_id")
	var top []int32
	if err = d.Mysql.QueryRowsPartial(ctx, &top,
		"SELECT COALESCE(MAX(id), 0) FROM channel_messages WHERE channel_id = ? AND deleted = 0"+own, append([]any{channelId}, args...)...); err != nil {
		return nil, true, err
	}
	if first(top) == 0 {
		return nil, true, nil
	}
	messages, err := d.Messages(ctx, selfId, channelId, []int32{first(top)})
	if err != nil || len(messages) == 0 {
		return nil, true, err
	}
	return messages[0], true, nil
}

// HideMonoforum is channels.leaveChannel of a monoforum, which nobody is in:
// the chat with the channel leaves the dialogs of b's owner, its messages
// and marks stay. tdesktop offers "Leave group" for it, the one way to
// remove it there, and removes the dialog itself before the answer
// (window_peer_menu.cpp:882-911, apiwrap.cpp:2287-2291, 2310-2311). The next
// message of the sublist brings it back (msg showToSublistSides), as the
// subscriber's own next message does. PARENT_PEER_INVALID for a channel
// that is no monoforum.
func (d *Dao) HideMonoforum(b *userbox.Box, channelId int64) error {
	side, err := monoforum.SideOf(b.Tx(), channelId)
	if err != nil {
		return err
	}
	if !side.Monoforum {
		return ErrParentPeerInvalid
	}
	if _, err = b.Tx().Exec("UPDATE dialogs SET deleted = 1 WHERE user_id = ? AND peer_type = ? AND peer_id = ?",
		b.UserId(), mtproto.PEER_CHANNEL, channelId); err != nil {
		return err
	}
	InvalidateDialog(b, channelId)
	return nil
}
