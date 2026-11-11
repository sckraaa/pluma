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

//go:build integration

package contactnotes

import (
	"context"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

// The note of an account on a contact (migrate-20261114-user-look.sql): it
// is the owner's alone, the last one set stays, with its entities; an
// empty one and the removal of the contact remove it.
func TestStore(t *testing.T) {
	ctx := context.Background()
	db, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Terminate(ctx) })
	conf := db.Config()
	conn := sqlx.NewMySQL(&conf)
	s := NewStore(conn)

	note := func(text string, entities ...*mtproto.MessageEntity) *mtproto.TextWithEntities {
		return mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{Text: text, Entities: entities}).To_TextWithEntities()
	}
	get := func(owner, contact int64) *mtproto.TextWithEntities {
		t.Helper()
		n, err := s.Get(ctx, owner, contact)
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	if n := get(1, 2); n != nil {
		t.Fatalf("a note nobody set: %v", n)
	}
	bold := mtproto.MakeTLMessageEntityBold(&mtproto.MessageEntity{Offset: 0, Length: 3}).To_MessageEntity()
	emoji := mtproto.MakeTLMessageEntityCustomEmoji(&mtproto.MessageEntity{Offset: 4, Length: 2, DocumentId: 777}).To_MessageEntity()
	must(s.Set(ctx, 1, 2, note("met \U0001F600 in Riga", bold, emoji), 100))
	must(s.Set(ctx, 1, 3, note("plain"), 100))
	must(s.Set(ctx, 2, 1, note("the other way"), 100))

	n := get(1, 2)
	if n.GetPredicateName() != mtproto.Predicate_textWithEntities || n.GetText() != "met \U0001F600 in Riga" || len(n.GetEntities()) != 2 ||
		n.GetEntities()[0].GetPredicateName() != mtproto.Predicate_messageEntityBold || n.GetEntities()[1].GetDocumentId() != 777 || n.GetEntities()[1].GetOffset() != 4 {
		t.Errorf("the note with entities read as %v", n)
	}
	if n = get(1, 3); n.GetText() != "plain" || n.GetEntities() == nil || len(n.GetEntities()) != 0 {
		t.Errorf("the plain note read as %v, want it with an empty list of entities", n)
	}
	if n = get(2, 1); n.GetText() != "the other way" {
		t.Errorf("the note of the other side read as %v", n)
	}

	must(s.Set(ctx, 1, 2, note("changed"), 200))
	if n = get(1, 2); n.GetText() != "changed" || len(n.GetEntities()) != 0 {
		t.Errorf("the changed note read as %v", n)
	}
	must(s.Set(ctx, 1, 2, note(""), 300))
	must(s.Set(ctx, 1, 2, note(""), 300)) // nothing to remove
	if n = get(1, 2); n != nil {
		t.Errorf("an empty note left %v", n)
	}

	// The note goes with the contact, in the transaction that removes it.
	must(conn.Transact(ctx, func(tx *sqlx.Tx) error { return DeleteTx(tx, 1, 3) }))
	if n = get(1, 3); n != nil {
		t.Errorf("the note of a removed contact stayed: %v", n)
	}
	if n = get(2, 1); n.GetText() != "the other way" {
		t.Errorf("the note of the other side after the removals: %v", n)
	}
}
