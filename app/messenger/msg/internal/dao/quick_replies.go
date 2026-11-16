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
	"crypto/md5"
	"encoding/binary"
	"slices"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/business"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// Quick replies of Telegram Business (task 105, ADR 0067;
// core.telegram.org/api/business#quick-reply-shortcuts). A shortcut and its
// messages are the account's own: every change of them is made in the
// account's box transaction, so that two devices adding to one shortcut, or
// creating one by the same name, are one after the other
// (migrate-20261119-quick-replies.sql).

// shortcutLengthMax is the longest name of a shortcut, in UTF-16 code units
// as the clients count it (tdesktop settings/business/
// settings_quick_replies.cpp:32 kShortcutLimit; Android
// ui/Business/QuickRepliesActivity.java:303 MAX_NAME_LENGTH).
const shortcutLengthMax = 32

// ValidShortcutName tells whether name may name a shortcut: 1 to 32 UTF-16
// code units, each a letter or a digit, '_', U+200C, U+00B7 or of
// U+0D80-U+0DFF (tdesktop settings_quick_replies.cpp:166-180
// ValidShortcutName; Android QuickRepliesActivity.java:359-361 filters the
// input to [\d_\p{L}\x{200c}\x{00b7}\x{0d80}-\x{0dff}]). tdesktop counts any
// number as a digit, Android any letter outside the BMP as a letter: the
// server takes what either client lets one type.
func ValidShortcutName(name string) bool {
	if name == "" || !utf8.ValidString(name) {
		return false
	}
	units := 0
	for _, r := range name {
		units += utf16.RuneLen(r)
		if units > shortcutLengthMax {
			return false
		}
		switch {
		case unicode.IsLetter(r), unicode.IsNumber(r) && r <= 0xFFFF:
		case r == '_', r == 0x200C, r == 0x00B7, r >= 0x0D80 && r <= 0x0DFF:
		default:
			return false
		}
	}
	return true
}

// ShortcutKey is the name of a shortcut as names are compared: case-folded
// letter by letter, as tdesktop compares them (data/business/
// data_shortcut_messages.cpp, lookupShortcutId and editShortcut,
// Qt::CaseInsensitive, a simple folding of each character). "Hello" and
// "HELLO" are one shortcut; "straße" and "strasse" are two.
func ShortcutKey(name string) string {
	return strings.Map(func(r rune) rune { return unicode.ToLower(unicode.ToUpper(r)) }, name)
}

// QuickReplyRow is a message of a shortcut.
type QuickReplyRow struct {
	Id          int32  `db:"id"`
	UserId      int64  `db:"user_id"`
	ShortcutId  int32  `db:"shortcut_id"`
	RandomId    int64  `db:"random_id"`
	GroupedId   int64  `db:"grouped_id"`
	MessageData string `db:"message_data"`
	Date        int32  `db:"date"`
	EditDate    int32  `db:"edit_date"`
}

const quickReplyColumns = "id, user_id, shortcut_id, random_id, grouped_id, message_data, date, edit_date"

// Message decodes the message of a row as its owner gets it: in the owner's
// Saved Messages, outgoing, with the shortcut's id
// (core.telegram.org/constructor/message quick_reply_shortcut_id), its id
// from the rows' own sequence (tdesktop data_shortcut_messages.cpp:113
// IsShortcutMsgId, iOS QuickReplyMessages.swift:191
// Namespaces.Message.QuickReplyCloud).
func (r *QuickReplyRow) Message() (*mtproto.Message, error) {
	var m mtproto.Message
	if err := jsonx.UnmarshalFromString(r.MessageData, &m); err != nil {
		return nil, err
	}
	m.Id = r.Id
	m.Date = r.Date
	m.EditDate = nil
	if r.EditDate != 0 {
		m.EditDate = mtproto.MakeFlagsInt32(r.EditDate)
	}
	m.Out = true
	m.PeerId = mtproto.MakePeerUser(r.UserId)
	m.FromId = mtproto.MakePeerUser(r.UserId)
	m.SavedPeerId = nil
	m.QuickReplyShortcutId = mtproto.MakeFlagsInt32(r.ShortcutId)
	return m.FixData(), nil
}

// QuickReplyMessages decodes rows, in order.
func QuickReplyMessages(rows []*QuickReplyRow) ([]*mtproto.Message, error) {
	out := make([]*mtproto.Message, len(rows))
	for i, r := range rows {
		m, err := r.Message()
		if err != nil {
			return nil, err
		}
		out[i] = m
	}
	return out, nil
}

// QuickReply is a shortcut with what messages.getQuickReplies tells of it.
type QuickReply struct {
	Id       int32
	Shortcut string
	// Count is how many messages the shortcut holds.
	Count int32
	// Top is its first message: the clients show it for the shortcut and
	// hash its id. A message added later does not replace it (Android
	// ui/Business/QuickRepliesController.java:563-588 counts it only; iOS
	// QuickReplyMessages.swift:255-292 takes the first message of the
	// thread, anchor .lowerBound).
	Top *QuickReplyRow
}

// ToQuickReply is the shortcut as the clients get it.
func (q *QuickReply) ToQuickReply() *mtproto.QuickReply {
	return mtproto.MakeTLQuickReply(&mtproto.QuickReply{
		ShortcutId: q.Id,
		Shortcut:   q.Shortcut,
		TopMessage: q.Top.Id,
		Count:      q.Count,
	}).To_QuickReply()
}

// QuickRepliesOf are the shortcuts as the clients get them.
func QuickRepliesOf(list []*QuickReply) []*mtproto.QuickReply {
	out := make([]*mtproto.QuickReply, len(list))
	for i, q := range list {
		out[i] = q.ToQuickReply()
	}
	return out
}

// quickReplyHash adds v to the hash as Android, iOS and tdesktop count
// theirs (Android MediaDataController.calcHash, iOS combineInt64Hash,
// tdesktop Api::HashUpdate): on an unsigned 64-bit value.
func quickReplyHash(h uint64, v uint64) uint64 {
	h ^= h >> 21
	h ^= h << 35
	h ^= h >> 4
	return h + v
}

// shortcutNameHash is the first 8 bytes of the MD5 of the name as a
// big-endian number (Android QuickRepliesController.java:209
// Long.parseUnsignedLong(MD5(name).substring(0, 16), 16); iOS
// ManagedConsumePersonalMessagesActions.swift:24-36 md5StringHash).
func shortcutNameHash(name string) uint64 {
	sum := md5.Sum([]byte(name))
	return binary.BigEndian.Uint64(sum[:8])
}

// QuickRepliesHash is the hash of messages.getQuickReplies: for each
// shortcut in order its id, the hash of its name, the id of its top message
// and that message's edit date, 0 when never edited
// (core.telegram.org/api/business; Android QuickRepliesController.java:
// 203-215, iOS QuickReplyMessages.swift:119-142).
func QuickRepliesHash(list []*QuickReply) int64 {
	var h uint64
	for _, q := range list {
		h = quickReplyHash(h, uint64(q.Id))
		h = quickReplyHash(h, shortcutNameHash(q.Shortcut))
		h = quickReplyHash(h, uint64(q.Top.Id))
		h = quickReplyHash(h, uint64(q.Top.EditDate))
	}
	return int64(h)
}

// QuickReplyMessagesHash is the hash of messages.getQuickReplyMessages:
// for each message, newest first, its id and its edit date, 0 when never
// edited (core.telegram.org/api/business; tdesktop data/business/
// data_shortcut_messages.cpp:763-781 countListHash). rows are in ascending
// id. iOS counts the date in as well (HistoryViewStateValidation.swift:
// 437-456 hashForScheduledMessages): it gets the messages each time.
func QuickReplyMessagesHash(rows []*QuickReplyRow) int64 {
	var h uint64
	for i := len(rows) - 1; i >= 0; i-- {
		h = quickReplyHash(h, uint64(rows[i].Id))
		h = quickReplyHash(h, uint64(rows[i].EditDate))
	}
	return int64(h)
}

// rowsQuerier reads rows: in a box transaction or outside one.
type rowsQuerier func(v any, query string, args ...any) error

func (d *Dao) dbRows(ctx context.Context) rowsQuerier {
	return func(v any, query string, args ...any) error {
		return d.Mysql.DB.QueryRowsPartial(ctx, v, query, args...)
	}
}

func txRows(b *userbox.Box) rowsQuerier { return b.Tx().QueryRowsPartial }

// quickReplyListRow is a shortcut with its count and its top message, read
// in one statement so that they agree.
type quickReplyListRow struct {
	ShortcutId  int32  `db:"shortcut_id"`
	Shortcut    string `db:"shortcut"`
	Count       int32  `db:"cnt"`
	Id          int32  `db:"id"`
	RandomId    int64  `db:"random_id"`
	GroupedId   int64  `db:"grouped_id"`
	MessageData string `db:"message_data"`
	Date        int32  `db:"date"`
	EditDate    int32  `db:"edit_date"`
}

// loadQuickReplies returns userId's shortcuts that hold a message, in the
// account's order. A shortcut left without messages is not shown: the
// clients take top_message of every shortcut for a message they can load
// (tdesktop data_shortcut_messages.cpp:256-270 reloads the list while one
// is unknown), and delete an empty shortcut themselves.
func loadQuickReplies(query rowsQuerier, userId int64) ([]*QuickReply, error) {
	var rows []*quickReplyListRow
	if err := query(&rows,
		"SELECT s.id AS shortcut_id, s.shortcut, t.cnt, m.id, m.random_id, m.grouped_id, m.message_data, m.date, m.edit_date"+
			" FROM quick_reply_shortcuts s"+
			" JOIN (SELECT shortcut_id, COUNT(*) AS cnt, MIN(id) AS top_id FROM quick_reply_messages WHERE user_id = ? GROUP BY shortcut_id) t ON t.shortcut_id = s.id"+
			" JOIN quick_reply_messages m ON m.id = t.top_id"+
			" WHERE s.user_id = ? ORDER BY s.sort, s.id",
		userId, userId); err != nil {
		return nil, err
	}
	out := make([]*QuickReply, len(rows))
	for i, r := range rows {
		out[i] = &QuickReply{Id: r.ShortcutId, Shortcut: r.Shortcut, Count: r.Count, Top: &QuickReplyRow{
			Id: r.Id, UserId: userId, ShortcutId: r.ShortcutId, RandomId: r.RandomId, GroupedId: r.GroupedId,
			MessageData: r.MessageData, Date: r.Date, EditDate: r.EditDate,
		}}
	}
	return out, nil
}

// LoadQuickReplies returns userId's shortcuts as messages.getQuickReplies
// answers them.
func (d *Dao) LoadQuickReplies(ctx context.Context, userId int64) ([]*QuickReply, error) {
	return loadQuickReplies(d.dbRows(ctx), userId)
}

// QuickRepliesIn is LoadQuickReplies in b's transaction: the list as the
// transaction leaves it.
func (d *Dao) QuickRepliesIn(b *userbox.Box) ([]*QuickReply, error) {
	return loadQuickReplies(txRows(b), b.UserId())
}

// QuickReplyShortcut is a shortcut of an account.
type QuickReplyShortcut struct {
	Id       int32  `db:"id"`
	Shortcut string `db:"shortcut"`
	Sort     int32  `db:"sort"`
}

// ShortcutOf returns userId's shortcut with id, nil when it has none.
func (d *Dao) ShortcutOf(ctx context.Context, userId int64, id int32) (*QuickReplyShortcut, error) {
	return shortcutById(d.dbRows(ctx), userId, id, false)
}

// ShortcutByName returns userId's shortcut named name, case aside, nil when
// it has none.
func (d *Dao) ShortcutByName(ctx context.Context, userId int64, name string) (*QuickReplyShortcut, error) {
	return shortcutByKey(d.dbRows(ctx), userId, ShortcutKey(name), false)
}

func shortcutById(query rowsQuerier, userId int64, id int32, lock bool) (*QuickReplyShortcut, error) {
	if id <= 0 {
		return nil, nil
	}
	return oneShortcut(query, lock, "SELECT id, shortcut, sort FROM quick_reply_shortcuts WHERE id = ? AND user_id = ?", id, userId)
}

func shortcutByKey(query rowsQuerier, userId int64, key string, lock bool) (*QuickReplyShortcut, error) {
	return oneShortcut(query, lock, "SELECT id, shortcut, sort FROM quick_reply_shortcuts WHERE user_id = ? AND shortcut_key = ?", userId, key)
}

func oneShortcut(query rowsQuerier, lock bool, q string, args ...any) (*QuickReplyShortcut, error) {
	if lock {
		q += " FOR UPDATE"
	}
	var rows []*QuickReplyShortcut
	if err := query(&rows, q, args...); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0], nil
}

// CountShortcuts is how many shortcuts userId has, empty ones included.
func (d *Dao) CountShortcuts(ctx context.Context, userId int64) (int, error) {
	return countShortcuts(d.dbRows(ctx), userId)
}

func countShortcuts(query rowsQuerier, userId int64) (int, error) {
	var n []int
	if err := query(&n, "SELECT COUNT(*) FROM quick_reply_shortcuts WHERE user_id = ?", userId); err != nil {
		return 0, err
	}
	if len(n) == 0 {
		return 0, nil
	}
	return n[0], nil
}

// CountQuickReplyMessages is how many messages the shortcut holds.
func (d *Dao) CountQuickReplyMessages(ctx context.Context, shortcutId int32) (int, error) {
	return countQuickReplyMessages(d.dbRows(ctx), shortcutId)
}

func countQuickReplyMessages(query rowsQuerier, shortcutId int32) (int, error) {
	var n []int
	if err := query(&n, "SELECT COUNT(*) FROM quick_reply_messages WHERE shortcut_id = ?", shortcutId); err != nil {
		return 0, err
	}
	if len(n) == 0 {
		return 0, nil
	}
	return n[0], nil
}

// LockShortcut locks and returns b's owner's shortcut with id, nil when it
// has none.
func (d *Dao) LockShortcut(b *userbox.Box, id int32) (*QuickReplyShortcut, error) {
	return shortcutById(txRows(b), b.UserId(), id, true)
}

// LockShortcutByName locks and returns b's owner's shortcut named name,
// case aside, nil when it has none.
func (d *Dao) LockShortcutByName(b *userbox.Box, name string) (*QuickReplyShortcut, error) {
	return shortcutByKey(txRows(b), b.UserId(), ShortcutKey(name), true)
}

// CreateShortcut adds a shortcut named name at the end of b's owner's
// order: QUICK_REPLIES_TOO_MUCH when the owner has quick_replies_limit
// shortcuts already. The name must be valid and free.
func (d *Dao) CreateShortcut(b *userbox.Box, name string) (*QuickReplyShortcut, error) {
	n, err := countShortcuts(txRows(b), b.UserId())
	if err != nil {
		return nil, err
	}
	if n >= business.QuickRepliesLimit {
		return nil, mtproto.ErrQuickRepliesTooMuch
	}
	var last []int32
	if err = b.Tx().QueryRowsPartial(&last, "SELECT COALESCE(MAX(sort), 0) FROM quick_reply_shortcuts WHERE user_id = ?", b.UserId()); err != nil {
		return nil, err
	}
	sort := int32(1)
	if len(last) > 0 {
		sort = last[0] + 1
	}
	res, err := b.Tx().Exec("INSERT INTO quick_reply_shortcuts (user_id, shortcut, shortcut_key, sort, created_at) VALUES (?, ?, ?, ?, ?)",
		b.UserId(), name, ShortcutKey(name), sort, b.Date())
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &QuickReplyShortcut{Id: int32(id), Shortcut: name, Sort: sort}, nil
}

// QuickRepliesBefore returns the messages b's owner already added with
// randomIds, all of them, in order, or nil when it added none of them: an
// add repeated by a client that lost the answer gets the first answer; a
// partial repeat is RANDOM_ID_DUPLICATE (as SentBefore).
func (d *Dao) QuickRepliesBefore(b *userbox.Box, randomIds []int64) ([]*QuickReplyRow, error) {
	if len(randomIds) == 0 {
		return nil, nil
	}
	args := make([]any, 0, len(randomIds)+1)
	args = append(args, b.UserId())
	for _, id := range randomIds {
		args = append(args, id)
	}
	var rows []*QuickReplyRow
	if err := b.Tx().QueryRowsPartial(&rows,
		"SELECT "+quickReplyColumns+" FROM quick_reply_messages WHERE user_id = ? AND out_random_id IN ("+inList(len(randomIds))+")",
		args...); err != nil {
		return nil, err
	}
	switch {
	case len(rows) == 0:
		return nil, nil
	case len(rows) != len(randomIds):
		return nil, mtproto.ErrRandomIdDuplicate
	}
	out := make([]*QuickReplyRow, 0, len(rows))
	for _, id := range randomIds {
		if i := slices.IndexFunc(rows, func(r *QuickReplyRow) bool { return r.RandomId == id }); i >= 0 {
			out = append(out, rows[i])
		}
	}
	return out, nil
}

// InsertQuickReplyMessages adds msgs to b's owner's shortcut, which the
// transaction holds locked: REPLY_MESSAGES_TOO_MUCH when it would hold more
// than quick_reply_messages_limit. It returns the rows in order, with their
// ids.
func (d *Dao) InsertQuickReplyMessages(b *userbox.Box, shortcutId int32, msgs []Outgoing) ([]*QuickReplyRow, error) {
	n, err := countQuickReplyMessages(txRows(b), shortcutId)
	if err != nil {
		return nil, err
	}
	if n+len(msgs) > business.QuickReplyMessagesLimit {
		return nil, mtproto.ErrReplyMessagesTooMuch
	}
	rows := make([]*QuickReplyRow, 0, len(msgs))
	for _, m := range msgs {
		// The id, the date and the edit date are the row's columns.
		data, err := jsonx.MarshalToString(m.Message)
		if err != nil {
			return nil, err
		}
		groupedId := m.Message.GetGroupedId().GetValue()
		res, err := b.Tx().Exec("INSERT INTO quick_reply_messages (user_id, shortcut_id, random_id, grouped_id, message_data, date) VALUES (?, ?, ?, ?, ?, ?)",
			b.UserId(), shortcutId, m.RandomId, groupedId, data, b.Date())
		if err != nil {
			return nil, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return nil, err
		}
		rows = append(rows, &QuickReplyRow{Id: int32(id), UserId: b.UserId(), ShortcutId: shortcutId, RandomId: m.RandomId,
			GroupedId: groupedId, MessageData: data, Date: int32(b.Date())})
	}
	return rows, nil
}

// LoadQuickReplyMessages returns the messages of the shortcut in ascending
// id, those with ids only when ids are given.
func (d *Dao) LoadQuickReplyMessages(ctx context.Context, shortcutId int32, ids []int32) ([]*QuickReplyRow, error) {
	var rows []*QuickReplyRow
	q := "SELECT " + quickReplyColumns + " FROM quick_reply_messages WHERE shortcut_id = ?"
	args := []any{shortcutId}
	if len(ids) > 0 {
		q += " AND id IN (" + inList(len(ids)) + ")"
		args = idArgs(ids, args...)
	}
	if err := d.Mysql.DB.QueryRowsPartial(ctx, &rows, q+" ORDER BY id", args...); err != nil {
		return nil, err
	}
	return rows, nil
}

// RenameShortcut gives b's owner's shortcut id the name, which must be
// valid: SHORTCUT_OCCUPIED when another shortcut of the owner has it, case
// aside (tdesktop data_shortcut_messages.cpp, editShortcut, checks the same
// before it asks). It tells whether the name changed.
func (d *Dao) RenameShortcut(b *userbox.Box, id int32, name string) (bool, error) {
	s, err := d.LockShortcut(b, id)
	if err != nil {
		return false, err
	}
	if s == nil {
		return false, mtproto.ErrShortcutInvalid
	}
	if s.Shortcut == name {
		return false, nil
	}
	other, err := shortcutByKey(txRows(b), b.UserId(), ShortcutKey(name), true)
	if err != nil {
		return false, err
	}
	if other != nil && other.Id != id {
		return false, mtproto.ErrShortcutOccupied
	}
	_, err = b.Tx().Exec("UPDATE quick_reply_shortcuts SET shortcut = ?, shortcut_key = ? WHERE id = ?", name, ShortcutKey(name), id)
	return err == nil, err
}

// ReorderShortcuts orders b's owner's shortcuts as order lists them: ids
// that are not its shortcuts are passed over, and the shortcuts order does
// not name follow in their present order. It tells whether the order
// changed.
func (d *Dao) ReorderShortcuts(b *userbox.Box, order []int32) (bool, error) {
	var list []*QuickReplyShortcut
	if err := b.Tx().QueryRowsPartial(&list, "SELECT id, shortcut, sort FROM quick_reply_shortcuts WHERE user_id = ? ORDER BY sort, id FOR UPDATE", b.UserId()); err != nil {
		return false, err
	}
	next := make([]*QuickReplyShortcut, 0, len(list))
	for _, id := range order {
		i := slices.IndexFunc(list, func(s *QuickReplyShortcut) bool { return s.Id == id })
		if i >= 0 && !slices.Contains(next, list[i]) {
			next = append(next, list[i])
		}
	}
	for _, s := range list {
		if !slices.Contains(next, s) {
			next = append(next, s)
		}
	}
	changed := false
	for i, s := range next {
		sort := int32(i + 1)
		if s.Sort == sort {
			continue
		}
		changed = true
		if _, err := b.Tx().Exec("UPDATE quick_reply_shortcuts SET sort = ? WHERE id = ?", sort, s.Id); err != nil {
			return false, err
		}
	}
	return changed, nil
}

// DeleteShortcut deletes b's owner's shortcut id with its messages. It
// tells whether the owner had it.
func (d *Dao) DeleteShortcut(b *userbox.Box, id int32) (bool, error) {
	s, err := d.LockShortcut(b, id)
	if err != nil || s == nil {
		return false, err
	}
	if _, err = b.Tx().Exec("DELETE FROM quick_reply_messages WHERE shortcut_id = ?", id); err != nil {
		return false, err
	}
	if _, err = b.Tx().Exec("DELETE FROM quick_reply_shortcuts WHERE id = ?", id); err != nil {
		return false, err
	}
	return true, nil
}

// QuickReplyMessageUpdates are updateQuickReplyMessage of msgs.
func QuickReplyMessageUpdates(msgs []*mtproto.Message) []*mtproto.Update {
	out := make([]*mtproto.Update, len(msgs))
	for i, m := range msgs {
		out[i] = mtproto.MakeTLUpdateQuickReplyMessage(&mtproto.Update{Message_MESSAGE: m}).To_Update()
	}
	return out
}

// NewQuickReplyUpdate is updateNewQuickReply of q.
func NewQuickReplyUpdate(q *QuickReply) *mtproto.Update {
	return mtproto.MakeTLUpdateNewQuickReply(&mtproto.Update{QuickReply: q.ToQuickReply()}).To_Update()
}

// QuickRepliesUpdate is updateQuickReplies of the whole list.
func QuickRepliesUpdate(list []*QuickReply) *mtproto.Update {
	return mtproto.MakeTLUpdateQuickReplies(&mtproto.Update{QuickReplies: QuickRepliesOf(list)}).To_Update()
}

// DeleteQuickReplyUpdate is updateDeleteQuickReply of the shortcut id.
func DeleteQuickReplyUpdate(id int32) *mtproto.Update {
	return mtproto.MakeTLUpdateDeleteQuickReply(&mtproto.Update{ShortcutId: id}).To_Update()
}
