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
	"errors"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/zeromicro/go-zero/core/logx"
)

func noiseCore(bot bool) *MessagesCore {
	ctx := context.Background()
	return &MessagesCore{ctx: ctx, Logger: logx.WithContext(ctx), MD: &metadata.RpcMetadata{UserId: 7, IsBot: bot}}
}

// summarizeText is refused, never answered with a made-up empty text, and
// not with TRANSLATIONS_DISABLED_ALT, on which Android summarizes on the
// device with a third-party translator.
func TestSummarizeTextRefused(t *testing.T) {
	peer := mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: 8}).To_InputPeer()
	text, err := noiseCore(false).MessagesSummarizeText(&mtproto.TLMessagesSummarizeText{Peer: peer, Id: 5})
	if text != nil || !errors.Is(err, mtproto.ErrEnterpriseIsBlocked) {
		t.Fatalf("messages.summarizeText: %v, %v; want ERR_ENTERPRISE_IS_BLOCKED", text, err)
	}
}

// receivedMessages cancels no push, there being none held back: the list is
// empty and never nil.
func TestReceivedMessages(t *testing.T) {
	for _, maxId := range []int32{0, 1, 1 << 30} {
		got, err := noiseCore(false).MessagesReceivedMessages(&mtproto.TLMessagesReceivedMessages{MaxId: maxId})
		if err != nil || got == nil || got.Datas == nil || len(got.Datas) != 0 {
			t.Errorf("max_id %d: %v, %v", maxId, got, err)
		}
	}
	if _, err := noiseCore(false).MessagesReceivedMessages(&mtproto.TLMessagesReceivedMessages{MaxId: -1}); !errors.Is(err, mtproto.ErrMsgIdInvalid) {
		t.Errorf("max_id -1: %v, want MSG_ID_INVALID", err)
	}
	if _, err := noiseCore(true).MessagesReceivedMessages(&mtproto.TLMessagesReceivedMessages{MaxId: 1}); !errors.Is(err, mtproto.ErrBotMethodInvalid) {
		t.Errorf("a bot: %v, want BOT_METHOD_INVALID", err)
	}
}
