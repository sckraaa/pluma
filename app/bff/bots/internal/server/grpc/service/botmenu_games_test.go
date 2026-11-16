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

package service

import (
	"context"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/svc"
)

// The service is the server of both RPCBotMenu and RPCGames, and the
// methods it does not serve (the games, until task 106 serves them) answer
// ERR_ENTERPRISE_IS_BLOCKED, a 400, not gRPC's Unimplemented, which the
// session proxy turns into a 500 that tgnet retries.
func TestBotMenuAndGamesNotServed(t *testing.T) {
	var (
		_ mtproto.RPCBotMenuServer = (*Service)(nil)
		_ mtproto.RPCGamesServer   = (*Service)(nil)
	)
	s := New(&svc.ServiceContext{})
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"setGameScore": func() error {
			_, err := s.MessagesSetGameScore(ctx, &mtproto.TLMessagesSetGameScore{})
			return err
		},
		"setInlineGameScore": func() error {
			_, err := s.MessagesSetInlineGameScore(ctx, &mtproto.TLMessagesSetInlineGameScore{})
			return err
		},
		"getGameHighScores": func() error {
			_, err := s.MessagesGetGameHighScores(ctx, &mtproto.TLMessagesGetGameHighScores{})
			return err
		},
		"getInlineGameHighScores": func() error {
			_, err := s.MessagesGetInlineGameHighScores(ctx, &mtproto.TLMessagesGetInlineGameHighScores{})
			return err
		},
	} {
		if err := call(); err != mtproto.ErrEnterpriseIsBlocked {
			t.Errorf("%s: %v, want ERR_ENTERPRISE_IS_BLOCKED", name, err)
		}
	}
}
