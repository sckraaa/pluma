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

package secretchat

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/teamgram/teamgram-server/pkg/userbox"
)

const (
	// retention is how long the server keeps what an authorization did not
	// confirm: "messages older than 7 days" may be deleted
	// (core.telegram.org/api/end-to-end). An unanswered request goes with
	// them, and so does a discarded chat's row.
	retention = 7 * 24 * time.Hour
	// eventMargin widens the window of the updates without qts: updates with
	// the same second as the last state must not be missed.
	eventMargin = 60
	// differencePage is the most queued messages one difference carries;
	// differenceBytes the most ciphertext.
	differencePage  = 100
	differenceBytes = 4 << 20
	idAttempts      = 16
	sweepBatch      = 1000
)

// Store keeps the chats, their queues and the encrypted files in MySQL.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// New returns the store on db.
func New(db *sql.DB) *Store { return &Store{db: db, now: time.Now} }

// MustOpen connects to the database at dsn, at the start of a service.
func MustOpen(dsn string) *Store {
	db, err := userbox.Open(dsn)
	if err != nil {
		panic(err)
	}
	return New(db)
}

func (s *Store) unix() int32 { return int32(s.now().Unix()) }

const chatColumns = "id, access_hash, admin_id, admin_auth_key_id, participant_id, participant_auth_key_id, random_id, g_a, g_b, " +
	"key_fingerprint, state, history_deleted, discarded_by, date, state_at, admin_read_date, admin_read_at, participant_read_date, participant_read_at"

func scanChat(row interface{ Scan(...any) error }) (*Chat, error) {
	c := new(Chat)
	err := row.Scan(&c.Id, &c.AccessHash, &c.AdminId, &c.AdminAuthKey, &c.ParticipantId, &c.ParticipantAuthKey, &c.RandomId, &c.GA, &c.GB,
		&c.KeyFingerprint, &c.State, &c.HistoryDeleted, &c.DiscardedBy, &c.Date, &c.StateAt, &c.AdminRead, &c.AdminReadAt, &c.ParticipantRead, &c.ParticipantReadAt)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func randomInt64() int64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return int64(binary.LittleEndian.Uint64(b[:]))
}

// randomChatId is a positive chat id; a request that collides takes another.
func randomChatId() int32 {
	return int32(uint64(randomInt64())%uint64(math.MaxInt32-1)) + 1
}

// tx runs fn in a transaction, rolled back if it fails.
func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func lockChat(ctx context.Context, tx *sql.Tx, id int32) (*Chat, error) {
	c, err := scanChat(tx.QueryRowContext(ctx, "SELECT "+chatColumns+" FROM encrypted_chats WHERE id = ? FOR UPDATE", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrChatNotFound
	}
	return c, err
}

// Get returns the chat id.
func (s *Store) Get(ctx context.Context, id int32) (*Chat, error) {
	c, err := scanChat(s.db.QueryRowContext(ctx, "SELECT "+chatColumns+" FROM encrypted_chats WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrChatNotFound
	}
	return c, err
}

// access tells whether d may use the chat: its request (any authorization
// of the participant until it is accepted) or its own after that. ErrChat
// NotFound for someone else's chat, ErrWrongDevice for another device of
// the same account.
func access(c *Chat, d Device, accessHash int64, checkHash bool) error {
	if (c.AdminId != d.UserId && c.ParticipantId != d.UserId) || (checkHash && c.AccessHash != accessHash) {
		return ErrChatNotFound
	}
	if c.AdminId == d.UserId && c.AdminAuthKey == d.AuthKey {
		return nil
	}
	if c.ParticipantId == d.UserId && (c.State == StateRequested || c.ParticipantAuthKey == d.AuthKey) {
		return nil
	}
	return ErrWrongDevice
}

// Peer is the other side's authorization of d in the chat: the admin's, or
// the one that accepted.
func (c *Chat) Peer(d Device) Device {
	if c.AdminId == d.UserId && c.AdminAuthKey == d.AuthKey {
		return Device{UserId: c.ParticipantId, AuthKey: c.ParticipantAuthKey}
	}
	return Device{UserId: c.AdminId, AuthKey: c.AdminAuthKey}
}

// Devices returns the authorizations of userId; with requests, only those
// that take secret chat requests (authorization.encrypted_requests_disabled).
func (s *Store) Devices(ctx context.Context, userId int64, requests bool) ([]int64, error) {
	q := "SELECT auth_key_id FROM auth_users WHERE user_id = ? AND deleted = 0"
	if requests {
		q += " AND encrypted_requests_disabled = 0"
	}
	rows, err := s.db.QueryContext(ctx, q, userId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// Request is the input of messages.requestEncryption.
type Request struct {
	Admin         Device
	ParticipantId int64
	RandomId      int32
	GA            []byte
}

// Request creates a chat in the requested state. The same random_id of the
// same authorization returns the same chat (a repeated request), created
// false; with other values it is ErrRandomIdUsed.
func (s *Store) Request(ctx context.Context, r Request) (*Chat, bool, error) {
	now := s.unix()
	for range idAttempts {
		id, hash := randomChatId(), randomInt64()
		res, err := s.db.ExecContext(ctx, "INSERT IGNORE INTO encrypted_chats (id, access_hash, admin_id, admin_auth_key_id, participant_id, random_id, g_a, state, date, state_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			id, hash, r.Admin.UserId, r.Admin.AuthKey, r.ParticipantId, r.RandomId, r.GA, StateRequested, now, now)
		if err != nil {
			return nil, false, fmt.Errorf("secretchat: request: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return nil, false, fmt.Errorf("secretchat: request: %w", err)
		}
		if n == 1 {
			c, err := s.Get(ctx, id)
			return c, true, err
		}
		// A taken id or the same request again.
		c, err := scanChat(s.db.QueryRowContext(ctx, "SELECT "+chatColumns+" FROM encrypted_chats WHERE admin_auth_key_id = ? AND random_id = ?", r.Admin.AuthKey, r.RandomId))
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, false, fmt.Errorf("secretchat: request: %w", err)
		}
		if c.State == StateDiscarded || c.ParticipantId != r.ParticipantId || !bytes.Equal(c.GA, r.GA) {
			return nil, false, ErrRandomIdUsed
		}
		return c, false, nil
	}
	return nil, false, errors.New("secretchat: no free chat id")
}

// Accept is the input of messages.acceptEncryption.
type Accept struct {
	Participant    Device
	ChatId         int32
	AccessHash     int64
	GB             []byte
	KeyFingerprint int64
}

// Accept moves a requested chat to the participant's authorization. A
// repeat from the same authorization returns the chat, repeated true.
func (s *Store) Accept(ctx context.Context, a Accept) (c *Chat, repeated bool, err error) {
	err = s.tx(ctx, func(tx *sql.Tx) error {
		if c, err = lockChat(ctx, tx, a.ChatId); err != nil {
			return err
		}
		if c.ParticipantId != a.Participant.UserId || c.AccessHash != a.AccessHash {
			return ErrChatNotFound
		}
		switch c.State {
		case StateDiscarded:
			return ErrAlreadyDeclined
		case StateActive:
			if c.ParticipantAuthKey == a.Participant.AuthKey && bytes.Equal(c.GB, a.GB) {
				repeated = true
				return nil
			}
			return ErrAlreadyAccepted
		}
		now := s.unix()
		if _, err = tx.ExecContext(ctx, "UPDATE encrypted_chats SET state = ?, participant_auth_key_id = ?, g_b = ?, key_fingerprint = ?, state_at = ? WHERE id = ?",
			StateActive, a.Participant.AuthKey, a.GB, a.KeyFingerprint, now, c.Id); err != nil {
			return err
		}
		c.State, c.ParticipantAuthKey, c.GB, c.KeyFingerprint, c.StateAt = StateActive, a.Participant.AuthKey, a.GB, a.KeyFingerprint, now
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return c, repeated, nil
}

// Discard is the input of messages.discardEncryption. Server: the server
// ends the chat (the end of an authorization, no answer to a request).
type Discard struct {
	By             Device
	ChatId         int32
	HistoryDeleted bool
	Server         bool
}

// Discard ends a chat and drops what is queued for it: a client ignores the
// messages of a discarded chat (Android SecretChatHelper.decryptMessage).
// It returns the chat and whether it was still only requested.
func (s *Store) Discard(ctx context.Context, d Discard) (c *Chat, wasRequested bool, err error) {
	err = s.tx(ctx, func(tx *sql.Tx) error {
		if c, err = lockChat(ctx, tx, d.ChatId); err != nil {
			return err
		}
		if !d.Server {
			if err = access(c, d.By, 0, false); err != nil {
				return err
			}
		}
		if c.State == StateDiscarded {
			return ErrAlreadyDeclined
		}
		wasRequested = c.State == StateRequested
		now := s.unix()
		by := d.By.AuthKey
		if _, err = tx.ExecContext(ctx, "UPDATE encrypted_chats SET state = ?, history_deleted = ?, discarded_by = ?, g_a = '', g_b = '', state_at = ? WHERE id = ?",
			StateDiscarded, d.HistoryDeleted, by, now, c.Id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM encrypted_chat_queue WHERE chat_id = ?", c.Id); err != nil {
			return err
		}
		c.State, c.HistoryDeleted, c.DiscardedBy, c.GA, c.GB, c.StateAt = StateDiscarded, d.HistoryDeleted, by, nil, nil, now
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return c, wasRequested, nil
}

// DiscardTargets returns the authorizations told that c was discarded: the
// two ends, and if it was still a request, every authorization of the
// participant (none has yet lost it).
func (s *Store) DiscardTargets(ctx context.Context, c *Chat, wasRequested bool) ([]Device, error) {
	out := []Device{{UserId: c.AdminId, AuthKey: c.AdminAuthKey}}
	if wasRequested {
		keys, err := s.Devices(ctx, c.ParticipantId, false)
		if err != nil {
			return nil, err
		}
		for _, k := range keys {
			out = append(out, Device{UserId: c.ParticipantId, AuthKey: k})
		}
	} else if c.ParticipantAuthKey != 0 {
		out = append(out, Device{UserId: c.ParticipantId, AuthKey: c.ParticipantAuthKey})
	}
	kept := out[:0]
	for _, d := range out {
		if d.AuthKey != c.DiscardedBy || c.DiscardedBy == 0 {
			kept = append(kept, d)
		}
	}
	return kept, nil
}

// Send is the input of messages.sendEncrypted*.
type Send struct {
	From       Device
	ChatId     int32
	AccessHash int64
	RandomId   int64
	Service    bool
	Data       []byte
	File       *File
}

// Sent is the answer of a send: Delivered is the queued message and its
// recipient unless the send repeats an earlier one.
type Sent struct {
	Date      int32
	File      *File
	Delivered *QueuedMessage
	To        Device
}

// Lookup returns what the chat's send with this random_id answered, nil if
// there is none. It lets a repeat skip the upload of its file.
func (s *Store) Lookup(ctx context.Context, from Device, chatId int32, randomId int64) (*Sent, error) {
	return querySent(ctx, s.db, from, chatId, randomId)
}

func querySent(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, from Device, chatId int32, randomId int64) (*Sent, error) {
	var (
		date int32
		f    File
	)
	err := q.QueryRowContext(ctx, "SELECT date, file_id, file_access_hash, file_size, file_dc_id, file_key_fingerprint FROM encrypted_chat_sent WHERE chat_id = ? AND sender_auth_key_id = ? AND random_id = ?",
		chatId, from.AuthKey, randomId).Scan(&date, &f.Id, &f.AccessHash, &f.Size, &f.DcId, &f.KeyFingerprint)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r := &Sent{Date: date}
	if f.Id != 0 {
		r.File = &f
	}
	return r, nil
}

// Send queues a message for the other end. A repeated random_id answers the
// same date and file without queueing anything. The chat's row is the lock
// of the send; the qts of the recipient is given under the lock of its own
// row, so the numbers have no gaps and commit in order.
func (s *Store) Send(ctx context.Context, in Send) (*Sent, error) {
	var out *Sent
	err := s.tx(ctx, func(tx *sql.Tx) error {
		c, err := lockChat(ctx, tx, in.ChatId)
		if err != nil {
			return err
		}
		if err = access(c, in.From, in.AccessHash, true); err != nil {
			return err
		}
		if out, err = querySent(ctx, tx, in.From, in.ChatId, in.RandomId); err != nil || out != nil {
			return err
		}
		if c.State != StateActive {
			return ErrDeclined
		}
		now := s.unix()
		to := c.Peer(in.From)
		f := File{}
		if in.File != nil {
			f = *in.File
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO encrypted_chat_sent (chat_id, sender_auth_key_id, random_id, date, file_id, file_access_hash, file_size, file_dc_id, file_key_fingerprint) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
			in.ChatId, in.From.AuthKey, in.RandomId, now, f.Id, f.AccessHash, f.Size, f.DcId, f.KeyFingerprint); err != nil {
			return err
		}
		qts, err := nextQts(ctx, tx, to)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO encrypted_chat_queue (auth_key_id, qts, chat_id, random_id, date, service, data, file_id, file_access_hash, file_size, file_dc_id, file_key_fingerprint) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			to.AuthKey, qts, in.ChatId, in.RandomId, now, in.Service, in.Data, f.Id, f.AccessHash, f.Size, f.DcId, f.KeyFingerprint); err != nil {
			return err
		}
		out = &Sent{
			Date: now, File: in.File, To: to,
			Delivered: &QueuedMessage{Qts: qts, ChatId: in.ChatId, RandomId: in.RandomId, Date: now, Service: in.Service, Data: in.Data, File: in.File},
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func nextQts(ctx context.Context, tx *sql.Tx, d Device) (int32, error) {
	if _, err := tx.ExecContext(ctx, "INSERT INTO encrypted_chat_devices (auth_key_id, user_id, qts) VALUES (?, ?, 1) ON DUPLICATE KEY UPDATE qts = qts + 1", d.AuthKey, d.UserId); err != nil {
		return 0, err
	}
	var qts int32
	err := tx.QueryRowContext(ctx, "SELECT qts FROM encrypted_chat_devices WHERE auth_key_id = ?", d.AuthKey).Scan(&qts)
	return qts, err
}

// ReadHistory records messages.readEncryptedHistory of d and returns the
// other end to tell; changed is false for an old max_date, nothing to tell.
func (s *Store) ReadHistory(ctx context.Context, d Device, chatId int32, accessHash int64, maxDate int32) (to Device, changed bool, err error) {
	err = s.tx(ctx, func(tx *sql.Tx) error {
		c, e := lockChat(ctx, tx, chatId)
		if e != nil {
			return e
		}
		if e = access(c, d, accessHash, true); e != nil {
			return e
		}
		if c.State != StateActive {
			return ErrDeclined
		}
		column, current := "participant_read", c.ParticipantRead
		if c.AdminId == d.UserId && c.AdminAuthKey == d.AuthKey {
			column, current = "admin_read", c.AdminRead
		}
		to = c.Peer(d)
		if maxDate <= current {
			return nil
		}
		if _, e = tx.ExecContext(ctx, fmt.Sprintf("UPDATE encrypted_chats SET %s_date = ?, %s_at = ? WHERE id = ?", column, column), maxDate, s.unix(), chatId); e != nil {
			return e
		}
		changed = true
		return nil
	})
	return to, changed, err
}

// Check tells whether d may use the chat (typing, a report) and returns it.
func (s *Store) Check(ctx context.Context, d Device, chatId int32, accessHash int64) (*Chat, error) {
	c, err := s.Get(ctx, chatId)
	if err != nil {
		return nil, err
	}
	if err = access(c, d, accessHash, true); err != nil {
		return nil, err
	}
	return c, nil
}

// Ack confirms the queue of an authorization up to maxQts and returns the
// random ids of the confirmed messages.
func (s *Store) Ack(ctx context.Context, authKey int64, maxQts int32) ([]int64, error) {
	var ids []int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT random_id FROM encrypted_chat_queue WHERE auth_key_id = ? AND qts <= ? ORDER BY qts FOR UPDATE", authKey, maxQts)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close() // the connection is needed for the DELETE
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM encrypted_chat_queue WHERE auth_key_id = ? AND qts <= ?", authKey, maxQts)
		return err
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// State returns the qts of d for updates.getState and marks that d was given
// a state.
func (s *Store) State(ctx context.Context, d Device) (int32, error) {
	if _, err := s.db.ExecContext(ctx, "INSERT INTO encrypted_chat_devices (auth_key_id, user_id, state_given_at) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE state_given_at = VALUES(state_given_at)", d.AuthKey, d.UserId, s.unix()); err != nil {
		return 0, err
	}
	var qts int32
	err := s.db.QueryRowContext(ctx, "SELECT qts FROM encrypted_chat_devices WHERE auth_key_id = ?", d.AuthKey).Scan(&qts)
	return qts, err
}

// Difference is what updates.getDifference adds for one authorization.
type Difference struct {
	Messages []*QueuedMessage
	Updates  []*DifferenceUpdate // updateEncryption and updateEncryptedMessagesRead
	Qts      int32               // the state's qts after this page
	More     bool                // more messages after this page
}

// DifferenceUpdate is an update without qts of the difference.
type DifferenceUpdate struct {
	Chat    *Chat
	ReadMax int32 // for updateEncryptedMessagesRead
	ReadAt  int32 // the time of the read, 0 for updateEncryption
}

// DifferenceInput is the state of the client.
type DifferenceInput struct {
	Device Device
	Qts    int32
	Date   int32
	// OnlyQueue: the request gives no date to go by (date INT32_MAX, as the
	// iOS notification extension asks): the queue only, not the updates
	// without qts.
	OnlyQueue bool
}

// Empty tells whether the difference changes nothing for a client at qts.
func (d *Difference) Empty(clientQts int32) bool {
	return len(d.Messages) == 0 && len(d.Updates) == 0 && !d.More && d.Qts == clientQts
}

// Difference confirms the queue up to in.Qts, the implicit acknowledgement
// (core.telegram.org/api/end-to-end), and returns the page of the queue
// after it and the changes of the chats. The changes are read off the rows,
// so a repeated difference gives them again instead of a lost update
// failing silently; every client takes them idempotently. They cover the
// time since the earlier of the client's date and the last state it was
// given: an update with no qts can be lost with a client still at a newer
// date.
func (s *Store) Difference(ctx context.Context, in DifferenceInput) (*Difference, error) {
	if in.Qts > 0 {
		if _, err := s.Ack(ctx, in.Device.AuthKey, in.Qts); err != nil {
			return nil, fmt.Errorf("secretchat: difference ack: %w", err)
		}
	}
	var (
		qts, given int32
		d          = new(Difference)
	)
	err := s.db.QueryRowContext(ctx, "SELECT qts, state_given_at FROM encrypted_chat_devices WHERE auth_key_id = ?", in.Device.AuthKey).Scan(&qts, &given)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("secretchat: difference state: %w", err)
	}
	d.Qts = qts
	if in.Qts < qts {
		if d.Messages, d.More, err = s.queuePage(ctx, in.Device.AuthKey, in.Qts, qts); err != nil {
			return nil, err
		}
		if d.More {
			d.Qts = d.Messages[len(d.Messages)-1].Qts
		}
	}
	if !in.OnlyQueue {
		if d.Updates, err = s.changes(ctx, in.Device, min(in.Date, given)-eventMargin); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// MarkDifference records that d was given a final difference.
func (s *Store) MarkDifference(ctx context.Context, d Device) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO encrypted_chat_devices (auth_key_id, user_id, state_given_at) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE state_given_at = VALUES(state_given_at)", d.AuthKey, d.UserId, s.unix())
	return err
}

// queuePage reads the queue after from, up to the qts upTo read before it:
// a row with a larger qts may not be alone in having committed.
func (s *Store) queuePage(ctx context.Context, authKey int64, from, upTo int32) ([]*QueuedMessage, bool, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT qts, chat_id, random_id, date, service, data, file_id, file_access_hash, file_size, file_dc_id, file_key_fingerprint FROM encrypted_chat_queue WHERE auth_key_id = ? AND qts > ? AND qts <= ? ORDER BY qts LIMIT ?",
		authKey, from, upTo, differencePage+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var (
		out  []*QueuedMessage
		more bool
		size int
	)
	for rows.Next() {
		if len(out) == differencePage || size >= differenceBytes {
			more = true
			break
		}
		m := new(QueuedMessage)
		var f File
		if err = rows.Scan(&m.Qts, &m.ChatId, &m.RandomId, &m.Date, &m.Service, &m.Data, &f.Id, &f.AccessHash, &f.Size, &f.DcId, &f.KeyFingerprint); err != nil {
			return nil, false, err
		}
		if f.Id != 0 {
			m.File = &f
		}
		size += len(m.Data)
		out = append(out, m)
	}
	return out, more, rows.Err()
}

// changes returns the chats of d that changed state or were read since.
func (s *Store) changes(ctx context.Context, d Device, since int32) ([]*DifferenceUpdate, error) {
	var disabled bool
	err := s.db.QueryRowContext(ctx, "SELECT encrypted_requests_disabled FROM auth_users WHERE auth_key_id = ? AND user_id = ? AND deleted = 0", d.AuthKey, d.UserId).Scan(&disabled)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+chatColumns+" FROM encrypted_chats WHERE (admin_auth_key_id = ? AND admin_id = ?) OR participant_id = ?", d.AuthKey, d.UserId, d.UserId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*DifferenceUpdate
	for rows.Next() {
		c, err := scanChat(rows)
		if err != nil {
			return nil, err
		}
		admin := c.AdminId == d.UserId && c.AdminAuthKey == d.AuthKey
		own := admin || c.ParticipantAuthKey == d.AuthKey
		if c.StateAt >= since && shouldTell(c, d, admin, disabled) {
			out = append(out, &DifferenceUpdate{Chat: c})
		}
		if c.State == StateActive && own {
			switch {
			case admin && c.ParticipantReadAt >= since && c.ParticipantRead > 0:
				out = append(out, &DifferenceUpdate{Chat: c, ReadMax: c.ParticipantRead, ReadAt: c.ParticipantReadAt})
			case !admin && c.AdminReadAt >= since && c.AdminRead > 0:
				out = append(out, &DifferenceUpdate{Chat: c, ReadMax: c.AdminRead, ReadAt: c.AdminReadAt})
			}
		}
	}
	return out, rows.Err()
}

// shouldTell says whether a state change of c is news to d. A request goes
// to the participant's authorizations that take requests; the requester
// knows its own. The authorization that discarded or accepted knows it too.
func shouldTell(c *Chat, d Device, admin, requestsDisabled bool) bool {
	switch c.State {
	case StateRequested:
		return !admin && !requestsDisabled
	case StateActive:
		return admin || c.ParticipantAuthKey != d.AuthKey
	default:
		return c.DiscardedBy == 0 || c.DiscardedBy != d.AuthKey
	}
}

// SaveFile records an encrypted file.
func (s *Store) SaveFile(ctx context.Context, f *File) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO encrypted_files (encrypted_file_id, access_hash, dc_id, file_size, key_fingerprint, file_path) VALUES (?, ?, ?, ?, ?, ?) ON DUPLICATE KEY UPDATE access_hash = access_hash",
		f.Id, f.AccessHash, f.DcId, f.Size, f.KeyFingerprint, fmt.Sprintf("%d.dat", f.Id))
	return err
}

// File returns the encrypted file id, nil if there is none.
func (s *Store) File(ctx context.Context, id int64) (*File, error) {
	f := &File{Id: id}
	err := s.db.QueryRowContext(ctx, "SELECT access_hash, dc_id, file_size, key_fingerprint FROM encrypted_files WHERE encrypted_file_id = ?", id).
		Scan(&f.AccessHash, &f.DcId, &f.Size, &f.KeyFingerprint)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return f, nil
}

// Discarded is a chat the sweep ended, with whom to tell.
type Discarded struct {
	Chat    *Chat
	Targets []Device
}

// Sweep is one round of the janitor. It ends the chats of authorizations
// that are gone (logged out, terminated; "the event queue of the
// corresponding device will be forcibly cleared",
// core.telegram.org/api/end-to-end) and the requests nobody answered, and
// deletes what the retention allows. It is safe to run on several
// instances at once: a chat is ended once.
func (s *Store) Sweep(ctx context.Context) ([]*Discarded, error) {
	cutoff := s.unix() - int32(retention/time.Second)
	queries := []struct {
		q    string
		args []any
	}{
		{"SELECT c.id FROM encrypted_chats c LEFT JOIN auth_users a ON a.auth_key_id = c.admin_auth_key_id AND a.user_id = c.admin_id AND a.deleted = 0 WHERE c.state IN (1, 2) AND a.id IS NULL LIMIT ?", []any{sweepBatch}},
		{"SELECT c.id FROM encrypted_chats c LEFT JOIN auth_users a ON a.auth_key_id = c.participant_auth_key_id AND a.user_id = c.participant_id AND a.deleted = 0 WHERE c.state = 2 AND a.id IS NULL LIMIT ?", []any{sweepBatch}},
		{"SELECT id FROM encrypted_chats WHERE state = 1 AND date < ? LIMIT ?", []any{cutoff, sweepBatch}},
	}
	var out []*Discarded
	for _, q := range queries {
		ids, err := s.idList(ctx, q.q, q.args...)
		if err != nil {
			return out, err
		}
		for _, id := range ids {
			c, wasRequested, err := s.Discard(ctx, Discard{ChatId: id, Server: true})
			if errors.Is(err, ErrAlreadyDeclined) || errors.Is(err, ErrChatNotFound) {
				continue
			}
			if err != nil {
				return out, err
			}
			targets, err := s.DiscardTargets(ctx, c, wasRequested)
			if err != nil {
				return out, err
			}
			out = append(out, &Discarded{Chat: c, Targets: targets})
		}
	}
	for _, q := range []struct {
		q    string
		args []any
	}{
		{"DELETE FROM encrypted_chat_queue WHERE date < ? LIMIT ?", []any{cutoff, sweepBatch}},
		{"DELETE FROM encrypted_chat_sent WHERE date < ? LIMIT ?", []any{cutoff, sweepBatch}},
		{"DELETE FROM encrypted_chats WHERE state = 3 AND state_at < ? LIMIT ?", []any{cutoff, sweepBatch}},
	} {
		if _, err := s.db.ExecContext(ctx, q.q, q.args...); err != nil {
			return out, err
		}
	}
	return out, s.sweepDevices(ctx)
}

// sweepDevices clears the state and queues of authorizations that are gone.
func (s *Store) sweepDevices(ctx context.Context) error {
	ids, err := s.idList64(ctx, "SELECT d.auth_key_id FROM encrypted_chat_devices d LEFT JOIN auth_users a ON a.auth_key_id = d.auth_key_id AND a.deleted = 0 WHERE a.id IS NULL LIMIT ?", sweepBatch)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err = s.db.ExecContext(ctx, "DELETE FROM encrypted_chat_queue WHERE auth_key_id = ?", id); err != nil {
			return err
		}
		if _, err = s.db.ExecContext(ctx, "DELETE FROM encrypted_chat_devices WHERE auth_key_id = ?", id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) idList(ctx context.Context, q string, args ...any) ([]int32, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int32
	for rows.Next() {
		var id int32
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) idList64(ctx context.Context, q string, args ...any) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
