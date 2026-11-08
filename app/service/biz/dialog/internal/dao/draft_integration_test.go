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
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// Drafts of the dialogs and of topics (task 063).
func TestDialogDrafts(t *testing.T) {
	ctx := context.Background()
	userId, peer := int64(71_000_001), int64(71_000_002)
	const data = `{"predicate_name":"draftMessage","message":"text"}`

	// No dialog yet: a hidden row holds the draft.
	if err := itDao.SaveDialogDraft(ctx, userId, mtproto.PEER_USER, peer, data, 5); err != nil {
		t.Fatal(err)
	}
	var (
		draftType int32
		deleted   bool
		stored    string
	)
	read := func() {
		t.Helper()
		if err := itDao.BoxDB.QueryRow("SELECT draft_type, deleted, draft_message_data FROM dialogs WHERE user_id = ? AND peer_type = ? AND peer_id = ?",
			userId, mtproto.PEER_USER, peer).Scan(&draftType, &deleted, &stored); err != nil {
			t.Fatal(err)
		}
	}
	read()
	// The column is JSON: MySQL keeps it in its own form.
	if draftType != 2 || !deleted || !strings.Contains(stored, `"text"`) {
		t.Fatalf("row: type %d, deleted %v, data %q", draftType, deleted, stored)
	}

	// Saving again over it changes the draft, not the row's state.
	if err := itDao.SaveDialogDraft(ctx, userId, mtproto.PEER_USER, peer, `{"predicate_name":"draftMessage","message":"new"}`, 6); err != nil {
		t.Fatal(err)
	}
	read()
	if !deleted || !strings.Contains(stored, `"new"`) {
		t.Fatalf("after a second save: deleted %v, data %q", deleted, stored)
	}

	// Only a draft there is gets cleared, and says so.
	const empty = `{"predicate_name":"draftMessageEmpty"}`
	cleared, err := itDao.ClearDialogDraft(ctx, userId, mtproto.PEER_USER, peer, empty)
	if err != nil || !cleared {
		t.Fatalf("clear: %v, %v", cleared, err)
	}
	if cleared, err = itDao.ClearDialogDraft(ctx, userId, mtproto.PEER_USER, peer, empty); err != nil || cleared {
		t.Fatalf("clear again: %v, %v", cleared, err)
	}
}

func TestThreadDrafts(t *testing.T) {
	ctx := context.Background()
	userId, channelId := int64(71_000_011), int64(71_000_012)
	const data = `{"predicate_name":"draftMessage","message":"topic"}`

	if err := itDao.SaveThreadDraft(ctx, userId, mtproto.PEER_CHANNEL, channelId, 7, data, 5); err != nil {
		t.Fatal(err)
	}
	if err := itDao.SaveThreadDraft(ctx, userId, mtproto.PEER_CHANNEL, channelId, 7, data, 6); err != nil {
		t.Fatal(err)
	}
	if err := itDao.SaveThreadDraft(ctx, userId, mtproto.PEER_CHANNEL, channelId, 9, data, 6); err != nil {
		t.Fatal(err)
	}
	rows, err := itDao.ThreadDrafts(ctx, userId)
	if err != nil || len(rows) != 2 {
		t.Fatalf("drafts: %v, %v", rows, err)
	}
	cleared, err := itDao.ClearThreadDraft(ctx, userId, mtproto.PEER_CHANNEL, channelId, 7)
	if err != nil || !cleared {
		t.Fatalf("clear: %v, %v", cleared, err)
	}
	if cleared, err = itDao.ClearThreadDraft(ctx, userId, mtproto.PEER_CHANNEL, channelId, 7); err != nil || cleared {
		t.Fatalf("clear again: %v, %v", cleared, err)
	}
	if err = itDao.ClearThreadDrafts(ctx, userId); err != nil {
		t.Fatal(err)
	}
	if rows, err = itDao.ThreadDrafts(ctx, userId); err != nil || len(rows) != 0 {
		t.Fatalf("after clear all: %v, %v", rows, err)
	}
}
