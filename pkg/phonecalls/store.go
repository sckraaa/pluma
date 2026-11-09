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
// not depend on what a client reports. video tells that the call ended with
// a camera on: phone.discardCall carries the state at the end (tdesktop
// calls_call.cpp:1563), a voice call may have become a video one.
func (s *Store) Discard(ctx context.Context, id int64, reason Reason, video bool) (*Call, bool, error) {
	return s.discard(ctx, id, reason, video, "")
}

// DiscardAsParticipant is Discard from the device authKeyId of the
// participant: it has no effect once another device accepted, so of a device
// that accepts and one that declines the first one wins.
func (s *Store) DiscardAsParticipant(ctx context.Context, id, authKeyId int64, reason Reason, video bool) (*Call, bool, error) {
	return s.discard(ctx, id, reason, video, " and participant_auth_key_id in (0, ?)", authKeyId)
}

// Expire ends a call Due returned, if it is still in state and its deadline
// is still past: a step that committed after Due renewed it.
func (s *Store) Expire(ctx context.Context, id int64, state State, reason Reason) (*Call, bool, error) {
	return s.discard(ctx, id, reason, false, " and state = ? and deadline > 0 and deadline <= ?", state, int32(s.now().Unix()))
}

func (s *Store) discard(ctx context.Context, id int64, reason Reason, video bool, cond string, condArgs ...any) (*Call, bool, error) {
	now := int32(s.now().Unix())
	args := append([]any{StateDiscarded, reason, now, video, now, id, StateRequested, StateAccepted, StateActive}, condArgs...)
	return s.transition(ctx, id,
		"update phone_calls set state = ?, discard_reason = ?, end_date = ?, deadline = 0, video = video or ?, duration = if(start_date > 0, ? - start_date, 0) where id = ? and state in (?, ?, ?)"+cond,
		args...)
}

// Due returns the calls whose state has expired. The sweep can run on any
// number of instances: Discard lets only one of them finish a call.
func (s *Store) Due(ctx context.Context, limit int) ([]*Call, error) {
	now := int32(s.now().Unix())
	return s.many(ctx, "where state in (?, ?, ?) and deadline > 0 and deadline <= ? order by deadline limit ?",
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

// MarkOffered records that the live update of the ringing call goes to the
// authorizations.
func (s *Store) MarkOffered(ctx context.Context, callId int64, authKeyIds ...int64) error {
	now := int32(s.now().Unix())
	for _, k := range authKeyIds {
		if _, err := s.db.Exec(ctx, "insert ignore into phone_call_deliveries (call_id, auth_key_id, offers, offered_at, live) values (?, ?, 1, ?, 1)", callId, k, now); err != nil {
			return err
		}
	}
	return nil
}

// MarkReceived records phone.receivedCall: the only proof that a device
// holds the call.
func (s *Store) MarkReceived(ctx context.Context, callId, authKeyId int64) error {
	_, err := s.db.Exec(ctx, "insert into phone_call_deliveries (call_id, auth_key_id, received) values (?, ?, 1) on duplicate key update received = 1", callId, authKeyId)
	return err
}

// Delivery is what is known about one authorization and one call.
type Delivery struct {
	Offers   int32 `db:"offers"`
	Live     bool  `db:"live"`
	Received bool  `db:"received"`
}

// Repeated tells whether the device may already hold the call when it says
// it is busy: then the answer can be Android's about this very call, since
// it declines any second phoneCallRequested without comparing ids
// (MessagesController.java:20377). A difference may have crossed the live
// update of the same call, so only one live offer that nothing followed
// proves the opposite.
func (d *Delivery) Repeated() bool {
	return d != nil && (d.Received || d.Offers > 1 || !d.Live)
}

// DeliveryOf returns the record of the authorization for the call, nil if
// the call was never offered to it.
func (s *Store) DeliveryOf(ctx context.Context, callId, authKeyId int64) (*Delivery, error) {
	var d Delivery
	err := s.db.QueryRowPartial(ctx, &d, "select offers, live, received from phone_call_deliveries where call_id = ? and auth_key_id = ?", callId, authKeyId)
	if errors.Is(err, sqlx.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// reoffer takes the next offer of the call to the authorization, if the
// device has not confirmed it and the last offer is ReofferAfter old.
func (s *Store) reoffer(ctx context.Context, callId, authKeyId int64, cond string, condArgs ...any) (bool, error) {
	now := s.now()
	args := append([]any{int32(now.Unix()), callId, authKeyId, int32(now.Add(-ReofferAfter).Unix())}, condArgs...)
	r, err := s.db.Exec(ctx, "update phone_call_deliveries set offers = offers + 1, offered_at = ? where call_id = ? and auth_key_id = ? and received = 0 and offered_at <= ?"+cond, args...)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n > 0, err
}

// ClaimRinging returns the calls ringing for userId that the authorization
// does not hold, and records the offer: a device woken by a push learns of
// the call from updates.getDifference. A call offered before and not
// confirmed by phone.receivedCall comes again after ReofferAfter: the answer
// or the live update may have been lost. When a step fails, the calls
// claimed so far are returned with the error.
func (s *Store) ClaimRinging(ctx context.Context, userId, authKeyId int64) ([]*Call, error) {
	calls, err := s.many(ctx,
		"where participant_id = ? and state = ? and not exists (select 1 from phone_call_deliveries d where d.call_id = phone_calls.id and d.auth_key_id = ? and (d.received = 1 or d.offered_at > ?)) order by `date` limit 4",
		userId, StateRequested, authKeyId, int32(s.now().Add(-ReofferAfter).Unix()))
	if err != nil {
		return nil, err
	}
	out := make([]*Call, 0, len(calls))
	for _, c := range calls {
		claimed, err := s.claimOffer(ctx, c.Id, authKeyId)
		if err != nil {
			return out, err
		}
		if claimed {
			out = append(out, c)
		}
	}
	return out, nil
}

func (s *Store) claimOffer(ctx context.Context, callId, authKeyId int64) (bool, error) {
	again, err := s.reoffer(ctx, callId, authKeyId, "")
	if err != nil || again {
		return again, err
	}
	r, err := s.db.Exec(ctx, "insert ignore into phone_call_deliveries (call_id, auth_key_id, offers, offered_at) values (?, ?, 1, ?)", callId, authKeyId, int32(s.now().Unix()))
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n > 0, err
}

// Device is one authorization and one call.
type Device struct {
	CallId    int64 `db:"call_id"`
	AuthKeyId int64 `db:"auth_key_id"`
}

// Unconfirmed returns the devices offered a ringing call that did not
// confirm it and may be offered it again by the sweep.
func (s *Store) Unconfirmed(ctx context.Context, limit int) ([]Device, error) {
	var out []Device
	err := s.db.QueryRowsPartial(ctx, &out,
		"select d.call_id, d.auth_key_id from phone_call_deliveries d join phone_calls c on c.id = d.call_id where d.received = 0 and d.offers between 1 and ? and d.offered_at <= ? and c.state = ? limit ?",
		maxSweepOffers-1, int32(s.now().Add(-ReofferAfter).Unix()), StateRequested, limit)
	if err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	return out, nil
}

// ClaimUnconfirmed takes the sweep's next offer of the call to the device;
// false if it was confirmed or taken meanwhile.
func (s *Store) ClaimUnconfirmed(ctx context.Context, d Device) (bool, error) {
	return s.reoffer(ctx, d.CallId, d.AuthKeyId, " and offers < ?", maxSweepOffers)
}

// Rung returns the authorizations a ringing call was offered to or that
// confirmed it: the devices that ring and must hear that it stopped.
func (s *Store) Rung(ctx context.Context, callId int64) ([]int64, error) {
	var rows []Device
	err := s.db.QueryRowsPartial(ctx, &rows, "select call_id, auth_key_id from phone_call_deliveries where call_id = ? and (offers > 0 or received = 1)", callId)
	if err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	out := make([]int64, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.AuthKeyId)
	}
	return out, nil
}

// MarkMissed records that an update of the call did not reach the
// authorizations.
func (s *Store) MarkMissed(ctx context.Context, callId int64, authKeyIds ...int64) error {
	for _, k := range authKeyIds {
		if _, err := s.db.Exec(ctx, "insert into phone_call_deliveries (call_id, auth_key_id, missed) values (?, ?, 1) on duplicate key update missed = 1", callId, k); err != nil {
			return err
		}
	}
	return nil
}

// Missed returns the devices that have an update of a call to catch up on.
func (s *Store) Missed(ctx context.Context, limit int) ([]Device, error) {
	var out []Device
	err := s.db.QueryRowsPartial(ctx, &out, "select call_id, auth_key_id from phone_call_deliveries where missed = 1 limit ?", limit)
	if err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	return out, nil
}

// ClaimMissed takes the catching up of the device: only one sweep sends it,
// once, because Telegram-iOS drops a call on a repeated phoneCallAccepted
// (CallSessionManager.swift:1131).
func (s *Store) ClaimMissed(ctx context.Context, d Device) (bool, error) {
	r, err := s.db.Exec(ctx, "update phone_call_deliveries set missed = 0 where call_id = ? and auth_key_id = ? and missed = 1", d.CallId, d.AuthKeyId)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n > 0, err
}

// DropMissed forgets what devices missed of the calls that ended before t.
func (s *Store) DropMissed(ctx context.Context, before time.Time) error {
	_, err := s.db.Exec(ctx, "update phone_call_deliveries d join phone_calls c on c.id = d.call_id set d.missed = 0 where d.missed = 1 and c.state = ? and c.end_date < ?", StateDiscarded, int32(before.Unix()))
	return err
}

// RecentCalls counts the calls admin started since the given moment, in all
// and to participant.
func (s *Store) RecentCalls(ctx context.Context, admin, participant int64, since time.Time) (total, toParticipant int64, err error) {
	var r struct {
		Total int64 `db:"total"`
		Peer  int64 `db:"peer"`
	}
	err = s.db.QueryRowPartial(ctx, &r, "select count(*) as total, cast(coalesce(sum(participant_id = ?), 0) as signed) as peer from phone_calls where admin_id = ? and `date` >= ?", participant, admin, int32(since.Unix()))
	return r.Total, r.Peer, err
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
