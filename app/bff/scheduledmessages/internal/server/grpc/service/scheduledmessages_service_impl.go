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
	"github.com/teamgram/teamgram-server/app/bff/scheduledmessages/internal/core"
)

// MessagesGetScheduledHistory
// messages.getScheduledHistory#f516760b peer:InputPeer hash:long = messages.Messages;
func (s *Service) MessagesGetScheduledHistory(ctx context.Context, request *mtproto.TLMessagesGetScheduledHistory) (*mtproto.Messages_Messages, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getScheduledHistory - metadata: {%s}", c.MD)

	r, err := c.MessagesGetScheduledHistory(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MessagesGetScheduledMessages
// messages.getScheduledMessages#bdbb0464 peer:InputPeer id:Vector<int> = messages.Messages;
func (s *Service) MessagesGetScheduledMessages(ctx context.Context, request *mtproto.TLMessagesGetScheduledMessages) (*mtproto.Messages_Messages, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getScheduledMessages - metadata: {%s}", c.MD)

	r, err := c.MessagesGetScheduledMessages(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MessagesSendScheduledMessages
// messages.sendScheduledMessages#bd38850a peer:InputPeer id:Vector<int> = Updates;
func (s *Service) MessagesSendScheduledMessages(ctx context.Context, request *mtproto.TLMessagesSendScheduledMessages) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.sendScheduledMessages - metadata: {%s}", c.MD)

	r, err := c.MessagesSendScheduledMessages(request)
	if err != nil {
		return nil, err
	}

	return r, err
}

// MessagesDeleteScheduledMessages
// messages.deleteScheduledMessages#59ae2b16 peer:InputPeer id:Vector<int> = Updates;
func (s *Service) MessagesDeleteScheduledMessages(ctx context.Context, request *mtproto.TLMessagesDeleteScheduledMessages) (*mtproto.Updates, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.deleteScheduledMessages - metadata: {%s}", c.MD)

	r, err := c.MessagesDeleteScheduledMessages(request)
	if err != nil {
		return nil, err
	}

	return r, err
}
