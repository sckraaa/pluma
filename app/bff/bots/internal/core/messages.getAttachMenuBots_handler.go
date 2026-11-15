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

// MessagesGetAttachMenuBots
// messages.getAttachMenuBots#16fcc2cb hash:long = AttachMenuBots;
//
// The attachment menu bot mini apps the account installed
// (core.telegram.org/method/messages.getAttachMenuBots). iOS asks on start
// with hash 0 (TelegramCore/Sources/TelegramEngine/Messages/AttachMenuBots.swift:286-301),
// Web on start and when the menu opens
// (lib/appManagers/appAttachMenuBotsManager.ts:70), Android keeps the hash
// of the answer for the next request (messenger/MediaDataController.java:568-582).
// attachMenuBotsNotModified only for the client's copy of a list: a hash
// that is ours and not 0.
func (c *BotsCore) MessagesGetAttachMenuBots(in *mtproto.TLMessagesGetAttachMenuBots) (*mtproto.AttachMenuBots, error) {
	if c.MD.IsBot {
		err := mtproto.ErrBotMethodInvalid
		c.Logger.Errorf("messages.getAttachMenuBots - error: %v", err)
		return nil, err
	}
	bots, users, err := c.installedAttachMenuBots()
	if err != nil {
		c.Logger.Errorf("messages.getAttachMenuBots - error: %v", err)
		return nil, err
	}
	hash := attachMenuBotsHash(bots)
	if hash != 0 && in.GetHash() == hash {
		return mtproto.MakeTLAttachMenuBotsNotModified(nil).To_AttachMenuBots(), nil
	}
	return mtproto.MakeTLAttachMenuBots(&mtproto.AttachMenuBots{
		Hash:  hash,
		Bots:  bots,
		Users: users,
	}).To_AttachMenuBots(), nil
}

// installedAttachMenuBots is the attachment menu of the account and the
// users of its bots. No bot of ours is an attachment menu bot yet: the
// list is empty until the bots platform gives them one (task 106), which
// fills it here.
func (c *BotsCore) installedAttachMenuBots() ([]*mtproto.AttachMenuBot, []*mtproto.User, error) {
	return []*mtproto.AttachMenuBot{}, []*mtproto.User{}, nil
}

// attachMenuBotsHash is the hash of the list as core.telegram.org/api/offsets
// counts it, over the bot ids in order: 0 for an empty list.
func attachMenuBotsHash(bots []*mtproto.AttachMenuBot) int64 {
	var h uint64
	for _, b := range bots {
		h ^= h >> 21
		h ^= h << 35
		h ^= h >> 4
		h += uint64(b.GetBotId())
	}
	return int64(h)
}
