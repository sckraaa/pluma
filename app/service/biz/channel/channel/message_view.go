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

// SetPostCounters sets the counters of a post. Views and forwards are one
// flag of message (flags.10), so the two are set together: a client reads
// both when it is set.
func SetPostCounters(m *mtproto.Message, views, forwards int32) {
	// Not MakeFlagsInt32, which leaves 0 unset.
	m.Views = wrapperspb.Int32(views)
	m.Forwards = wrapperspb.Int32(forwards)
}
