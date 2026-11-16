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

// RPCPreparedInlineMessages (task 106, ADR 0068): the messages a bot
// prepares for its mini app to share.

// MessagesSavePreparedInlineMessage
// messages.savePreparedInlineMessage#f21f7f2f flags:# result:InputBotInlineResult user_id:InputUser peer_types:flags.0?Vector<InlineQueryPeerType> = messages.BotPreparedInlineMessage;
func (s *Service) MessagesSavePreparedInlineMessage(ctx context.Context, request *mtproto.TLMessagesSavePreparedInlineMessage) (*mtproto.Messages_BotPreparedInlineMessage, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.savePreparedInlineMessage - metadata: {%s}", c.MD)
	return c.MessagesSavePreparedInlineMessage(request)
}

// MessagesGetPreparedInlineMessage
// messages.getPreparedInlineMessage#857ebdb8 bot:InputUser id:string = messages.PreparedInlineMessage;
func (s *Service) MessagesGetPreparedInlineMessage(ctx context.Context, request *mtproto.TLMessagesGetPreparedInlineMessage) (*mtproto.Messages_PreparedInlineMessage, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getPreparedInlineMessage - metadata: {%s}", c.MD)
	return c.MessagesGetPreparedInlineMessage(request)
}
