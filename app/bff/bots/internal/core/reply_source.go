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
	"strings"

	"github.com/teamgram/proto/mtproto"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// replySource reads the sources of a reply of c's user (replyheader.Source).
type replySource struct {
	c *BotsCore
}

func (s replySource) Message(id int32) (*mtproto.MessageBox, error) {
	box, err := s.c.svcCtx.MessageGetUserMessage(s.c.ctx, &message.TLMessageGetUserMessage{UserId: s.c.MD.UserId, Id: id})
	if errors.Is(err, mtproto.ErrMessageIdInvalid) {
		return nil, nil
	}
	return box, err
}

func (s replySource) ChatNoforwards(chatId int64) (bool, error) {
	chat, err := s.c.svcCtx.Chat.ChatGetMutableChat(s.c.ctx, &chatpb.TLChatGetMutableChat{ChatId: chatId})
	if err != nil {
		return false, err
	}
	return chat.Noforwards(), nil
}

// AuthorOrigin names the author, or only their name when their forward
// privacy hides the account from the user, as the header of a forward does.
func (s replySource) AuthorOrigin(box *mtproto.MessageBox) (*mtproto.MessageFwdHeader, error) {
	ids := []int64{box.GetSenderUserId(), s.c.MD.UserId}
	users, err := s.c.svcCtx.UserGetMutableUsersV2(s.c.ctx, &userpb.TLUserGetMutableUsersV2{Id: ids, Privacy: true, HasTo: true, To: ids})
	if err != nil {
		return nil, err
	}
	header := &mtproto.MessageFwdHeader{Date: box.GetMessage().GetDate()}
	author, ok := users.GetImmutableUser(box.GetSenderUserId())
	me, ok2 := users.GetImmutableUser(s.c.MD.UserId)
	switch {
	case ok && ok2 && author.CheckPrivacyFor(mtproto.FORWARDS, me):
		header.FromId = mtproto.MakePeerUser(box.GetSenderUserId())
	case ok:
		header.FromName = wrapperspb.String(strings.TrimSpace(author.FirstName() + " " + author.LastName()))
	default:
		header.FromName = wrapperspb.String("")
	}
	return mtproto.MakeTLMessageFwdHeader(header).To_MessageFwdHeader(), nil
}
