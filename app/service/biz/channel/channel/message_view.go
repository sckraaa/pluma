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

package channel

import (
	"github.com/teamgram/proto/mtproto"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// ViewMessage is a message of a channel, stored once for all (ADR 0034,
// p. 1), as viewer sees it: out when the viewer sent it. The stored message
// is not changed.
func ViewMessage(stored *mtproto.Message, viewer int64) *mtproto.Message {
	m := proto.Clone(stored).(*mtproto.Message)
	m.Out = m.GetFromId().GetUserId() == viewer && viewer != 0
	return m
}

// SentAs tells whether m was written by a member of a supergroup as a
// channel or as the group (send_as, task 079): it comes from a channel and
// is no post, nor the automatic copy of a post in its discussion group,
// whose fwd_from keeps the post (saved_from_peer).
func SentAs(m *mtproto.Message) bool {
	return m.GetFromId().GetPredicateName() == mtproto.Predicate_peerChannel && !m.GetPost() && m.GetFwdFrom().GetSavedFromPeer() == nil
}

// ViewSentMessage is ViewMessage for a viewer who knows whether it wrote
// stored (sentByViewer): a message it wrote as a channel is out for it as
// well, as the clients draw their own (Android MessageObject.isOutOwner,
// MessageObject.java:9901: out with a channel's from_id in a supergroup).
func ViewSentMessage(stored *mtproto.Message, viewer int64, sentByViewer bool) *mtproto.Message {
	m := ViewMessage(stored, viewer)
	if sentByViewer && viewer != 0 && SentAs(m) {
		m.Out = true
	}
	return m
}

// SetPostCounters sets the counters of a post. Views and forwards are one
// flag of message (flags.10), so the two are set together: a client reads
// both when it is set.
func SetPostCounters(m *mtproto.Message, views, forwards int32) {
	// Not MakeFlagsInt32, which leaves 0 unset.
	m.Views = wrapperspb.Int32(views)
	m.Forwards = wrapperspb.Int32(forwards)
}
