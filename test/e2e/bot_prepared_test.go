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

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// TestPreparedAndRequestedPeer (task 106, part F): a bot prepares a message
// its mini app shares, the user fetches it and sends it to a chat of the
// kinds the bot named, not to another; the bot prepares a button that asks
// the user for a user and for a channel, the user answers with
// messages.sendBotRequestedPeer and webapp_req_id, the bot gets
// messageActionRequestedPeerSentMe with what the button asked for, and the
// channel's answer makes the bot an admin with the rights the button named.
func TestPreparedAndRequestedPeer(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	phone := phones[0]
	alice := login(ctx, t, phone, "Alice")
	getState(ctx, t, alice)
	username := fmt.Sprintf("e2e%dbot", randomID()&0xffffff)
	// bots.requestWebViewButton takes the typed keyboardButton of layer 229
	// and later.
	bot := botLoggedInAtLayer(ctx, t, phone, username, mtclient.DefaultLayer)
	getState(ctx, t, bot)
	toBot := resolveBot(ctx, t, alice, username)
	botUser := inputUserOfPeer(alice.c, toBot)
	aliceForBot := &mtproto.InputUser{Constructor: bot.c.Constructor(mtproto.Predicate_inputUser), UserId: alice.self.GetId(), AccessHash: alice.self.GetAccessHash().GetValue()}
	self := &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerSelf)}

	// The prepared message, for basic groups only.
	o, err := bot.c.Invoke(ctx, &mtproto.TLMessagesSavePreparedInlineMessage{
		Constructor: bot.c.Constructor(mtproto.Predicate_messages_savePreparedInlineMessage),
		Result: &mtproto.InputBotInlineResult{
			Constructor: bot.c.Constructor(mtproto.Predicate_inputBotInlineResult),
			Id:          "1", Type: "article", Title: wrapperspb.String("Invite"),
			SendMessage: &mtproto.InputBotInlineMessage{
				Constructor: bot.c.Constructor(mtproto.Predicate_inputBotInlineMessageText),
				Message:     "Join my game",
			},
		},
		UserId:    aliceForBot,
		PeerTypes: []*mtproto.InlineQueryPeerType{{Constructor: bot.c.Constructor(mtproto.Predicate_inlineQueryPeerTypeChat)}},
	})
	if err != nil {
		t.Fatalf("messages.savePreparedInlineMessage: %v", err)
	}
	saved := o.(interface {
		To_Messages_BotPreparedInlineMessage() *mtproto.Messages_BotPreparedInlineMessage
	}).To_Messages_BotPreparedInlineMessage()
	if saved.GetId() == "" || int64(saved.GetExpireDate()) <= time.Now().Unix() {
		t.Fatalf("the prepared message: %v", saved)
	}

	getPrepared := func(id string) (*mtproto.Messages_PreparedInlineMessage, error) {
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetPreparedInlineMessage{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_getPreparedInlineMessage),
			Bot:         botUser, Id: id,
		})
		if err != nil {
			return nil, err
		}
		return o.(interface {
			To_Messages_PreparedInlineMessage() *mtproto.Messages_PreparedInlineMessage
		}).To_Messages_PreparedInlineMessage(), nil
	}
	if _, err = getPrepared("garbage"); !mtclient.IsRPCError(err, "ID_INVALID") {
		t.Fatalf("a malformed id: %v", err)
	}
	if _, err = getPrepared("AAAAAAAAAAAAAAAAAAAAAAAA"); !mtclient.IsRPCError(err, "ID_EXPIRED") {
		t.Fatalf("an unknown id: %v", err)
	}
	prepared, err := getPrepared(saved.GetId())
	if err != nil {
		t.Fatalf("messages.getPreparedInlineMessage: %v", err)
	}
	if prepared.GetQueryId() == 0 || prepared.GetResult().GetId() != "1" || len(prepared.GetPeerTypes()) != 1 ||
		prepared.GetPeerTypes()[0].GetPredicateName() != mtproto.Predicate_inlineQueryPeerTypeChat {
		t.Fatalf("the prepared message as the user gets it: %v", prepared)
	}

	send := func(peer *mtproto.InputPeer) (mtproto.TLObject, error) {
		return alice.c.Invoke(ctx, &mtproto.TLMessagesSendInlineBotResult{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendInlineBotResult),
			Peer:        peer, RandomId: randomID(), QueryId: prepared.GetQueryId(), Id: "1",
		})
	}
	if _, err = send(self); !mtclient.IsRPCError(err, "PEER_ID_INVALID") {
		t.Fatalf("a prepared message for groups to Saved Messages: %v", err)
	}
	title := fmt.Sprintf("Players %d", randomID()&0xffff)
	// A basic group has a user besides its creator; a bot alone is
	// USERS_TOO_FEW (bff chats, messages.createChat).
	bob := login(ctx, t, phones[1], "Bob")
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesCreateChat92CEDDD4{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_createChat92CEDDD4),
		Users:       []*mtproto.InputUser{botUser, inputUserOf(ctx, t, alice, bob)},
		Title:       title,
	})
	if err != nil {
		t.Fatalf("messages.createChat: %v", err)
	}
	var chatId int64
	for _, c := range o.(interface {
		To_Messages_InvitedUsers() *mtproto.Messages_InvitedUsers
	}).To_Messages_InvitedUsers().GetUpdates().GetChats() {
		if c.GetTitle() == title {
			chatId = c.GetId()
		}
	}
	if chatId == 0 {
		t.Fatal("messages.createChat: no chat")
	}
	o, err = send(chatPeer(alice, chatId))
	if err != nil {
		t.Fatalf("messages.sendInlineBotResult of the prepared message: %v", err)
	}
	if m := sentMessage(t, o); m.GetMessage() != "Join my game" || m.GetViaBotId().GetValue() != bot.self.GetId() {
		t.Fatalf("the shared message: %v", m)
	}

	// The prepared buttons: a user with the name, a channel the bot is to
	// post in.
	prepare := func(buttonId int32, peerType *mtproto.RequestPeerType, name bool) string {
		t.Helper()
		o, err := bot.c.Invoke(ctx, &mtproto.TLBotsRequestWebViewButton{
			Constructor: bot.c.Constructor(mtproto.Predicate_bots_requestWebViewButton),
			UserId:      aliceForBot,
			Button: &mtproto.KeyboardButton{
				Constructor: bot.c.Constructor(mtproto.Predicate_keyboardButton),
				Text:        "Choose",
				Type: &mtproto.ButtonType{
					Constructor:   bot.c.Constructor(mtproto.Predicate_inputButtonTypeRequestPeer),
					NameRequested: name,
					ButtonId:      buttonId,
					PeerType:      peerType,
					MaxQuantity:   1,
				},
			},
		})
		if err != nil {
			t.Fatalf("bots.requestWebViewButton: %v", err)
		}
		return o.(interface {
			To_Bots_RequestedButton() *mtproto.Bots_RequestedButton
		}).To_Bots_RequestedButton().GetWebappReqId()
	}
	userReq := prepare(5, &mtproto.RequestPeerType{Constructor: bot.c.Constructor(mtproto.Predicate_requestPeerTypeUser)}, true)

	if _, err = alice.c.Invoke(ctx, &mtproto.TLBotsGetRequestedWebViewButton{
		Constructor: alice.c.Constructor(mtproto.Predicate_bots_getRequestedWebViewButton),
		Bot:         botUser, WebappReqId: "AAAAAAAAAAAAAAAAAAAAAAAA",
	}); !mtclient.IsRPCError(err, "WEBAPP_REQ_ID_INVALID") {
		t.Fatalf("an unknown webapp_req_id: %v", err)
	}
	o, err = alice.c.Invoke(ctx, &mtproto.TLBotsGetRequestedWebViewButton{
		Constructor: alice.c.Constructor(mtproto.Predicate_bots_getRequestedWebViewButton),
		Bot:         botUser, WebappReqId: userReq,
	})
	if err != nil {
		t.Fatalf("bots.getRequestedWebViewButton: %v", err)
	}
	// Layer 233 has the button as keyboardButton with buttonTypeRequestPeer.
	b := o.(interface {
		To_KeyboardButton() *mtproto.KeyboardButton
	}).To_KeyboardButton()
	if b.GetText() != "Choose" || b.GetType().GetPredicateName() != mtproto.Predicate_buttonTypeRequestPeer || b.GetType().GetButtonId() != 5 ||
		b.GetType().GetPeerType().GetPredicateName() != mtproto.Predicate_requestPeerTypeUser {
		t.Fatalf("the prepared button: %v", b)
	}

	share := func(req string, buttonId int32, peers ...*mtproto.InputPeer) (mtproto.TLObject, error) {
		return alice.c.Invoke(ctx, &mtproto.TLMessagesSendBotRequestedPeer{
			Constructor:    alice.c.Constructor(mtproto.Predicate_messages_sendBotRequestedPeer),
			Peer:           toBot,
			WebappReqId:    wrapperspb.String(req),
			ButtonId:       buttonId,
			RequestedPeers: peers,
		})
	}
	if _, err = share(userReq, 6, self); !mtclient.IsRPCError(err, "BUTTON_ID_INVALID") {
		t.Fatalf("another button id: %v", err)
	}
	if _, err = share(userReq, 5, chatPeer(alice, chatId)); !mtclient.IsRPCError(err, "PEER_ID_INVALID") {
		t.Fatalf("a group for a user button: %v", err)
	}
	o, err = share(userReq, 5, self)
	if err != nil {
		t.Fatalf("messages.sendBotRequestedPeer: %v", err)
	}
	if m := sentMessage(t, o); m.GetAction().GetPredicateName() != mtproto.Predicate_messageActionRequestedPeer ||
		len(m.GetAction().GetPeers_VECTORPEER()) != 1 || m.GetAction().GetPeers_VECTORPEER()[0].GetUserId() != alice.self.GetId() {
		t.Fatalf("the user's message: %v", m)
	}
	waitUpdate(ctx, t, bot, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		a := u.GetMessage_MESSAGE().GetAction()
		if a.GetPredicateName() != mtproto.Predicate_messageActionRequestedPeerSentMe || a.GetButtonId() != 5 {
			return false
		}
		peers := a.GetPeers_VECTORREQUESTEDPEER()
		if len(peers) != 1 || peers[0].GetPredicateName() != mtproto.Predicate_requestedPeerUser || peers[0].GetUserId() != alice.self.GetId() ||
			peers[0].GetFirstName().GetValue() != "Alice" || peers[0].GetUsername() != nil {
			t.Fatalf("the bot's message: %v", a)
		}
		return true
	})

	// A channel of Alice's where the bot is to post: the answer makes the
	// bot an admin that may post.
	_, channel := createChannel(ctx, t, alice, fmt.Sprintf("News %d", randomID()&0xffff), false)
	channelReq := prepare(6, &mtproto.RequestPeerType{
		Constructor: bot.c.Constructor(mtproto.Predicate_requestPeerTypeBroadcast),
		Creator:     true,
		BotAdminRights: &mtproto.ChatAdminRights{
			Constructor:  bot.c.Constructor(mtproto.Predicate_chatAdminRights),
			PostMessages: true,
		},
	}, true)
	toChannel := &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: channel.GetId(), AccessHash: channel.GetAccessHash_FLAGINT64().GetValue()}
	if _, err = share(channelReq, 6, toChannel); err != nil {
		t.Fatalf("messages.sendBotRequestedPeer of a channel: %v", err)
	}
	waitUpdate(ctx, t, bot, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		a := u.GetMessage_MESSAGE().GetAction()
		if a.GetPredicateName() != mtproto.Predicate_messageActionRequestedPeerSentMe || a.GetButtonId() != 6 {
			return false
		}
		peers := a.GetPeers_VECTORREQUESTEDPEER()
		if len(peers) != 1 || peers[0].GetPredicateName() != mtproto.Predicate_requestedPeerChannel || peers[0].GetChannelId() != channel.GetId() ||
			peers[0].GetTitle().GetValue() != channel.GetTitle() {
			t.Fatalf("the bot's message of the channel: %v", a)
		}
		return true
	})
	o, err = alice.c.Invoke(ctx, &mtproto.TLChannelsGetParticipant{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_getParticipant),
		Channel:     &mtproto.InputChannel{Constructor: alice.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: channel.GetId(), AccessHash: channel.GetAccessHash_FLAGINT64().GetValue()},
		Participant: toBot,
	})
	if err != nil {
		t.Fatalf("channels.getParticipant of the bot: %v", err)
	}
	p := o.(interface {
		To_Channels_ChannelParticipant() *mtproto.Channels_ChannelParticipant
	}).To_Channels_ChannelParticipant().GetParticipant()
	if p.GetPredicateName() != mtproto.Predicate_channelParticipantAdmin || !p.GetAdminRights().GetPostMessages() {
		t.Fatalf("the bot in the channel: %v", p)
	}
}
