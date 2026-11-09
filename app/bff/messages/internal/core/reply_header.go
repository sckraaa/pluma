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
	"errors"

	"github.com/teamgram/proto/mtproto"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
	"github.com/teamgram/teamgram-server/pkg/replyheader"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// coreReplySource reads the sources of a reply of c's user.
type coreReplySource struct {
	c *MessagesCore
}

func (s coreReplySource) Message(id int32) (*mtproto.MessageBox, error) {
	box, err := s.c.svcCtx.Dao.MessageClient.MessageGetUserMessage(s.c.ctx, &message.TLMessageGetUserMessage{
		UserId: s.c.MD.UserId,
		Id:     id,
	})
	if errors.Is(err, mtproto.ErrMessageIdInvalid) {
		return nil, nil
	}
	return box, err
}

func (s coreReplySource) ChatNoforwards(chatId int64) (bool, error) {
	chat, err := s.c.svcCtx.Dao.ChatClient.Client().ChatGetMutableChat(s.c.ctx, &chatpb.TLChatGetMutableChat{
		ChatId: chatId,
	})
	if err != nil {
		return false, err
	}
	return chat.Noforwards(), nil
}

func (s coreReplySource) AuthorOrigin(box *mtproto.MessageBox) (*mtproto.MessageFwdHeader, error) {
	return s.c.forwardOrigin(box)
}

// makeReplyHeader is the reply header of a message c's user sends to peer
// (a user or a basic group, the user itself as PEER_USER), from the
// legacy reply_to_msg_id or from reply_to; nil when the message replies to
// no message (a reply to a story is built by the caller).
func (c *MessagesCore) makeReplyHeader(peer *mtproto.PeerUtil, replyToMsgId *wrapperspb.Int32Value, replyTo *mtproto.InputReplyTo) (*mtproto.MessageReplyHeader, error) {
	h, err := replyheader.Make(coreReplySource{c: c}, c.MD.UserId, peer, replyToMsgId, replyTo)
	if err != nil {
		return nil, err
	}
	return replyheader.WithTopic(h, peer, replyTo), nil
}

// forwardTopicHeader names the topic of a forward to a channel: top_msg_id
// (Android SendMessagesHelper.java:2508-2513), or that of reply_to; biz
// resolves it (task 060). nil for no topic.
func forwardTopicHeader(to *mtproto.PeerUtil, topMsgId *wrapperspb.Int32Value, replyTo *mtproto.InputReplyTo) *mtproto.MessageReplyHeader {
	if to.PeerType != mtproto.PEER_CHANNEL {
		return nil
	}
	if topMsgId == nil {
		topMsgId = replyTo.GetTopMsgId()
	}
	if topMsgId == nil {
		return nil
	}
	return mtproto.MakeTLMessageReplyHeader(&mtproto.MessageReplyHeader{ReplyToTopId: topMsgId}).To_MessageReplyHeader()
}
