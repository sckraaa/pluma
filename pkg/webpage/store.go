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

package webpage

import (
	"context"
	"crypto/sha256"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/proto"
)

// Store keeps the cards of pages (table webpages,
// migrate-20261102-webpages.sql): one row per normalized address, with the
// card's id, which stays the same when the card is fetched again, so that
// clients that cached it by id recognise it.
type Store struct {
	db *sqlx.DB
}

// NewStore returns the store of db.
func NewStore(db *sqlx.DB) *Store {
	return &Store{db: db}
}

type cached struct {
	id      int64
	page    *mtproto.WebPage
	expires int64
}

type pageRow struct {
	WebpageID int64  `db:"webpage_id"`
	Data      []byte `db:"data"`
	ExpiresAt int64  `db:"expires_at"`
}

func urlHash(u string) []byte {
	h := sha256.Sum256([]byte(u))
	return h[:]
}

// get returns the stored card of the address u, nil if there is none.
func (s *Store) get(ctx context.Context, u string) (*cached, error) {
	var rows []pageRow
	if err := s.db.QueryRowsPartial(ctx, &rows,
		"SELECT webpage_id, data, expires_at FROM webpages WHERE url_hash = ?", urlHash(u)); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	page := &mtproto.WebPage{}
	if err := proto.Unmarshal(rows[0].Data, page); err != nil {
		return nil, err
	}
	return &cached{id: rows[0].WebpageID, page: page, expires: rows[0].ExpiresAt}, nil
}

// byID returns the stored card with id, nil if there is none.
func (s *Store) byID(ctx context.Context, id int64) (*mtproto.WebPage, error) {
	var rows []pageRow
	if err := s.db.QueryRowsPartial(ctx, &rows,
		"SELECT webpage_id, data, expires_at FROM webpages WHERE webpage_id = ?", id); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	page := &mtproto.WebPage{}
	if err := proto.Unmarshal(rows[0].Data, page); err != nil {
		return nil, err
	}
	return page, nil
}

// put stores the card of u until expires; the id of a stored card stays.
func (s *Store) put(ctx context.Context, u string, page *mtproto.WebPage, fetched, expires int64) error {
	data, err := proto.Marshal(page)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx,
		"INSERT INTO webpages (url_hash, url, webpage_id, data, fetched_at, expires_at) VALUES (?, ?, ?, ?, ?, ?) "+
			"ON DUPLICATE KEY UPDATE data = VALUES(data), fetched_at = VALUES(fetched_at), expires_at = VALUES(expires_at)",
		urlHash(u), clip(u, 2048), page.GetId(), data, fetched, expires)
	return err
}
