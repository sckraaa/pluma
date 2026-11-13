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

package groupcalls

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/teamgram/marmota/pkg/stores/sqlx"
)

// Errors of the Store.
var (
	// ErrNotFound: no such call, or no such participant.
	ErrNotFound = errors.New("groupcalls: not found")
	// ErrAlreadyActive: the peer has a call that is not over.
	ErrAlreadyActive = errors.New("groupcalls: the peer has a call")
	// ErrDiscarded: the call is over.
	ErrDiscarded = errors.New("groupcalls: the call is over")
	// ErrScheduled: the call has not started.
	ErrScheduled = errors.New("groupcalls: the call has not started")
	// ErrNotScheduled: the call has started already.
	ErrNotScheduled = errors.New("groupcalls: the call is not scheduled")
	// ErrNotJoined: the account is not in the call (with that source).
	ErrNotJoined = errors.New("groupcalls: not in the call")
	// ErrSourceInUse: another participant has the source
	// (GROUPCALL_SSRC_DUPLICATE_MUCH).
	ErrSourceInUse = errors.New("groupcalls: source in use")
	// ErrFull: the call has MaxParticipants.
	ErrFull = errors.New("groupcalls: too many participants")
	// ErrMoved: the room of the call went to another node while a join
	// connected.
	ErrMoved = errors.New("groupcalls: the room is on another node")
)

// Store is the tables of migrate-20261117-group-calls.sql and
// migrate-20261118-group-call-chain.sql. A join or a
// leave locks the row of its call and changes the version by one in the
// same transaction, so that the versions the clients follow have no gaps
// and no repeats (ADR 0059 §2); every other change is one statement.
// Reads go to the primary: a call moves on within milliseconds.
type Store struct {
	db  *sqlx.DB
	now func() time.Time
}

// NewStore returns the Store on db.
func NewStore(db *sqlx.DB) *Store {
	return &Store{db: db.Master(), now: time.Now}
}

type callRow struct {
	Id                int64        `db:"id"`
	AccessHash        int64        `db:"access_hash"`
	PeerType          int32        `db:"peer_type"`
	PeerId            int64        `db:"peer_id"`
	CreatorId         int64        `db:"creator_id"`
	RandomId          int32        `db:"random_id"`
	Title             string       `db:"title"`
	JoinMuted         bool         `db:"join_muted"`
	Version           int32        `db:"version"`
	ScheduleDate      int32        `db:"schedule_date"`
	Date              int32        `db:"date"`
	StartDate         int32        `db:"start_date"`
	EndDate           int32        `db:"end_date"`
	Duration          int32        `db:"duration"`
	Live              sql.NullBool `db:"live"`
	ListenerHash      string       `db:"listener_hash"`
	SpeakerHash       string       `db:"speaker_hash"`
	SfuNode           string       `db:"sfu_node"`
	Posted            int8         `db:"posted"`
	ChainHeight       int32        `db:"chain_height"`
	ChainHash         []byte       `db:"chain_hash"`
	BroadcastCount    int32        `db:"broadcast_count"`
	ParticipantsCount int32        `db:"participants_count"`
	VideoCount        int32        `db:"video_count"`
}

// callColumns reads a call with the number of its participants and of
// their cameras: at most MaxParticipants rows each, counted in the same
// statement, so that the numbers never drift from the rows.
const callColumns = "c.id, c.access_hash, c.peer_type, c.peer_id, c.creator_id, c.random_id, c.title, c.join_muted, c.version, c.schedule_date, c.`date`, c.start_date, c.end_date, c.duration, c.live, c.listener_hash, c.speaker_hash, c.sfu_node, c.posted, c.chain_height, c.chain_hash, c.broadcast_count," +
	" (select count(*) from group_call_participants p where p.call_id = c.id and p.in_call = 1) as participants_count," +
	" (select count(*) from group_call_participants p where p.call_id = c.id and p.in_call = 1 and p.video_joined = 1 and p.video_stopped = 0 and p.video <> '') as video_count"

func (r *callRow) call() *Call {
	c := &Call{
		Id: r.Id, AccessHash: r.AccessHash, PeerType: r.PeerType, PeerId: r.PeerId, CreatorId: r.CreatorId,
		RandomId: r.RandomId, Title: r.Title, JoinMuted: r.JoinMuted, Version: r.Version,
		ParticipantsCount: r.ParticipantsCount, VideoCount: r.VideoCount, ScheduleDate: r.ScheduleDate,
		Date: r.Date, StartDate: r.StartDate, EndDate: r.EndDate, Duration: r.Duration,
		Discarded: !r.Live.Valid, ListenerHash: r.ListenerHash, SpeakerHash: r.SpeakerHash,
		SfuNode: r.SfuNode, Posted: r.Posted, ChainHeight: r.ChainHeight, BroadcastCount: r.BroadcastCount,
	}
	copy(c.ChainHash[:], r.ChainHash)
	return c
}

type querier interface {
	QueryRowPartial(ctx context.Context, v any, query string, args ...any) error
	QueryRowsPartial(ctx context.Context, v any, query string, args ...any) error
}

// dbq and txq read through the pool or a transaction.
type dbq struct{ db *sqlx.DB }

func (q dbq) QueryRowPartial(ctx context.Context, v any, query string, args ...any) error {
	return q.db.QueryRowPartial(ctx, v, query, args...)
}

func (q dbq) QueryRowsPartial(ctx context.Context, v any, query string, args ...any) error {
	return q.db.QueryRowsPartial(ctx, v, query, args...)
}

type txq struct{ tx *sqlx.Tx }

func (q txq) QueryRowPartial(_ context.Context, v any, query string, args ...any) error {
	return q.tx.QueryRowPartial(v, query, args...)
}

func (q txq) QueryRowsPartial(_ context.Context, v any, query string, args ...any) error {
	return q.tx.QueryRowsPartial(v, query, args...)
}

func oneCall(ctx context.Context, q querier, where string, args ...any) (*Call, error) {
	var r callRow
	err := q.QueryRowPartial(ctx, &r, "select "+callColumns+" from group_calls c "+where, args...)
	if errors.Is(err, sqlx.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return r.call(), nil
}

func manyCalls(ctx context.Context, q querier, where string, args ...any) ([]*Call, error) {
	var rows []callRow
	err := q.QueryRowsPartial(ctx, &rows, "select "+callColumns+" from group_calls c "+where, args...)
	if err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	out := make([]*Call, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].call())
	}
	return out, nil
}

// Get returns a call, ErrNotFound if there is none.
func (s *Store) Get(ctx context.Context, id int64) (*Call, error) {
	return oneCall(ctx, dbq{s.db}, "where c.id = ?", id)
}

// Active returns the call of a peer that is not over (scheduled or
// started), ErrNotFound if it has none.
func (s *Store) Active(ctx context.Context, peerType int32, peerId int64) (*Call, error) {
	return oneCall(ctx, dbq{s.db}, "where c.peer_type = ? and c.peer_id = ? and c.live = 1", peerType, peerId)
}

// ActiveOf returns the calls that are not over of the peers of a type
// among ids, by peer id.
func (s *Store) ActiveOf(ctx context.Context, peerType int32, ids []int64) (map[int64]*Call, error) {
	out := make(map[int64]*Call, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	args := []any{peerType}
	for _, id := range ids {
		args = append(args, id)
	}
	calls, err := manyCalls(ctx, dbq{s.db}, "where c.peer_type = ? and c.live = 1 and c.peer_id in ("+placeholders(len(ids))+")", args...)
	if err != nil {
		return nil, err
	}
	for _, c := range calls {
		out[c.PeerId] = c
	}
	return out, nil
}

// Create stores the new call c (Date, Version and, unless it is scheduled,
// StartDate are set here). A repeat of the same request (creator, peer,
// random_id) returns the call stored the first time with created false;
// ErrAlreadyActive if the peer has another call that is not over.
func (s *Store) Create(ctx context.Context, c *Call) (*Call, bool, error) {
	if first, err := oneCall(ctx, dbq{s.db}, "where c.creator_id = ? and c.peer_type = ? and c.peer_id = ? and c.random_id = ?",
		c.CreatorId, c.PeerType, c.PeerId, c.RandomId); err == nil {
		return first, false, nil
	} else if !errors.Is(err, ErrNotFound) {
		return nil, false, err
	}
	now := int32(s.now().Unix())
	c.Date, c.Version = now, 1
	if c.ScheduleDate == 0 {
		c.StartDate = now
	}
	_, err := s.db.Exec(ctx, "insert into group_calls (id, access_hash, peer_type, peer_id, creator_id, random_id, title, version, schedule_date, `date`, start_date, live) values (?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, 1)",
		c.Id, c.AccessHash, c.PeerType, c.PeerId, c.CreatorId, c.RandomId, c.Title, c.ScheduleDate, c.Date, c.StartDate)
	if isDuplicate(err) {
		// The same request racing itself, or another call of the peer.
		if first, err := oneCall(ctx, dbq{s.db}, "where c.creator_id = ? and c.peer_type = ? and c.peer_id = ? and c.random_id = ?",
			c.CreatorId, c.PeerType, c.PeerId, c.RandomId); err == nil {
			return first, false, nil
		}
		return nil, false, ErrAlreadyActive
	}
	if err != nil {
		return nil, false, err
	}
	created, err := s.Get(ctx, c.Id)
	return created, err == nil, err
}

// Start starts a scheduled call. won is false when it had started or was
// over already; the call is returned either way.
func (s *Store) Start(ctx context.Context, id int64) (*Call, bool, error) {
	return s.change(ctx, id, "update group_calls set start_date = ? where id = ? and live = 1 and start_date = 0", int32(s.now().Unix()), id)
}

// Discard ends a call: its participants leave with it. won is false when
// it was over already. left are those who were in it.
func (s *Store) Discard(ctx context.Context, id int64) (call *Call, left []*Participant, won bool, err error) {
	err = s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		c, err := oneCall(ctx, txq{tx}, "where c.id = ? for update", id)
		if err != nil {
			return err
		}
		if c.Discarded {
			call = c
			return nil
		}
		if left, err = participants(ctx, txq{tx}, "where call_id = ? and in_call = 1", id); err != nil {
			return err
		}
		now := int32(s.now().Unix())
		duration := int32(0)
		if c.StartDate != 0 {
			duration = max(now-c.StartDate, 0)
		}
		if _, err = tx.Exec("update group_call_participants set in_call = 0, source = null, presentation_source = null, presentation = '', raise_hand_rating = 0 where call_id = ? and in_call = 1", id); err != nil {
			return err
		}
		if _, err = tx.Exec("update group_calls set live = null, end_date = ?, duration = ? where id = ?", now, duration, id); err != nil {
			return err
		}
		if _, err = tx.Exec("delete from group_call_subscriptions where call_id = ?", id); err != nil {
			return err
		}
		won = true
		call, err = oneCall(ctx, txq{tx}, "where c.id = ?", id)
		return err
	})
	for _, p := range left {
		p.InCall, p.Source, p.PresentationSource, p.Presentation, p.RaiseHandRating = false, 0, 0, nil, 0
	}
	return call, left, won, err
}

// change applies one conditional update of a call and returns the call
// afterwards and whether this update made a change.
func (s *Store) change(ctx context.Context, id int64, query string, args ...any) (*Call, bool, error) {
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

// SetTitle renames a call that is not over.
func (s *Store) SetTitle(ctx context.Context, id int64, title string) (*Call, bool, error) {
	return s.change(ctx, id, "update group_calls set title = ? where id = ? and live = 1 and title <> ?", title, id, title)
}

// SetJoinMuted turns join_muted of a call that is not over on or off.
func (s *Store) SetJoinMuted(ctx context.Context, id int64, on bool) (*Call, bool, error) {
	return s.change(ctx, id, "update group_calls set join_muted = ? where id = ? and live = 1 and join_muted <> ?", on, id, on)
}

// SetInviteHashes stores new invite hashes of a call that is not over;
// an empty one keeps the old.
func (s *Store) SetInviteHashes(ctx context.Context, id int64, listener, speaker string) (*Call, bool, error) {
	return s.change(ctx, id, "update group_calls set listener_hash = if(? = '', listener_hash, ?), speaker_hash = if(? = '', speaker_hash, ?) where id = ? and live = 1",
		listener, listener, speaker, speaker, id)
}

// InitInviteHashes sets the invite hashes of a call that has none; a
// call that has them keeps them (two exports racing get the same links).
func (s *Store) InitInviteHashes(ctx context.Context, id int64, listener, speaker string) (*Call, error) {
	c, _, err := s.change(ctx, id, "update group_calls set listener_hash = ?, speaker_hash = ? where id = ? and live = 1 and listener_hash = ''", listener, speaker, id)
	return c, err
}

// SetNode moves a call's room to node if it is still on old: of two joins
// that race to place a lost room, one wins (ADR 0059 §10).
func (s *Store) SetNode(ctx context.Context, id int64, old, node string) (*Call, bool, error) {
	return s.change(ctx, id, "update group_calls set sfu_node = ? where id = ? and live = 1 and sfu_node = ?", node, id, old)
}

// MarkPosted records that a service message of a call is in the history.
func (s *Store) MarkPosted(ctx context.Context, id int64, bit int8) error {
	_, err := s.db.Exec(ctx, "update group_calls set posted = posted | ? where id = ?", bit, id)
	return err
}

// Unposted returns the calls created after since with a service message
// that is not in the history yet, at most limit: the schedule of a
// scheduled call, the start of a started one, the end of one that started
// and is over.
func (s *Store) Unposted(ctx context.Context, since time.Time, limit int) ([]*Call, error) {
	return manyCalls(ctx, dbq{s.db},
		"where c.`date` > ? and (c.schedule_date <> 0 and (c.posted & ?) = 0 or c.start_date <> 0 and (c.posted & ?) = 0 or c.start_date <> 0 and c.live is null and (c.posted & ?) = 0) order by c.id limit ?",
		int32(since.Unix()), PostedScheduled, PostedStart, PostedEnd, limit)
}

// Placed returns the calls that are not over and have a room on a node
// or someone in them, at most limit after the id after.
func (s *Store) Placed(ctx context.Context, after int64, limit int) ([]*Call, error) {
	return manyCalls(ctx, dbq{s.db}, "where c.live = 1 and c.sfu_node <> '' and c.id > ? order by c.id limit ?", after, limit)
}

// Over returns the calls that ended after since with a room still named
// on a node, at most limit after the id after: their rooms are closed.
func (s *Store) Over(ctx context.Context, since time.Time, after int64, limit int) ([]*Call, error) {
	return manyCalls(ctx, dbq{s.db}, "where c.live is null and c.end_date > ? and c.sfu_node <> '' and c.id > ? order by c.id limit ?",
		int32(since.Unix()), after, limit)
}

// ForgetEmpty clears the node of a call that is not over, still on node,
// and has no one in it: the collector closed its empty room. A join that
// is in the call by then keeps the node; one that comes later places the
// room again (Join).
func (s *Store) ForgetEmpty(ctx context.Context, id int64, node string) (bool, error) {
	var done bool
	err := s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		done = false
		var nodes []string
		if err := tx.QueryRowsPartial(&nodes, "select sfu_node from group_calls where id = ? and live = 1 and sfu_node = ? for update", id, node); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
			return err
		}
		if len(nodes) == 0 {
			return nil
		}
		var in []int64
		if err := tx.QueryRowsPartial(&in, "select peer_id from group_call_participants where call_id = ? and in_call = 1 limit 1 lock in share mode", id); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
			return err
		}
		if len(in) > 0 {
			return nil
		}
		if _, err := tx.Exec("update group_calls set sfu_node = '' where id = ?", id); err != nil {
			return err
		}
		done = true
		return nil
	})
	return done, err
}

// ForgetNode clears the node of a call that is over and still on node.
func (s *Store) ForgetNode(ctx context.Context, id int64, node string) error {
	_, err := s.db.Exec(ctx, "update group_calls set sfu_node = '' where id = ? and live is null and sfu_node = ?", id, node)
	return err
}

// Subscribe adds or removes the subscription of userId to a scheduled call.
func (s *Store) Subscribe(ctx context.Context, id, userId int64, on bool) error {
	var err error
	if on {
		_, err = s.db.Exec(ctx, "insert ignore into group_call_subscriptions (call_id, user_id) values (?, ?)", id, userId)
	} else {
		_, err = s.db.Exec(ctx, "delete from group_call_subscriptions where call_id = ? and user_id = ?", id, userId)
	}
	return err
}

// Subscribed reports whether userId waits for a call.
func (s *Store) Subscribed(ctx context.Context, id, userId int64) (bool, error) {
	var n []int64
	if err := s.db.QueryRowsPartial(ctx, &n, "select user_id from group_call_subscriptions where call_id = ? and user_id = ?", id, userId); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return false, err
	}
	return len(n) > 0, nil
}

// Subscribers returns who waits for a call.
func (s *Store) Subscribers(ctx context.Context, id int64) ([]int64, error) {
	var ids []int64
	if err := s.db.QueryRowsPartial(ctx, &ids, "select user_id from group_call_subscriptions where call_id = ?", id); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	return ids, nil
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

func isDuplicate(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}
