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

// AccountGetBusinessChatLinks
// account.getBusinessChatLinks#6f70dde1 = account.BusinessChatLinks;
//
// The links of the account, the oldest first (the order the clients keep:
// tdesktop appends a new link to the end, api/api_chat_links.cpp:66-67),
// with the users their mentions name; the clients take them before they
// read the entities (tdesktop api/api_chat_links.cpp:138-140). A link
// names no chat, so chats is empty.
func (c *BusinessCore) AccountGetBusinessChatLinks(in *mtproto.TLAccountGetBusinessChatLinks) (*mtproto.Account_BusinessChatLinks, error) {
	const method = "account.getBusinessChatLinks"
	if err := c.notBot(method); err != nil {
		return nil, err
	}
	links, err := c.svcCtx.Dao.Store.ChatLinks(c.ctx, c.MD.UserId)
	if err != nil {
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	out := make([]*mtproto.BusinessChatLink, 0, len(links))
	var mentioned []*mtproto.MessageEntity
	for _, l := range links {
		out = append(out, chatLinkTL(l))
		mentioned = append(mentioned, l.Entities...)
	}
	users, err := c.usersFor(method, mentionedUsers(mentioned))
	if err != nil {
		return nil, err
	}
	return mtproto.MakeTLAccountBusinessChatLinks(&mtproto.Account_BusinessChatLinks{
		Links: out,
		Chats: []*mtproto.Chat{},
		Users: users,
	}).To_Account_BusinessChatLinks(), nil
}
