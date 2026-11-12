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

package stories

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
)

// Store is the tables of the stories. Reads go to the primary: a story is
// read back right after it is written.
type Store struct {
	db *sqlx.DB
}

// NewStore returns the Store on db.
func NewStore(db *sqlx.DB) *Store {
	return &Store{db: db.Master()}
}

type storyRow struct {
	PeerType      int32  `db:"peer_type"`
	PeerId        int64  `db:"peer_id"`
	StoryId       int32  `db:"story_id"`
	RandomId      int64  `db:"random_id"`
	SenderUserId  int64  `db:"sender_user_id"`
	Date          int32  `db:"date"`
	ExpireDate    int32  `db:"expire_date"`
	Pinned        bool   `db:"pinned"`
	PinnedToTop   int32  `db:"pinned_to_top"`
	Noforwards    bool   `db:"noforwards"`
	Edited        bool   `db:"edited"`
	Privacy       string `db:"privacy"`
	Caption       string `db:"caption"`
	Entities      string `db:"entities"`
	Media         string `db:"media"`
	MediaAreas    string `db:"media_areas"`
	FwdFrom       string `db:"fwd_from"`
	Music         string `db:"music"`
	Mentions      string `db:"mentions"`
	FwdPeerType   int32  `db:"fwd_peer_type"`
	FwdPeerId     int64  `db:"fwd_peer_id"`
	FwdStoryId    int32  `db:"fwd_story_id"`
	ForwardsCount int32  `db:"forwards_count"`
}

const storyColumns = "peer_type, peer_id, story_id, random_id, sender_user_id, `date`, expire_date, pinned, pinned_to_top, noforwards, edited," +
	" privacy, caption, entities, media, media_areas, fwd_from, music, mentions, fwd_peer_type, fwd_peer_id, fwd_story_id, forwards_count"

// decode reads a JSON column; an empty one is nothing.
func decode(column, data string, v any) error {
	if data == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(data), v); err != nil {
		return fmt.Errorf("stories: column %s: %w", column, err)
	}
	return nil
}

// encode writes a JSON column; nothing is the empty string.
func encode(empty bool, v any) (string, error) {
	if empty {
		return "", nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (r *storyRow) story() (*Story, error) {
	s := &Story{
		Peer: Peer{Type: r.PeerType, Id: r.PeerId}, Id: r.StoryId, RandomId: r.RandomId, SenderId: r.SenderUserId,
		Date: r.Date, ExpireDate: r.ExpireDate, Pinned: r.Pinned, PinnedToTop: r.PinnedToTop, Noforwards: r.Noforwards,
		Edited: r.Edited, Caption: r.Caption, FwdPeer: Peer{Type: r.FwdPeerType, Id: r.FwdPeerId}, FwdStoryId: r.FwdStoryId,
		ForwardsCount: r.ForwardsCount,
	}
	if err := decode("privacy", r.Privacy, &s.Privacy); err != nil {
		return nil, err
	}
	if err := decode("entities", r.Entities, &s.Entities); err != nil {
		return nil, err
	}
	if err := decode("media", r.Media, &s.Media); err != nil {
		return nil, err
	}
	if err := decode("media_areas", r.MediaAreas, &s.MediaAreas); err != nil {
		return nil, err
	}
	if err := decode("fwd_from", r.FwdFrom, &s.FwdFrom); err != nil {
		return nil, err
	}
	if err := decode("music", r.Music, &s.Music); err != nil {
		return nil, err
	}
	if err := decode("mentions", r.Mentions, &s.Mentions); err != nil {
		return nil, err
	}
	return s, nil
}

// content are the JSON columns of a story.
type content struct {
	privacy, entities, media, areas, fwd, music, mentions string
}

func contentOf(s *Story) (c content, err error) {
	if c.privacy, err = encode(len(s.Privacy) == 0, s.Privacy); err != nil {
		return
	}
	if c.entities, err = encode(len(s.Entities) == 0, s.Entities); err != nil {
		return
	}
	if c.media, err = encode(s.Media == nil, s.Media); err != nil {
		return
	}
	if c.areas, err = encode(len(s.MediaAreas) == 0, s.MediaAreas); err != nil {
		return
	}
	if c.fwd, err = encode(s.FwdFrom == nil, s.FwdFrom); err != nil {
		return
	}
	if c.music, err = encode(s.Music == nil, s.Music); err != nil {
		return
	}
	c.mentions, err = encode(len(s.Mentions) == 0, s.Mentions)
	return
}

func stories(rows []storyRow) ([]*Story, error) {
	out := make([]*Story, 0, len(rows))
	for i := range rows {
		s, err := rows[i].story()
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// in is "?, ?, ..." for n values.
func in(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

func int32Args(args []any, ids []int32) []any {
	for _, id := range ids {
		args = append(args, id)
	}
	return args
}

func int64Args(args []any, ids []int64) []any {
	for _, id := range ids {
		args = append(args, id)
	}
	return args
}

func (s *Store) list(ctx context.Context, query string, args ...any) ([]*Story, error) {
	var rows []storyRow
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT "+storyColumns+" FROM stories "+query, args...); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	return stories(rows)
}

func count(ctx context.Context, db *sqlx.DB, query string, args ...any) (int32, error) {
	var n int32
	if err := db.QueryRowPartial(ctx, &n, query, args...); err != nil {
		return 0, err
	}
	return n, nil
}

// Create stores the new story st of st.Peer posted by st.SenderId at
// st.Date, under the next id of the peer. A repeat of the request (the same
// random_id of the same sender) returns the story stored the first time,
// with created false. ErrTooMany and *FloodError are the limits of the
// peer, counted in the same transaction as the new id, so two requests at
// once do not pass them together.
func (s *Store) Create(ctx context.Context, st *Story) (stored *Story, created bool, err error) {
	c, err := contentOf(st)
	if err != nil {
		return nil, false, err
	}
	find := func(q func(v any, query string, args ...any) error) (*Story, error) {
		var rows []storyRow
		if err := q(&rows, "SELECT "+storyColumns+" FROM stories WHERE peer_type = ? AND peer_id = ? AND sender_user_id = ? AND random_id = ?",
			st.Peer.Type, st.Peer.Id, st.SenderId, st.RandomId); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
			return nil, err
		}
		if len(rows) == 0 {
			return nil, nil
		}
		return rows[0].story()
	}
	err = s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		last, err := lockPeerTx(tx, st.Peer)
		if err != nil {
			return err
		}
		if stored, err = find(tx.QueryRowsPartial); err != nil || stored != nil {
			return err
		}
		if err := limitsTx(tx, st.Peer, st.Date); err != nil {
			return err
		}
		id := last + 1
		if _, err := tx.Exec("UPDATE story_counters SET last_id = ?, dirty = 1, dirty_at = ? WHERE peer_type = ? AND peer_id = ?", id, st.Date, st.Peer.Type, st.Peer.Id); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO stories ("+storyColumns+", active, mentions_pending) VALUES ("+in(23)+", 1, ?)",
			st.Peer.Type, st.Peer.Id, id, st.RandomId, st.SenderId, st.Date, st.ExpireDate, st.Pinned, 0, st.Noforwards, false,
			c.privacy, st.Caption, c.entities, c.media, c.areas, c.fwd, c.music, c.mentions, st.FwdPeer.Type, st.FwdPeer.Id, st.FwdStoryId, 0,
			len(st.Mentions) > 0); err != nil {
			return err
		}
		if st.FwdStoryId != 0 {
			if _, err := tx.Exec("UPDATE stories SET forwards_count = forwards_count + 1 WHERE peer_type = ? AND peer_id = ? AND story_id = ?",
				st.FwdPeer.Type, st.FwdPeer.Id, st.FwdStoryId); err != nil {
				return err
			}
		}
		copied := *st
		copied.Id, copied.PinnedToTop, copied.Edited, copied.ForwardsCount = id, 0, false, 0
		stored, created = &copied, true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	return stored, created, nil
}

// lockPeerTx takes the lock of the peer's stories for the transaction, the
// row of its counter, and returns the last id given out. Every transaction
// that changes the stories of a peer takes it first, so they run one after
// another and cannot wait for each other's rows.
func lockPeerTx(tx *sqlx.Tx, peer Peer) (int32, error) {
	if _, err := tx.Exec("INSERT INTO story_counters (peer_type, peer_id, last_id) VALUES (?, ?, 0) ON DUPLICATE KEY UPDATE last_id = last_id",
		peer.Type, peer.Id); err != nil {
		return 0, err
	}
	var last int32
	if err := tx.QueryRowPartial(&last, "SELECT last_id FROM story_counters WHERE peer_type = ? AND peer_id = ? FOR UPDATE", peer.Type, peer.Id); err != nil {
		return 0, err
	}
	return last, nil
}

// limitsTx checks the limits of the peer for a story posted at now.
func limitsTx(tx *sqlx.Tx, peer Peer, now int32) error {
	var active int32
	if err := tx.QueryRowPartial(&active, "SELECT COUNT(*) FROM stories WHERE peer_type = ? AND peer_id = ? AND expire_date > ?", peer.Type, peer.Id, now); err != nil {
		return err
	}
	if active >= MaxActive {
		return ErrTooMany
	}
	for _, l := range []struct {
		window  int32
		max     int32
		monthly bool
	}{{7 * 86400, MaxWeekly, false}, {30 * 86400, MaxMonthly, true}} {
		var dates []int32
		if err := tx.QueryRowsPartial(&dates, "SELECT `date` FROM stories WHERE peer_type = ? AND peer_id = ? AND `date` > ? ORDER BY `date` DESC LIMIT ?",
			peer.Type, peer.Id, now-l.window, l.max); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
			return err
		}
		if int32(len(dates)) >= l.max {
			// The oldest of them leaves the window first.
			return &FloodError{Monthly: l.monthly, Until: dates[len(dates)-1] + l.window}
		}
	}
	return nil
}

// Remains is how many more stories the peer may have in the feeds at now,
// or the limit that stops a new one (ErrTooMany, *FloodError).
func (s *Store) Remains(ctx context.Context, peer Peer, now int32) (int32, error) {
	var remains int32
	err := s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		if err := limitsTx(tx, peer, now); err != nil {
			return err
		}
		var active int32
		if err := tx.QueryRowPartial(&active, "SELECT COUNT(*) FROM stories WHERE peer_type = ? AND peer_id = ? AND expire_date > ?", peer.Type, peer.Id, now); err != nil {
			return err
		}
		remains = MaxActive - active
		return nil
	})
	return remains, err
}

// Get returns the stories ids of peer that exist, oldest first.
func (s *Store) Get(ctx context.Context, peer Peer, ids []int32) ([]*Story, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return s.list(ctx, "WHERE peer_type = ? AND peer_id = ? AND story_id IN ("+in(len(ids))+") ORDER BY story_id",
		int32Args([]any{peer.Type, peer.Id}, ids)...)
}

// One returns the story id of peer, nil if there is none.
func (s *Store) One(ctx context.Context, peer Peer, id int32) (*Story, error) {
	list, err := s.Get(ctx, peer, []int32{id})
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return list[0], nil
}

// Active returns the stories of peer that are in the feeds at now, oldest
// first.
func (s *Store) Active(ctx context.Context, peer Peer, now int32) ([]*Story, error) {
	return s.list(ctx, "WHERE peer_type = ? AND peer_id = ? AND expire_date > ? ORDER BY story_id", peer.Type, peer.Id, now)
}

// ActiveOf returns the stories in the feeds at now of the peers ids of one
// type, oldest first for each.
func (s *Store) ActiveOf(ctx context.Context, peerType int32, ids []int64, now int32) (map[int64][]*Story, error) {
	out := make(map[int64][]*Story)
	if len(ids) == 0 {
		return out, nil
	}
	args := int64Args([]any{peerType}, ids)
	list, err := s.list(ctx, "WHERE peer_type = ? AND peer_id IN ("+in(len(ids))+") AND expire_date > ? ORDER BY peer_id, story_id", append(args, now)...)
	if err != nil {
		return nil, err
	}
	for _, st := range list {
		out[st.Peer.Id] = append(out[st.Peer.Id], st)
	}
	return out, nil
}

// page reads a page of the stories of peer that match where, newest first,
// below offsetId (0: from the newest), with the count of all that match.
func (s *Store) page(ctx context.Context, peer Peer, where string, offsetId, limit int32) ([]*Story, int32, error) {
	total, err := count(ctx, s.db, "SELECT COUNT(*) FROM stories WHERE peer_type = ? AND peer_id = ?"+where, peer.Type, peer.Id)
	if err != nil {
		return nil, 0, err
	}
	q := "WHERE peer_type = ? AND peer_id = ?" + where
	args := []any{peer.Type, peer.Id}
	if offsetId > 0 {
		q += " AND story_id < ?"
		args = append(args, offsetId)
	}
	list, err := s.list(ctx, q+" ORDER BY story_id DESC LIMIT ?", append(args, limit)...)
	return list, total, err
}

// Pinned returns a page of the stories of the profile of peer, newest
// first, and how many there are.
func (s *Store) Pinned(ctx context.Context, peer Peer, offsetId, limit int32) ([]*Story, int32, error) {
	return s.page(ctx, peer, " AND pinned = 1", offsetId, limit)
}

// PinnedToTop returns the ids of the stories at the top of the profile of
// peer, in their order.
func (s *Store) PinnedToTop(ctx context.Context, peer Peer) ([]int32, error) {
	var ids []int32
	if err := s.db.QueryRowsPartial(ctx, &ids, "SELECT story_id FROM stories WHERE peer_type = ? AND peer_id = ? AND pinned = 1 AND pinned_to_top > 0 ORDER BY pinned_to_top",
		peer.Type, peer.Id); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	return ids, nil
}

// HasPinned tells whether the profile of peer has stories.
func (s *Store) HasPinned(ctx context.Context, peer Peer) (bool, error) {
	n, err := count(ctx, s.db, "SELECT COUNT(*) FROM (SELECT 1 FROM stories WHERE peer_type = ? AND peer_id = ? AND pinned = 1 LIMIT 1) t", peer.Type, peer.Id)
	return n > 0, err
}

// Archive returns a page of every story of peer, newest first, and how
// many there are.
func (s *Store) Archive(ctx context.Context, peer Peer, offsetId, limit int32) ([]*Story, int32, error) {
	return s.page(ctx, peer, "", offsetId, limit)
}

// Update applies edit to the story id of peer as it is stored under the
// lock of the peer, and stores what an edit may change: its media, areas,
// caption with entities, privacy and music. The story becomes edited, and
// its peer is marked changed (Dirty). Two edits at once each change only
// what they set: the second applies its own to the story the first left.
// It returns the story before and after the edit, both nil when it is
// gone.
func (s *Store) Update(ctx context.Context, peer Peer, id int32, at int32, edit func(st *Story)) (before, after *Story, err error) {
	err = s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		before, after = nil, nil
		if _, err := lockPeerTx(tx, peer); err != nil {
			return err
		}
		var rows []storyRow
		if err := tx.QueryRowsPartial(&rows, "SELECT "+storyColumns+" FROM stories WHERE peer_type = ? AND peer_id = ? AND story_id = ?", peer.Type, peer.Id, id); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		was, err := rows[0].story()
		if err != nil {
			return err
		}
		st, err := rows[0].story()
		if err != nil {
			return err
		}
		edit(st)
		c, err := contentOf(st)
		if err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE stories SET privacy = ?, caption = ?, entities = ?, media = ?, media_areas = ?, music = ?, edited = 1"+
			" WHERE peer_type = ? AND peer_id = ? AND story_id = ?",
			c.privacy, st.Caption, c.entities, c.media, c.areas, c.music, peer.Type, peer.Id, id); err != nil {
			return err
		}
		st.Edited = true
		before, after = was, st
		return dirtyTx(tx, peer, at)
	})
	if err != nil {
		return nil, nil, err
	}
	return before, after, nil
}

// dirtyTx marks, in the transaction of a change that holds the lock of the
// peer (lockPeerTx), that the stories of peer in the feeds changed at the
// time at.
func dirtyTx(tx *sqlx.Tx, peer Peer, at int32) error {
	_, err := tx.Exec("UPDATE story_counters SET dirty = 1, dirty_at = ? WHERE peer_type = ? AND peer_id = ?", at, peer.Type, peer.Id)
	return err
}

// MarkDirtyTx is dirtyTx for a change made outside this package in tx: the
// stories of peer read anew by the services that show it (the blocklists
// of their owner keep users out of them, Audiences). A peer that never had
// a story has no mark to set and needs none.
func MarkDirtyTx(tx *sqlx.Tx, peer Peer, at int32) error {
	return dirtyTx(tx, peer, at)
}

// Dirty returns the peers whose stories in the feeds changed before the
// time before and whose services have not been told, limit at most, each
// with the time of its change.
func (s *Store) Dirty(ctx context.Context, before int32, limit int) (map[Peer]int32, error) {
	var rows []struct {
		PeerType int32 `db:"peer_type"`
		PeerId   int64 `db:"peer_id"`
		DirtyAt  int32 `db:"dirty_at"`
	}
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT peer_type, peer_id, dirty_at FROM story_counters WHERE dirty = 1 AND dirty_at < ? LIMIT ?", before, limit); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	out := make(map[Peer]int32, len(rows))
	for _, r := range rows {
		out[Peer{Type: r.PeerType, Id: r.PeerId}] = r.DirtyAt
	}
	return out, nil
}

// DirtyAt returns when the stories of peer in the feeds last changed
// without its services being told, 0 if they have been.
func (s *Store) DirtyAt(ctx context.Context, peer Peer) (int32, error) {
	var at []int32
	if err := s.db.QueryRowsPartial(ctx, &at, "SELECT dirty_at FROM story_counters WHERE peer_type = ? AND peer_id = ? AND dirty = 1", peer.Type, peer.Id); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return 0, err
	}
	if len(at) == 0 {
		return 0, nil
	}
	return at[0], nil
}

// ClearDirty records that the services of peer have been told of the
// change of the time at; a later change stays marked.
func (s *Store) ClearDirty(ctx context.Context, peer Peer, at int32) error {
	_, err := s.db.Exec(ctx, "UPDATE story_counters SET dirty = 0 WHERE peer_type = ? AND peer_id = ? AND dirty = 1 AND dirty_at <= ?", peer.Type, peer.Id, at)
	return err
}

// Delete removes the stories ids of peer with their views, marks the peer
// changed at the time at (Dirty) and returns the ones that were there.
func (s *Store) Delete(ctx context.Context, peer Peer, ids []int32, at int32) (deleted []*Story, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		deleted = nil
		if _, err := lockPeerTx(tx, peer); err != nil {
			return err
		}
		var rows []storyRow
		args := int32Args([]any{peer.Type, peer.Id}, ids)
		if err := tx.QueryRowsPartial(&rows, "SELECT "+storyColumns+" FROM stories WHERE peer_type = ? AND peer_id = ? AND story_id IN ("+in(len(ids))+") ORDER BY story_id",
			args...); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
			return err
		}
		if len(rows) == 0 {
			return nil
		}
		if deleted, err = stories(rows); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM stories WHERE peer_type = ? AND peer_id = ? AND story_id IN ("+in(len(ids))+")", args...); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM story_views WHERE peer_type = ? AND peer_id = ? AND story_id IN ("+in(len(ids))+")", args...); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM story_shares WHERE peer_type = ? AND peer_id = ? AND story_id IN ("+in(len(ids))+")", args...); err != nil {
			return err
		}
		return dirtyTx(tx, peer, at)
	})
	return deleted, err
}

// SetPinned puts the stories ids of peer into its profile or takes them
// out, and returns the ones that exist. A story taken out also leaves the
// top of the profile.
func (s *Store) SetPinned(ctx context.Context, peer Peer, ids []int32, pinned bool) (found []int32, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		found = nil
		if _, err := lockPeerTx(tx, peer); err != nil {
			return err
		}
		args := int32Args([]any{peer.Type, peer.Id}, ids)
		if err := tx.QueryRowsPartial(&found, "SELECT story_id FROM stories WHERE peer_type = ? AND peer_id = ? AND story_id IN ("+in(len(ids))+") ORDER BY story_id",
			args...); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
			return err
		}
		if len(found) == 0 {
			return nil
		}
		q := "UPDATE stories SET pinned = 1"
		if !pinned {
			q = "UPDATE stories SET pinned = 0, pinned_to_top = 0"
		}
		_, err := tx.Exec(q+" WHERE peer_type = ? AND peer_id = ? AND story_id IN ("+in(len(ids))+")", args...)
		return err
	})
	return found, err
}

// SetPinnedToTop makes ids, in their order, the stories at the top of the
// profile of peer; none clears the top. ErrNotPinned if one of them is not
// in the profile.
func (s *Store) SetPinnedToTop(ctx context.Context, peer Peer, ids []int32) error {
	return s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		if _, err := lockPeerTx(tx, peer); err != nil {
			return err
		}
		if len(ids) > 0 {
			var pinned []int32
			if err := tx.QueryRowsPartial(&pinned, "SELECT story_id FROM stories WHERE peer_type = ? AND peer_id = ? AND pinned = 1 AND story_id IN ("+in(len(ids))+")",
				int32Args([]any{peer.Type, peer.Id}, ids)...); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
				return err
			}
			if len(pinned) != len(ids) {
				return ErrNotPinned
			}
		}
		if _, err := tx.Exec("UPDATE stories SET pinned_to_top = 0 WHERE peer_type = ? AND peer_id = ? AND pinned_to_top > 0", peer.Type, peer.Id); err != nil {
			return err
		}
		for i, id := range ids {
			if _, err := tx.Exec("UPDATE stories SET pinned_to_top = ? WHERE peer_type = ? AND peer_id = ? AND story_id = ?", i+1, peer.Type, peer.Id, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// AddShare counts the message of user with randomId that shared the story
// id of peer at now, once: a repeated request with the same random_id
// counts nothing more.
func (s *Store) AddShare(ctx context.Context, peer Peer, id int32, user, randomId int64, now int32) error {
	return s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		res, err := tx.Exec("INSERT IGNORE INTO story_shares (peer_type, peer_id, story_id, user_id, random_id, `date`) VALUES (?, ?, ?, ?, ?, ?)",
			peer.Type, peer.Id, id, user, randomId, now)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		_, err = tx.Exec("UPDATE stories SET forwards_count = forwards_count + 1 WHERE peer_type = ? AND peer_id = ? AND story_id = ?", peer.Type, peer.Id, id)
		return err
	})
}

// AddViews records that viewer opened the stories ids of peer at now and
// returns the ones it had not opened before.
func (s *Store) AddViews(ctx context.Context, peer Peer, ids []int32, viewer int64, now int32) ([]int32, error) {
	var added []int32
	for _, id := range ids {
		res, err := s.db.Exec(ctx, "INSERT IGNORE INTO story_views (peer_type, peer_id, story_id, viewer_id, `date`) VALUES (?, ?, ?, ?, ?)",
			peer.Type, peer.Id, id, viewer, now)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			added = append(added, id)
		}
	}
	return added, nil
}

// SetReaction stores the reaction of viewer to the story id of peer ("":
// takes it back); the viewer becomes a viewer of the story if it was not.
func (s *Store) SetReaction(ctx context.Context, peer Peer, id int32, viewer int64, reaction string, now int32) error {
	date := now
	if reaction == "" {
		date = 0
	}
	_, err := s.db.Exec(ctx, "INSERT INTO story_views (peer_type, peer_id, story_id, viewer_id, `date`, reaction, reaction_date) VALUES (?, ?, ?, ?, ?, ?, ?)"+
		" ON DUPLICATE KEY UPDATE reaction = VALUES(reaction), reaction_date = VALUES(reaction_date)",
		peer.Type, peer.Id, id, viewer, now, reaction, date)
	return err
}

type viewRow struct {
	StoryId      int32  `db:"story_id"`
	ViewerId     int64  `db:"viewer_id"`
	Date         int32  `db:"date"`
	Reaction     string `db:"reaction"`
	ReactionDate int32  `db:"reaction_date"`
}

// Views returns the viewers of the story id of peer, newest first,
// MaxViewers at most.
func (s *Store) Views(ctx context.Context, peer Peer, id int32) ([]View, error) {
	var rows []viewRow
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT story_id, viewer_id, `date`, reaction, reaction_date FROM story_views"+
		" WHERE peer_type = ? AND peer_id = ? AND story_id = ? ORDER BY `date` DESC, viewer_id DESC LIMIT ?",
		peer.Type, peer.Id, id, MaxViewers); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	out := make([]View, len(rows))
	for i, r := range rows {
		out[i] = View{ViewerId: r.ViewerId, Date: r.Date, Reaction: r.Reaction, ReactionDate: r.ReactionDate}
	}
	return out, nil
}

// StatsOf returns the counters of the stories ids of peer, without the
// views of the users of except (the owner's own).
func (s *Store) StatsOf(ctx context.Context, peer Peer, ids []int32, except int64) (map[int32]*Stats, error) {
	out := make(map[int32]*Stats, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []viewRow
	args := int32Args([]any{peer.Type, peer.Id}, ids)
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT story_id, viewer_id, `date`, reaction, reaction_date FROM story_views"+
		" WHERE peer_type = ? AND peer_id = ? AND story_id IN ("+in(len(ids))+") AND viewer_id <> ? ORDER BY story_id, `date` DESC, viewer_id DESC",
		append(args, except)...); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	for _, r := range rows {
		st := out[r.StoryId]
		if st == nil {
			st = &Stats{}
			out[r.StoryId] = st
		}
		st.Views++
		if len(st.Recent) < 3 {
			st.Recent = append(st.Recent, r.ViewerId)
		}
		if r.Reaction == "" {
			continue
		}
		st.Reactions++
		found := false
		for i := range st.ByKind {
			if st.ByKind[i].Reaction == r.Reaction {
				st.ByKind[i].Count++
				found = true
				break
			}
		}
		if !found {
			st.ByKind = append(st.ByKind, ReactionCount{Reaction: r.Reaction, Count: 1})
		}
	}
	return out, nil
}

// SentReactions returns the reactions viewer left on the stories ids of
// peer, by story.
func (s *Store) SentReactions(ctx context.Context, peer Peer, ids []int32, viewer int64) (map[int32]string, error) {
	out := make(map[int32]string)
	if len(ids) == 0 {
		return out, nil
	}
	var rows []viewRow
	args := int32Args([]any{peer.Type, peer.Id}, ids)
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT story_id, viewer_id, `date`, reaction, reaction_date FROM story_views"+
		" WHERE peer_type = ? AND peer_id = ? AND story_id IN ("+in(len(ids))+") AND viewer_id = ? AND reaction <> ''",
		append(args, viewer)...); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	for _, r := range rows {
		out[r.StoryId] = r.Reaction
	}
	return out, nil
}

// EraseViews removes the views viewer left since the time since on the
// stories of users, except those with a reaction, which the viewer chose to
// show: the "past" of the stealth mode.
func (s *Store) EraseViews(ctx context.Context, viewer int64, since int32) error {
	_, err := s.db.Exec(ctx, "DELETE FROM story_views WHERE viewer_id = ? AND `date` >= ? AND peer_type = ? AND reaction = ''", viewer, since, mtproto.PEER_USER)
	return err
}

// MarkRead moves what user has read of peer up to maxId and reports
// whether that was news.
func (s *Store) MarkRead(ctx context.Context, user int64, peer Peer, maxId int32) (bool, error) {
	res, err := s.db.Exec(ctx, "INSERT INTO story_reads (user_id, peer_type, peer_id, max_read_id) VALUES (?, ?, ?, ?)"+
		" ON DUPLICATE KEY UPDATE max_read_id = GREATEST(max_read_id, VALUES(max_read_id))", user, peer.Type, peer.Id, maxId)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

type readRow struct {
	PeerType  int32 `db:"peer_type"`
	PeerId    int64 `db:"peer_id"`
	MaxReadId int32 `db:"max_read_id"`
}

// Reads returns up to which story user has read each peer it has read
// any of.
func (s *Store) Reads(ctx context.Context, user int64) (map[Peer]int32, error) {
	var rows []readRow
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT peer_type, peer_id, max_read_id FROM story_reads WHERE user_id = ?", user); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	out := make(map[Peer]int32, len(rows))
	for _, r := range rows {
		out[Peer{Type: r.PeerType, Id: r.PeerId}] = r.MaxReadId
	}
	return out, nil
}

// MaxRead returns up to which story of peer user has read, 0 for none.
func (s *Store) MaxRead(ctx context.Context, user int64, peer Peer) (int32, error) {
	var ids []int32
	if err := s.db.QueryRowsPartial(ctx, &ids, "SELECT max_read_id FROM story_reads WHERE user_id = ? AND peer_type = ? AND peer_id = ?", user, peer.Type, peer.Id); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	return ids[0], nil
}

// SetHidden moves the stories of the channel peer to the archive row of
// user, or back.
func (s *Store) SetHidden(ctx context.Context, user int64, peer Peer, hidden bool) error {
	var err error
	if hidden {
		_, err = s.db.Exec(ctx, "INSERT IGNORE INTO story_hidden_peers (user_id, peer_type, peer_id) VALUES (?, ?, ?)", user, peer.Type, peer.Id)
	} else {
		_, err = s.db.Exec(ctx, "DELETE FROM story_hidden_peers WHERE user_id = ? AND peer_type = ? AND peer_id = ?", user, peer.Type, peer.Id)
	}
	return err
}

// Stealth returns the stealth mode of user.
func (s *Store) Stealth(ctx context.Context, user int64) (Stealth, error) {
	return stealthOf(func(v any, query string, args ...any) error { return s.db.QueryRowsPartial(ctx, v, query, args...) }, user, "")
}

type stealthRow struct {
	ActiveUntil   int32 `db:"active_until"`
	CooldownUntil int32 `db:"cooldown_until"`
}

func stealthOf(q func(v any, query string, args ...any) error, user int64, lock string) (Stealth, error) {
	var rows []stealthRow
	if err := q(&rows, "SELECT active_until, cooldown_until FROM story_stealth WHERE user_id = ?"+lock, user); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return Stealth{}, err
	}
	if len(rows) == 0 {
		return Stealth{}, nil
	}
	return Stealth{ActiveUntil: rows[0].ActiveUntil, CooldownUntil: rows[0].CooldownUntil}, nil
}

// ActivateStealth turns the stealth mode of user on at now: with future its
// views are hidden for StealthFuture, with past those of the last
// StealthPast are erased, and the mode cannot be turned on again for
// StealthCooldown. ErrCooldown while an earlier use still cools down.
func (s *Store) ActivateStealth(ctx context.Context, user int64, past, future bool, now int32) (st Stealth, err error) {
	err = s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		if _, err := tx.Exec("INSERT IGNORE INTO story_stealth (user_id) VALUES (?)", user); err != nil {
			return err
		}
		cur, err := stealthOf(tx.QueryRowsPartial, user, " FOR UPDATE")
		if err != nil {
			return err
		}
		if cur.CooldownUntil > now {
			return ErrCooldown
		}
		st = Stealth{CooldownUntil: now + StealthCooldown}
		if future {
			st.ActiveUntil = now + StealthFuture
		}
		if _, err := tx.Exec("UPDATE story_stealth SET active_until = ?, cooldown_until = ? WHERE user_id = ?", st.ActiveUntil, st.CooldownUntil, user); err != nil {
			return err
		}
		if past {
			_, err = tx.Exec("DELETE FROM story_views WHERE viewer_id = ? AND `date` >= ? AND peer_type = ? AND reaction = ''", user, now-StealthPast, mtproto.PEER_USER)
		}
		return err
	})
	return st, err
}

type peerRow struct {
	PeerType int32 `db:"peer_type"`
	PeerId   int64 `db:"peer_id"`
}

// Expired returns the peers that have stories which left the feeds by now
// and have not been announced as over yet, limit at most.
func (s *Store) Expired(ctx context.Context, now int32, limit int) ([]Peer, error) {
	var rows []peerRow
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT DISTINCT peer_type, peer_id FROM stories WHERE active = 1 AND expire_date <= ? LIMIT ?", now, limit); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	out := make([]Peer, len(rows))
	for i, r := range rows {
		out[i] = Peer{Type: r.PeerType, Id: r.PeerId}
	}
	return out, nil
}

// MarkExpired records that the stories of peer which left the feeds by now
// have been announced as over.
func (s *Store) MarkExpired(ctx context.Context, peer Peer, now int32) error {
	_, err := s.db.Exec(ctx, "UPDATE stories SET active = 0 WHERE peer_type = ? AND peer_id = ? AND active = 1 AND expire_date <= ?", peer.Type, peer.Id, now)
	return err
}

// MaxActiveId returns the newest story of peer in the feeds at now, 0 for
// none.
func (s *Store) MaxActiveId(ctx context.Context, peer Peer, now int32) (int32, error) {
	ids, err := ActiveMaxIds(ctx, s.db, peer.Type, []int64{peer.Id}, time.Unix(int64(now), 0))
	return ids[peer.Id], err
}

type maxRow struct {
	PeerId int64 `db:"peer_id"`
	MaxId  int32 `db:"max_id"`
}

// ActiveMaxIds returns, for the peers ids of one type that have stories in
// the feeds at now, the id of the newest. The services of users and of
// channels read it for stories_max_id of their objects.
func ActiveMaxIds(ctx context.Context, db *sqlx.DB, peerType int32, ids []int64, now time.Time) (map[int64]int32, error) {
	out := make(map[int64]int32)
	if len(ids) == 0 {
		return out, nil
	}
	var rows []maxRow
	args := int64Args([]any{peerType}, ids)
	if err := db.QueryRowsPartial(ctx, &rows, "SELECT peer_id, MAX(story_id) AS max_id FROM stories WHERE peer_type = ? AND peer_id IN ("+in(len(ids))+") AND expire_date > ? GROUP BY peer_id",
		append(args, now.Unix())...); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	for _, r := range rows {
		out[r.PeerId] = r.MaxId
	}
	return out, nil
}

type audienceRow struct {
	StoryId    int32  `db:"story_id"`
	ExpireDate int32  `db:"expire_date"`
	Privacy    string `db:"privacy"`
}

// Audiences returns the stories of the user userId that are in the feeds at
// now as the entries its ImmutableUser carries (mtproto.MakeStoryAudience):
// who sees each and when it expires. The users on either blocklist of
// userId are kept out of each by name, whatever its rules say, so that
// user.stories_max_id draws no ring for them (ADR 0054). The user service
// drops the entries when either list changes.
func Audiences(ctx context.Context, db *sqlx.DB, userId int64, now time.Time) ([]*mtproto.PrivacyKeyRules, error) {
	var rows []audienceRow
	if err := db.QueryRowsPartial(ctx, &rows, "SELECT story_id, expire_date, privacy FROM stories WHERE peer_type = ? AND peer_id = ? AND expire_date > ? ORDER BY story_id",
		mtproto.PEER_USER, userId, now.Unix()); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	out := make([]*mtproto.PrivacyKeyRules, 0, len(rows))
	blocked, err := shutOut(ctx, db, userId)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		var rules []*mtproto.PrivacyRule
		if err := decode("privacy", r.Privacy, &rules); err != nil {
			return nil, err
		}
		if len(blocked) > 0 {
			rules = append(rules, mtproto.MakeTLPrivacyValueDisallowUsers(&mtproto.PrivacyRule{Users: blocked}).To_PrivacyRule())
		}
		out = append(out, mtproto.MakeStoryAudience(r.StoryId, r.ExpireDate, rules))
	}
	return out, nil
}

// HiddenChannels returns which of the channels ids the user userId moved to
// the archive row of its stories.
func HiddenChannels(ctx context.Context, db *sqlx.DB, userId int64, ids []int64) (map[int64]bool, error) {
	out := make(map[int64]bool)
	if len(ids) == 0 {
		return out, nil
	}
	var hidden []int64
	args := int64Args([]any{userId, mtproto.PEER_CHANNEL}, ids)
	if err := db.QueryRowsPartial(ctx, &hidden, "SELECT peer_id FROM story_hidden_peers WHERE user_id = ? AND peer_type = ? AND peer_id IN ("+in(len(ids))+")",
		args...); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	for _, id := range hidden {
		out[id] = true
	}
	return out, nil
}

// HidingUsers returns which of the users ids moved the channel channelId to
// the archive row of their stories.
func HidingUsers(ctx context.Context, db *sqlx.DB, channelId int64, ids []int64) (map[int64]bool, error) {
	out := make(map[int64]bool)
	if len(ids) == 0 {
		return out, nil
	}
	var hiding []int64
	args := int64Args([]any{mtproto.PEER_CHANNEL, channelId}, ids)
	if err := db.QueryRowsPartial(ctx, &hiding, "SELECT user_id FROM story_hidden_peers WHERE peer_type = ? AND peer_id = ? AND user_id IN ("+in(len(ids))+")",
		args...); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	for _, id := range hiding {
		out[id] = true
	}
	return out, nil
}

// ActivePeers returns the peers of one type that have stories in the feeds
// at now.
func (s *Store) ActivePeers(ctx context.Context, peerType int32, now int32) ([]int64, error) {
	var ids []int64
	if err := s.db.QueryRowsPartial(ctx, &ids, "SELECT DISTINCT peer_id FROM stories WHERE peer_type = ? AND expire_date > ? ORDER BY peer_id", peerType, now); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	return ids, nil
}

// PendingMentions returns the stories of users posted before the time
// before whose mentions have not been told to the mentioned yet, limit at
// most.
func (s *Store) PendingMentions(ctx context.Context, before int32, limit int) ([]*Story, error) {
	return s.list(ctx, "WHERE mentions_pending = 1 AND `date` < ? ORDER BY `date` LIMIT ?", before, limit)
}

// SetMentionsPending records whether the mentions of the story id of peer
// still have to be told.
func (s *Store) SetMentionsPending(ctx context.Context, peer Peer, id int32, pending bool) error {
	_, err := s.db.Exec(ctx, "UPDATE stories SET mentions_pending = ? WHERE peer_type = ? AND peer_id = ? AND story_id = ?", pending, peer.Type, peer.Id, id)
	return err
}

// ChatsOf returns the chats the rules of a story name, none for rules that
// need no membership to be read.
func ChatsOf(rules []*mtproto.PrivacyRule) []int64 {
	var chats []int64
	for _, r := range rules {
		switch r.GetPredicateName() {
		case mtproto.Predicate_privacyValueAllowChatParticipants, mtproto.Predicate_privacyValueDisallowChatParticipants:
			chats = append(chats, r.GetChats()...)
		}
	}
	return chats
}

type memberRow struct {
	ChatId int64 `db:"chat_id"`
	UserId int64 `db:"user_id"`
}

// ChatMembers returns which of the users viewers are members of each of
// chats: a live basic group, or a supergroup they are in (not left, not
// removed). The clients name both in the rules of a story: iOS puts a
// supergroup among the chats of inputPrivacyValue*ChatParticipants
// (TelegramCore Stories.swift:1023-1027), and the ids of the two never
// meet (channels count from 1000000000, migrate-20261104-channel-ids.sql).
func ChatMembers(ctx context.Context, db *sqlx.DB, chats, viewers []int64) (map[int64][]int64, error) {
	members := make(map[int64][]int64)
	if len(chats) == 0 || len(viewers) == 0 {
		return members, nil
	}
	var rows []memberRow
	args := int64Args(nil, chats)
	args = int64Args(args, viewers)
	args = append(args, mtproto.ChatMemberStateNormal)
	args = int64Args(args, chats)
	args = int64Args(args, viewers)
	if err := db.QueryRowsPartial(ctx, &rows, "SELECT p.chat_id, p.user_id FROM chat_participants p JOIN chats c ON c.id = p.chat_id AND c.deactivated = 0"+
		" WHERE p.chat_id IN ("+in(len(chats))+") AND p.user_id IN ("+in(len(viewers))+") AND p.state = ?"+
		" UNION SELECT p.channel_id AS chat_id, p.user_id FROM channel_participants p JOIN channels c ON c.id = p.channel_id AND c.megagroup = 1 AND c.deleted = 0"+
		" WHERE p.channel_id IN ("+in(len(chats))+") AND p.user_id IN ("+in(len(viewers))+") AND p.state = 0",
		args...); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	for _, r := range rows {
		members[r.ChatId] = append(members[r.ChatId], r.UserId)
	}
	return members, nil
}

// ResolveChats returns rules with every "members of these chats" rule
// resolved for the users viewers (mtproto.ResolvePrivacyChats): the rule
// then names those of them who are members of its chats (ChatMembers), as
// the user service resolves the rules of the privacy keys (ADR 0031).
// Rules without chat rules are returned as they are.
func ResolveChats(ctx context.Context, db *sqlx.DB, rules []*mtproto.PrivacyRule, viewers []int64) ([]*mtproto.PrivacyRule, error) {
	chats := ChatsOf(rules)
	if len(chats) == 0 || len(viewers) == 0 {
		return rules, nil
	}
	members, err := ChatMembers(ctx, db, chats, viewers)
	if err != nil {
		return nil, err
	}
	return mtproto.ResolvePrivacyChats(rules, func(list []int64) []int64 {
		var out []int64
		for _, id := range list {
			out = append(out, members[id]...)
		}
		return out
	}), nil
}

type postingRow struct {
	ChannelId   int64 `db:"channel_id"`
	Role        int32 `db:"role"`
	AdminRights int32 `db:"admin_rights"`
}

// PostingChannels returns the channels the user userId may post stories
// to: those it created and those it is an admin of with post_stories,
// newest first.
func PostingChannels(ctx context.Context, db *sqlx.DB, userId int64) ([]int64, error) {
	var rows []postingRow
	if err := db.QueryRowsPartial(ctx, &rows, "SELECT p.channel_id, p.role, p.admin_rights FROM channel_participants p JOIN channels c ON c.id = p.channel_id AND c.deleted = 0"+
		" WHERE p.user_id = ? AND p.state = 0 AND p.role > 0 ORDER BY p.channel_id DESC", userId); err != nil && !errors.Is(err, sqlx.ErrNotFound) {
		return nil, err
	}
	var out []int64
	for _, r := range rows {
		// Role 2 is the creator (channel.RoleCreator), who holds every
		// right.
		if r.Role == 2 || r.AdminRights&mtproto.ADMIN_POST_STORIES != 0 {
			out = append(out, r.ChannelId)
		}
	}
	return out, nil
}

// ByRandom returns the story of peer that sender posted with the random_id
// randomId, nil if there is none: the story a repeated request stored.
func (s *Store) ByRandom(ctx context.Context, peer Peer, sender, randomId int64) (*Story, error) {
	list, err := s.list(ctx, "WHERE peer_type = ? AND peer_id = ? AND sender_user_id = ? AND random_id = ?", peer.Type, peer.Id, sender, randomId)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return list[0], nil
}
