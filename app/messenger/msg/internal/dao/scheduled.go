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
	"errors"
	"slices"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/pkg/channelperm"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
	"google.golang.org/protobuf/proto"
)

// Scheduled messages (task 061). A row of scheduled_messages is a message
// waiting in a schedule queue. It leaves the table in the very transaction
// that sends it (ErrScheduledGone), so a message is sent once whatever
// happens to the worker that sends it.

const (
	// ScheduleWhenOnline is the schedule_date of a message waiting for the
	// peer to come online (core.telegram.org/api/scheduled-messages;
	// tdesktop api_common.h kScheduledUntilOnlineTimestamp).
	ScheduleWhenOnline int32 = 0x7FFFFFFE
	// ScheduledQueueLimit is how many messages one queue holds: the limit
	// the clients name to the user (tdesktop lng_error_schedule_limit,
	// Android MessageScheduledLimitReached).
	ScheduledQueueLimit = 100
	// ScheduledLease is how long a worker holds rows it took: longer than a
	// delivery can take (MaxRequestTime, and the RPCs before it), so a
	// lease runs out only after its worker is gone.
	ScheduledLease = 2 * time.Minute
)

// ErrScheduledGone: a row was taken by someone else after it was read (sent
// or deleted): the operation changes nothing.
var ErrScheduledGone = errors.New("msg: scheduled message gone")

// ScheduledQueue names a schedule queue.
type ScheduledQueue struct {
	UserId   int64 // the sender; 0 for the queue shared by a broadcast channel
	PeerType int32
	PeerId   int64
}

// Broadcast tells the queue of a broadcast channel.
func (q ScheduledQueue) Broadcast() bool { return q.UserId == 0 }

// ScheduledQueueOf is the queue of the dialog as userId uses it: each
// sender has its own but in a broadcast channel, whose admins share one
// (Android MessageObject.canEditMessageScheduleTime: another admin's
// scheduled post is told by message.out; a supergroup's are all one's own).
func ScheduledQueueOf(userId int64, peerType int32, peerId int64, broadcast bool) ScheduledQueue {
	if peerType == mtproto.PEER_CHANNEL && broadcast {
		return ScheduledQueue{PeerType: peerType, PeerId: peerId}
	}
	return ScheduledQueue{UserId: userId, PeerType: peerType, PeerId: peerId}
}

// ScheduledRow is a waiting message.
type ScheduledRow struct {
	Id           int32  `db:"id"`
	QueueUserId  int64  `db:"queue_user_id"`
	PeerType     int32  `db:"peer_type"`
	PeerId       int64  `db:"peer_id"`
	SenderUserId int64  `db:"sender_user_id"`
	RandomId     int64  `db:"random_id"`
	GroupedId    int64  `db:"grouped_id"`
	ScheduleDate int32  `db:"schedule_date"`
	MessageData  string `db:"message_data"`
	ClaimUntil   int64  `db:"claim_until"`
}

// Queue is the queue the row is in.
func (r *ScheduledRow) Queue() ScheduledQueue {
	return ScheduledQueue{UserId: r.QueueUserId, PeerType: r.PeerType, PeerId: r.PeerId}
}

// Message decodes the message of a row, with its queue id and date.
func (r *ScheduledRow) Message() (*mtproto.Message, error) {
	var m mtproto.Message
	if err := jsonx.UnmarshalFromString(r.MessageData, &m); err != nil {
		return nil, err
	}
	m.Id = r.Id
	m.Date = r.ScheduleDate
	return m.FixData(), nil
}

const scheduledColumns = "id, queue_user_id, peer_type, peer_id, sender_user_id, random_id, grouped_id, schedule_date, message_data, claim_until"

func idArgs(ids []int32, args ...any) []any {
	for _, id := range ids {
		args = append(args, id)
	}
	return args
}

// byIds orders rows as ids asks, leaving out the ids without a row.
func byIds(rows []*ScheduledRow, ids []int32) []*ScheduledRow {
	out := make([]*ScheduledRow, 0, len(rows))
	for _, id := range ids {
		if i := slices.IndexFunc(rows, func(r *ScheduledRow) bool { return r.Id == id }); i >= 0 {
			out = append(out, rows[i])
		}
	}
	return out
}

// SortScheduled orders rows newest first, as the queue goes to clients: by
// date, then id. Android and tdesktop count the queue's hash in that order
// (tdesktop scheduled_messages.cpp countListHash).
func SortScheduled(rows []*ScheduledRow) {
	slices.SortFunc(rows, func(a, b *ScheduledRow) int {
		if a.ScheduleDate != b.ScheduleDate {
			return int(b.ScheduleDate) - int(a.ScheduleDate)
		}
		return int(b.Id) - int(a.Id)
	})
}

// ScheduledHash is the hash of the queue as clients count it
// (core.telegram.org/method/messages.getScheduledHistory): for each message
// in order its id, edit_date (0 when never edited) and date. edit_date is
// never set on a waiting message; an edit shows as edit_hide (tdesktop
// scheduled_messages.cpp, ScheduledMessages::append).
func ScheduledHash(rows []*ScheduledRow) int64 {
	var h uint64
	update := func(v int64) {
		h ^= h >> 21
		h ^= h << 35
		h ^= h >> 4
		h += uint64(v)
	}
	for _, r := range rows {
		update(int64(r.Id))
		update(0)
		update(int64(r.ScheduleDate))
	}
	return int64(h)
}

// channelMember reads userId as a participant of the channel, for a queue
// read outside a box transaction. A channel gone is CHANNEL_INVALID.
func (d *Dao) channelMember(ctx context.Context, userId, channelId int64) (*sender, error) {
	var rows []senderRow
	if err := d.Mysql.DB.QueryRowsPartial(ctx, &rows,
		"SELECT c.broadcast, c.deleted, c.default_banned_rights, c.signatures, COALESCE(p.role, 0) AS role, COALESCE(p.state, ?) AS state,"+
			" COALESCE(p.admin_rights, 0) AS admin_rights, COALESCE(p.banned_rights, 0) AS banned_rights, COALESCE(p.until_date, 0) AS until_date"+
			" FROM channels c LEFT JOIN channel_participants p ON p.channel_id = c.id AND p.user_id = ? WHERE c.id = ?",
		channelperm.StateLeft, userId, channelId); err != nil {
		return nil, err
	}
	if len(rows) == 0 || rows[0].Deleted {
		return nil, mtproto.ErrChannelInvalid
	}
	r := rows[0]
	return &sender{
		ch: channelperm.Channel{Broadcast: r.Broadcast, Default: r.DefaultBannedRights},
		p: channelperm.Participant{Role: r.Role, State: r.State, AdminRights: r.AdminRights,
			BannedRights: r.BannedRights, UntilDate: r.UntilDate},
	}, nil
}

// ScheduledQueueFor is the queue userId reads in the dialog, with the right
// to read it: the queue of a broadcast channel is for those who may post to
// it (tdesktop ScheduledMessages::request asks only when
// Data::CanSendAnything; core.telegram.org/method/messages.getScheduledHistory
// lists CHAT_ADMIN_REQUIRED). Anyone's own queue is its own to read.
func (d *Dao) ScheduledQueueFor(ctx context.Context, userId int64, peerType int32, peerId int64) (ScheduledQueue, error) {
	if peerType != mtproto.PEER_CHANNEL {
		return ScheduledQueueOf(userId, peerType, peerId, false), nil
	}
	s, err := d.channelMember(ctx, userId, peerId)
	if err != nil {
		return ScheduledQueue{}, err
	}
	if s.ch.Broadcast {
		if err = s.can(channelperm.SendPlain, time.Now().Unix()); err != nil {
			return ScheduledQueue{}, mtproto.ErrChatAdminRequired
		}
	}
	return ScheduledQueueOf(userId, peerType, peerId, s.ch.Broadcast), nil
}

// ScheduledBroadcastAdmins are the admins of the broadcast channel who may
// post to it: those the shared queue is for. A channel gone has none.
func (d *Dao) ScheduledBroadcastAdmins(ctx context.Context, channelId int64) ([]int64, error) {
	var rows []struct {
		UserId      int64 `db:"user_id"`
		Role        int32 `db:"role"`
		AdminRights int32 `db:"admin_rights"`
	}
	if err := d.Mysql.DB.QueryRowsPartial(ctx, &rows,
		"SELECT p.user_id, p.role, p.admin_rights FROM channel_participants p JOIN channels c ON c.id = p.channel_id"+
			" WHERE p.channel_id = ? AND p.state = ? AND p.role >= ? AND c.broadcast = 1 AND c.deleted = 0",
		channelId, channelperm.StateIn, channelperm.RoleAdmin); err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(rows))
	for _, r := range rows {
		if r.Role == channelperm.RoleCreator || r.AdminRights&mtproto.ADMIN_POST_MESSAGES != 0 {
			out = append(out, r.UserId)
		}
	}
	slices.Sort(out)
	return out, nil
}

// LoadScheduledQueue returns the messages waiting in the queue (at most
// ScheduledQueueLimit), newest first.
func (d *Dao) LoadScheduledQueue(ctx context.Context, q ScheduledQueue) ([]*ScheduledRow, error) {
	var rows []*ScheduledRow
	if err := d.Mysql.DB.QueryRowsPartial(ctx, &rows,
		"SELECT "+scheduledColumns+" FROM scheduled_messages WHERE queue_user_id = ? AND peer_type = ? AND peer_id = ? ORDER BY schedule_date DESC, id DESC LIMIT ?",
		q.UserId, q.PeerType, q.PeerId, ScheduledQueueLimit); err != nil {
		return nil, err
	}
	return rows, nil
}

// LoadScheduledById returns the messages of the queue with ids, in that
// order, leaving out those it does not hold.
func (d *Dao) LoadScheduledById(ctx context.Context, q ScheduledQueue, ids []int32) ([]*ScheduledRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []*ScheduledRow
	if err := d.Mysql.DB.QueryRowsPartial(ctx, &rows,
		"SELECT "+scheduledColumns+" FROM scheduled_messages WHERE queue_user_id = ? AND peer_type = ? AND peer_id = ? AND id IN ("+inList(len(ids))+")",
		idArgs(ids, q.UserId, q.PeerType, q.PeerId)...); err != nil {
		return nil, err
	}
	return byIds(rows, ids), nil
}

// HasScheduled tells whether the queue holds a message.
func (d *Dao) HasScheduled(ctx context.Context, q ScheduledQueue) (bool, error) {
	var ids []int32
	if err := d.Mysql.DB.QueryRowsPartial(ctx, &ids,
		"SELECT id FROM scheduled_messages WHERE queue_user_id = ? AND peer_type = ? AND peer_id = ? LIMIT 1", q.UserId, q.PeerType, q.PeerId); err != nil {
		return false, err
	}
	return len(ids) > 0, nil
}

// ScheduledBefore returns the waiting messages b's owner already scheduled
// with randomIds, all of them, in order, or nil when it scheduled none of
// them: a schedule repeated by a client that lost the answer gets the first
// answer, a partial repeat is RANDOM_ID_DUPLICATE (as dao.SentBefore).
func (d *Dao) ScheduledBefore(b *userbox.Box, randomIds []int64) ([]*ScheduledRow, error) {
	if len(randomIds) == 0 {
		return nil, nil
	}
	args := make([]any, 0, len(randomIds)+1)
	args = append(args, b.UserId())
	for _, id := range randomIds {
		args = append(args, id)
	}
	var rows []*ScheduledRow
	if err := b.Tx().QueryRowsPartial(&rows,
		"SELECT "+scheduledColumns+" FROM scheduled_messages WHERE sender_user_id = ? AND out_random_id IN ("+inList(len(randomIds))+")",
		args...); err != nil {
		return nil, err
	}
	switch {
	case len(rows) == 0:
		return nil, nil
	case len(rows) != len(randomIds):
		return nil, mtproto.ErrRandomIdDuplicate
	}
	out := make([]*ScheduledRow, 0, len(rows))
	for _, id := range randomIds {
		if i := slices.IndexFunc(rows, func(r *ScheduledRow) bool { return r.RandomId == id }); i >= 0 {
			out = append(out, rows[i])
		}
	}
	return out, nil
}

// InsertScheduled writes messages to the queue with date, in the sender's
// box transaction (for a broadcast channel with the channel locked): the
// queue cannot go past ScheduledQueueLimit, SCHEDULE_TOO_MUCH otherwise.
// It returns the rows in order, with their ids.
func (d *Dao) InsertScheduled(b *userbox.Box, q ScheduledQueue, date int32, msgs []Outgoing) ([]*ScheduledRow, error) {
	var count []int
	if err := b.Tx().QueryRowsPartial(&count,
		"SELECT COUNT(*) FROM scheduled_messages WHERE queue_user_id = ? AND peer_type = ? AND peer_id = ?", q.UserId, q.PeerType, q.PeerId); err != nil {
		return nil, err
	}
	if len(count) > 0 && count[0]+len(msgs) > ScheduledQueueLimit {
		return nil, mtproto.ErrScheduleTooMuch
	}
	rows := make([]*ScheduledRow, 0, len(msgs))
	for _, m := range msgs {
		// The id and the date of a waiting message are the row's columns.
		data, err := jsonx.MarshalToString(m.Message)
		if err != nil {
			return nil, err
		}
		res, err := b.Tx().Exec("INSERT INTO scheduled_messages (queue_user_id, peer_type, peer_id, sender_user_id, random_id, grouped_id, schedule_date, message_data, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
			q.UserId, q.PeerType, q.PeerId, b.UserId(), m.RandomId, m.Message.GetGroupedId().GetValue(), date, data, b.Date())
		if err != nil {
			return nil, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		rows = append(rows, &ScheduledRow{Id: int32(id), QueueUserId: q.UserId, PeerType: q.PeerType, PeerId: q.PeerId,
			SenderUserId: b.UserId(), RandomId: m.RandomId, GroupedId: m.Message.GetGroupedId().GetValue(), ScheduleDate: date, MessageData: data})
	}
	return rows, nil
}

// LockScheduled locks and returns the rows of the queue with ids, in that
// order, leaving out those it does not hold.
func (d *Dao) LockScheduled(b *userbox.Box, q ScheduledQueue, ids []int32) ([]*ScheduledRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []*ScheduledRow
	if err := b.Tx().QueryRowsPartial(&rows,
		"SELECT "+scheduledColumns+" FROM scheduled_messages WHERE queue_user_id = ? AND peer_type = ? AND peer_id = ? AND id IN ("+inList(len(ids))+") FOR UPDATE",
		idArgs(ids, q.UserId, q.PeerType, q.PeerId)...); err != nil {
		return nil, err
	}
	return byIds(rows, ids), nil
}

// DeleteScheduled deletes the rows with ids. Fewer rows than ids is
// ErrScheduledGone: another worker or an admin took them first, and the
// transaction must not go on.
func (d *Dao) DeleteScheduled(b *userbox.Box, ids []int32) error {
	if len(ids) == 0 {
		return nil
	}
	res, err := b.Tx().Exec("DELETE FROM scheduled_messages WHERE id IN ("+inList(len(ids))+")", idArgs(ids)...)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != int64(len(ids)) {
		return ErrScheduledGone
	}
	return nil
}

// EditScheduled replaces the message and the date of a row.
func (d *Dao) EditScheduled(b *userbox.Box, id, date int32, m *mtproto.Message) error {
	data, err := jsonx.MarshalToString(m)
	if err != nil {
		return err
	}
	_, err = b.Tx().Exec("UPDATE scheduled_messages SET schedule_date = ?, message_data = ? WHERE id = ?", date, data, id)
	return err
}

// ScheduledOp is what is about to be done to waiting messages.
type ScheduledOp int

const (
	ScheduledOpEdit ScheduledOp = iota
	ScheduledOpDelete
	ScheduledOpSend
)

// LockScheduledFor locks the rows of b's owner's queue of the dialog with
// ids, checking that the owner may do op to each: a queue of its own is its
// own to change; in a broadcast channel, an admin who may post changes its
// own posts, and edit_messages or delete_messages let it change others'
// (Android MessageObject.canEditMessageScheduleTime and tdesktop
// HistoryItem::canBeEdited/canDelete; sending now counts as an edit). The
// channel is locked first, as a send locks it.
func (d *Dao) LockScheduledFor(b *userbox.Box, peerType int32, peerId int64, ids []int32, op ScheduledOp) (ScheduledQueue, []*ScheduledRow, error) {
	if peerType != mtproto.PEER_CHANNEL {
		q := ScheduledQueueOf(b.UserId(), peerType, peerId, false)
		rows, err := d.LockScheduled(b, q, ids)
		return q, rows, err
	}
	_, s, err := openChannel(b, peerId)
	if err != nil {
		return ScheduledQueue{}, nil, err
	}
	q := ScheduledQueueOf(b.UserId(), peerType, peerId, s.ch.Broadcast)
	rows, err := d.LockScheduled(b, q, ids)
	if err != nil {
		return ScheduledQueue{}, nil, err
	}
	if !s.ch.Broadcast {
		return q, rows, nil
	}
	others := channelperm.EditOthers
	if op == ScheduledOpDelete {
		others = channelperm.DeleteOthers
	}
	for _, r := range rows {
		action := channelperm.SendPlain
		if r.SenderUserId != b.UserId() {
			action = others
		}
		if err = s.can(action, b.Date()); err != nil {
			return ScheduledQueue{}, nil, err
		}
	}
	return q, rows, nil
}

// ApplyScheduledEdit is stored with the editable parts of edit and date. A
// waiting message has no edit date: an edit shows as edit_hide (tdesktop
// scheduled_messages.cpp, ScheduledMessages::append).
func ApplyScheduledEdit(stored, edit *mtproto.Message, date int32) *mtproto.Message {
	stored = applyEdit(stored, edit)
	stored.EditDate = nil
	stored.EditHide = true
	stored.Date = date
	return stored
}

// CheckChannelSchedule checks, with the channel locked, that b's owner may
// send the messages to the channel: scheduling needs what sending needs.
func (d *Dao) CheckChannelSchedule(b *userbox.Box, channelId int64, list []*msg.OutboxMessage) error {
	_, s, err := openChannel(b, channelId)
	if err != nil {
		return err
	}
	for _, o := range list {
		if err = checkSend(s, proto.Clone(o.GetMessage()).(*mtproto.Message), b.Date()); err != nil {
			return err
		}
	}
	return nil
}

// UnclaimedDue returns up to limit rows free at now whose time came.
func (d *Dao) UnclaimedDue(ctx context.Context, now int64, limit int) ([]*ScheduledRow, error) {
	var rows []*ScheduledRow
	if err := d.Mysql.DB.QueryRowsPartial(ctx, &rows,
		"SELECT "+scheduledColumns+" FROM scheduled_messages WHERE schedule_date <= ? AND claim_until <= ? ORDER BY schedule_date, id LIMIT ?",
		now, now, limit); err != nil {
		return nil, err
	}
	return rows, nil
}

// UnclaimedWhenOnline returns up to limit rows after afterId, free at now,
// that wait for their peer to come online. Those rows stay for as long as
// their peers are away, so a reader pages through them by id: otherwise the
// oldest would hide the rest.
func (d *Dao) UnclaimedWhenOnline(ctx context.Context, now int64, afterId int32, limit int) ([]*ScheduledRow, error) {
	var rows []*ScheduledRow
	if err := d.Mysql.DB.QueryRowsPartial(ctx, &rows,
		"SELECT "+scheduledColumns+" FROM scheduled_messages WHERE schedule_date = ? AND claim_until <= ? AND id > ? ORDER BY id LIMIT ?",
		ScheduleWhenOnline, now, afterId, limit); err != nil {
		return nil, err
	}
	return rows, nil
}

// UnclaimedAlbum returns the free rows at now of r's album: every message of
// the album whose time came, r's date for one waiting for the peer.
func (d *Dao) UnclaimedAlbum(ctx context.Context, r *ScheduledRow, now int64) ([]*ScheduledRow, error) {
	var rows []*ScheduledRow
	dateCond := "schedule_date = ?"
	date := int64(r.ScheduleDate)
	if r.ScheduleDate != ScheduleWhenOnline {
		dateCond, date = "schedule_date <= ?", now
	}
	if err := d.Mysql.DB.QueryRowsPartial(ctx, &rows,
		"SELECT "+scheduledColumns+" FROM scheduled_messages WHERE queue_user_id = ? AND peer_type = ? AND peer_id = ? AND sender_user_id = ? AND grouped_id = ? AND claim_until <= ? AND "+dateCond+" ORDER BY id",
		r.QueueUserId, r.PeerType, r.PeerId, r.SenderUserId, r.GroupedId, now, date); err != nil {
		return nil, err
	}
	return rows, nil
}

// ClaimScheduled takes the rows with ids for a worker until until, if all of
// them are free at now: true means this worker delivers them. A worker that
// dies leaves the rows to be taken again when the lease runs out; the send
// transaction deletes the rows, so a second delivery finds them gone.
func (d *Dao) ClaimScheduled(ctx context.Context, ids []int32, now, until int64) (bool, error) {
	if len(ids) == 0 {
		return false, nil
	}
	res, err := d.Mysql.DB.Exec(ctx, "UPDATE scheduled_messages SET claim_until = ? WHERE claim_until <= ? AND id IN ("+inList(len(ids))+")",
		idArgs(ids, until, now)...)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == int64(len(ids)), nil
}

// LoadScheduledRows returns the rows with ids, in that order, leaving out
// those gone: what a worker reads again once it holds them.
func (d *Dao) LoadScheduledRows(ctx context.Context, ids []int32) ([]*ScheduledRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []*ScheduledRow
	if err := d.Mysql.DB.QueryRowsPartial(ctx, &rows,
		"SELECT "+scheduledColumns+" FROM scheduled_messages WHERE id IN ("+inList(len(ids))+")", idArgs(ids)...); err != nil {
		return nil, err
	}
	return byIds(rows, ids), nil
}

// ReleaseScheduled gives up the lease of a worker on the rows with ids.
func (d *Dao) ReleaseScheduled(ctx context.Context, ids []int32) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := d.Mysql.DB.Exec(ctx, "UPDATE scheduled_messages SET claim_until = 0 WHERE id IN ("+inList(len(ids))+")", idArgs(ids)...)
	return err
}

// NewScheduledUpdates are updateNewScheduledMessage with the message as
// viewer sees it.
func NewScheduledUpdates(msgs []*mtproto.Message) []*mtproto.Update {
	out := make([]*mtproto.Update, len(msgs))
	for i, m := range msgs {
		out[i] = mtproto.MakeTLUpdateNewScheduledMessage(&mtproto.Update{Message_MESSAGE: m}).To_Update()
	}
	return out
}

// DeleteScheduledUpdate is updateDeleteScheduledMessages of the ids of a
// queue. sent are the ids of the sent messages, index by index with ids
// (core.telegram.org/constructor/updateDeleteScheduledMessages); nil when
// the messages were only deleted.
func DeleteScheduledUpdate(peer *mtproto.Peer, ids, sent []int32) *mtproto.Update {
	return mtproto.MakeTLUpdateDeleteScheduledMessages(&mtproto.Update{
		Peer_PEER:    peer,
		Messages:     ids,
		SentMessages: sent,
	}).To_Update()
}

// UpdatesOf is an updates container of us alone.
func UpdatesOf(us []*mtproto.Update, users []*mtproto.User, chats []*mtproto.Chat, now int32) *mtproto.Updates {
	return mtproto.MakeTLUpdates(&mtproto.Updates{Updates: us, Users: users, Chats: chats, Date: now}).To_Updates()
}

// EmitToDevices pushes updates to userId's devices, but the one on
// permAuthKeyId, which got them in its answer (0: every device).
func (d *Dao) EmitToDevices(b *userbox.Box, userId, permAuthKeyId int64, updates *mtproto.Updates) error {
	if permAuthKeyId != 0 {
		return d.EmitUpdatesNotMe(b, userId, permAuthKeyId, updates)
	}
	return d.EmitPushUpdates(b, userId, false, updates)
}

// ScheduledIds are the queue ids of rows, in order.
func ScheduledIds(rows []*ScheduledRow) []int32 {
	ids := make([]int32, len(rows))
	for i, r := range rows {
		ids[i] = r.Id
	}
	return ids
}
