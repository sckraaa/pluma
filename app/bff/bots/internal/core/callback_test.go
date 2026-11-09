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
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/svc"
	msg_client "github.com/teamgram/teamgram-server/app/messenger/msg/msg/client"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	message_client "github.com/teamgram/teamgram-server/app/service/biz/message/client"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"

	"github.com/zeromicro/go-zero/core/jsonx"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// peerMessages answers only the look-up a press needs; any other method of
// the client would panic on the nil interface.
type peerMessages struct {
	message_client.MessageClient
	got *message.TLMessageGetPeerUserMessage
}

func (p *peerMessages) MessageGetPeerUserMessage(_ context.Context, in *message.TLMessageGetPeerUserMessage) (*mtproto.MessageBox, error) {
	p.got = in
	return &mtproto.MessageBox{MessageId: 77}, nil
}

func testCore(d *dao.Dao, userId int64) *BotsCore {
	return &BotsCore{
		ctx:    context.Background(),
		svcCtx: &svc.ServiceContext{Dao: d},
		Logger: logx.WithContext(context.Background()),
		MD:     &metadata.RpcMetadata{UserId: userId},
	}
}

// The bot is told of a press by the id of the message in its own box.
func TestCallbackUpdateNamesTheBotsCopy(t *testing.T) {
	const user, bot = int64(10), int64(20)
	messages := &peerMessages{}
	c := testCore(&dao.Dao{MessageClient: messages}, user)
	p := &pressed{peer: mtproto.MakeUserPeerUtil(bot), botId: bot}

	u, chats, err := c.callbackUpdate(5, p, 33, []byte("go"))
	if err != nil {
		t.Fatal(err)
	}
	if got := messages.got; got == nil || got.UserId != user || got.PeerUserId != bot || got.MsgId != 33 {
		t.Fatalf("look-up: %+v", got)
	}
	if u.GetPredicateName() != mtproto.Predicate_updateBotCallbackQuery || u.GetMsgId_INT32() != 77 || u.GetPeer_PEER().GetUserId() != user {
		t.Fatalf("update: %v", u)
	}
	if len(chats) != 0 {
		t.Fatalf("chats of a private press: %v", chats)
	}
}

// Clients send inputPeerEmpty for a query from a secret chat; the update then
// has no peer_type.
func TestInlinePeerTypeOfNoChat(t *testing.T) {
	c := testCore(&dao.Dao{}, 10)
	peer := mtproto.FromInputPeer2(10, mtproto.MakeTLInputPeerEmpty(nil).To_InputPeer())
	pt, kind, err := c.inlinePeerType(20, peer)
	if err != nil || pt != nil || kind == "" {
		t.Fatalf("peer type %v, kind %q, error %v", pt, kind, err)
	}
}

// userMessages answers the look-up of a press with one copy.
type userMessages struct {
	message_client.MessageClient
	box *mtproto.MessageBox
}

func (u *userMessages) MessageGetUserMessage(_ context.Context, _ *message.TLMessageGetUserMessage) (*mtproto.MessageBox, error) {
	return u.box, nil
}

// copyIn is a copy in the box of owner of a message from sender, which the
// sender addressed to peer: an incoming copy has the owner as its peer.
func copyIn(sender int64, peerType int32, peerId int64) *mtproto.MessageBox {
	did := mtproto.MakeDialogId(sender, peerType, peerId)
	return &mtproto.MessageBox{PeerType: peerType, PeerId: peerId, DialogId1: did.A, DialogId2: did.B, Message: &mtproto.Message{}}
}

// A press names the chat as the user sees it: in a private chat the bot,
// though the user's copy has the user as its peer.
func TestPressedMessageOfTheChat(t *testing.T) {
	const user, bot, other, chat = int64(10), int64(20), int64(30), int64(40)
	for _, tc := range []struct {
		name string
		box  *mtproto.MessageBox
		peer *mtproto.PeerUtil
		ok   bool
	}{
		{"incoming copy, the bot's chat", copyIn(bot, mtproto.PEER_USER, user), mtproto.MakeUserPeerUtil(bot), true},
		{"incoming copy, another chat", copyIn(bot, mtproto.PEER_USER, user), mtproto.MakeUserPeerUtil(other), false},
		{"own copy of a message via a bot", copyIn(user, mtproto.PEER_USER, bot), mtproto.MakeUserPeerUtil(bot), true},
		{"a basic group", copyIn(bot, mtproto.PEER_CHAT, chat), &mtproto.PeerUtil{PeerType: mtproto.PEER_CHAT, PeerId: chat}, true},
		{"a private copy named as a group", copyIn(bot, mtproto.PEER_USER, user), &mtproto.PeerUtil{PeerType: mtproto.PEER_CHAT, PeerId: chat}, false},
	} {
		c := testCore(&dao.Dao{MessageClient: &userMessages{box: tc.box}}, user)
		p, err := c.pressedMessage(tc.peer, 33)
		switch {
		case tc.ok && (err != nil || p == nil):
			t.Errorf("%s: %v", tc.name, err)
		case !tc.ok && !errors.Is(err, mtproto.ErrMessageIdInvalid):
			t.Errorf("%s: got %v", tc.name, err)
		}
	}
	// Saved Messages: inputPeerSelf is the chat with oneself.
	c := testCore(&dao.Dao{MessageClient: &userMessages{box: copyIn(user, mtproto.PEER_USER, user)}}, user)
	if _, err := c.pressedMessage(c.chatPeer(mtproto.MakeTLInputPeerSelf(nil).To_InputPeer()), 33); err != nil {
		t.Errorf("Saved Messages: %v", err)
	}
}

// memoryKV holds what a test puts in Redis.
type memoryKV struct {
	kv.Store
	values map[string]string
}

func (m *memoryKV) GetCtx(_ context.Context, key string) (string, error) {
	return m.values[key], nil
}

// sentMessages records what is given to msg.
type sentMessages struct {
	msg_client.MsgClient
	got *msgpb.TLMsgSendMessageV2
}

func (s *sentMessages) MsgSendMessageV2(_ context.Context, in *msgpb.TLMsgSendMessageV2) (*mtproto.Updates, error) {
	s.got = in
	return mtproto.MakeEmptyUpdates(), nil
}

// inlineCore is a core with the answer of query 5, one article "a".
func inlineCore(t *testing.T, d *dao.Dao, user, bot int64) *BotsCore {
	t.Helper()
	data, err := jsonx.Marshal(&dao.InlineAnswer{Bot: bot, Results: []*mtproto.BotInlineResult{
		mtproto.MakeTLBotInlineResult(&mtproto.BotInlineResult{Id: "a", Type: "article", SendMessage: mtproto.MakeTLBotInlineMessageText(&mtproto.BotInlineMessage{Message: "hi"}).To_BotInlineMessage()}).To_BotInlineResult(),
	}})
	if err != nil {
		t.Fatal(err)
	}
	d.KV = &memoryKV{values: map[string]string{"bot_inline_answer:5": string(data)}}
	return testCore(d, user)
}

// A result chosen in Saved Messages (inputPeerSelf) goes to msg as the chat
// with oneself, in the sub-chat of the user.
func TestSendInlineResultToSavedMessages(t *testing.T) {
	const user, bot = int64(10), int64(20)
	sent := &sentMessages{}
	c := inlineCore(t, &dao.Dao{MsgClient: sent}, user, bot)
	if _, err := c.MessagesSendInlineBotResult(&mtproto.TLMessagesSendInlineBotResult{
		Peer: mtproto.MakeTLInputPeerSelf(nil).To_InputPeer(), RandomId: 1, QueryId: 5, Id: "a",
	}); err != nil {
		t.Fatal(err)
	}
	if sent.got.GetPeerType() != mtproto.PEER_USER || sent.got.GetPeerId() != user {
		t.Fatalf("peer %d/%d", sent.got.GetPeerType(), sent.got.GetPeerId())
	}
	m := sent.got.GetMessage()[0].GetMessage()
	if m.GetSavedPeerId().GetUserId() != user || m.GetPeerId().GetUserId() != user {
		t.Fatalf("saved_peer_id %v, peer_id %v", m.GetSavedPeerId(), m.GetPeerId())
	}
}

// A result sent as a reply keeps what the client said of the reply: the quote
// and, for another topic of the forum, the forum named as the chat.
func TestSendInlineResultAsReply(t *testing.T) {
	const user, bot, forum = int64(10), int64(20), int64(70)
	sent := &sentMessages{}
	c := inlineCore(t, &dao.Dao{MsgClient: sent}, user, bot)
	send := func(peer *mtproto.InputPeer, replyTo *mtproto.InputReplyTo) *mtproto.MessageReplyHeader {
		t.Helper()
		if _, err := c.MessagesSendInlineBotResult(&mtproto.TLMessagesSendInlineBotResult{Peer: peer, ReplyTo: replyTo, RandomId: 1, QueryId: 5, Id: "a"}); err != nil {
			t.Fatal(err)
		}
		return sent.got.GetMessage()[0].GetMessage().GetReplyTo()
	}

	h := send(mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: bot}).To_InputPeer(), mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{
		ReplyToMsgId: 3,
		QuoteText:    wrapperspb.String("part"),
		QuoteOffset:  wrapperspb.Int32(2),
	}).To_InputReplyTo())
	if h.GetFixedReplyToMsgId() != 3 || !h.GetQuote() || h.GetQuoteText().GetValue() != "part" || h.GetQuoteOffset().GetValue() != 2 {
		t.Fatalf("a quote: %v", h)
	}

	channel := mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: forum}).To_InputPeer()
	h = send(channel, mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{ReplyToMsgId: 3, TopMsgId: wrapperspb.Int32(9), ReplyToPeerId: channel}).To_InputReplyTo())
	if h.GetFixedReplyToMsgId() != 3 || h.GetReplyToPeerId().GetChannelId() != forum || h.GetReplyToTopId().GetValue() != 9 {
		t.Fatalf("another topic: %v", h)
	}
}
