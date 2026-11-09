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

package effects

import (
	"slices"
	"testing"

	"github.com/teamgram/teamgram-server/pkg/moderation"
)

// A deletion leaves the reporter's own messages of the evidence.
func TestIdsToDelete(t *testing.T) {
	report := &moderation.Report{ReporterUserId: 1}
	got := idsToDelete(report, &moderation.Snapshot{Messages: []moderation.SnapshotMessage{
		{Id: 10, FromId: 2}, {Id: 11, FromId: 1}, {Id: 12, FromId: 2},
	}})
	if !slices.Equal(got, []int32{10, 12}) {
		t.Errorf("ids: %v", got)
	}
	if got = idsToDelete(report, &moderation.Snapshot{Messages: []moderation.SnapshotMessage{{Id: 11, FromId: 1}}}); len(got) != 0 {
		t.Errorf("only the reporter's messages: %v", got)
	}
}
