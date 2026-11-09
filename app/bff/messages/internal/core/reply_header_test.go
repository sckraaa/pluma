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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// A forward to a channel names only the topic.
func TestForwardTopic(t *testing.T) {
	channel := &mtproto.PeerUtil{PeerType: mtproto.PEER_CHANNEL, PeerId: 7}
	user := &mtproto.PeerUtil{PeerType: mtproto.PEER_USER, PeerId: 7}
	if got := forwardTopicHeader(channel, wrapperspb.Int32(9), nil); got.GetReplyToTopId().GetValue() != 9 || got.GetReplyToMsgId() != 0 {
		t.Errorf("a forward to a topic: %v", got)
	}
	if got := forwardTopicHeader(user, wrapperspb.Int32(9), nil); got != nil {
		t.Errorf("a forward to a user: %v", got)
	}
}
