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
	"reflect"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// The pinned list of a batch is answered before the getDialogs page asked
// right before it; every other request keeps its place.
func TestPinnedDialogsFirst(t *testing.T) {
	var (
		dialogs = func(id int64) *rpcApiMessage {
			return &rpcApiMessage{reqMsgId: id, reqMsg: &mtproto.TLMessagesGetDialogs{}}
		}
		pinned = func(id int64) *rpcApiMessage {
			return &rpcApiMessage{reqMsgId: id, reqMsg: &mtproto.TLMessagesGetPinnedDialogs{}}
		}
		toggle = func(id int64) *rpcApiMessage {
			return &rpcApiMessage{reqMsgId: id, reqMsg: &mtproto.TLMessagesToggleDialogPin{}}
		}
		getState = func(id int64) *rpcApiMessage {
			return &rpcApiMessage{reqMsgId: id, reqMsg: &mtproto.TLUpdatesGetState{}}
		}
	)
	for _, tc := range []struct {
		name  string
		batch []*rpcApiMessage
		want  []int64
	}{
		{"Desktop start", []*rpcApiMessage{dialogs(1), pinned(2)}, []int64{2, 1}},
		{"after other requests", []*rpcApiMessage{getState(1), dialogs(2), pinned(3), getState(4)}, []int64{1, 3, 2, 4}},
		{"two folders", []*rpcApiMessage{dialogs(1), pinned(2), dialogs(3), pinned(4)}, []int64{2, 1, 4, 3}},
		{"several pages", []*rpcApiMessage{dialogs(1), dialogs(2), pinned(3)}, []int64{3, 1, 2}},
		{"already first", []*rpcApiMessage{pinned(1), dialogs(2)}, []int64{1, 2}},
		{"a change between", []*rpcApiMessage{dialogs(1), toggle(2), pinned(3)}, []int64{1, 2, 3}},
		{"only pinned", []*rpcApiMessage{pinned(1)}, []int64{1}},
		{"empty", nil, nil},
	} {
		var got []int64
		for _, r := range pinnedDialogsFirst(tc.batch) {
			got = append(got, r.reqMsgId)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: order %v, want %v", tc.name, got, tc.want)
		}
	}
}
