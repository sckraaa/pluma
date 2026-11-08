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

package dao

import (
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestScheduledHash(t *testing.T) {
	// The value tdesktop counts for ids 7 and 5 (countListHash, newest
	// first), by HashUpdate of api_hash.h.
	var h uint64
	update := func(v uint64) {
		h ^= h >> 21
		h ^= h << 35
		h ^= h >> 4
		h += v
	}
	for _, v := range []uint64{7, 0, 200, 5, 0, 100} {
		update(v)
	}
	rows := []*ScheduledRow{{Id: 5, ScheduleDate: 100}, {Id: 7, ScheduleDate: 200}}
	SortScheduled(rows)
	if rows[0].Id != 7 {
		t.Fatalf("order: %d first", rows[0].Id)
	}
	if got := ScheduledHash(rows); got != int64(h) {
		t.Errorf("hash %d, want %d", got, int64(h))
	}
	if ScheduledHash(nil) != 0 {
		t.Error("an empty queue hashes to 0")
	}
}

func TestScheduledQueueOf(t *testing.T) {
	if q := ScheduledQueueOf(1, mtproto.PEER_USER, 2, false); q.UserId != 1 || q.Broadcast() {
		t.Errorf("private: %+v", q)
	}
	if q := ScheduledQueueOf(1, mtproto.PEER_CHANNEL, 2, false); q.UserId != 1 || q.Broadcast() {
		t.Errorf("supergroup: %+v", q)
	}
	if q := ScheduledQueueOf(1, mtproto.PEER_CHANNEL, 2, true); q.UserId != 0 || !q.Broadcast() {
		t.Errorf("broadcast: %+v", q)
	}
}

func TestApplyScheduledEdit(t *testing.T) {
	stored := &mtproto.Message{Message: "old", Date: 10}
	got := ApplyScheduledEdit(stored, &mtproto.Message{Message: "new", EditDate: wrapperspb.Int32(99)}, 20)
	if got.Message != "new" || got.EditDate != nil || !got.EditHide || got.Date != 20 {
		t.Errorf("edited %v", got)
	}
}

// A lease must outlive a delivery, or a slow delivery would be taken by
// another worker (the delete would then stop one of them).
func TestScheduledLease(t *testing.T) {
	if ScheduledLease/2 < MaxBoxOpTime {
		t.Errorf("lease %v against a box operation of %v", ScheduledLease, MaxBoxOpTime)
	}
	if ScheduledLease < time.Minute {
		t.Errorf("lease %v", ScheduledLease)
	}
}
