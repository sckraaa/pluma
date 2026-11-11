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

// Package attached keeps the stickers put on photos and videos (task 084;
// migrate-20261114-own-sticker-sets.sql): the stickers of
// inputMediaUploadedPhoto and inputMediaUploadedDocument, by the id of the
// photo or the document they made. Clients show "stickers used on this
// photo" for media marked as having them (photo.has_stickers,
// documentAttributeHasStickers) and ask for their sets with
// messages.getAttachedStickers.
package attached

import (
	"context"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/media/media"
)

// Max is the most stickers kept for one photo or video: this server's
// bound on what a client may name.
const Max = 100

// Store is the attached stickers in MySQL.
type Store struct {
	db *sqlx.DB
}

// NewStore returns the store of db.
func NewStore(db *sqlx.DB) *Store {
	return &Store{db: db}
}

// Put records the stickers documentIDs, in their order and each once, as
// those put on the photo or document mediaID. Recording them again changes
// nothing: the media is made once.
func (s *Store) Put(ctx context.Context, mediaID int64, documentIDs []int64) error {
	seen := map[int64]bool{}
	pos := 0
	for _, id := range documentIDs {
		if seen[id] || pos >= Max {
			continue
		}
		seen[id] = true
		if _, err := s.db.Exec(ctx, "INSERT IGNORE INTO media_attached_stickers (media_id, pos, document_id) VALUES (?, ?, ?)", mediaID, pos, id); err != nil {
			return err
		}
		pos++
	}
	return nil
}

// Of returns the stickers put on the photo or document mediaID, in their
// order; none for media without stickers.
func (s *Store) Of(ctx context.Context, mediaID int64) ([]int64, error) {
	var rows []struct {
		DocumentID int64 `db:"document_id"`
	}
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT document_id FROM media_attached_stickers WHERE media_id = ? ORDER BY pos", mediaID); err != nil {
		return nil, err
	}
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.DocumentID
	}
	return out, nil
}

// Has tells if stickers were put on the photo or document mediaID.
func (s *Store) Has(ctx context.Context, mediaID int64) (bool, error) {
	var rows []struct {
		Pos int32 `db:"pos"`
	}
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT pos FROM media_attached_stickers WHERE media_id = ? LIMIT 1", mediaID); err != nil {
		return false, err
	}
	return len(rows) > 0, nil
}

// Documents reads documents (the media service).
type Documents interface {
	MediaGetDocument(ctx context.Context, in *media.TLMediaGetDocument) (*mtproto.Document, error)
}

// Mark makes the media made of in tell of the stickers put on it. For an
// upload (inputMediaUploadedPhoto, inputMediaUploadedDocument) it records
// the stickers the client names (those that are stickers, named with their
// access hash) for the photo or the document made; for a photo or a
// document sent again by its id it finds whether any were recorded. Media
// with stickers gets the mark clients show "stickers on this photo" by:
// has_stickers of a photo, documentAttributeHasStickers of a document
// (Android StickersAlert.java:414, Telegram Desktop
// api_attached_stickers.cpp:27).
func (s *Store) Mark(ctx context.Context, docs Documents, in *mtproto.InputMedia, made *mtproto.MessageMedia) error {
	var id int64
	switch made.GetPredicateName() {
	case mtproto.Predicate_messageMediaPhoto:
		id = made.GetPhoto_FLAGPHOTO().GetId()
	case mtproto.Predicate_messageMediaDocument:
		if isSticker(made.GetDocument()) {
			return nil // a sticker has none on it
		}
		id = made.GetDocument().GetId()
	}
	if id == 0 {
		return nil
	}
	has := false
	switch in.GetPredicateName() {
	case mtproto.Predicate_inputMediaUploadedPhoto, mtproto.Predicate_inputMediaUploadedDocument:
		var ids []int64
		for _, st := range in.GetStickers() {
			if st.GetPredicateName() != mtproto.Predicate_inputDocument || len(ids) >= Max {
				continue
			}
			doc, err := docs.MediaGetDocument(ctx, &media.TLMediaGetDocument{Id: st.GetId()})
			if err != nil {
				return err
			}
			if isSticker(doc) && doc.GetAccessHash() == st.GetAccessHash() {
				ids = append(ids, doc.GetId())
			}
		}
		if len(ids) == 0 {
			return nil
		}
		if err := s.Put(ctx, id, ids); err != nil {
			return err
		}
		has = true
	case mtproto.Predicate_inputMediaPhoto, mtproto.Predicate_inputMediaDocument:
		var err error
		if has, err = s.Has(ctx, id); err != nil {
			return err
		}
	}
	if !has {
		return nil
	}
	if photo := made.GetPhoto_FLAGPHOTO(); photo != nil {
		photo.HasStickers = true
		return nil
	}
	doc := made.GetDocument()
	for _, a := range doc.GetAttributes() {
		if a.GetPredicateName() == mtproto.Predicate_documentAttributeHasStickers {
			return nil
		}
	}
	doc.Attributes = append(doc.Attributes, mtproto.MakeTLDocumentAttributeHasStickers(nil).To_DocumentAttribute())
	return nil
}

func isSticker(doc *mtproto.Document) bool {
	if doc.GetPredicateName() != mtproto.Predicate_document {
		return false
	}
	for _, a := range doc.GetAttributes() {
		if a.GetPredicateName() == mtproto.Predicate_documentAttributeSticker {
			return true
		}
	}
	return false
}
