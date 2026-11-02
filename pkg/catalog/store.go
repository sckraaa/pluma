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

// Package catalog keeps the catalog imported from Telegram (task 024, ADR
// 0028): what every account gets alike, with ids of this server. The
// answers of methods are kept ready to send; sticker and emoji sets by id,
// short name and kind; and the document each imported one became, so that
// importing again gives the same ids.
package catalog

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
)

// Store is the catalog in MySQL (migrate-20261102-catalog.sql).
type Store struct {
	db *sqlx.DB
}

// NewStore returns the store of db.
func NewStore(db *sqlx.DB) *Store {
	return &Store{db: db}
}

// Answer is the stored answer of a method: an mtproto object in protobuf,
// and the hash the method compares with the client's.
type Answer struct {
	Hash int64
	Data []byte
}

// Set is a stored sticker or emoji set; Data is its messages.stickerSet in
// protobuf.
type Set struct {
	ID         int64
	AccessHash int64
	ShortName  string
	Kind       string // "" for a set clients ask for only by id or name
	Hash       int32
	Data       []byte
}

type answerRow struct {
	Hash int64  `db:"hash"`
	Data []byte `db:"data"`
}

type setRow struct {
	SetID      int64          `db:"set_id"`
	AccessHash int64          `db:"access_hash"`
	ShortName  string         `db:"short_name"`
	Kind       sql.NullString `db:"kind"`
	Hash       int32          `db:"hash"`
	Data       []byte         `db:"data"`
}

func (r setRow) set() *Set {
	return &Set{ID: r.SetID, AccessHash: r.AccessHash, ShortName: r.ShortName, Kind: r.Kind.String, Hash: r.Hash, Data: r.Data}
}

// Answer returns the stored answer of method for arg ("" for none), nil if
// the catalog has none.
func (s *Store) Answer(ctx context.Context, method, arg string) (*Answer, error) {
	var rows []answerRow
	if err := s.db.QueryRowsPartial(ctx, &rows,
		"SELECT hash, data FROM catalog_answers WHERE method = ? AND arg = ?", method, arg); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &Answer{Hash: rows[0].Hash, Data: rows[0].Data}, nil
}

const setColumns = "set_id, access_hash, short_name, kind, hash, data"

func (s *Store) set(ctx context.Context, where string, arg any) (*Set, error) {
	var rows []setRow
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT "+setColumns+" FROM catalog_sticker_sets WHERE "+where, arg); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows[0].set(), nil
}

// SetByID returns the set with id, nil if there is none.
func (s *Store) SetByID(ctx context.Context, id int64) (*Set, error) {
	return s.set(ctx, "set_id = ?", id)
}

// SetByShortName returns the set named name (case-insensitively, as
// t.me/addstickers names are), nil if there is none.
func (s *Store) SetByShortName(ctx context.Context, name string) (*Set, error) {
	if name == "" {
		return nil, nil
	}
	return s.set(ctx, "short_name = ?", name)
}

// SetByKind returns the set of a kind (KindAnimatedEmoji…), nil if there is
// none.
func (s *Store) SetByKind(ctx context.Context, kind string) (*Set, error) {
	if kind == "" {
		return nil, nil
	}
	return s.set(ctx, "kind = ?", kind)
}

// DocumentID returns the document a document of the catalog became, 0 if
// it was not imported yet.
func (s *Store) DocumentID(ctx context.Context, key [32]byte) (int64, error) {
	var rows []struct {
		DocumentID int64 `db:"document_id"`
	}
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT document_id FROM catalog_documents WHERE doc_key = ?", key[:]); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return rows[0].DocumentID, nil
}

// CatalogDocuments returns those of ids that are documents of the
// catalog, in the order of ids, each once.
func (s *Store) CatalogDocuments(ctx context.Context, ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return []int64{}, nil
	}
	query, args := "SELECT document_id FROM catalog_documents WHERE document_id IN (?"+strings.Repeat(", ?", len(ids)-1)+")", make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	var rows []struct {
		DocumentID int64 `db:"document_id"`
	}
	if err := s.db.QueryRowsPartial(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	known := make(map[int64]bool, len(rows))
	for _, r := range rows {
		known[r.DocumentID] = true
	}
	out := make([]int64, 0, len(rows))
	for _, id := range ids {
		if known[id] {
			out = append(out, id)
			delete(known, id)
		}
	}
	return out, nil
}

// PutDocument records the document a document of the catalog became. It
// is written as soon as the document is stored, before the catalog that
// uses it: an import that stops halfway does not store it twice.
func (s *Store) PutDocument(ctx context.Context, key [32]byte, documentID int64) error {
	_, err := s.db.Exec(ctx, "INSERT INTO catalog_documents (doc_key, document_id) VALUES (?, ?)", key[:], documentID)
	return err
}

// IDFor returns the id and access hash of this server for the thing of
// kind ("set", "theme", "wallpaper") named name: the ones it was given
// before, or the new ones, which are written at once and stay (catalog_ids).
func (s *Store) IDFor(ctx context.Context, kind, name string, newID func() (int64, int64, error)) (int64, int64, error) {
	read := func() (int64, int64, bool, error) {
		var rows []struct {
			ID         int64 `db:"id"`
			AccessHash int64 `db:"access_hash"`
		}
		if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT id, access_hash FROM catalog_ids WHERE kind = ? AND name = ?", kind, name); err != nil {
			return 0, 0, false, err
		}
		if len(rows) == 0 {
			return 0, 0, false, nil
		}
		return rows[0].ID, rows[0].AccessHash, true, nil
	}
	id, hash, ok, err := read()
	if err != nil || ok {
		return id, hash, err
	}
	if id, hash, err = newID(); err != nil {
		return 0, 0, err
	}
	// Two imports at once give one name one id: the first insert stays.
	if _, err := s.db.Exec(ctx, "INSERT IGNORE INTO catalog_ids (kind, name, id, access_hash) VALUES (?, ?, ?, ?)", kind, name, id, hash); err != nil {
		return 0, 0, err
	}
	id, hash, ok, err = read()
	if err == nil && !ok {
		err = errors.New("catalog: an id not kept")
	}
	return id, hash, err
}

// PutSet stores a set fetched on demand (task 030) beside the catalog's:
// a new import of the catalog keeps it. A set stored again is replaced.
func (s *Store) PutSet(ctx context.Context, set *Set, now int64) error {
	_, err := s.db.Exec(ctx, "INSERT INTO catalog_sticker_sets (set_id, access_hash, short_name, source, hash, data, imported_at) VALUES (?, ?, ?, 'demand', ?, ?, ?)"+
		" ON DUPLICATE KEY UPDATE access_hash = VALUES(access_hash), hash = VALUES(hash), data = VALUES(data), imported_at = VALUES(imported_at)",
		set.ID, set.AccessHash, set.ShortName, set.Hash, set.Data, now)
	return err
}

// Catalog is a whole catalog to store at once.
type Catalog struct {
	Answers map[[2]string]Answer // by method and argument
	Sets    []*Set
	Now     int64
}

// ErrEmptyCatalog is a catalog without answers: storing it would leave
// clients with nothing.
var ErrEmptyCatalog = errors.New("catalog: nothing to store")

// Replace stores c in one transaction in place of the stored catalog:
// clients get either the old catalog or the new one, whole. Answers and
// sets c no longer has are removed; a set keeps its id by short name.
func (s *Store) Replace(ctx context.Context, c *Catalog) error {
	if len(c.Answers) == 0 {
		return ErrEmptyCatalog
	}
	r := sqlx.TxWrapper(ctx, s.db, func(tx *sqlx.Tx, result *sqlx.StoreResult) {
		if _, result.Err = tx.Exec("DELETE FROM catalog_answers"); result.Err != nil {
			return
		}
		// Sets fetched on demand (task 030) stay; one the catalog now has
		// becomes the catalog's below.
		if _, result.Err = tx.Exec("DELETE FROM catalog_sticker_sets WHERE source = 'catalog'"); result.Err != nil {
			return
		}
		for key, a := range c.Answers {
			if _, result.Err = tx.Exec("INSERT INTO catalog_answers (method, arg, hash, data, imported_at) VALUES (?, ?, ?, ?, ?)",
				key[0], key[1], a.Hash, a.Data, c.Now); result.Err != nil {
				return
			}
		}
		for _, set := range c.Sets {
			var kind any
			if set.Kind != "" {
				kind = set.Kind
			}
			if _, result.Err = tx.Exec("INSERT INTO catalog_sticker_sets (set_id, access_hash, short_name, kind, source, hash, data, imported_at) VALUES (?, ?, ?, ?, 'catalog', ?, ?, ?)"+
				" ON DUPLICATE KEY UPDATE access_hash = VALUES(access_hash), kind = VALUES(kind), source = 'catalog', hash = VALUES(hash), data = VALUES(data), imported_at = VALUES(imported_at)",
				set.ID, set.AccessHash, set.ShortName, kind, set.Hash, set.Data, c.Now); result.Err != nil {
				return
			}
		}
	})
	return r.Err
}
