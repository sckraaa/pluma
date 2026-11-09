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

package notify

import (
	"testing"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestTopicOf(t *testing.T) {
	m := func(forum bool, top, msg int32) *mtproto.Message {
		h := &mtproto.MessageReplyHeader{ForumTopic: forum, ReplyToMsgId_INT32: msg}
		if top != 0 {
			h.ReplyToTopId = wrapperspb.Int32(top)
		}
		return &mtproto.Message{ReplyTo: h}
	}
	for _, tc := range []struct {
		m    *mtproto.Message
		want int32
	}{
		{&mtproto.Message{}, 0},
		{m(false, 7, 8), 0},
		{m(true, 7, 8), 7},
		{m(true, 0, 8), 8},
	} {
		if got := topicOf(tc.m); got != tc.want {
			t.Errorf("topicOf(%v) = %d, want %d", tc.m.GetReplyTo(), got, tc.want)
		}
	}
}
