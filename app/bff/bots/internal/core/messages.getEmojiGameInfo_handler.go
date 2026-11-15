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
	"github.com/teamgram/proto/mtproto"
)

// MessagesGetEmojiGameInfo
// messages.getEmojiGameInfo#fb7e8ca7 = messages.EmojiGameInfo;
//
// The dice game with a stake in TON, which Pluma does not have: always
// messages.emojiGameUnavailable, the answer clients keep as "no game".
// iOS asks on start and every hour
// (TelegramCore/Sources/State/ManagedEmojiGameUpdates.swift:79-105);
// Android caches the answer, and without one asks again on every dice
// (messenger/MessagesController.java:25338-25357); Desktop takes it as no
// game options (chat_helpers/stickers_dice_pack.cpp:156-163).
func (c *BotsCore) MessagesGetEmojiGameInfo(in *mtproto.TLMessagesGetEmojiGameInfo) (*mtproto.Messages_EmojiGameInfo, error) {
	if c.MD.IsBot {
		err := mtproto.ErrBotMethodInvalid
		c.Logger.Errorf("messages.getEmojiGameInfo - error: %v", err)
		return nil, err
	}
	return mtproto.MakeTLMessagesEmojiGameUnavailable(nil).To_Messages_EmojiGameInfo(), nil
}
