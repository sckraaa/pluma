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

package business

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
)

// The automatic replies of Telegram Business (task 105, part 5; ADR 0067):
// the greeting (account.updateBusinessGreetingMessage) and the away
// message (account.updateBusinessAwayMessage). Each sends a quick reply
// shortcut of the account, "hello" and "away" in the clients (Android
// ui/Business/QuickRepliesController.java:31-32, tdesktop
// settings/business/settings_shortcut_messages.cpp:319-323), on the
// account's behalf to a private chat whose incoming message triggers it.
// The decision is taken in the box transaction of the account that takes
// the message (the inbox of the service msg), which queues a row of
// business_auto_replies; the worker of msg sends it.

// AutoReplyKind is the kind of an automatic reply, as kept.
type AutoReplyKind int8

const (
	KindGreeting AutoReplyKind = 1
	KindAway     AutoReplyKind = 2
)

// String is the kind as a label of the metrics.
func (k AutoReplyKind) String() string {
	switch k {
	case KindGreeting:
		return "greeting"
	case KindAway:
		return "away"
	}
	return "unknown"
}

// AwaySchedule is when an away message is on (businessAwayMessageSchedule*).
type AwaySchedule int8

const (
	AwayAlways           AwaySchedule = 1
	AwayOutsideWorkHours AwaySchedule = 2
	AwayCustom           AwaySchedule = 3
)

const (
	// OfflineOnlyWindow: an away message with offline_only is not sent
	// "if the account was online in the last 10 minutes"
	// (core.telegram.org/constructor/inputBusinessAwayMessage).
	OfflineOnlyWindow = 10 * time.Minute
	// AwayRepeatInterval is the least time between two away messages to
	// the same user. Telegram publishes no rule (not verified; task 105,
	// decisions to confirm, 3): an away message goes once, again only after
	// the account wrote to the user itself, and not more often than this.
	AwayRepeatInterval = 24 * time.Hour
	// AutoReplyAttemptsMax is how many times the worker tries a queued
	// reply before it gives it up.
	AutoReplyAttemptsMax = 10
	// AutoReplyLease is how long a claim of a queued reply holds: a worker
	// that dies with a claim costs one lease.
	AutoReplyLease = 2 * time.Minute
	// ownerWroteScanMax bounds the messages of a chat read back to tell
	// whether the account wrote after its last away message: a chat with
	// more incoming messages than this since then counts as unanswered.
	ownerWroteScanMax = 500
)

// ValidNoActivityDays tells whether days is a period of inactivity of a
// greeting: 7, 14, 21 or 28 (core.telegram.org/constructor/
// inputBusinessGreetingMessage).
func ValidNoActivityDays(days int32) bool {
	return days == 7 || days == 14 || days == 21 || days == 28
}

// AutoReply is an automatic reply an account set up.
type AutoReply struct {
	UserID     int64
	Kind       AutoReplyKind
	ShortcutID int32
	Recipients Recipients
	// NoActivityDays is the greeting's: the days without messages in a
	// chat after which it greets again.
	NoActivityDays int32
	// Schedule, ScheduleStart and ScheduleEnd (unix time, AwayCustom) and
	// OfflineOnly are the away message's.
	Schedule      AwaySchedule
	ScheduleStart int32
	ScheduleEnd   int32
	OfflineOnly   bool
	// EnabledAt is unix time: when the kind was turned on, the line
	// between the existing chats and the new ones of the recipients. A
	// change of the setting keeps it; turning the kind off and on again
	// moves it.
	EnabledAt int32
}

// AwayScheduleFromTL reads s: the schedule, and its dates for AwayCustom.
// An unknown schedule, or a custom one that does not end after it starts,
// is not valid.
func AwayScheduleFromTL(s *mtproto.BusinessAwayMessageSchedule) (schedule AwaySchedule, start, end int32, ok bool) {
	switch s.GetPredicateName() {
	case mtproto.Predicate_businessAwayMessageScheduleAlways:
		return AwayAlways, 0, 0, true
	case mtproto.Predicate_businessAwayMessageScheduleOutsideWorkHours:
		return AwayOutsideWorkHours, 0, 0, true
	case mtproto.Predicate_businessAwayMessageScheduleCustom:
		if s.GetStartDate() >= s.GetEndDate() {
			return 0, 0, 0, false
		}
		return AwayCustom, s.GetStartDate(), s.GetEndDate(), true
	}
	return 0, 0, 0, false
}

// GreetingTL is a, a greeting, as businessGreetingMessage.
func (a *AutoReply) GreetingTL() *mtproto.BusinessGreetingMessage {
	return mtproto.MakeTLBusinessGreetingMessage(&mtproto.BusinessGreetingMessage{
		ShortcutId:     a.ShortcutID,
		Recipients:     a.Recipients.ToTL(),
		NoActivityDays: a.NoActivityDays,
	}).To_BusinessGreetingMessage()
}

// AwayTL is a, an away message, as businessAwayMessage.
func (a *AutoReply) AwayTL() *mtproto.BusinessAwayMessage {
	var schedule *mtproto.BusinessAwayMessageSchedule
	switch a.Schedule {
	case AwayOutsideWorkHours:
		schedule = mtproto.MakeTLBusinessAwayMessageScheduleOutsideWorkHours(nil).To_BusinessAwayMessageSchedule()
	case AwayCustom:
		schedule = mtproto.MakeTLBusinessAwayMessageScheduleCustom(&mtproto.BusinessAwayMessageSchedule{
			StartDate: a.ScheduleStart,
			EndDate:   a.ScheduleEnd,
		}).To_BusinessAwayMessageSchedule()
	default:
		schedule = mtproto.MakeTLBusinessAwayMessageScheduleAlways(nil).To_BusinessAwayMessageSchedule()
	}
	return mtproto.MakeTLBusinessAwayMessage(&mtproto.BusinessAwayMessage{
		OfflineOnly: a.OfflineOnly,
		ShortcutId:  a.ShortcutID,
		Schedule:    schedule,
		Recipients:  a.Recipients.ToTL(),
	}).To_BusinessAwayMessage()
}

// ChatHistory is what the box of an account holds of its private chat with
// a user before a message comes: the dates (unix time) of the first and the
// last message, deleted or not, when it holds any.
type ChatHistory struct {
	Any   bool
	First int32
	Last  int32
}

// IsNew tells whether the chat is a new one for a setting turned on at
// since: it held nothing before, or began at since or later. A chat that
// began earlier is an existing one (existing_chats; the docs do not say
// where the line is, task 105, decisions to confirm, 2).
func (h ChatHistory) IsNew(since int32) bool {
	return !h.Any || h.First >= since
}

// GreetingDue tells whether the greeting a goes to a chat with history h
// at now (unix time): "on the first message or after a period without
// activity", the days that "must pass since your last communication with
// the recipient" (Android res/values/strings.xml BusinessGreetInfo,
// BusinessGreetPeriodInfo; tdesktop lng_greeting_about,
// lng_greeting_period_about). Any message of the chat, either way, is
// communication.
func (a *AutoReply) GreetingDue(h ChatHistory, now int32) bool {
	return !h.Any || int64(now)-int64(h.Last) >= int64(a.NoActivityDays)*86400
}

// AwayScheduled tells whether the schedule of the away message a holds at
// now. Outside the work hours needs work hours: without them it never
// holds (not verified what Telegram does; task 105, decisions to confirm,
// 3).
func (a *AutoReply) AwayScheduled(hours *Hours, now time.Time) (bool, error) {
	switch a.Schedule {
	case AwayAlways:
		return true, nil
	case AwayOutsideWorkHours:
		if hours == nil {
			return false, nil
		}
		open, err := hours.OpenNow(now)
		if err != nil {
			return false, err
		}
		return !open, nil
	case AwayCustom:
		t := now.Unix()
		return int64(a.ScheduleStart) <= t && t < int64(a.ScheduleEnd), nil
	}
	return false, nil
}

// OwnerAway tells whether an account seen online until seenUntil (unix
// time; 0 never) has been away for OfflineOnlyWindow at now: it is not
// online and was not in the last ten minutes. seenUntil is the end of the
// last online window of the account, the was_online its contacts see
// (account.updateStatus, ADR 0030).
func OwnerAway(seenUntil int64, now time.Time) bool {
	return seenUntil <= now.Add(-OfflineOnlyWindow).Unix()
}

// AwayRepeatDue tells whether an away message goes again to a user it
// last went to at lastSent (unix time; 0 never) at now: never sent, or
// sent at least AwayRepeatInterval ago and answered by the account since
// (ownerWrote, asked only then).
func AwayRepeatDue(lastSent int32, now time.Time, ownerWrote func(since int32) (bool, error)) (bool, error) {
	if lastSent == 0 {
		return true, nil
	}
	if now.Sub(time.Unix(int64(lastSent), 0)) < AwayRepeatInterval {
		return false, nil
	}
	return ownerWrote(lastSent)
}

// ErrShortcutUnusable: the shortcut of an automatic reply is not one of
// the account's with a message to send.
var ErrShortcutUnusable = errors.New("business: the shortcut is not the account's or holds no message")

type autoReplyRow struct {
	UserID         int64  `db:"user_id"`
	Kind           int8   `db:"kind"`
	ShortcutID     int32  `db:"shortcut_id"`
	Recipients     string `db:"recipients"`
	NoActivityDays int32  `db:"no_activity_days"`
	Schedule       int8   `db:"schedule"`
	ScheduleStart  int32  `db:"schedule_start"`
	ScheduleEnd    int32  `db:"schedule_end"`
	OfflineOnly    bool   `db:"offline_only"`
	EnabledAt      int32  `db:"enabled_at"`
}

const autoReplyColumns = "user_id, kind, shortcut_id, recipients, no_activity_days, schedule, schedule_start, schedule_end, offline_only, enabled_at"

func autoReplies(rows []autoReplyRow) ([]*AutoReply, error) {
	out := make([]*AutoReply, 0, len(rows))
	for _, r := range rows {
		recipients, err := UnmarshalRecipients(r.Recipients)
		if err != nil {
			return nil, fmt.Errorf("business: the recipients of the automatic reply %d of %d: %w", r.Kind, r.UserID, err)
		}
		out = append(out, &AutoReply{
			UserID:         r.UserID,
			Kind:           AutoReplyKind(r.Kind),
			ShortcutID:     r.ShortcutID,
			Recipients:     recipients,
			NoActivityDays: r.NoActivityDays,
			Schedule:       AwaySchedule(r.Schedule),
			ScheduleStart:  r.ScheduleStart,
			ScheduleEnd:    r.ScheduleEnd,
			OfflineOnly:    r.OfflineOnly,
			EnabledAt:      r.EnabledAt,
		})
	}
	return out, nil
}

// AutoReplyOf returns the automatic reply of kind in list, nil for none.
func AutoReplyOf(list []*AutoReply, kind AutoReplyKind) *AutoReply {
	for _, a := range list {
		if a.Kind == kind {
			return a
		}
	}
	return nil
}

// AutoReplies returns the automatic replies userID turned on, by kind.
func (s *Store) AutoReplies(ctx context.Context, userID int64) ([]*AutoReply, error) {
	var rows []autoReplyRow
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT "+autoReplyColumns+" FROM business_auto_reply_settings WHERE user_id = ? ORDER BY kind", userID); err != nil {
		return nil, err
	}
	return autoReplies(rows)
}

// AutoRepliesTx is AutoReplies read in tx.
func AutoRepliesTx(tx *sqlx.Tx, userID int64) ([]*AutoReply, error) {
	var rows []autoReplyRow
	if err := tx.QueryRowsPartial(&rows, "SELECT "+autoReplyColumns+" FROM business_auto_reply_settings WHERE user_id = ? ORDER BY kind", userID); err != nil {
		return nil, err
	}
	return autoReplies(rows)
}

// SetAutoReplyTx keeps a in tx, a box transaction of a.UserID, at now
// (unix time). Its shortcut must be one of the account's with at least one
// message, else ErrShortcutUnusable; the shortcut is read in the same box
// transaction as its delete (msg.deleteQuickReplyShortcut), so no setting
// is kept on a shortcut deleted meanwhile. A kind turned on already keeps
// its EnabledAt.
func SetAutoReplyTx(tx *sqlx.Tx, a *AutoReply, now int32) error {
	var found []struct {
		ID int32 `db:"id"`
	}
	if err := tx.QueryRowsPartial(&found, "SELECT s.id FROM quick_reply_shortcuts s WHERE s.id = ? AND s.user_id = ?"+
		" AND EXISTS (SELECT 1 FROM quick_reply_messages m WHERE m.shortcut_id = s.id) FOR SHARE", a.ShortcutID, a.UserID); err != nil {
		return err
	}
	if len(found) == 0 {
		return ErrShortcutUnusable
	}
	recipients, err := MarshalRecipients(a.Recipients)
	if err != nil {
		return fmt.Errorf("business: the recipients of the automatic reply %d of %d: %w", a.Kind, a.UserID, err)
	}
	_, err = tx.Exec("INSERT INTO business_auto_reply_settings ("+autoReplyColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?) AS new"+
		" ON DUPLICATE KEY UPDATE shortcut_id = new.shortcut_id, recipients = new.recipients, no_activity_days = new.no_activity_days,"+
		" schedule = new.schedule, schedule_start = new.schedule_start, schedule_end = new.schedule_end, offline_only = new.offline_only",
		a.UserID, int8(a.Kind), a.ShortcutID, recipients, a.NoActivityDays, int8(a.Schedule), a.ScheduleStart, a.ScheduleEnd, a.OfflineOnly, now)
	return err
}

// DeleteAutoReplyTx turns the automatic reply of kind of userID off in tx,
// with its replies queued and not sent yet.
func DeleteAutoReplyTx(tx *sqlx.Tx, userID int64, kind AutoReplyKind) error {
	if _, err := tx.Exec("DELETE FROM business_auto_reply_settings WHERE user_id = ? AND kind = ?", userID, int8(kind)); err != nil {
		return err
	}
	_, err := tx.Exec("DELETE FROM business_auto_replies WHERE user_id = ? AND kind = ?", userID, int8(kind))
	return err
}

// DisableShortcutTx turns off, in tx, the automatic replies of userID that
// send its shortcut shortcutID, which is being deleted, and returns their
// kinds. Android turns them off itself before it deletes the shortcut
// (ui/Business/QuickRepliesController.java:467-482); tdesktop and iOS do
// not, and no setting may point at a shortcut that is gone.
func DisableShortcutTx(tx *sqlx.Tx, userID int64, shortcutID int32) ([]AutoReplyKind, error) {
	var kinds []int8
	if err := tx.QueryRowsPartial(&kinds, "SELECT kind FROM business_auto_reply_settings WHERE user_id = ? AND shortcut_id = ? ORDER BY kind FOR UPDATE",
		userID, shortcutID); err != nil {
		return nil, err
	}
	out := make([]AutoReplyKind, 0, len(kinds))
	for _, k := range kinds {
		if err := DeleteAutoReplyTx(tx, userID, AutoReplyKind(k)); err != nil {
			return nil, err
		}
		out = append(out, AutoReplyKind(k))
	}
	return out, nil
}

// ChatHistoryTx reads, in tx, what the box of userID holds of its private
// chat with peerID: each half of the union reads one row of the index
// (user_id, dialog_id1, dialog_id2, deleted, user_message_box_id), the
// first and the last message of either state.
func ChatHistoryTx(tx *sqlx.Tx, userID, peerID int64) (ChatHistory, error) {
	did := mtproto.MakeDialogId(userID, mtproto.PEER_USER, peerID)
	var rows []struct {
		Date2 int64 `db:"date2"`
		BoxID int32 `db:"user_message_box_id"`
	}
	const end = "(SELECT date2, user_message_box_id FROM messages" +
		" WHERE user_id = ? AND dialog_id1 = ? AND dialog_id2 = ? AND deleted = ? ORDER BY user_message_box_id %s LIMIT 1)"
	first, last := fmt.Sprintf(end, "ASC"), fmt.Sprintf(end, "DESC")
	if err := tx.QueryRowsPartial(&rows, first+" UNION ALL "+first+" UNION ALL "+last+" UNION ALL "+last,
		userID, did.A, did.B, 0, userID, did.A, did.B, 1, userID, did.A, did.B, 0, userID, did.A, did.B, 1); err != nil {
		return ChatHistory{}, err
	}
	if len(rows) == 0 {
		return ChatHistory{}, nil
	}
	lo, hi := rows[0], rows[0]
	for _, r := range rows[1:] {
		if r.BoxID < lo.BoxID {
			lo = r
		}
		if r.BoxID > hi.BoxID {
			hi = r
		}
	}
	return ChatHistory{Any: true, First: int32(lo.Date2), Last: int32(hi.Date2)}, nil
}

// OwnerWroteSinceTx tells, in tx, whether userID wrote to peerID after
// since (unix time) and the message is still in its box: the last
// ownerWroteScanMax messages of the chat are read back.
func OwnerWroteSinceTx(tx *sqlx.Tx, userID, peerID int64, since int32) (bool, error) {
	did := mtproto.MakeDialogId(userID, mtproto.PEER_USER, peerID)
	var rows []struct {
		Sender int64 `db:"sender_user_id"`
		Date2  int64 `db:"date2"`
	}
	if err := tx.QueryRowsPartial(&rows, "SELECT sender_user_id, date2 FROM messages"+
		" WHERE user_id = ? AND dialog_id1 = ? AND dialog_id2 = ? AND deleted = 0 ORDER BY user_message_box_id DESC LIMIT ?",
		userID, did.A, did.B, ownerWroteScanMax); err != nil {
		return false, err
	}
	for _, r := range rows {
		if r.Date2 <= int64(since) {
			return false, nil
		}
		if r.Sender == userID {
			return true, nil
		}
	}
	return false, nil
}

// AutoReplyLastSentTx is when the automatic reply of kind of userID last
// went to peerID (unix time), 0 for never.
func AutoReplyLastSentTx(tx *sqlx.Tx, userID, peerID int64, kind AutoReplyKind) (int32, error) {
	var sent []int32
	if err := tx.QueryRowsPartial(&sent, "SELECT sent_at FROM business_auto_reply_log WHERE user_id = ? AND peer_id = ? AND kind = ?",
		userID, peerID, int8(kind)); err != nil || len(sent) == 0 {
		return 0, err
	}
	return sent[0], nil
}

// AutoReplyQueuedTx tells whether an automatic reply of kind of userID to
// peerID waits in the queue: the next message of the chat does not queue
// another.
func AutoReplyQueuedTx(tx *sqlx.Tx, userID, peerID int64, kind AutoReplyKind) (bool, error) {
	var ids []int64
	if err := tx.QueryRowsPartial(&ids, "SELECT id FROM business_auto_replies WHERE user_id = ? AND peer_id = ? AND kind = ? LIMIT 1",
		userID, peerID, int8(kind)); err != nil {
		return false, err
	}
	return len(ids) > 0, nil
}

// QueuedAutoReply is a row of the queue of automatic replies: the reply of
// Kind from UserID to PeerID that the message TriggerBoxID of UserID's box
// triggered.
type QueuedAutoReply struct {
	ID           int64         `db:"id"`
	UserID       int64         `db:"user_id"`
	PeerID       int64         `db:"peer_id"`
	Kind         AutoReplyKind `db:"kind"`
	TriggerBoxID int32         `db:"trigger_box_id"`
	ShortcutID   int32         `db:"shortcut_id"`
	ClaimUntil   int64         `db:"claim_until"`
	Attempts     int32         `db:"attempts"`
	CreatedAt    int64         `db:"created_at"`
}

const queuedColumns = "id, user_id, peer_id, kind, trigger_box_id, shortcut_id, claim_until, attempts, created_at"

// QueueAutoReplyTx queues q in tx, the box transaction of q.UserID that
// takes the triggering message, at now (unix ms). The same trigger queues
// one row however often it comes.
func QueueAutoReplyTx(tx *sqlx.Tx, q QueuedAutoReply, now int64) error {
	_, err := tx.Exec("INSERT INTO business_auto_replies (user_id, peer_id, kind, trigger_box_id, shortcut_id, created_at)"+
		" VALUES (?, ?, ?, ?, ?, ?) AS new ON DUPLICATE KEY UPDATE id = business_auto_replies.id",
		q.UserID, q.PeerID, int8(q.Kind), q.TriggerBoxID, q.ShortcutID, now)
	return err
}

// UnclaimedAutoReplies returns at most limit queued replies no worker
// holds at now (unix ms), oldest first.
func (s *Store) UnclaimedAutoReplies(ctx context.Context, now int64, limit int) ([]*QueuedAutoReply, error) {
	var rows []*QueuedAutoReply
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT "+queuedColumns+" FROM business_auto_replies WHERE claim_until < ? ORDER BY id LIMIT ?",
		now, limit); err != nil {
		return nil, err
	}
	return rows, nil
}

// ClaimAutoReply claims the queued reply id for a worker until until (unix
// ms) when no worker holds it at now, and counts the attempt. It returns
// the row as claimed, nil when it is gone or another worker holds it.
func (s *Store) ClaimAutoReply(ctx context.Context, id, now, until int64) (*QueuedAutoReply, error) {
	res, err := s.db.Exec(ctx, "UPDATE business_auto_replies SET claim_until = ?, attempts = attempts + 1 WHERE id = ? AND claim_until < ?",
		until, id, now)
	if err != nil {
		return nil, err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return nil, err
	}
	var rows []*QueuedAutoReply
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT "+queuedColumns+" FROM business_auto_replies WHERE id = ? AND claim_until = ?",
		id, until); err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

// DropAutoReply takes the queued reply id off the queue without sending it.
func (s *Store) DropAutoReply(ctx context.Context, id int64) error {
	_, err := s.db.Exec(ctx, "DELETE FROM business_auto_replies WHERE id = ?", id)
	return err
}

// ErrAutoReplyGone: the queued reply left the queue before it went, as its
// setting was turned off (DeleteAutoReplyTx) in a box transaction of the
// account between the worker's look at the setting and its send.
var ErrAutoReplyGone = errors.New("business: the queued automatic reply is gone")

// FinishAutoReplyTx takes q off the queue in tx, the box transaction of
// q.UserID that sends it, and records it went at sentAt (unix time). A row
// no longer queued is ErrAutoReplyGone: the box lock orders the send after
// the change of the setting that took it off, and the send must not go.
func FinishAutoReplyTx(tx *sqlx.Tx, q *QueuedAutoReply, sentAt int32) error {
	res, err := tx.Exec("DELETE FROM business_auto_replies WHERE id = ?", q.ID)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrAutoReplyGone
	}
	_, err = tx.Exec("INSERT INTO business_auto_reply_log (user_id, peer_id, kind, sent_at) VALUES (?, ?, ?, ?) AS new"+
		" ON DUPLICATE KEY UPDATE sent_at = new.sent_at", q.UserID, q.PeerID, int8(q.Kind), sentAt)
	return err
}

// Incoming is what DecideAutoReplies knows of a private message that came
// to an account from a person.
type Incoming struct {
	// Peer is the sender, as the recipients see it; Peer.New is set by
	// DecideAutoReplies for each setting.
	Peer Peer
	// History is the chat before the message.
	History ChatHistory
	// Hours are the work hours of the account, nil for none.
	Hours *Hours
	// SeenUntil is the end of the last online window of the account (unix
	// time; 0 never).
	SeenUntil int64
	Now       time.Time
}

// AutoReplyState is what DecideAutoReplies reads of the queue and the log.
type AutoReplyState interface {
	Queued(kind AutoReplyKind) (bool, error)
	LastSent(kind AutoReplyKind) (int32, error)
	OwnerWroteSince(since int32) (bool, error)
}

// DecideAutoReplies returns the kinds of settings that go to the sender of
// in, the greeting first (both may go; not verified whether Telegram sends
// both, task 105, decisions to confirm, 3). A kind already queued for the
// chat is not queued again.
func DecideAutoReplies(settings []*AutoReply, in Incoming, st AutoReplyState) ([]*AutoReply, error) {
	var out []*AutoReply
	list := slices.Clone(settings)
	slices.SortFunc(list, func(a, b *AutoReply) int { return int(a.Kind) - int(b.Kind) })
	now := int32(in.Now.Unix())
	for _, a := range list {
		peer := in.Peer
		peer.New = in.History.IsNew(a.EnabledAt)
		if !a.Recipients.Match(peer) {
			continue
		}
		switch a.Kind {
		case KindGreeting:
			if !a.GreetingDue(in.History, now) {
				continue
			}
		case KindAway:
			on, err := a.AwayScheduled(in.Hours, in.Now)
			if err != nil {
				return nil, err
			}
			if !on || (a.OfflineOnly && !OwnerAway(in.SeenUntil, in.Now)) {
				continue
			}
			last, err := st.LastSent(KindAway)
			if err != nil {
				return nil, err
			}
			due, err := AwayRepeatDue(last, in.Now, st.OwnerWroteSince)
			if err != nil {
				return nil, err
			}
			if !due {
				continue
			}
		default:
			continue
		}
		queued, err := st.Queued(a.Kind)
		if err != nil {
			return nil, err
		}
		if !queued {
			out = append(out, a)
		}
	}
	return out, nil
}

// TxAutoReplyState is AutoReplyState read in tx, the box transaction of
// UserID that takes a message from PeerID.
type TxAutoReplyState struct {
	Tx     *sqlx.Tx
	UserID int64
	PeerID int64
}

func (s TxAutoReplyState) Queued(kind AutoReplyKind) (bool, error) {
	return AutoReplyQueuedTx(s.Tx, s.UserID, s.PeerID, kind)
}

func (s TxAutoReplyState) LastSent(kind AutoReplyKind) (int32, error) {
	return AutoReplyLastSentTx(s.Tx, s.UserID, s.PeerID, kind)
}

func (s TxAutoReplyState) OwnerWroteSince(since int32) (bool, error) {
	return OwnerWroteSinceTx(s.Tx, s.UserID, s.PeerID, since)
}
