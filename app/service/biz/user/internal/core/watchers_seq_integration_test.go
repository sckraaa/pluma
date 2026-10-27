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

package core

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	inboxpb "github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/app/service/biz/user/internal/dao"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// The account's name, username, photo and Premium for the users who see it
// (ADR 0021): the transaction of the change writes an inbox event with the
// update to each of them, msg journals it with seq in their accounts.

// watcherEvents describes the events userId's changes wrote, in order: a
// push with seq to the account itself as "self <update>", an inbox event
// with updates with seq as "<recipient> <update>".
func watcherEvents(t *testing.T, userId int64) []string {
	t.Helper()
	var out []string
	pushes := seqContainers(t, userId)
	for _, e := range eventsOf(t, userId) {
		switch e.method {
		case "sync.TL_sync_pushUpdates":
			if len(pushes) == 0 {
				t.Fatalf("a push of user %d without seq", userId)
			}
			out = append(out, "self "+watcherUpdate(t, userId, pushes[0].GetUpdates()))
			pushes = pushes[1:]
		case "inbox.TL_inbox_seqUpdatesToInbox":
			var in inboxpb.TLInboxSeqUpdatesToInbox
			if err := jsonx.Unmarshal(e.payload, &in); err != nil {
				t.Fatal(err)
			}
			if e.topic != "Inbox-T" || in.GetFromId() != userId || in.GetUserId() != e.recipient {
				t.Errorf("event to %d on %s: %v", e.recipient, e.topic, &in)
			}
			out = append(out, fmt.Sprintf("%d %s", e.recipient, watcherUpdate(t, userId, in.GetUpdates())))
		default:
			t.Errorf("event %s to %d", e.method, e.recipient)
		}
	}
	return out
}

func watcherUpdate(t *testing.T, userId int64, updates []*mtproto.Update) string {
	t.Helper()
	if len(updates) != 1 || updates[0].GetUserId() != userId {
		t.Fatalf("updates %v, want one about user %d", updates, userId)
	}
	u := updates[0]
	if u.GetPredicateName() == mtproto.Predicate_updateUserName {
		return fmt.Sprintf("name %s %s [%s]", u.GetFirstName(), u.GetLastName(), usernamesOf(u))
	}
	return u.GetPredicateName()
}

func TestNameAndPhotoToWatchers(t *testing.T) {
	const p1 = 7101
	ctx := context.Background()
	c := mediaCore(t, newFakeMedia(p1))
	me, _ := newUser(t, "Mia")
	ann, _ := newUser(t, "Ann") // has Mia as a contact
	bob, _ := newUser(t, "Bob") // has a private dialog with Mia
	cat, _ := newUser(t, "Cat") // both
	dan, _ := newUser(t, "Dan") // deleted Mia from the contacts
	eve, _ := newUser(t, "Eve") // has Mia as a contact, deleted the account
	fay, _ := newUser(t, "Fay") // Mia has a dialog with Fay, not Fay with Mia
	contact := func(owner, contact int64, deleted bool) {
		t.Helper()
		if _, err := itRaw.Exec("INSERT INTO user_contacts (owner_user_id, contact_user_id, contact_first_name, is_deleted, date2) VALUES (?, ?, 'M.', ?, 1)",
			owner, contact, deleted); err != nil {
			t.Fatal(err)
		}
	}
	privateDialog := func(owner, peer int64) {
		t.Helper()
		if _, err := itRaw.Exec("INSERT INTO dialogs (user_id, peer_type, peer_id, peer_dialog_id, draft_message_data, date2) VALUES (?, ?, ?, ?, 'null', 1)",
			owner, mtproto.PEER_USER, peer, mtproto.MakePeerDialogId(mtproto.PEER_USER, peer)); err != nil {
			t.Fatal(err)
		}
	}
	contact(ann, me, false)
	contact(cat, me, false)
	contact(dan, me, true)
	contact(eve, me, false)
	contact(me, ann, false)
	privateDialog(bob, me)
	privateDialog(cat, me)
	privateDialog(me, fay)
	privateDialog(me, me) // Saved Messages
	if _, err := itRaw.Exec("UPDATE users SET deleted = 1, phone = CONCAT('-', id) WHERE id = ?", eve); err != nil {
		t.Fatal(err)
	}

	name := func(first, last string) {
		t.Helper()
		if _, err := c.UserUpdateFirstAndLastName(&user.TLUserUpdateFirstAndLastName{UserId: me, FirstName: first, LastName: last}); err != nil {
			t.Fatal(err)
		}
	}
	name("Mia", "Moss")
	name("Mia", "Moss") // changes nothing
	username := fmt.Sprintf("mia%d", me)
	if _, err := c.UserUpdateUsername(&user.TLUserUpdateUsername{UserId: me, Username: username}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UserUpdateProfilePhoto(&user.TLUserUpdateProfilePhoto{UserId: me, Id: p1}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.UserUpdateProfilePhoto(&user.TLUserUpdateProfilePhoto{UserId: me, Id: p1}); err != nil { // changes nothing
		t.Fatal(err)
	}
	for _, change := range []dao.PremiumChange{{Grant: true}, {Grant: true}} { // the second changes nothing
		if _, err := ChangePremium(ctx, c.svcCtx, me, change); err != nil {
			t.Fatal(err)
		}
	}

	var want []string
	for _, u := range []string{
		"name Mia Moss []",
		fmt.Sprintf("name Mia Moss [%s/true/true]", username),
		mtproto.Predicate_updateUser,
		mtproto.Predicate_updateUser,
	} {
		want = append(want, "self "+u)
		for _, to := range []int64{ann, bob, cat} {
			want = append(want, fmt.Sprintf("%d %s", to, u))
		}
	}
	if got := watcherEvents(t, me); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("events\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, u := range []int64{ann, bob, cat, dan, eve, fay} {
		if n := len(eventsOf(t, u)); n != 0 {
			t.Errorf("user %d wrote %d events", u, n)
		}
	}
	// The account's own pushes have their rows of the seq journal; the
	// others' accounts get theirs when msg applies the events.
	if n := len(seqPushes(t, me)); n != 4 || stateSeq(t, me) != userbox.BaseSeq+4 {
		t.Errorf("%d own pushes with seq, seq %d", n, stateSeq(t, me))
	}
	for _, u := range []int64{ann, bob, cat} {
		if s := stateSeq(t, u); s != userbox.BaseSeq {
			t.Errorf("user %d: seq %d before msg applies the events", u, s)
		}
	}
}

// The migration of the indexes the reads of the watchers use finds the
// tables without them, and runs again without a change.
func TestProfileWatchersMigration(t *testing.T) {
	path, err := testinfra.SchemaFile("migrate-20261027-profile-watchers.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"ALTER TABLE user_contacts DROP INDEX contact_user_id",
		"ALTER TABLE dialogs DROP INDEX peer",
	} {
		if _, err = itRaw.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for run := range 2 {
		if err = itDB.ApplySQLFile(context.Background(), path); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		for _, ix := range [][2]string{{"user_contacts", "contact_user_id"}, {"dialogs", "peer"}} {
			if n := count(t, "SELECT COUNT(DISTINCT INDEX_NAME) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND INDEX_NAME = ?", ix[0], ix[1]); n != 1 {
				t.Errorf("run %d: %s has %d indexes %s", run, ix[0], n, ix[1])
			}
		}
	}
}
