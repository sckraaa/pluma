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

import "testing"

func TestHistoryMaxId(t *testing.T) {
	for _, c := range []struct{ maxId, top, want int32 }{
		{0, 10, 10},         // no limit given
		{5, 10, 5},          // a range
		{10, 10, 10},        // up to the last
		{1<<31 - 1, 10, 10}, // Android's Integer.MAX_VALUE
		{11, 10, 10},        // ahead of the channel
		{0, 0, 0},           // an empty channel
	} {
		if got := historyMaxId(c.maxId, c.top); got != c.want {
			t.Errorf("historyMaxId(%d, %d) = %d, want %d", c.maxId, c.top, got, c.want)
		}
	}
}
