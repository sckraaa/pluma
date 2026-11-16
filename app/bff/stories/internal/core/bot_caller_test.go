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
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

// stories.getChatsToSend and stories.getPeerMaxIDs are for users only
// (core.telegram.org): a bot gets BOT_METHOD_INVALID, not an answer.
func TestBotStoriesMethodsInvalid(t *testing.T) {
	bot := testCore()
	bot.MD.IsBot = true
	if _, err := bot.StoriesGetChatsToSend(&mtproto.TLStoriesGetChatsToSend{}); !errors.Is(err, mtproto.ErrBotMethodInvalid) {
		t.Errorf("stories.getChatsToSend of a bot: %v", err)
	}
	ids := []*mtproto.InputPeer{selfInputPeer()}
	if _, err := bot.StoriesGetPeerMaxIDs78499170(&mtproto.TLStoriesGetPeerMaxIDs78499170{Id: ids}); !errors.Is(err, mtproto.ErrBotMethodInvalid) {
		t.Errorf("stories.getPeerMaxIDs of a bot: %v", err)
	}
	if _, err := bot.StoriesGetPeerMaxIDs535983C3(&mtproto.TLStoriesGetPeerMaxIDs535983C3{Id: ids}); !errors.Is(err, mtproto.ErrBotMethodInvalid) {
		t.Errorf("stories.getPeerMaxIDs#535983c3 of a bot: %v", err)
	}
}
