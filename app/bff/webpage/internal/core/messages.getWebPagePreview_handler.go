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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/webpage"
)

// A preview is asked for while the user types: it waits for the page up
// to this long.
const previewWait = 12 * time.Second

// MessagesGetWebPagePreview570D6F6F
// messages.getWebPagePreview#570d6f6f flags:# message:string entities:flags.3?Vector<MessageEntity> = messages.WebPagePreview;
//
// The card of the first link of a message being typed. As Telegram answers
// (2026-11-02): no link, or a link into a network, is messageMediaEmpty; a
// page without a card is webPageEmpty.
func (c *WebPageCore) MessagesGetWebPagePreview570D6F6F(in *mtproto.TLMessagesGetWebPagePreview570D6F6F) (*mtproto.Messages_WebPagePreview, error) {
	media := mtproto.MakeTLMessageMediaEmpty(nil).To_MessageMedia()
	if link := webpage.FirstLink(in.GetMessage(), in.GetEntities()); link != "" {
		ctx, cancel := context.WithTimeout(c.ctx, previewWait)
		defer cancel()
		page, err := c.svcCtx.WebPages.Preview(ctx, link)
		switch {
		case err == nil:
			media = mtproto.MakeTLMessageMediaWebPage(&mtproto.MessageMedia{Webpage: page}).To_MessageMedia()
		case errors.Is(err, webpage.ErrNoLink):
		default:
			// A page that took too long gets no card now; the fetch goes
			// on, and the next ask finds the card stored.
			c.Logger.Infof("messages.getWebPagePreview - no card yet: %v", err)
		}
	}
	return mtproto.MakeTLMessagesWebPagePreview(&mtproto.Messages_WebPagePreview{
		Media: media,
		Chats: []*mtproto.Chat{},
		Users: []*mtproto.User{},
	}).To_Messages_WebPagePreview(), nil
}

// MessagesGetWebPagePreview8B68B0CC
// messages.getWebPagePreview#8b68b0cc flags:# message:string entities:flags.3?Vector<MessageEntity> = MessageMedia;
//
// The method of layers before 172, which no client of this server's layer
// sends: not served.
func (c *WebPageCore) MessagesGetWebPagePreview8B68B0CC(in *mtproto.TLMessagesGetWebPagePreview8B68B0CC) (*mtproto.MessageMedia, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}
