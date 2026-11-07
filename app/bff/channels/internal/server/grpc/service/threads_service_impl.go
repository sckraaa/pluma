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
	"github.com/teamgram/teamgram-server/app/bff/channels/internal/core"
)

// The threads of replies (RPCMessageThreads, task 048): the comments of
// channel posts and the threads of supergroups.

// MessagesGetReplies
// messages.getReplies#22ddd30c peer:InputPeer msg_id:int offset_id:int offset_date:int add_offset:int limit:int max_id:int min_id:int hash:long = messages.Messages;
func (s *Service) MessagesGetReplies(ctx context.Context, request *mtproto.TLMessagesGetReplies) (*mtproto.Messages_Messages, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getReplies - metadata: {%s}", c.MD)
	return c.MessagesGetReplies(request)
}

// MessagesGetDiscussionMessage
// messages.getDiscussionMessage#446972fd peer:InputPeer msg_id:int = messages.DiscussionMessage;
func (s *Service) MessagesGetDiscussionMessage(ctx context.Context, request *mtproto.TLMessagesGetDiscussionMessage) (*mtproto.Messages_DiscussionMessage, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.getDiscussionMessage - metadata: {%s}", c.MD)
	return c.MessagesGetDiscussionMessage(request)
}

// MessagesReadDiscussion
// messages.readDiscussion#f731a9f4 peer:InputPeer msg_id:int read_max_id:int = Bool;
func (s *Service) MessagesReadDiscussion(ctx context.Context, request *mtproto.TLMessagesReadDiscussion) (*mtproto.Bool, error) {
	c := core.New(ctx, s.svcCtx)
	c.Logger.Debugf("messages.readDiscussion - metadata: {%s}", c.MD)
	return c.MessagesReadDiscussion(request)
}

// ContactsBlockFromReplies is not served: contacts.blockFromReplies#29a8962c
// (the Replies bot's chat, which Pluma has not).
func (s *Service) ContactsBlockFromReplies(ctx context.Context, request *mtproto.TLContactsBlockFromReplies) (*mtproto.Updates, error) {
	return nil, mtproto.ErrEnterpriseIsBlocked
}
