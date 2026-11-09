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
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	chatpb "github.com/teamgram/teamgram-server/app/service/biz/chat/chat"
	chat_client "github.com/teamgram/teamgram-server/app/service/biz/chat/client"
)

// addingChats adds the bot once: the next addition finds it in the chat.
type addingChats struct {
	chat_client.ChatClient
	chat    *mtproto.MutableChat
	present bool
}

func (a *addingChats) ChatAddChatUser(_ context.Context, _ *chatpb.TLChatAddChatUser) (*mtproto.MutableChat, error) {
	if a.present {
		return nil, mtproto.ErrUserAlreadyParticipant
	}
	a.present = true
	return a.chat, nil
}

func (a *addingChats) ChatGetMutableChat(_ context.Context, _ *chatpb.TLChatGetMutableChat) (*mtproto.MutableChat, error) {
	return a.chat, nil
}

// A repeat of messages.startBot after its service message was lost sends it,
// with the first attempt's random_id; a bot that has long been in the chat,
// or that another user added, gets no line.
func TestStartBotRepeatSendsTheAddition(t *testing.T) {
	const user, bot, other, group = int64(10), int64(20), int64(30), int64(40)
	chat := func(inviter, at int64) *mtproto.MutableChat {
		return mtproto.MakeTLMutableChat(&mtproto.MutableChat{
			Chat:             mtproto.MakeTLImmutableChat(&mtproto.ImmutableChat{Id: group}).To_ImmutableChat(),
			ChatParticipants: []*mtproto.ImmutableChatParticipant{mtproto.MakeTLImmutableChatParticipant(&mtproto.ImmutableChatParticipant{UserId: bot, InviterUserId: inviter, InvitedAt: at}).To_ImmutableChatParticipant()},
		}).To_MutableChat()
	}
	peer := &mtproto.PeerUtil{PeerType: mtproto.PEER_CHAT, PeerId: group}
	now := time.Now().Unix()

	sent := &sentMessages{}
	chats := &addingChats{chat: chat(user, now)}
	c := testCore(&dao.Dao{MsgClient: sent, Chat: chats}, user)
	if _, err := c.addBot(peer, bot); err != nil {
		t.Fatal(err)
	}
	first := sent.got.GetMessage()[0].GetRandomId()
	sent.got = nil
	if _, err := c.addBot(peer, bot); err != nil {
		t.Fatal(err)
	}
	if sent.got == nil || sent.got.GetMessage()[0].GetRandomId() != first {
		t.Fatalf("the repeat sent %v, first random_id %d", sent.got, first)
	}

	for name, in := range map[string]*mtproto.MutableChat{
		"added long ago":   chat(user, now-addRetryWindow-1),
		"added by another": chat(other, now),
	} {
		sent.got = nil
		chats.chat, chats.present = in, true
		if u, err := c.addBot(peer, bot); err != nil || u != nil || sent.got != nil {
			t.Errorf("%s: updates %v, error %v, sent %v", name, u, err, sent.got)
		}
	}
}
