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

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/webpage"
)

// MessagesGetWebPage8D9692A3
// messages.getWebPage#8d9692a3 url:string hash:int = messages.WebPage;
//
// The card of a link, as a client asks for it to refresh one it shows
// (messages.webPage). A card that did not change since hash is
// webPageNotModified.
func (c *WebPageCore) MessagesGetWebPage8D9692A3(in *mtproto.TLMessagesGetWebPage8D9692A3) (*mtproto.Messages_WebPage, error) {
	link, hash := in.GetUrl(), in.GetHash()
	ctx, cancel := context.WithTimeout(c.ctx, previewWait)
	defer cancel()
	page, err := c.svcCtx.WebPages.Preview(ctx, link)
	if err != nil {
		// A link without a card, or a page not fetched in time: an empty
		// card, as for a page that has none.
		if !errors.Is(err, webpage.ErrNoLink) {
			c.Logger.Infof("messages.getWebPage - no card: %v", err)
		}
		page = mtproto.MakeTLWebPageEmpty(&mtproto.WebPage{Url_FLAGSTRING: mtproto.MakeFlagsString(link)}).To_WebPage()
	}
	if hash != 0 && page.GetHash() == hash {
		page = mtproto.MakeTLWebPageNotModified(nil).To_WebPage()
	}
	return mtproto.MakeTLMessagesWebPage(&mtproto.Messages_WebPage{
		Webpage: page,
		Chats:   []*mtproto.Chat{},
		Users:   []*mtproto.User{},
	}).To_Messages_WebPage(), nil
}

// MessagesGetWebPage32CA8F91
// messages.getWebPage#32ca8f91 url:string hash:int = WebPage;
//
// The method of older layers: not served.
func (c *WebPageCore) MessagesGetWebPage32CA8F91(in *mtproto.TLMessagesGetWebPage32CA8F91) (*mtproto.WebPage, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}
