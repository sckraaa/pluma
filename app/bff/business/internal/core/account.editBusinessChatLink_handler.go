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

// AccountEditBusinessChatLink
// account.editBusinessChatLink#8c3410af slug:string link:InputBusinessChatLink = BusinessChatLink;
//
// Replaces the text, the entities and the title of a link of the account;
// the link keeps its slug and its views (tdesktop takes an answer with
// another link for an error, api/api_chat_links.cpp:87-90). The clients
// name the link by the whole link (business.SlugOf). A slug of another
// account, or of no link, is CHATLINK_SLUG_EXPIRED.
func (c *BusinessCore) AccountEditBusinessChatLink(in *mtproto.TLAccountEditBusinessChatLink) (*mtproto.BusinessChatLink, error) {
	const method = "account.editBusinessChatLink"
	if err := c.notBot(method); err != nil {
		return nil, err
	}
	slug, err := c.slugArg(method, in.GetSlug())
	if err != nil {
		return nil, err
	}
	link, err := c.chatLinkOf(method, in.GetLink())
	if err != nil {
		return nil, err
	}
	link, err = c.svcCtx.Dao.Store.EditChatLink(c.ctx, slug, link)
	if err != nil {
		err = chatLinkError(err)
		c.Logger.Errorf("%s - error: %v", method, err)
		return nil, err
	}
	return chatLinkTL(link), nil
}
