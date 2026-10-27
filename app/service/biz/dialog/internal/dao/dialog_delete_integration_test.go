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

package dao

import (
	"context"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// dialog.deleteDialog (messages.deleteChatUser of the removed user): the row
// and the unpin of a pinned dialog with seq are one box transaction
// (ADR 0020).
func TestDeleteDialogUnpins(t *testing.T) {
	ctx := context.Background()
	u := newUser()
	pinAddDialog(t, u, mtproto.PEER_CHAT, 11, 0) // pinned in the main list
	pinAddDialog(t, u, mtproto.PEER_USER, 12, 1) // pinned in the archive
	pinAddDialog(t, u, mtproto.PEER_CHAT, 13, 0) // not pinned
	pinAddDialog(t, u, mtproto.PEER_CHAT, 14, 1) // in the archive with a pin of the main list kept
	for _, q := range []string{
		"UPDATE dialogs SET pinned = 5 << 32 WHERE user_id = ? AND peer_id IN (11, 14)",
		"UPDATE dialogs SET folder_pinned = 6 << 32 WHERE user_id = ? AND peer_id = 12",
	} {
		if _, err := itDao.BoxDB.Exec(q, u); err != nil {
			t.Fatal(err)
		}
	}
	del := func(peerType int32, peerId int64) {
		t.Helper()
		if err := itDao.DeleteDialog(ctx, u, peerType, peerId); err != nil {
			t.Fatalf("delete %d:%d: %v", peerType, peerId, err)
		}
		var n int
		if err := itDao.BoxDB.QueryRow("SELECT COUNT(*) FROM dialogs WHERE user_id = ? AND peer_type = ? AND peer_id = ?", u, peerType, peerId).Scan(&n); err != nil || n != 0 {
			t.Fatalf("dialog %d:%d: %d rows left, %v", peerType, peerId, n, err)
		}
	}

	// Not pinned, pinned only by the column of the other folder, not
	// there: nothing with seq.
	del(mtproto.PEER_CHAT, 13)
	del(mtproto.PEER_CHAT, 14)
	del(mtproto.PEER_CHAT, 99)
	pinCheck(t, u, 0)

	del(mtproto.PEER_CHAT, 11)
	del(mtproto.PEER_USER, 12)
	for _, p := range pinCheck(t, u, 0, "unpin c11/0", "unpin u12/1") {
		if p.users != 0 || p.chats != 0 {
			t.Errorf("push of seq %d with %d users and %d chats", p.seq, p.users, p.chats)
		}
	}
	if got := pinStored(t, u, 0) + pinStored(t, u, 1); got != "[][]" {
		t.Errorf("pins left %s", got)
	}
}
