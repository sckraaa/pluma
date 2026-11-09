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

// Package botprivacy decides which messages of a group a bot gets (task 070,
// ADR 0041). core.telegram.org/bots/features#privacy-mode: all bots run in
// privacy mode unless turned off or added as admins, and then see commands
// meant for them, inline messages sent via them, replies to messages meant
// for them and all service messages. Channels and private chats are not
// filtered.
package botprivacy

import (
	"strings"

	"github.com/teamgram/proto/mtproto"
)

// Bot is what the rule needs of the recipient.
type Bot struct {
	Id       int64
	Username string
	// ReadsAll: privacy mode off (user.bot_chat_history), or the bot
	// administers the group.
	ReadsAll bool
}

// Visible tells whether the bot gets m. repliedToBot: m replies to a message
// the bot sent.
//
// A general command, without @name, goes to every bot of the group. Telegram
// sends it only to the last bot that wrote there; that needs per-chat state
// and ours is a deliberate superset (the brief's decisions to confirm).
func Visible(b Bot, m *mtproto.Message, repliedToBot bool) bool {
	if m == nil {
		return false
	}
	if b.ReadsAll || repliedToBot {
		return true
	}
	if m.GetPredicateName() == mtproto.Predicate_messageService || m.GetAction() != nil {
		return true
	}
	if m.GetViaBotId() != nil && m.GetViaBotId().GetValue() == b.Id {
		return true
	}
	return commandForBot(m.GetMessage(), b.Username)
}

// commandForBot tells whether text opens with a command meant for the bot:
// /name or /name@username. Clients write a command only at the start of a
// message; another bot's command is not this bot's.
func commandForBot(text, username string) bool {
	if !strings.HasPrefix(text, "/") {
		return false
	}
	word, _, _ := strings.Cut(text, " ")
	word, _, _ = strings.Cut(word, "\n")
	name, target, addressed := strings.Cut(word[1:], "@")
	if name == "" {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return !addressed || strings.EqualFold(target, username)
}
