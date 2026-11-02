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
	"github.com/teamgram/teamgram-server/app/bff/webpage/internal/core"
)

// MessagesGetWebPagePreview570D6F6F
// messages.getWebPagePreview#570d6f6f flags:# message:string entities:flags.3?Vector<MessageEntity> = messages.WebPagePreview;
func (s *Service) MessagesGetWebPagePreview570D6F6F(ctx context.Context, request *mtproto.TLMessagesGetWebPagePreview570D6F6F) (*mtproto.Messages_WebPagePreview, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getWebPagePreview - metadata: {%s}", c.MD)
	return c.MessagesGetWebPagePreview570D6F6F(request)
}

// MessagesGetWebPage8D9692A3
// messages.getWebPage#8d9692a3 url:string hash:int = messages.WebPage;
func (s *Service) MessagesGetWebPage8D9692A3(ctx context.Context, request *mtproto.TLMessagesGetWebPage8D9692A3) (*mtproto.Messages_WebPage, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getWebPage - metadata: {%s}", c.MD)
	return c.MessagesGetWebPage8D9692A3(request)
}

// MessagesGetWebPagePreview8B68B0CC
// messages.getWebPagePreview#8b68b0cc flags:# message:string entities:flags.3?Vector<MessageEntity> = MessageMedia;
func (s *Service) MessagesGetWebPagePreview8B68B0CC(ctx context.Context, request *mtproto.TLMessagesGetWebPagePreview8B68B0CC) (*mtproto.MessageMedia, error) {
	c := core.New(ctx, s.svcCtx)
	return c.MessagesGetWebPagePreview8B68B0CC(request)
}

// MessagesGetWebPage32CA8F91
// messages.getWebPage#32ca8f91 url:string hash:int = WebPage;
func (s *Service) MessagesGetWebPage32CA8F91(ctx context.Context, request *mtproto.TLMessagesGetWebPage32CA8F91) (*mtproto.WebPage, error) {
	c := core.New(ctx, s.svcCtx)
	return c.MessagesGetWebPage32CA8F91(request)
}
