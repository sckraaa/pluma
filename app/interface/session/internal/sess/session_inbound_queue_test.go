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

package sess

import "testing"

func msgIdAt(unix int64, frac uint32) int64 { return unix<<32 | int64(frac&^3) }

// The queue keeps every msg_id of the last msgIdMaxAge seconds, forgets the
// older ones, and refuses a forgotten msg_id from then on.
func TestInboundQueueShrinkKeepsTheWindow(t *testing.T) {
	const now = 1_800_000_000
	q := newSessionInboundQueue()
	old := msgIdAt(now-msgIdMaxAge-1, 4)
	q.AddMsgId(old)
	var recent []int64
	for i := int64(0); i < 1000; i++ { // more than the old limit of 400
		id := msgIdAt(now-msgIdMaxAge+1+i/10, uint32(i+1)*4)
		recent = append(recent, id)
		q.AddMsgId(id)
	}
	q.shrink(now)

	if q.Lookup(old) != nil {
		t.Fatal("a msg_id older than the window is kept")
	}
	for _, id := range recent {
		if q.Lookup(id) == nil {
			t.Fatalf("msg_id %d of the window forgotten", id)
		}
	}
	if q.GetMinMsgId() <= old {
		t.Fatalf("minMsgId %d does not refuse the forgotten msg_id %d", q.GetMinMsgId(), old)
	}
}

func TestInboundQueueShrinkCap(t *testing.T) {
	const now = 1_800_000_000
	q := newSessionInboundQueue()
	var ids []int64
	for i := 0; i < maxQueueSize+10; i++ {
		id := msgIdAt(now, uint32(i+1)*4)
		ids = append(ids, id)
		q.AddMsgId(id)
	}
	q.shrink(now)
	if q.msgIds.Len() != maxQueueSize {
		t.Fatalf("%d msg_ids kept, want %d", q.msgIds.Len(), maxQueueSize)
	}
	if q.GetMinMsgId() <= ids[9] || q.GetMinMsgId() > ids[10] {
		t.Fatalf("minMsgId %d, want just above the 10th msg_id %d", q.GetMinMsgId(), ids[9])
	}
}
