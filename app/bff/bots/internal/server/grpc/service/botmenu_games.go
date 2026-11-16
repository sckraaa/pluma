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

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/core"
)

// RPCBotMenu and RPCGames (task 098): the attachment menu of the bots
// platform (task 106) and the dice game, which is never available. Their
// other methods keep the 400 the session proxy answered without a route:
// tgnet retries a 500.

// MessagesGetAttachMenuBots
// messages.getAttachMenuBots#16fcc2cb hash:long = AttachMenuBots;
func (s *Service) MessagesGetAttachMenuBots(ctx context.Context, request *mtproto.TLMessagesGetAttachMenuBots) (*mtproto.AttachMenuBots, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getAttachMenuBots - metadata: {%s}", c.MD)
	return c.MessagesGetAttachMenuBots(request)
}

// MessagesGetEmojiGameInfo
// messages.getEmojiGameInfo#fb7e8ca7 = messages.EmojiGameInfo;
func (s *Service) MessagesGetEmojiGameInfo(ctx context.Context, request *mtproto.TLMessagesGetEmojiGameInfo) (*mtproto.Messages_EmojiGameInfo, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getEmojiGameInfo - metadata: {%s}", c.MD)
	return c.MessagesGetEmojiGameInfo(request)
}

// MessagesGetAttachMenuBot
// messages.getAttachMenuBot#77216192 bot:InputUser = AttachMenuBotsBot;
func (s *Service) MessagesGetAttachMenuBot(ctx context.Context, request *mtproto.TLMessagesGetAttachMenuBot) (*mtproto.AttachMenuBotsBot, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getAttachMenuBot - metadata: {%s}", c.MD)
	return c.MessagesGetAttachMenuBot(request)
}

// MessagesToggleBotInAttachMenu
// messages.toggleBotInAttachMenu#69f59d69 flags:# write_allowed:flags.0?true bot:InputUser enabled:Bool = Bool;
func (s *Service) MessagesToggleBotInAttachMenu(ctx context.Context, request *mtproto.TLMessagesToggleBotInAttachMenu) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.toggleBotInAttachMenu - metadata: {%s}", c.MD)
	return c.MessagesToggleBotInAttachMenu(request)
}

// MessagesSetGameScore is not served: games of bots are task 106.
func (s *Service) MessagesSetGameScore(ctx context.Context, request *mtproto.TLMessagesSetGameScore) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesSetInlineGameScore is not served, see MessagesSetGameScore.
func (s *Service) MessagesSetInlineGameScore(ctx context.Context, request *mtproto.TLMessagesSetInlineGameScore) (*mtproto.Bool, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesGetGameHighScores is not served, see MessagesSetGameScore.
func (s *Service) MessagesGetGameHighScores(ctx context.Context, request *mtproto.TLMessagesGetGameHighScores) (*mtproto.Messages_HighScores, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}

// MessagesGetInlineGameHighScores is not served, see MessagesSetGameScore.
func (s *Service) MessagesGetInlineGameHighScores(ctx context.Context, request *mtproto.TLMessagesGetInlineGameHighScores) (*mtproto.Messages_HighScores, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}
