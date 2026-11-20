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
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
)

// Conferences (task 117, ADR 0075) are group calls without a chat, in the
// same tables: the row of the call has peer_type 0 and peer_id its own id,
// so that the key live_peer holds, and group_call_conferences keeps the
// creator, the random_id of the request that created it and the slug of
// its link. Who may see and join one is the business of bff (its policy);
// the Store keeps the rows consistent under the lock of the call's row, as
// for the calls of chats.

const (
	// ConferencesPerCreator is the conferences one account has that are
	// not over (ADR 0075 §9).
	ConferencesPerCreator = 20
	// ConferenceEmptyEnd: a conference no one is in ends this long after
	// the last one left (decision 8 of task 117).
	ConferenceEmptyEnd = 5 * time.Minute
	// ConferenceUnusedEnd: a conference no one ever joined (a link made
	// ahead) ends this long after it was created.
	ConferenceUnusedEnd = 24 * time.Hour
	// ConferenceSlugKeep: the link of a conference that is over still
	// finds it (groupCallDiscarded) this long after its end.
	ConferenceSlugKeep = 7 * 24 * time.Hour
	// slugBytes is the randomness of a slug: 22 characters of base64url,
	// within what Web K (^[A-Za-z0-9_-]{1,64}$, constants.ts:54) and
	// tdesktop ([a-zA-Z0-9._-], local_url_handlers.cpp:2001-2002) take.
	slugBytes = 16
)

// ErrConferenceLimit: the creator has ConferencesPerCreator conferences
// that are not over.
var ErrConferenceLimit = errors.New("groupcalls: too many conferences")

// NewSlug is a new random slug.
func NewSlug() (string, error) {
	b := make([]byte, slugBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ConferenceParams is a phone.createConferenceCall.
type ConferenceParams struct {
	Id         int64
	AccessHash int64
	CreatorId  int64
	RandomId   int32
	Slug       string
	// Join, when set, puts the creator into the conference in the same
	// transaction with his block (the zero block of the chain, ADR 0060
	// §2); its CallId is set here.
	Join *JoinParams
}

// CreateConference stores a new conference, and with p.Join the creator's
// join. A repeat of the same request (creator, random_id) returns the
// conference created the first time with created false; with p.Join the
// join is made on it (the block of a repeated zero block is stale:
// CONF_WRITE_CHAIN_INVALID, on which the client reads the chain and joins
// again). ErrConferenceLimit refuses a new one over the limit; the errors
// of Join refuse the join, and then nothing is written.
func (s *Store) CreateConference(ctx context.Context, p ConferenceParams) (call *Call, created bool, joined *JoinResult, err error) {
	err = s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		call, created, joined = nil, false, nil
		var ids []int64
		// The rows of the creator's conferences are locked: two creates of
		// his race for the last place one after the other.
		if err := tx.QueryRowsPartial(&ids, "select call_id from group_call_conferences where creator_id = ? and random_id = ? for update", p.CreatorId, p.RandomId); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
			return err
		}
		id := p.Id
		if len(ids) == 0 {
			var live []int64
			if err := tx.QueryRowsPartial(&live, "select f.call_id from group_call_conferences f join group_calls c on c.id = f.call_id where f.creator_id = ? and c.live = 1 for update", p.CreatorId); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
				return err
			}
			if len(live) >= ConferencesPerCreator {
				return ErrConferenceLimit
			}
			now := int32(s.now().Unix())
			if _, err := tx.Exec("insert into group_calls (id, access_hash, peer_type, peer_id, creator_id, random_id, version, `date`, start_date, live, conference) values (?, ?, 0, ?, ?, ?, 1, ?, ?, 1, 1)",
				p.Id, p.AccessHash, p.Id, p.CreatorId, p.RandomId, now, now); err != nil {
				return err
			}
			if _, err := tx.Exec("insert into group_call_conferences (call_id, creator_id, random_id, slug, empty_since) values (?, ?, ?, ?, ?)",
				p.Id, p.CreatorId, p.RandomId, p.Slug, now); err != nil {
				return err
			}
			created = true
		} else {
			id = ids[0]
		}
		if p.Join != nil {
			j := *p.Join
			j.CallId = id
			res, err := s.joinTx(ctx, tx, j)
			if err != nil {
				return err
			}
			joined = res
			if _, err = tx.Exec("update group_call_conferences set empty_since = 0 where call_id = ?", id); err != nil {
				return err
			}
			call = res.Call
			return nil
		}
		var err error
		call, err = oneCall(ctx, txq{tx}, "where c.id = ?", id)
		return err
	})
	if err != nil {
		return nil, false, nil, err
	}
	return call, created, joined, nil
}

// BySlug returns the conference of a slug, over or not; ErrNotFound if
// no conference has it (a slug that was reset).
func (s *Store) BySlug(ctx context.Context, slug string) (*Call, error) {
	if slug == "" {
		return nil, ErrNotFound
	}
	return oneCall(ctx, dbq{s.db}, "where c.id = (select f.call_id from group_call_conferences f where f.slug = ?)", slug)
}

// ResetSlug gives a conference that is not over a new slug: the old one
// finds nothing from now on.
func (s *Store) ResetSlug(ctx context.Context, id int64, slug string) (*Call, error) {
	r, err := s.db.Exec(ctx, "update group_call_conferences f join group_calls c on c.id = f.call_id set f.slug = ? where f.call_id = ? and c.live = 1", slug, id)
	if err != nil {
		return nil, err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, ErrDiscarded
	}
	return s.Get(ctx, id)
}

// ClearKicked lets a participant the creator took out of a conference
// join again (the creator's own invitation, ADR 0075 §7).
func (s *Store) ClearKicked(ctx context.Context, callId, userId int64) error {
	_, err := s.db.Exec(ctx, "update group_call_participants set kicked = 0 where call_id = ? and user_id = ? and kicked = 1", callId, userId)
	return err
}

// MarkJoined records that someone is in a conference: it is not empty.
func (s *Store) MarkJoined(ctx context.Context, callId int64) error {
	_, err := s.db.Exec(ctx, "update group_call_conferences set empty_since = 0 where call_id = ? and empty_since <> 0", callId)
	return err
}

// LiveConference is a conference that is not over, as the collector sees
// it.
type LiveConference struct {
	Call *Call
	// EmptySince is since when no one is in it (unix seconds), 0 while
	// someone is.
	EmptySince int32
	// Used: someone joined it once.
	Used bool
}

// LiveConferences returns at most limit conferences that are not over,
// after the id after.
func (s *Store) LiveConferences(ctx context.Context, after int64, limit int) ([]*LiveConference, error) {
	calls, err := manyCalls(ctx, dbq{s.db}, "where c.conference = 1 and c.live = 1 and c.id > ? order by c.id limit ?", after, limit)
	if err != nil || len(calls) == 0 {
		return nil, err
	}
	args := make([]any, 0, len(calls))
	for _, c := range calls {
		args = append(args, c.Id)
	}
	var rows []struct {
		CallId     int64 `db:"call_id"`
		EmptySince int32 `db:"empty_since"`
		Used       bool  `db:"used"`
	}
	err = s.db.QueryRowsPartial(ctx, &rows, "select f.call_id, f.empty_since, exists(select 1 from group_call_participants p where p.call_id = f.call_id) as used from group_call_conferences f where f.call_id in ("+placeholders(len(args))+")", args...)
	if err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	by := make(map[int64]int, len(rows))
	for i, r := range rows {
		by[r.CallId] = i
	}
	out := make([]*LiveConference, 0, len(calls))
	for _, c := range calls {
		lc := &LiveConference{Call: c}
		if i, ok := by[c.Id]; ok {
			lc.EmptySince, lc.Used = rows[i].EmptySince, rows[i].Used
		}
		out = append(out, lc)
	}
	return out, nil
}

// MarkEmpty records that no one is in a conference since since, if it
// was not known to be empty.
func (s *Store) MarkEmpty(ctx context.Context, callId int64, since time.Time) error {
	_, err := s.db.Exec(ctx, "update group_call_conferences set empty_since = ? where call_id = ? and empty_since = 0", int32(since.Unix()), callId)
	return err
}

// EmptyEnds tells whether a conference that is empty since emptySince
// (0: someone is in it) ends at now: ConferenceEmptyEnd after the last
// one left, ConferenceUnusedEnd after it was created if no one ever came.
func EmptyEnds(c *LiveConference, now time.Time) bool {
	if c.Call.ParticipantsCount > 0 || c.EmptySince == 0 {
		return false
	}
	if !c.Used {
		return now.Sub(time.Unix(int64(c.Call.Date), 0)) >= ConferenceUnusedEnd
	}
	return now.Sub(time.Unix(int64(c.EmptySince), 0)) >= ConferenceEmptyEnd
}

// DiscardEmpty ends a conference that no one is in, as Discard does;
// won is false and nothing changes when it is over already or a join came
// in the meantime.
func (s *Store) DiscardEmpty(ctx context.Context, id int64) (*Call, bool, error) {
	call, _, won, err := s.discardIf(ctx, id, true)
	return call, won, err
}

// ConferencesLive is the number of conferences that are not over.
func (s *Store) ConferencesLive(ctx context.Context) (int64, error) {
	var n []int64
	if err := s.db.QueryRowsPartial(ctx, &n, "select count(*) from group_calls where conference = 1 and live = 1"); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return 0, err
	}
	if len(n) == 0 {
		return 0, nil
	}
	return n[0], nil
}
