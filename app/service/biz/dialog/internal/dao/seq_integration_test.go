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
	"github.com/teamgram/teamgram-server/app/messenger/sync/sync"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// Updates with seq of the account (ADR 0020): the change, its row of the
// seq journal and its push are one box transaction.

// seqPush is a push of an update with seq, as the transaction wrote it.
type seqPush struct {
	seq    int32
	update *mtproto.Update
	users  int // users of the container
	chats  int
}

// seqPushes returns userId's pushes of updates with seq in mq_outbox, in
// order, and checks each one: sync.pushUpdates to the account itself
// (every device), one update in an updates container with the seq and date
// of its row of the seq journal.
func seqPushes(t *testing.T, userId int64) []seqPush {
	t.Helper()
	rows, err := itDao.BoxDB.Query("SELECT topic, recipient_id, method, payload FROM mq_outbox WHERE initiator_id = ? ORDER BY id", userId)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []seqPush
	for rows.Next() {
		var (
			topic, method string
			recipient     int64
			payload       []byte
		)
		if err = rows.Scan(&topic, &recipient, &method, &payload); err != nil {
			t.Fatal(err)
		}
		if method != "sync.TL_sync_pushUpdates" {
			continue
		}
		in := &sync.TLSyncPushUpdates{}
		if err = jsonx.Unmarshal(payload, in); err != nil {
			t.Fatal(err)
		}
		c := in.GetUpdates()
		if c.GetSeq() == 0 {
			continue
		}
		if topic != "Sync-T" || recipient != userId || in.GetUserId() != userId {
			t.Errorf("push of seq %d: topic %s, recipient %d, user %d", c.GetSeq(), topic, recipient, in.GetUserId())
		}
		if c.GetPredicateName() != mtproto.Predicate_updates || len(c.GetUpdates()) != 1 {
			t.Fatalf("push of seq %d: %s with %d updates", c.GetSeq(), c.GetPredicateName(), len(c.GetUpdates()))
		}
		var (
			date2 int64
			kind  string
		)
		if err = itDao.BoxDB.QueryRow("SELECT date2, update_type FROM user_seq_updates WHERE user_id = ? AND seq = ?", userId, c.GetSeq()).Scan(&date2, &kind); err != nil {
			t.Fatalf("seq %d has no row: %v", c.GetSeq(), err)
		}
		if int64(c.GetDate()) != date2 || kind != c.GetUpdates()[0].GetPredicateName() {
			t.Errorf("push of seq %d: date %d, %s; row: date %d, %s", c.GetSeq(), c.GetDate(), c.GetUpdates()[0].GetPredicateName(), date2, kind)
		}
		out = append(out, seqPush{seq: c.GetSeq(), update: c.GetUpdates()[0], users: len(c.GetUsers()), chats: len(c.GetChats())})
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// stateSeq returns userId's committed seq.
func stateSeq(t *testing.T, userId int64) int32 {
	t.Helper()
	st, err := userbox.Read(context.Background(), itDao.BoxDB, userId)
	if err != nil {
		t.Fatal(err)
	}
	return st.Seq
}
