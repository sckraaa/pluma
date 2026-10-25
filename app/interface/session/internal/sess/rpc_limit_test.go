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

import (
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/app/interface/session/internal/dao"
)

// A key gets its burst at once, then requests at the rate; over it the
// client is told how long to wait, and the refused request is not counted.
func TestRpcFloodWait(t *testing.T) {
	manager := NewMainAuthWrapperManager(&dao.Dao{AuthsessionClient: &fakeAuthsessionClient{}})
	manager.SetRpcLimit(1, 3)
	m := newMainAuthWrapper(1, 0, 0, nil, 0, manager)
	now := time.Unix(1_800_000_000, 0)

	for i := 0; i < 3; i++ {
		if wait := m.rpcFloodWait(now); wait != 0 {
			t.Fatalf("request %d of the burst: wait %d", i+1, wait)
		}
	}
	if wait := m.rpcFloodWait(now); wait != 1 {
		t.Fatalf("over the burst: wait %d, want 1", wait)
	}
	// The refused request did not take a token: one second later one passes.
	if wait := m.rpcFloodWait(now.Add(time.Second)); wait != 0 {
		t.Fatalf("a second later: wait %d", wait)
	}
	if wait := m.rpcFloodWait(now.Add(time.Second)); wait != 1 {
		t.Fatalf("again over: wait %d, want 1", wait)
	}
}
