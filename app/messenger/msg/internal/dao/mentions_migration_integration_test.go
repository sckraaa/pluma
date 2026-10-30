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

package dao_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
)

const mentionsMigration = "migrate-20261030-unread-mentions.sql"

// TestUnreadMentionsMigration: the migration sets the unread mention counter
// of a basic group dialog to the incoming mentions with the unread media
// flag that are not deleted, leaves other dialogs alone and can run again.
func TestUnreadMentionsMigration(t *testing.T) {
	ctx := context.Background()
	files, err := testinfra.SchemaFilesBefore(mentionsMigration)
	if err != nil {
		t.Fatal(err)
	}
	old, err := testinfra.StartMySQL(ctx, testinfra.WithSchemaFiles(files))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = old.Terminate(ctx) })
	db, err := sql.Open("mysql", old.DSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}

	const user, other, chat = int64(1001), int64(1002), int64(77)
	exec("INSERT INTO dialogs (user_id, peer_type, peer_id, peer_dialog_id, unread_mentions_count, draft_message_data, date2) VALUES (?, ?, ?, ?, 3, 'null', 0), (?, ?, ?, ?, 4, 'null', 0)",
		user, mtproto.PEER_CHAT, chat, mtproto.MakePeerDialogId(mtproto.PEER_CHAT, chat),
		user, mtproto.PEER_USER, other, mtproto.MakePeerDialogId(mtproto.PEER_USER, other))
	did := mtproto.MakeDialogId(user, mtproto.PEER_CHAT, chat)
	for i, m := range []struct {
		sender                         int64
		mentioned, mediaUnread, delete bool
	}{
		{sender: other, mentioned: true, mediaUnread: true}, // an unread mention
		{sender: other, mentioned: true},                    // the pin of the user's message
		{sender: other, mentioned: true, mediaUnread: true, delete: true},
		{sender: other, mediaUnread: true}, // an unplayed voice message
		{sender: user, mentioned: true, mediaUnread: true},
	} {
		exec("INSERT INTO messages (user_id, user_message_box_id, dialog_id1, dialog_id2, dialog_message_id, sender_user_id, peer_type, peer_id, mentioned, media_unread, deleted) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			user, i+1, did.A, did.B, int64(i+1), m.sender, mtproto.PEER_CHAT, chat, m.mentioned, m.mediaUnread, m.delete)
	}

	path, err := testinfra.SchemaFile(mentionsMigration)
	if err != nil {
		t.Fatal(err)
	}
	for run := range 2 {
		if err = old.ApplySQLFile(ctx, path); err != nil {
			t.Fatal(err)
		}
		for peerType, want := range map[int32]int32{mtproto.PEER_CHAT: 1, mtproto.PEER_USER: 4} {
			var n int32
			if err = db.QueryRow("SELECT unread_mentions_count FROM dialogs WHERE user_id = ? AND peer_type = ?", user, peerType).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != want {
				t.Errorf("run %d: unread mentions of the dialog of type %d: %d, want %d", run, peerType, n, want)
			}
		}
	}
}
