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

package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/contacttokens"
	"github.com/teamgram/teamgram-server/pkg/env2"
)

// noteKey is a pair of an owner and a contact.
type noteKey struct{ owner, contact int64 }

// fakeNotes keeps the notes as they were set; an empty text removes one.
type fakeNotes struct {
	notes map[noteKey]*mtproto.TextWithEntities
	sets  int
}

func (f *fakeNotes) Set(_ context.Context, userID, contactID int64, note *mtproto.TextWithEntities, _ int64) error {
	f.sets++
	if note.GetText() == "" {
		delete(f.notes, noteKey{userID, contactID})
		return nil
	}
	f.notes[noteKey{userID, contactID}] = note
	return nil
}

// fakeTokens gives tokens that live contacttokens.Lifetime.
type fakeTokens struct {
	tokens map[string][2]int64 // the user and the end
	n      int
}

func (f *fakeTokens) Export(_ context.Context, userID, now int64) (string, int64, error) {
	f.n++
	token := strings.Repeat("A", 23) + string(rune('A'+f.n))
	f.tokens[token] = [2]int64{userID, now + contacttokens.Lifetime}
	return token, now + contacttokens.Lifetime, nil
}

func (f *fakeTokens) Import(_ context.Context, token string, now int64) (int64, error) {
	if t, ok := f.tokens[token]; ok && t[1] > now {
		return t[0], nil
	}
	return 0, nil
}

func note(text string) *mtproto.TextWithEntities {
	return mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{Text: text, Entities: []*mtproto.MessageEntity{}}).To_TextWithEntities()
}

// A note is kept on a contact only: CONTACT_ID_INVALID for nobody and for
// the account itself, CONTACT_MISSING for a user who is no contact; an
// empty note removes it.
func TestUpdateContactNote(t *testing.T) {
	self := immutableUser(me, "Me", "", "", "", nil)
	self.Contacts = []*mtproto.ContactData{contactData(me, 2, "Bob", "")}
	gone := immutableUser(4, "Gone", "", "", "", nil)
	gone.User.Deleted = true
	users := &fakeUsers{users: map[int64]*mtproto.ImmutableUser{me: self, 2: immutableUser(2, "Bob", "", "", "", nil), 3: immutableUser(3, "Cat", "", "", "", nil), 4: gone}}
	c := newCore(users, &fakeSync{}, nil, nil)
	notes := &fakeNotes{notes: map[noteKey]*mtproto.TextWithEntities{}}
	c.svcCtx.Dao.Notes = notes

	for name, k := range map[string]struct {
		id   *mtproto.InputUser
		want string
	}{
		"the account itself":   {mtproto.MakeTLInputUserSelf(nil).To_InputUser(), "CONTACT_ID_INVALID"},
		"the account by id":    {inputUser(me), "CONTACT_ID_INVALID"},
		"nobody":               {inputUser(99), "CONTACT_ID_INVALID"},
		"a deleted account":    {inputUser(4), "CONTACT_ID_INVALID"},
		"a user not a contact": {inputUser(3), "CONTACT_MISSING"},
		"no user":              {nil, "CONTACT_ID_INVALID"},
	} {
		_, err := c.ContactsUpdateContactNote(&mtproto.TLContactsUpdateContactNote{Id: k.id, Note: note("x")})
		if _, msg := rpcError(err); msg != k.want {
			t.Errorf("%s: %v, want %s", name, err, k.want)
		}
	}
	if notes.sets != 0 {
		t.Fatalf("%d notes set by refused requests", notes.sets)
	}

	if ok, err := c.ContactsUpdateContactNote(&mtproto.TLContactsUpdateContactNote{Id: inputUser(2), Note: note("met in Riga")}); err != nil || !mtproto.FromBool(ok) {
		t.Fatalf("a note on a contact: %v, %v", ok, err)
	}
	if got := notes.notes[noteKey{me, 2}].GetText(); got != "met in Riga" {
		t.Errorf("the kept note %q", got)
	}
	if _, err := c.ContactsUpdateContactNote(&mtproto.TLContactsUpdateContactNote{Id: inputUser(2), Note: note("")}); err != nil {
		t.Fatal(err)
	}
	if _, ok := notes.notes[noteKey{me, 2}]; ok {
		t.Error("an empty note left the note in place")
	}
}

// contacts.addContact keeps the note sent with the contact and leaves the
// kept one alone when none is sent.
func TestAddContactNote(t *testing.T) {
	users := &fakeUsers{users: map[int64]*mtproto.ImmutableUser{me: immutableUser(me, "Me", "", "", "", nil), 2: immutableUser(2, "Bob", "", "", "", nil)}}
	c := newCore(users, &fakeSync{}, nil, nil)
	notes := &fakeNotes{notes: map[noteKey]*mtproto.TextWithEntities{}}
	c.svcCtx.Dao.Notes = notes

	add := func(n *mtproto.TextWithEntities) {
		t.Helper()
		if _, err := c.ContactsAddContact(&mtproto.TLContactsAddContact{Id: inputUser(2), FirstName: "Bob", Note: n}); err != nil {
			t.Fatal(err)
		}
	}
	add(note("a colleague"))
	if got := notes.notes[noteKey{me, 2}].GetText(); got != "a colleague" {
		t.Errorf("the note of the new contact %q", got)
	}
	add(nil)
	if got := notes.notes[noteKey{me, 2}].GetText(); got != "a colleague" || notes.sets != 1 {
		t.Errorf("without a note: kept %q, set %d times; want the note untouched", got, notes.sets)
	}
	add(note(""))
	if _, ok := notes.notes[noteKey{me, 2}]; ok {
		t.Error("an empty note left the note in place")
	}
}

// A token is a link of the link host with its end; while it lasts it gives
// the user as the account sees it, to anybody, the owner too; later, or
// unknown, or of a deleted account, it is IMPORT_TOKEN_INVALID.
func TestContactTokens(t *testing.T) {
	bob := immutableUser(2, "Bob", "B", "", "100", nil)
	users := &fakeUsers{users: map[int64]*mtproto.ImmutableUser{me: immutableUser(me, "Me", "", "", "", nil), 2: bob}}
	tokens := &fakeTokens{tokens: map[string][2]int64{}}
	asMe := newCore(users, &fakeSync{}, nil, nil)
	asMe.svcCtx.Dao.Tokens = tokens

	before := time.Now().Unix()
	exported, err := asMe.ContactsExportContactToken(&mtproto.TLContactsExportContactToken{})
	if err != nil {
		t.Fatal(err)
	}
	prefix := "https://" + env2.TDotMe + "/contact/"
	token := strings.TrimPrefix(exported.GetUrl(), prefix)
	if exported.GetPredicateName() != mtproto.Predicate_exportedContactToken || !strings.HasPrefix(exported.GetUrl(), prefix) || !contacttokens.Valid(token) {
		t.Fatalf("exported %v, want a link %s<token>", exported, prefix)
	}
	if left := int64(exported.GetExpires()) - before; left < contacttokens.Lifetime || left > contacttokens.Lifetime+5 {
		t.Errorf("the token ends in %d seconds, want %d", left, contacttokens.Lifetime)
	}

	// The token of Bob, imported by the account.
	tokens.tokens["bobsbobsbobsbobsbobsbobs"] = [2]int64{2, time.Now().Unix() + 60}
	u, err := asMe.ContactsImportContactToken(&mtproto.TLContactsImportContactToken{Token: "bobsbobsbobsbobsbobsbobs"})
	if err != nil || u.GetId() != 2 || u.GetSelf() || u.GetAccessHash().GetValue() != 20 || u.GetFirstName().GetValue() != "Bob" || u.GetContact() {
		t.Fatalf("the user of the token: %v, %v; want Bob, with an access hash, not a contact", u, err)
	}
	// Its own token gives the account itself.
	if u, err = asMe.ContactsImportContactToken(&mtproto.TLContactsImportContactToken{Token: token}); err != nil || u.GetId() != me || !u.GetSelf() {
		t.Errorf("the account's own token: %v, %v", u, err)
	}

	tokens.tokens["pastpastpastpastpastpast"] = [2]int64{2, time.Now().Unix() - 1}
	tokens.tokens["gonegonegonegonegonegone"] = [2]int64{77, time.Now().Unix() + 60}
	for _, bad := range []string{"", "nobodynobodynobodynobody", "pastpastpastpastpastpast", "gonegonegonegonegonegone"} {
		_, err = asMe.ContactsImportContactToken(&mtproto.TLContactsImportContactToken{Token: bad})
		if _, msg := rpcError(err); msg != "IMPORT_TOKEN_INVALID" {
			t.Errorf("token %q: %v, want IMPORT_TOKEN_INVALID", bad, err)
		}
	}
	bob.User.Deleted = true
	_, err = asMe.ContactsImportContactToken(&mtproto.TLContactsImportContactToken{Token: "bobsbobsbobsbobsbobsbobs"})
	if _, msg := rpcError(err); msg != "IMPORT_TOKEN_INVALID" {
		t.Errorf("the token of a deleted account: %v, want IMPORT_TOKEN_INVALID", err)
	}
}
