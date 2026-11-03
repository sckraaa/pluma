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

package core

import (
	"context"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// The list of stories is empty and complete: has_more is what makes Android
// show "1 Story" in the archive row while it waits for more.
func TestGetAllStories(t *testing.T) {
	c := New(context.Background(), nil)
	for _, hidden := range []bool{false, true} {
		r, err := c.StoriesGetAllStories(&mtproto.TLStoriesGetAllStories{Hidden: hidden})
		if err != nil {
			t.Fatal(err)
		}
		if r.GetPredicateName() != mtproto.Predicate_stories_allStories || r.GetHasMore() || r.GetCount() != 0 || r.GetStealthMode() == nil {
			t.Errorf("hidden %v: %v", hidden, r)
		}
	}
}
