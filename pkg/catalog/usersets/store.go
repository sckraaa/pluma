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

package usersets

import (
	"context"
	"errors"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/catalog"
	"google.golang.org/protobuf/proto"
)

// Store is the sets users made, in MySQL (migrate-20261114-own-sticker-sets.sql).
type Store struct {
	db *sqlx.DB
}

// NewStore returns the store of db.
func NewStore(db *sqlx.DB) *Store {
	return &Store{db: db}
}

var (
	// ErrShortNameOccupied is a set to make whose short name a set has.
	ErrShortNameOccupied = errors.New("usersets: the short name is taken")
	// ErrNoSet is a set to change that is not there.
	ErrNoSet = errors.New("usersets: no such set")
	// ErrUnchanged is returned by a change that leaves the set as it is:
	// nothing is written.
	ErrUnchanged = errors.New("usersets: nothing to change")
)

// Link is a sticker of a set and the document it was made from.
type Link struct {
	DocumentID int64 `db:"document_id"`
	SourceID   int64 `db:"source_id"`
}

func marshal(ms *mtproto.Messages_StickerSet) ([]byte, error) {
	return proto.MarshalOptions{Deterministic: true}.Marshal(ms)
}

// Create stores a new set of owner with its stickers. ms is sealed here.
func (s *Store) Create(ctx context.Context, owner int64, ms *mtproto.Messages_StickerSet, thumbDocumentID int64, links []Link, now int64) (*catalog.Set, error) {
	if err := Seal(ms); err != nil {
		return nil, err
	}
	data, err := marshal(ms)
	if err != nil {
		return nil, err
	}
	set := &catalog.Set{
		ID: ms.GetSet().GetId(), AccessHash: ms.GetSet().GetAccessHash(), ShortName: ms.GetSet().GetShortName(),
		Hash: ms.GetSet().GetHash(), Data: data, OwnerID: owner, ThumbDocumentID: thumbDocumentID,
	}
	r := sqlx.TxWrapper(ctx, s.db, func(tx *sqlx.Tx, result *sqlx.StoreResult) {
		if _, result.Err = tx.Exec("INSERT INTO catalog_sticker_sets (set_id, access_hash, short_name, source, owner_id, thumb_document_id, hash, data, imported_at)"+
			" VALUES (?, ?, ?, 'user', ?, ?, ?, ?, ?)", set.ID, set.AccessHash, set.ShortName, owner, thumbDocumentID, set.Hash, data, now); result.Err != nil {
			return
		}
		for _, l := range links {
			if _, result.Err = tx.Exec("INSERT INTO user_sticker_set_documents (document_id, set_id, source_id) VALUES (?, ?, ?)", l.DocumentID, set.ID, l.SourceID); result.Err != nil {
				return
			}
		}
	})
	if sqlx.IsDuplicate(r.Err) {
		return nil, ErrShortNameOccupied
	}
	if r.Err != nil {
		return nil, r.Err
	}
	return set, nil
}

// Change is a set being changed: the set as stored, its messages.stickerSet
// to change in place, and the stickers it has.
type Change struct {
	Set   *catalog.Set
	MS    *mtproto.Messages_StickerSet
	Links []Link

	added   []Link
	removed []int64
}

// Add records a sticker put in the set.
func (c *Change) Add(l Link) { c.added = append(c.added, l) }

// Drop records a sticker taken out of the set.
func (c *Change) Drop(documentID int64) { c.removed = append(c.removed, documentID) }

// SourceOf is the sticker of the set made from the document source, or
// that is this document; 0 if there is none.
func (c *Change) SourceOf(source int64) int64 {
	for _, l := range c.Links {
		if l.SourceID == source || l.DocumentID == source {
			return l.DocumentID
		}
	}
	return 0
}

type lockedRow struct {
	SetID      int64  `db:"set_id"`
	AccessHash int64  `db:"access_hash"`
	ShortName  string `db:"short_name"`
	Hash       int32  `db:"hash"`
	Data       []byte `db:"data"`
	OwnerID    int64  `db:"owner_id"`
	ThumbID    int64  `db:"thumb_document_id"`
}

// Update changes the set id of a user in one transaction, with the set's
// row locked: fn gets the set as it is and changes c.MS (and
// c.Set.ThumbDocumentID); the set is sealed and stored after it. fn
// returning ErrUnchanged leaves everything as it is, and Update returns
// the set unchanged; any other error of fn is returned and nothing is
// stored. A set that is not there, or that no user made, is ErrNoSet.
func (s *Store) Update(ctx context.Context, id int64, fn func(c *Change) error) (*catalog.Set, error) {
	var out *catalog.Set
	r := sqlx.TxWrapper(ctx, s.db, func(tx *sqlx.Tx, result *sqlx.StoreResult) {
		var rows []lockedRow
		if result.Err = tx.QueryRowsPartial(&rows, "SELECT set_id, access_hash, short_name, hash, data, owner_id, thumb_document_id FROM catalog_sticker_sets"+
			" WHERE set_id = ? AND source = 'user' FOR UPDATE", id); result.Err != nil {
			return
		}
		if len(rows) == 0 {
			result.Err = ErrNoSet
			return
		}
		row := rows[0]
		c := &Change{
			Set: &catalog.Set{ID: row.SetID, AccessHash: row.AccessHash, ShortName: row.ShortName, Hash: row.Hash, Data: row.Data,
				OwnerID: row.OwnerID, ThumbDocumentID: row.ThumbID},
			MS: &mtproto.Messages_StickerSet{},
		}
		out = c.Set
		if result.Err = proto.Unmarshal(row.Data, c.MS); result.Err != nil {
			return
		}
		if result.Err = tx.QueryRowsPartial(&c.Links, "SELECT document_id, source_id FROM user_sticker_set_documents WHERE set_id = ? AND removed = 0", id); result.Err != nil {
			return
		}
		if result.Err = fn(c); result.Err != nil {
			return
		}
		if result.Err = Seal(c.MS); result.Err != nil {
			return
		}
		var data []byte
		if data, result.Err = marshal(c.MS); result.Err != nil {
			return
		}
		c.Set.Hash, c.Set.Data = c.MS.GetSet().GetHash(), data
		if _, result.Err = tx.Exec("UPDATE catalog_sticker_sets SET hash = ?, data = ?, thumb_document_id = ? WHERE set_id = ?",
			c.Set.Hash, data, c.Set.ThumbDocumentID, id); result.Err != nil {
			return
		}
		for _, l := range c.added {
			if _, result.Err = tx.Exec("INSERT INTO user_sticker_set_documents (document_id, set_id, source_id) VALUES (?, ?, ?)", l.DocumentID, id, l.SourceID); result.Err != nil {
				return
			}
		}
		for _, d := range c.removed {
			if _, result.Err = tx.Exec("UPDATE user_sticker_set_documents SET removed = 1 WHERE document_id = ? AND set_id = ?", d, id); result.Err != nil {
				return
			}
		}
	})
	if errors.Is(r.Err, ErrUnchanged) {
		return out, nil
	}
	if r.Err != nil {
		return nil, r.Err
	}
	return out, nil
}

// Delete removes the set id if owner made it; a set that is not there, or
// not owner's, is ErrNoSet. Its stickers stay as documents: messages that
// carry them still show them.
func (s *Store) Delete(ctx context.Context, id, owner int64) error {
	r := sqlx.TxWrapper(ctx, s.db, func(tx *sqlx.Tx, result *sqlx.StoreResult) {
		res, err := tx.Exec("DELETE FROM catalog_sticker_sets WHERE set_id = ? AND source = 'user' AND owner_id = ?", id, owner)
		if err != nil {
			result.Err = err
			return
		}
		if n, err := res.RowsAffected(); err != nil || n == 0 {
			result.Err = cmpErr(err, ErrNoSet)
			return
		}
		_, result.Err = tx.Exec("UPDATE user_sticker_set_documents SET removed = 1 WHERE set_id = ?", id)
	})
	return r.Err
}

func cmpErr(err, otherwise error) error {
	if err != nil {
		return err
	}
	return otherwise
}

// Owned returns the ids of the sets owner made, the last made first.
func (s *Store) Owned(ctx context.Context, owner int64) ([]int64, error) {
	var rows []struct {
		SetID int64 `db:"set_id"`
	}
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT set_id FROM catalog_sticker_sets WHERE owner_id = ? AND source = 'user' ORDER BY imported_at DESC, set_id DESC", owner); err != nil {
		return nil, err
	}
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.SetID
	}
	return out, nil
}

// Links returns the stickers the set id has, each with the document it was
// made from.
func (s *Store) Links(ctx context.Context, id int64) ([]Link, error) {
	var rows []Link
	err := s.db.QueryRowsPartial(ctx, &rows, "SELECT document_id, source_id FROM user_sticker_set_documents WHERE set_id = ? AND removed = 0", id)
	return rows, err
}

// SetOf returns the set the sticker documentID is in, 0 if it is in none
// (a sticker taken out of its set is in none).
func (s *Store) SetOf(ctx context.Context, documentID int64) (int64, error) {
	var rows []struct {
		SetID int64 `db:"set_id"`
	}
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT set_id FROM user_sticker_set_documents WHERE document_id = ? AND removed = 0", documentID); err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return rows[0].SetID, nil
}

// ShortNameTaken tells if a set has the short name (without case, as the
// names of t.me/addstickers links compare).
func (s *Store) ShortNameTaken(ctx context.Context, name string) (bool, error) {
	var rows []struct {
		SetID int64 `db:"set_id"`
	}
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT set_id FROM catalog_sticker_sets WHERE short_name = ? LIMIT 1", name); err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}
