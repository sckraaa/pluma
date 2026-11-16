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

package inlinemedia

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
)

// Row is a medium imported into dfs: a row of inline_media
// (teamgramd/deploy/sql/migrate-20261119-inline-bots.sql).
type Row struct {
	Provider    string `db:"provider"`
	SourceKey   []byte `db:"source_key"`
	Kind        Kind   `db:"kind"`
	MediaId     int64  `db:"media_id"`
	ProviderRef string `db:"provider_ref"`
	Bytes       int64  `db:"bytes"`
	CreatedAt   int64  `db:"created_at"`
	ServedAt    int64  `db:"last_served_at"`
}

// touchEvery is how stale last_served_at must be before a showing writes
// it again: a popular medium is shown many times an hour, and the column
// only has to tell the media nobody is shown from the rest.
const touchEvery = time.Hour

// Store keeps inline_media and inline_gif_sends in MySQL.
type Store struct {
	db *sqlx.DB
}

// NewStore returns the Store on db.
func NewStore(db *sqlx.DB) *Store {
	return &Store{db: db}
}

// Lookup returns the rows of provider with the keys, by key.
func (s *Store) Lookup(ctx context.Context, provider string, keys []Key) (map[Key]Row, error) {
	out := make(map[Key]Row, len(keys))
	if len(keys) == 0 {
		return out, nil
	}
	args := make([]interface{}, 0, len(keys)+1)
	args = append(args, provider)
	for _, k := range keys {
		args = append(args, k[:])
	}
	var rows []Row
	query := "SELECT provider, source_key, kind, media_id, provider_ref, bytes, created_at, last_served_at FROM inline_media WHERE provider = ? AND source_key IN (?" +
		strings.Repeat(", ?", len(keys)-1) + ")"
	if err := s.db.QueryRowsPartial(ctx, &rows, query, args...); err != nil {
		return nil, fmt.Errorf("inlinemedia: lookup: %w", err)
	}
	for _, r := range rows {
		var k Key
		if len(r.SourceKey) != len(k) {
			return nil, fmt.Errorf("inlinemedia: source_key of %d bytes", len(r.SourceKey))
		}
		copy(k[:], r.SourceKey)
		out[k] = r
	}
	return out, nil
}

// Put records an imported medium. A key another import recorded first
// keeps its medium (the other copy stays without a row: two replicas of
// bff importing the same medium at once is rare and costs one file); only
// last_served_at moves.
func (s *Store) Put(ctx context.Context, r Row) error {
	_, err := s.db.Exec(ctx,
		"INSERT INTO inline_media (provider, source_key, kind, media_id, provider_ref, bytes, created_at, last_served_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?) "+
			"ON DUPLICATE KEY UPDATE last_served_at = VALUES(last_served_at)",
		r.Provider, r.SourceKey, r.Kind, r.MediaId, r.ProviderRef, r.Bytes, r.CreatedAt, r.ServedAt)
	if err != nil {
		return fmt.Errorf("inlinemedia: put: %w", err)
	}
	return nil
}

// Touch moves last_served_at of the rows to now, for the rows where it is
// older than touchEvery.
func (s *Store) Touch(ctx context.Context, provider string, keys []Key, now time.Time) error {
	if len(keys) == 0 {
		return nil
	}
	args := make([]interface{}, 0, len(keys)+3)
	args = append(args, now.Unix(), provider, now.Add(-touchEvery).Unix())
	for _, k := range keys {
		args = append(args, k[:])
	}
	_, err := s.db.Exec(ctx,
		"UPDATE inline_media SET last_served_at = ? WHERE provider = ? AND last_served_at < ? AND source_key IN (?"+strings.Repeat(", ?", len(keys)-1)+")",
		args...)
	if err != nil {
		return fmt.Errorf("inlinemedia: touch: %w", err)
	}
	return nil
}

// CountGifSend counts one send of the document from @gif's results on the
// UTC day of now.
func (s *Store) CountGifSend(ctx context.Context, documentId int64, now time.Time) error {
	_, err := s.db.Exec(ctx,
		"INSERT INTO inline_gif_sends (document_id, day, sends) VALUES (?, ?, 1) ON DUPLICATE KEY UPDATE sends = sends + 1",
		documentId, now.UTC().Format(time.DateOnly))
	if err != nil {
		return fmt.Errorf("inlinemedia: gif send: %w", err)
	}
	return nil
}
