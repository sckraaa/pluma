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

//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// stories.getAllStories answers an empty, complete list through the session
// routing: Android showed "1 Story" in the archive row while it had no
// answer. Other stories methods answer ERR_ENTERPRISE_IS_BLOCKED.
func TestStoriesList(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	alice := login(ctx, t, testPhones(t, 1)[0], "Alice")
	o, err := alice.c.Invoke(ctx, &mtproto.TLStoriesGetAllStories{
		Constructor: alice.c.Constructor(mtproto.Predicate_stories_getAllStories),
		Hidden:      true,
	})
	if err != nil {
		t.Fatalf("stories.getAllStories: %v", err)
	}
	r, ok := o.(interface {
		To_Stories_AllStories() *mtproto.Stories_AllStories
	})
	if !ok {
		t.Fatalf("stories.getAllStories returned %T", o)
	}
	if all := r.To_Stories_AllStories(); all.GetHasMore() || all.GetCount() != 0 {
		t.Errorf("stories.getAllStories: has_more %v, count %d", all.GetHasMore(), all.GetCount())
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLStoriesGetStoriesArchive{
		Constructor: alice.c.Constructor(mtproto.Predicate_stories_getStoriesArchive),
		Peer:        &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerSelf)},
		Limit:       10,
	}); !mtclient.IsRPCError(err, "ERR_ENTERPRISE_IS_BLOCKED") {
		t.Errorf("stories.getStoriesArchive: %v, want ERR_ENTERPRISE_IS_BLOCKED", err)
	}
}
