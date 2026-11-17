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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/grpc/status"
)

func menuCore(bot bool) *BotsCore {
	ctx := context.Background()
	return &BotsCore{ctx: ctx, Logger: logx.WithContext(ctx), MD: &metadata.RpcMetadata{UserId: 5, IsBot: bot}}
}

func wantStatus(t *testing.T, what string, err error, want string) {
	t.Helper()
	if s, ok := status.FromError(err); !ok || s.Message() != want {
		t.Fatalf("%s: %v, want %s", what, err, want)
	}
}

// A bot has no attachment menu of its own: the menu is a user's.
func TestGetAttachMenuBotsRefusesBots(t *testing.T) {
	_, err := menuCore(true).MessagesGetAttachMenuBots(&mtproto.TLMessagesGetAttachMenuBots{})
	wantStatus(t, "bot", err, "BOT_METHOD_INVALID")
}

func TestGetEmojiGameInfo(t *testing.T) {
	r, err := menuCore(false).MessagesGetEmojiGameInfo(&mtproto.TLMessagesGetEmojiGameInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if r.GetPredicateName() != mtproto.Predicate_messages_emojiGameUnavailable {
		t.Errorf("%s, want messages.emojiGameUnavailable", r.GetPredicateName())
	}
	_, err = menuCore(true).MessagesGetEmojiGameInfo(&mtproto.TLMessagesGetEmojiGameInfo{})
	wantStatus(t, "bot", err, "BOT_METHOD_INVALID")
}
