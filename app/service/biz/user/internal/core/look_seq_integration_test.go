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
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	inboxpb "github.com/teamgram/teamgram-server/app/messenger/msg/inbox/inbox"
	"github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/testinfra"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// The look of an account with seq (task 084, ADR 0020 and 0021): a color
// or an emoji status, its row of the seq journal, the push to every device
// of the account and the inbox event to each user who sees it are one box
// transaction; a change that changes nothing writes none of them.

// lookEvents describes the events userId's changes wrote, in order: a push
// with seq to the account itself as "self <update>", an inbox event with
// an update with seq as "<recipient> <update>". A color is "color/pattern"
// of the user the push carries, a status "document/until".
func lookEvents(t *testing.T, userId int64) []string {
	t.Helper()
	status := func(s *mtproto.EmojiStatus) string {
		if s.GetPredicateName() == mtproto.Predicate_emojiStatusEmpty {
			return "empty"
		}
		return fmt.Sprintf("%d/%d", s.GetDocumentId(), s.GetUntil_FLAGINT32().GetValue())
	}
	color := func(c *mtproto.PeerColor) string {
		if c == nil {
			return "none"
		}
		v := "-"
		if c.GetColor() != nil {
			v = fmt.Sprint(c.GetColor().GetValue())
		}
		return fmt.Sprintf("%s/%d", v, c.GetBackgroundEmojiId_FLAGINT64().GetValue())
	}
	var out []string
	pushes := seqContainers(t, userId)
	for _, e := range eventsOf(t, userId) {
		switch e.method {
		case "sync.TL_sync_pushUpdates":
			if len(pushes) == 0 || len(pushes[0].GetUpdates()) != 1 {
				t.Fatalf("a push of user %d without seq or without its one update", userId)
			}
			u := pushes[0].GetUpdates()[0]
			switch u.GetPredicateName() {
			case mtproto.Predicate_updateUser:
				if len(pushes[0].GetUsers()) != 1 || !pushes[0].GetUsers()[0].GetSelf() {
					t.Fatalf("updateUser to the account without the account: %v", pushes[0].GetUsers())
				}
				me := pushes[0].GetUsers()[0]
				out = append(out, fmt.Sprintf("self updateUser %s %s", color(me.GetColor_FLAGPEERCOLOR()), color(me.GetProfileColor())))
			case mtproto.Predicate_updateUserEmojiStatus:
				if u.GetUserId() != userId || len(pushes[0].GetUsers()) != 1 {
					t.Fatalf("updateUserEmojiStatus of user %d with %d users", u.GetUserId(), len(pushes[0].GetUsers()))
				}
				carried := "none"
				if s := pushes[0].GetUsers()[0].GetEmojiStatus(); s != nil {
					carried = status(s)
				}
				out = append(out, fmt.Sprintf("self updateUserEmojiStatus %s user %s", status(u.GetEmojiStatus()), carried))
			default:
				out = append(out, "self "+u.GetPredicateName())
			}
			pushes = pushes[1:]
		case "inbox.TL_inbox_seqUpdatesToInbox":
			var in inboxpb.TLInboxSeqUpdatesToInbox
			if err := jsonx.Unmarshal(e.payload, &in); err != nil {
				t.Fatal(err)
			}
			if e.topic != "Inbox-T" || in.GetFromId() != userId || in.GetUserId() != e.recipient || len(in.GetUpdates()) != 1 || in.GetUpdates()[0].GetUserId() != userId {
				t.Fatalf("event to %d on %s: %v", e.recipient, e.topic, &in)
			}
			u := in.GetUpdates()[0]
			if u.GetPredicateName() == mtproto.Predicate_updateUserEmojiStatus {
				out = append(out, fmt.Sprintf("%d updateUserEmojiStatus %s", e.recipient, status(u.GetEmojiStatus())))
			} else {
				out = append(out, fmt.Sprintf("%d %s", e.recipient, u.GetPredicateName()))
			}
		default:
			t.Errorf("event %s to %d", e.method, e.recipient)
		}
	}
	return out
}

func TestOwnColorWithSeq(t *testing.T) {
	c := userCore(t)
	me, _ := newUser(t, "Cora")
	ann, _ := newUser(t, "Ann") // has Cora as a contact
	if _, err := itRaw.Exec("INSERT INTO user_contacts (owner_user_id, contact_user_id, contact_first_name, is_deleted, date2) VALUES (?, ?, 'C.', 0, 1)", ann, me); err != nil {
		t.Fatal(err)
	}

	// A new account has no color: the columns default to none.
	var color, profile int32
	if err := itRaw.QueryRow("SELECT color, profile_color FROM users WHERE id = ?", me).Scan(&color, &profile); err != nil || color != user.NoColor || profile != user.NoColor {
		t.Fatalf("a new account: color %d, profile color %d, %v; want none", color, profile, err)
	}
	served, err := c.svcCtx.Dao.GetImmutableUser(c.ctx, me, false)
	if err != nil || served.Color() != nil || served.ProfileColor() != nil {
		t.Fatalf("a new account is served with colors %v %v, %v", served.Color(), served.ProfileColor(), err)
	}

	set := func(forProfile bool, color int32, emoji int64) {
		t.Helper()
		if _, err := c.UserSetColor(&user.TLUserSetColor{UserId: me, ForProfile: forProfile, Color: color, BackgroundEmojiId: emoji}); err != nil {
			t.Fatal(err)
		}
	}
	set(false, 0, 0)  // the first color is a color
	set(false, 0, 0)  // changes nothing
	set(false, 5, 77) // with a pattern
	set(true, user.NoColor, 88)
	set(true, 3, 0)
	set(true, user.NoColor, 0) // the profile is plain again
	set(true, user.NoColor, 0) // changes nothing
	set(false, user.NoColor, 0)

	served, err = c.svcCtx.Dao.GetImmutableUser(c.ctx, me, false)
	if err != nil || served.Color() != nil || served.ProfileColor() != nil {
		t.Fatalf("after the resets the account is served with colors %v %v, %v: a stale cache", served.Color(), served.ProfileColor(), err)
	}

	got := fmt.Sprint(lookEvents(t, me))
	want := fmt.Sprint([]string{
		"self updateUser 0/0 none", fmt.Sprintf("%d updateUser", ann),
		"self updateUser 5/77 none", fmt.Sprintf("%d updateUser", ann),
		"self updateUser 5/77 -/88", fmt.Sprintf("%d updateUser", ann),
		"self updateUser 5/77 3/0", fmt.Sprintf("%d updateUser", ann),
		"self updateUser 5/77 none", fmt.Sprintf("%d updateUser", ann),
		"self updateUser none none", fmt.Sprintf("%d updateUser", ann),
	})
	if got != want {
		t.Errorf("events\n%s\nwant\n%s", got, want)
	}
}

func TestOwnEmojiStatusWithSeq(t *testing.T) {
	c := userCore(t)
	me, _ := newUser(t, "Stella")
	bob, _ := newUser(t, "Bob") // has a private dialog with Stella
	if _, err := itRaw.Exec("INSERT INTO dialogs (user_id, peer_type, peer_id, peer_dialog_id, draft_message_data, date2) VALUES (?, ?, ?, ?, 'null', 1)",
		bob, mtproto.PEER_USER, me, mtproto.MakePeerDialogId(mtproto.PEER_USER, me)); err != nil {
		t.Fatal(err)
	}
	set := func(document int64, until int32) {
		t.Helper()
		if _, err := c.UserUpdateEmojiStatus(&user.TLUserUpdateEmojiStatus{UserId: me, EmojiStatusDocumentId: document, EmojiStatusUntil: until}); err != nil {
			t.Fatal(err)
		}
	}
	later := int32(time.Now().Unix()) + 3600
	set(901, 0)
	set(901, 0)                           // changes nothing
	set(902, later)                       // a second status, with an end
	set(901, 0)                           // the first again: it heads the recent ones
	set(0, 0)                             // none
	set(0, 0)                             // changes nothing
	set(903, int32(time.Now().Unix())-10) // an end that has passed is no status
	set(901, later)                       // the head of the recent ones stays the head

	served, err := c.svcCtx.Dao.GetImmutableUser(c.ctx, me, false)
	if err != nil || served.EmojiStatus().GetDocumentId() != 901 || served.EmojiStatus().GetUntil_FLAGINT32().GetValue() != later {
		t.Fatalf("served status %v, %v: a stale cache", served.EmojiStatus(), err)
	}

	rows, err := itRaw.Query("SELECT document_id FROM user_recent_emoji_statuses WHERE user_id = ? ORDER BY order2 DESC", me)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var recent []int64
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		recent = append(recent, id)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(recent) != "[901 902]" {
		t.Errorf("recent statuses %v, want [901 902]", recent)
	}

	got := fmt.Sprint(lookEvents(t, me))
	st := func(document int64, until int32) string { return fmt.Sprintf("%d/%d", document, until) }
	want := fmt.Sprint([]string{
		"self updateRecentEmojiStatuses", "self updateUserEmojiStatus " + st(901, 0) + " user " + st(901, 0), fmt.Sprintf("%d updateUserEmojiStatus %s", bob, st(901, 0)),
		"self updateRecentEmojiStatuses", "self updateUserEmojiStatus " + st(902, later) + " user " + st(902, later), fmt.Sprintf("%d updateUserEmojiStatus %s", bob, st(902, later)),
		"self updateRecentEmojiStatuses", "self updateUserEmojiStatus " + st(901, 0) + " user " + st(901, 0), fmt.Sprintf("%d updateUserEmojiStatus %s", bob, st(901, 0)),
		"self updateUserEmojiStatus empty user none", fmt.Sprintf("%d updateUserEmojiStatus empty", bob),
		"self updateUserEmojiStatus " + st(901, later) + " user " + st(901, later), fmt.Sprintf("%d updateUserEmojiStatus %s", bob, st(901, later)),
	})
	if got != want {
		t.Errorf("events\n%s\nwant\n%s", got, want)
	}
}

// migrate-20261114-user-look.sql turns the color 0 nobody chose into "none"
// once: run again it leaves a color 0 chosen since.
func TestUserColorMigration(t *testing.T) {
	path, err := testinfra.SchemaFile("migrate-20261114-user-look.sql")
	if err != nil {
		t.Fatal(err)
	}
	// The table as it was before the migration.
	if _, err = itRaw.Exec("ALTER TABLE users ALTER COLUMN color SET DEFAULT '0', ALTER COLUMN profile_color SET DEFAULT '0'"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := itRaw.Exec("ALTER TABLE users ALTER COLUMN color SET DEFAULT '-1', ALTER COLUMN profile_color SET DEFAULT '-1'"); err != nil {
			t.Error(err)
		}
	})
	plain, _ := newUser(t, "Plain")     // never chose anything
	pattern, _ := newUser(t, "Pattern") // 0 with a pattern was written on purpose
	chosen, _ := newUser(t, "Chosen")   // another color
	if _, err = itRaw.Exec("UPDATE users SET color_background_emoji_id = 5, profile_color_background_emoji_id = 6 WHERE id = ?", pattern); err != nil {
		t.Fatal(err)
	}
	if _, err = itRaw.Exec("UPDATE users SET color = 4, profile_color = 2 WHERE id = ?", chosen); err != nil {
		t.Fatal(err)
	}
	colors := func(id int64) string {
		t.Helper()
		var c, p int32
		if err := itRaw.QueryRow("SELECT color, profile_color FROM users WHERE id = ?", id).Scan(&c, &p); err != nil {
			t.Fatal(err)
		}
		return fmt.Sprintf("%d/%d", c, p)
	}
	if got := colors(plain); got != "0/0" {
		t.Fatalf("before the migration a new account has %s, want 0/0", got)
	}
	for run := range 2 {
		if err = itDB.ApplySQLFile(context.Background(), path); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
		if n := count(t, "SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users'"+
			" AND COLUMN_NAME IN ('color', 'profile_color') AND COLUMN_DEFAULT = '-1'"); n != 2 {
			t.Errorf("run %d: %d of the two columns default to -1", run, n)
		}
		want := map[int64]string{plain: "-1/-1", pattern: "0/0", chosen: "4/2"}
		if run == 1 {
			want[plain] = "0/-1" // chosen between the runs
		}
		for id, w := range want {
			if got := colors(id); got != w {
				t.Errorf("run %d: user %d has %s, want %s", run, id, got, w)
			}
		}
		if run == 0 {
			if _, err = itRaw.Exec("UPDATE users SET color = 0 WHERE id = ?", plain); err != nil {
				t.Fatal(err)
			}
		}
	}
	fresh, _ := newUser(t, "Fresh")
	if got := colors(fresh); got != "-1/-1" {
		t.Errorf("after the migration a new account has %s, want -1/-1", got)
	}
}
