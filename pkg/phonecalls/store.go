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

package phonecalls

import (
	"context"
	"errors"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
)

// ErrNotFound is the error of Get for an unknown call.
var ErrNotFound = errors.New("phonecalls: no such call")

// ErrDuplicate: phone.requestCall with a random_id its author already used
// for another call.
var ErrDuplicate = errors.New("phonecalls: random_id already used")

// Store is the table phone_calls (migrate-20261109-phone-calls.sql). Every
// transition is one conditional UPDATE, so it needs no lock and survives a
// restart; reads go to the primary, a call moves on within milliseconds.
type Store struct {
	db  *sqlx.DB
	now func() time.Time
}

// NewStore returns the Store on db.
func NewStore(db *sqlx.DB) *Store {
	return &Store{db: db.Master(), now: time.Now}
}

type row struct {
	Id                   int64  `db:"id"`
	AccessHash           int64  `db:"access_hash"`
	AdminId              int64  `db:"admin_id"`
	ParticipantId        int64  `db:"participant_id"`
	AdminAuthKeyId       int64  `db:"admin_auth_key_id"`
	ParticipantAuthKeyId int64  `db:"participant_auth_key_id"`
	RandomId             int32  `db:"random_id"`
	Video                bool   `db:"video"`
	P2PAllowed           bool   `db:"p2p_allowed"`
	State                int8   `db:"state"`
	GAHash               []byte `db:"g_a_hash"`
	GB                   []byte `db:"g_b"`
	GA                   []byte `db:"g_a"`
	KeyFingerprint       int64  `db:"key_fingerprint"`
	AdminProtocol        string `db:"admin_protocol"`
	AcceptProtocol       string `db:"accept_protocol"`
	ConfirmProtocol      string `db:"confirm_protocol"`
	Date                 int32  `db:"date"`
	ReceiveDate          int32  `db:"receive_date"`
	StartDate            int32  `db:"start_date"`
	EndDate              int32  `db:"end_date"`
	Deadline             int32  `db:"deadline"`
	DiscardReason        int8   `db:"discard_reason"`
	Duration             int32  `db:"duration"`
	MessagePosted        bool   `db:"message_posted"`
}

const columns = "id, access_hash, admin_id, participant_id, admin_auth_key_id, participant_auth_key_id, random_id, video, p2p_allowed, state, g_a_hash, g_b, g_a, key_fingerprint, admin_protocol, accept_protocol, confirm_protocol, `date`, receive_date, start_date, end_date, deadline, discard_reason, duration, message_posted"

func (r *row) call() (*Call, error) {
	c := &Call{
		Id: r.Id, AccessHash: r.AccessHash, AdminId: r.AdminId, ParticipantId: r.ParticipantId,
		AdminAuthKeyId: r.AdminAuthKeyId, ParticipantAuthKeyId: r.ParticipantAuthKeyId, RandomId: r.RandomId,
		Video: r.Video, P2PAllowed: r.P2PAllowed, State: State(r.State), GAHash: r.GAHash, GB: r.GB, GA: r.GA,
		KeyFingerprint: r.KeyFingerprint, Date: r.Date, ReceiveDate: r.ReceiveDate, StartDate: r.StartDate,
		EndDate: r.EndDate, Deadline: r.Deadline, Reason: Reason(r.DiscardReason), Duration: r.Duration,
		MessagePosted: r.MessagePosted,
	}
	var err error
	if c.AdminProtocol, err = unmarshalProtocol(r.AdminProtocol); err != nil {
		return nil, err
	}
	if c.AcceptProtocol, err = unmarshalProtocol(r.AcceptProtocol); err != nil {
		return nil, err
	}
	if c.ConfirmProtocol, err = unmarshalProtocol(r.ConfirmProtocol); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) one(ctx context.Context, query string, args ...any) (*Call, error) {
	var r row
	err := s.db.QueryRowPartial(ctx, &r, "select "+columns+" from phone_calls "+query, args...)
	if errors.Is(err, sqlx.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return r.call()
}

func (s *Store) many(ctx context.Context, query string, args ...any) ([]*Call, error) {
	var rows []row
	err := s.db.QueryRowsPartial(ctx, &rows, "select "+columns+" from phone_calls "+query, args...)
	if err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	out := make([]*Call, 0, len(rows))
	for i := range rows {
		c, err := rows[i].call()
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// Get returns a call, ErrNotFound if there is none.
func (s *Store) Get(ctx context.Context, id int64) (*Call, error) {
	return s.one(ctx, "where id = ?", id)
}

// Create stores the new call c (Date and the first deadline are set here).
// A repeat of the same request returns the call stored the first time, with
// created false; ErrDuplicate if its random_id was used for another call.
func (s *Store) Create(ctx context.Context, c *Call) (*Call, bool, error) {
	proto, err := c.AdminProtocol.marshal()
	if err != nil {
		return nil, false, err
	}
	now := s.now()
	c.Date = int32(now.Unix())
	c.State = StateRequested
	c.Deadline = int32(now.Add(ReceiveTimeout + Grace).Unix())
	r, err := s.db.Exec(ctx, "insert ignore into phone_calls (id, access_hash, admin_id, participant_id, admin_auth_key_id, random_id, video, p2p_allowed, state, g_a_hash, g_b, g_a, admin_protocol, accept_protocol, confirm_protocol, `date`, deadline) values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', '', ?, '', '', ?, ?)",
		c.Id, c.AccessHash, c.AdminId, c.ParticipantId, c.AdminAuthKeyId, c.RandomId, c.Video, c.P2PAllowed, StateRequested, c.GAHash, proto, c.Date, c.Deadline)
	if err != nil {
		return nil, false, err
	}
	if n, err := r.RowsAffected(); err != nil {
		return nil, false, err
	} else if n > 0 {
		return c, true, nil
	}
	first, err := s.one(ctx, "where admin_id = ? and random_id = ?", c.AdminId, c.RandomId)
	if err != nil {
		return nil, false, err
	}
	if first.ParticipantId != c.ParticipantId || string(first.GAHash) != string(c.GAHash) {
		return nil, false, ErrDuplicate
	}
	return first, false, nil
}

// transition applies one conditional update and returns the row afterwards
// and whether this call made the change.
func (s *Store) transition(ctx context.Context, id int64, query string, args ...any) (*Call, bool, error) {
	r, err := s.db.Exec(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return nil, false, err
	}
	c, err := s.Get(ctx, id)
	if err != nil {
		return nil, false, err
	}
	return c, n > 0, nil
}

// Received records that a device of the participant got the call: its ring
// time starts and the admin's phone may ring back. Only the first device
// counts.
func (s *Store) Received(ctx context.Context, id int64) (*Call, bool, error) {
	now := s.now()
	return s.transition(ctx, id,
		"update phone_calls set receive_date = ?, deadline = ? where id = ? and state = ? and receive_date = 0",
		int32(now.Unix()), int32(now.Add(RingTimeout+Grace).Unix()), id, StateRequested)
}

// Accept moves the call to Accepted for the device authKeyId if the call is
// still ringing: of the devices that race, one wins.
func (s *Store) Accept(ctx context.Context, id, authKeyId int64, gB []byte, p Protocol) (*Call, bool, error) {
	proto, err := p.marshal()
	if err != nil {
		return nil, false, err
	}
	now := s.now()
	return s.transition(ctx, id,
		"update phone_calls set state = ?, participant_auth_key_id = ?, g_b = ?, accept_protocol = ?, deadline = ? where id = ? and state = ?",
		StateAccepted, authKeyId, gB, proto, int32(now.Add(ConnectTimeout+Grace).Unix()), id, StateRequested)
}

// Confirm moves the call to Active with the admin's key material.
func (s *Store) Confirm(ctx context.Context, id int64, gA []byte, fingerprint int64, p Protocol) (*Call, bool, error) {
	proto, err := p.marshal()
	if err != nil {
		return nil, false, err
	}
	now := s.now()
	return s.transition(ctx, id,
		"update phone_calls set state = ?, g_a = ?, key_fingerprint = ?, confirm_protocol = ?, start_date = ?, deadline = ? where id = ? and state = ?",
		StateActive, gA, fingerprint, proto, int32(now.Unix()), int32(now.Add(MaxActive).Unix()), id, StateAccepted)
}

// Discard ends the call if it still goes on. The duration is measured here
// from the moment the keys were exchanged: the message in the history must
// not depend on what a client reports.
func (s *Store) Discard(ctx context.Context, id int64, reason Reason) (*Call, bool, error) {
	now := int32(s.now().Unix())
	return s.transition(ctx, id,
		"update phone_calls set state = ?, discard_reason = ?, end_date = ?, deadline = 0, duration = if(start_date > 0, ? - start_date, 0) where id = ? and state in (?, ?, ?)",
		StateDiscarded, reason, now, now, id, StateRequested, StateAccepted, StateActive)
}

// Due returns the calls whose state has expired. The sweep can run on any
// number of instances: Discard lets only one of them finish a call.
func (s *Store) Due(ctx context.Context, limit int) ([]*Call, error) {
	now := int32(s.now().Unix())
	return s.many(ctx, "where state in (?, ?, ?) and deadline > 0 and deadline <= ? limit ?",
		StateRequested, StateAccepted, StateActive, now, limit)
}

// Unposted returns the calls discarded before the given moment whose service
// message did not reach the history (a crash, msg unavailable).
func (s *Store) Unposted(ctx context.Context, before time.Time, limit int) ([]*Call, error) {
	return s.many(ctx, "where state = ? and message_posted = 0 and end_date < ? order by end_date limit ?",
		StateDiscarded, int32(before.Unix()), limit)
}

// MarkPosted records that the service message is in the history.
func (s *Store) MarkPosted(ctx context.Context, id int64) error {
	_, err := s.db.Exec(ctx, "update phone_calls set message_posted = 1 where id = ?", id)
	return err
}

// MarkDelivered records that the authorizations were told about the
// ringing call.
func (s *Store) MarkDelivered(ctx context.Context, callId int64, authKeyIds ...int64) error {
	for _, k := range authKeyIds {
		if _, err := s.db.Exec(ctx, "insert ignore into phone_call_deliveries (call_id, auth_key_id) values (?, ?)", callId, k); err != nil {
			return err
		}
	}
	return nil
}

// ClaimRinging returns the calls ringing for userId that the authorization
// was not told about, and records that it was: a device woken by a push
// learns of the call from updates.getDifference, once.
func (s *Store) ClaimRinging(ctx context.Context, userId, authKeyId int64) ([]*Call, error) {
	calls, err := s.many(ctx,
		"where participant_id = ? and state = ? and not exists (select 1 from phone_call_deliveries d where d.call_id = phone_calls.id and d.auth_key_id = ?) order by `date` limit 4",
		userId, StateRequested, authKeyId)
	if err != nil {
		return nil, err
	}
	out := calls[:0]
	for _, c := range calls {
		r, err := s.db.Exec(ctx, "insert ignore into phone_call_deliveries (call_id, auth_key_id) values (?, ?)", c.Id, authKeyId)
		if err != nil {
			return nil, err
		}
		if n, err := r.RowsAffected(); err != nil {
			return nil, err
		} else if n > 0 {
			out = append(out, c)
		}
	}
	return out, nil
}

// SetRating stores the rating one side gave the call.
func (s *Store) SetRating(ctx context.Context, id int64, role Role, rating int32, comment string) error {
	q := "update phone_calls set admin_rating = ?, admin_comment = ? where id = ?"
	if role == Participant {
		q = "update phone_calls set participant_rating = ?, participant_comment = ? where id = ?"
	}
	_, err := s.db.Exec(ctx, q, rating, comment, id)
	return err
}

// SetDebug stores the debug log of the call's library.
func (s *Store) SetDebug(ctx context.Context, id int64, debug string) error {
	_, err := s.db.Exec(ctx, "update phone_calls set debug = ? where id = ?", debug, id)
	return err
}

// Purge removes up to limit calls that ended before t and whose history
// message is written, with what was recorded about them.
func (s *Store) Purge(ctx context.Context, before time.Time, limit int) (int64, error) {
	cut := int32(before.Unix())
	if _, err := s.db.Exec(ctx, "delete from phone_call_deliveries where call_id in (select id from (select id from phone_calls where state = ? and message_posted = 1 and end_date < ? order by end_date limit ?) x)", StateDiscarded, cut, limit); err != nil {
		return 0, err
	}
	r, err := s.db.Exec(ctx, "delete from phone_calls where state = ? and message_posted = 1 and end_date < ? order by end_date limit ?", StateDiscarded, cut, limit)
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}
