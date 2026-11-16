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
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
)

// Only users ask a bot inline and send what it answered
// (core.telegram.org/method/messages.getInlineBotResults and
// messages.sendInlineBotResult): a bot gets BOT_METHOD_INVALID before
// anything is looked up.
func TestBotInlineMethodsInvalid(t *testing.T) {
	c := testCore(&dao.Dao{}, 20)
	c.MD.IsBot = true
	bot := mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: 30, AccessHash: 1}).To_InputUser()
	peer := mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: 10, AccessHash: 1}).To_InputPeer()
	if _, err := c.MessagesGetInlineBotResults(&mtproto.TLMessagesGetInlineBotResults{Bot: bot, Peer: peer}); !errors.Is(err, mtproto.ErrBotMethodInvalid) {
		t.Fatalf("messages.getInlineBotResults of a bot: %v", err)
	}
	if _, err := c.MessagesSendInlineBotResult(&mtproto.TLMessagesSendInlineBotResult{Peer: peer, QueryId: 1, Id: "1"}); !errors.Is(err, mtproto.ErrBotMethodInvalid) {
		t.Fatalf("messages.sendInlineBotResult of a bot: %v", err)
	}
}
