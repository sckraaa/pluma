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

	"github.com/teamgram/proto/mtproto"
)

// Requests that wait for a bot or for another host run aside the queue of
// the key; the rest keep their order in it.
func TestRunsAside(t *testing.T) {
	for _, tc := range []struct {
		req   mtproto.TLObject
		aside bool
	}{
		{&mtproto.TLMessagesGetBotCallbackAnswer{}, true},
		{&mtproto.TLMessagesGetInlineBotResults{}, true},
		{&mtproto.TLUploadGetWebFile{}, true},
		{&mtproto.TLMessagesGetBotWidgetQueryAnswer{}, true},
		{&mtproto.TLMessagesGetWidgetPublicData{}, false},
		{&mtproto.TLUploadGetFile{}, false},
		{&mtproto.TLMessagesSendMessage{}, false},
		{&mtproto.TLMessagesSendInlineBotResult{}, false},
	} {
		if got := runsAside(tc.req); got != tc.aside {
			t.Errorf("runsAside(%T) = %v, want %v", tc.req, got, tc.aside)
		}
	}
}
