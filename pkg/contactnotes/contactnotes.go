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

// Package contactnotes keeps the private note an account has on a contact
// (task 084): contacts.updateContactNote and the note of contacts.addContact
// write it, users.getFullUser gives it in userFull.note. "Notes for
// contacts are only visible to you" (core.telegram.org/api/contacts): the
// note belongs to the pair (owner, contact) and nobody else reads it.
package contactnotes

import (
	"context"
	"fmt"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/proto"
)

// LengthMax is the longest note, in UTF-16 code units, as the clients count
// it: contact_note_length_limit of help.getAppConfig, 128 when the key is
// missing (tdesktop data_premium_limits.cpp:182-184, Android
// AppGlobalConfig.java:55).
const LengthMax = 128

// Store reads and writes the notes.
type Store struct {
	db *sqlx.DB
}

// NewStore returns a Store on db.
func NewStore(db *sqlx.DB) *Store {
	return &Store{db: db}
}

type row struct {
	Note     string `db:"note"`
	Entities []byte `db:"entities"`
}

// Get returns the note userID has on contactID, nil for none.
func (s *Store) Get(ctx context.Context, userID, contactID int64) (*mtproto.TextWithEntities, error) {
	var rows []row
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT note, entities FROM user_contact_notes WHERE user_id = ? AND contact_id = ?", userID, contactID); err != nil {
		return nil, err
	}
	if len(rows) == 0 || rows[0].Note == "" {
		return nil, nil
	}
	note := &mtproto.TextWithEntities{}
	if len(rows[0].Entities) > 0 {
		if err := proto.Unmarshal(rows[0].Entities, note); err != nil {
			return nil, fmt.Errorf("contactnotes: the entities of the note of %d on %d: %w", userID, contactID, err)
		}
	}
	note.Text = rows[0].Note
	if note.Entities == nil {
		note.Entities = []*mtproto.MessageEntity{}
	}
	return mtproto.MakeTLTextWithEntities(note).To_TextWithEntities(), nil
}

// Set keeps note as the note userID has on contactID; an empty text removes
// it (tdesktop removes a note by sending an empty one,
// info_profile_actions.cpp:787-800). The note is kept as Clean returns it.
// The caller checks that contactID is a contact of userID.
func (s *Store) Set(ctx context.Context, userID, contactID int64, note *mtproto.TextWithEntities, now int64) error {
	note = Clean(note)
	if note == nil {
		_, err := s.db.Exec(ctx, "DELETE FROM user_contact_notes WHERE user_id = ? AND contact_id = ?", userID, contactID)
		return err
	}
	entities, err := proto.Marshal(&mtproto.TextWithEntities{Entities: note.GetEntities()})
	if err != nil {
		return fmt.Errorf("contactnotes: marshal: %w", err)
	}
	_, err = s.db.Exec(ctx, "INSERT INTO user_contact_notes (user_id, contact_id, note, entities, updated_at) VALUES (?, ?, ?, ?, ?)"+
		" AS new ON DUPLICATE KEY UPDATE note = new.note, entities = new.entities, updated_at = new.updated_at",
		userID, contactID, note.GetText(), entities, now)
	return err
}

// DeleteTx removes the note userID has on contactID in tx: the note goes
// with the contact (the transaction that deletes it).
func DeleteTx(tx *sqlx.Tx, userID, contactID int64) error {
	_, err := tx.Exec("DELETE FROM user_contact_notes WHERE user_id = ? AND contact_id = ?", userID, contactID)
	return err
}

// Clean returns note as it is kept, nil for no note: the text cut to
// LengthMax UTF-16 code units at a character, with the entities that lie
// inside it. The clients do not let a longer note be sent; a mention by an
// input user, which only the sender's client can read, is left out.
func Clean(note *mtproto.TextWithEntities) *mtproto.TextWithEntities {
	text := note.GetText()
	if text == "" || !utf8.ValidString(text) {
		return nil
	}
	units := 0
	for i, r := range text {
		n := utf16.RuneLen(r)
		if n < 0 {
			n = 1
		}
		if units+n > LengthMax {
			text = text[:i]
			break
		}
		units += n
	}
	out := &mtproto.TextWithEntities{Text: text, Entities: []*mtproto.MessageEntity{}}
	for _, e := range note.GetEntities() {
		if e.GetPredicateName() == mtproto.Predicate_inputMessageEntityMentionName {
			continue
		}
		if e.GetOffset() < 0 || e.GetLength() <= 0 || int(e.GetOffset())+int(e.GetLength()) > units {
			continue
		}
		out.Entities = append(out.Entities, e)
	}
	return mtproto.MakeTLTextWithEntities(out).To_TextWithEntities()
}
