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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/business"
)

// AccountCreateBusinessChatLink
// account.createBusinessChatLink#8851e68e link:InputBusinessChatLink = BusinessChatLink;
//
// Creates a business chat link of the account under a new slug: up to
// business_chat_links_limit links (CHATLINKS_TOO_MUCH,
// core.telegram.org/method/account.createBusinessChatLink), each with no
// views yet. Nothing goes to the other devices: core.telegram.org names no
// update of the links, and the clients read the list when they open it
// (tdesktop api/api_chat_links.cpp, ChatLinks::preload).
func (c *BusinessCore) AccountCreateBusinessChatLink(in *mtproto.TLAccountCreateBusinessChatLink) (*mtproto.BusinessChatLink, error) {
	const method = "account.createBusinessChatLink"
	if err := c.notBot(method); err != nil {
		return nil, err
	}
	link, err := c.chatLinkOf(method, in.GetLink())
	if err != nil {
		return nil, err
	}
	link, err = c.svcCtx.Dao.Store.CreateChatLink(c.ctx, link, business.ChatLinksLimit, time.Now().UnixMilli())
	if err != nil {
		err = chatLinkError(err)
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	return chatLinkTL(link), nil
}
