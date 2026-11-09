//go:build e2e

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

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

func userPeer(c *mtclient.Client, to *user) *mtproto.InputPeer {
	return &mtproto.InputPeer{
		Constructor: c.Constructor(mtproto.Predicate_inputPeerUser),
		UserId:      to.self.GetId(),
		AccessHash:  to.self.GetAccessHash().GetValue(),
	}
}

// Two users of different layers in the same chats (ADR 0045): each gets what
// the other sends in the form of its own layer, in a private chat with a reply
// and a keyboard, in a channel and in a forum topic.
func TestMixedLayersPrivateChat(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	old := loginAtLayer(ctx, t, phones[0], "Old", 228)
	cur := loginAtLayer(ctx, t, phones[1], "New", 233)
	getState(ctx, t, old)
	getState(ctx, t, cur)

	// A message and a reply, both ways.
	id := sendMessageID(ctx, t, old, userPeer(old.c, cur), "from the old layer", randomID())
	waitUpdate(ctx, t, cur, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		return u.GetMessage_MESSAGE().GetMessage() == "from the old layer"
	})
	reply := func(u *user, to *mtproto.InputPeer, text string, replyTo int32) {
		t.Helper()
		if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_sendMessage),
			Peer:        to,
			Message:     text,
			RandomId:    randomID(),
			ReplyTo:     mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{ReplyToMsgId: replyTo}).To_InputReplyTo(),
		}); err != nil {
			t.Fatalf("messages.sendMessage: %v", err)
		}
	}
	idForNew := newestDialogMessageID(ctx, t, cur, userPeer(cur.c, old))
	reply(cur, userPeer(cur.c, old), "a reply from the new layer", idForNew)
	var replyId int32
	waitUpdate(ctx, t, old, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		if m.GetMessage() != "a reply from the new layer" {
			return false
		}
		if m.GetReplyTo().GetReplyToMsgId_FLAGINT32().GetValue() != id && m.GetReplyTo().GetReplyToMsgId() != id {
			t.Errorf("the reply header at 228: %v, want the message %d", m.GetReplyTo(), id)
		}
		replyId = m.GetId()
		return true
	})
	reply(old, userPeer(old.c, cur), "and back", replyId)
	waitUpdate(ctx, t, cur, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		if m.GetMessage() != "and back" {
			return false
		}
		if m.GetReplyTo().GetPredicateName() != mtproto.Predicate_messageReplyHeader {
			t.Errorf("the reply header at 233: %v", m.GetReplyTo())
		}
		return true
	})

	// A keyboard of a bot of each layer, shown to both.
	for _, layer := range []int32{228, 233} {
		username := fmt.Sprintf("mixed%d%dbot", layer, randomID()&0xffffff)
		bot := botLoggedInAtLayer(ctx, t, old.phone, username, layer)
		getState(ctx, t, bot)
		toBotOld := resolveBot(ctx, t, old, username)
		toBotNew := resolveBot(ctx, t, cur, username)
		for _, m := range []struct {
			u  *user
			to *mtproto.InputPeer
		}{{old, toBotOld}, {cur, toBotNew}} {
			if _, err := m.u.c.Invoke(ctx, &mtproto.TLMessagesStartBot{
				Constructor: m.u.c.Constructor(mtproto.Predicate_messages_startBot),
				Bot:         inputUserOfPeer(m.u.c, m.to),
				Peer:        m.to,
				RandomId:    randomID(),
				StartParam:  "x",
			}); err != nil {
				t.Fatalf("messages.startBot: %v", err)
			}
		}

		markup := mtproto.MakeTLReplyInlineMarkup(&mtproto.ReplyMarkup{
			Rows: []*mtproto.KeyboardButtonRow{mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{
				Buttons: []*mtproto.KeyboardButton{mtproto.MakeTLKeyboardButtonCallback(&mtproto.KeyboardButton{Text: "Go", Data: []byte("go")}).To_KeyboardButton()},
			}).To_KeyboardButtonRow()},
		}).To_ReplyMarkup()
		if layer >= 229 {
			// a bot of this layer has no keyboardButtonCallback at all
			markup = mtproto.MakeTLReplyInlineMarkup(&mtproto.ReplyMarkup{
				Rows_VECTORKEYBOARDINLINEBUTTONROW: []*mtproto.KeyboardInlineButtonRow{mtproto.MakeTLKeyboardInlineButtonRow(&mtproto.KeyboardInlineButtonRow{
					Buttons: []*mtproto.KeyboardInlineButton{mtproto.MakeTLKeyboardInlineButton(&mtproto.KeyboardInlineButton{
						Text: "Go",
						Type: mtproto.MakeTLInlineButtonTypeCallback(&mtproto.InlineButtonType{Data: []byte("go")}).To_InlineButtonType(),
					}).To_KeyboardInlineButton()},
				}).To_KeyboardInlineButtonRow()},
			}).To_ReplyMarkup()
		}
		for _, to := range []*user{old, cur} {
			text := fmt.Sprintf("keys %d for %s", layer, to.self.GetFirstName().GetValue())
			if _, err := bot.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
				Constructor: bot.c.Constructor(mtproto.Predicate_messages_sendMessage),
				Peer:        userPeer(bot.c, to),
				Message:     text,
				RandomId:    randomID(),
				ReplyMarkup: markup,
			}); err != nil {
				t.Fatalf("messages.sendMessage from the bot of layer %d: %v", layer, err)
			}
			waitUpdate(ctx, t, to, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
				m := u.GetMessage_MESSAGE()
				if m.GetMessage() != text {
					return false
				}
				rm := m.GetReplyMarkup()
				if to == old {
					if len(rm.GetRows()) != 1 || rm.GetRows()[0].GetButtons()[0].GetPredicateName() != mtproto.Predicate_keyboardButtonCallback ||
						string(rm.GetRows()[0].GetButtons()[0].GetData()) != "go" {
						t.Errorf("keyboard at 228 from layer %d: %v", layer, rm)
					}
				} else {
					rows := rm.GetRows_VECTORKEYBOARDINLINEBUTTONROW()
					if len(rows) != 1 || rows[0].GetButtons()[0].GetType().GetPredicateName() != mtproto.Predicate_inlineButtonTypeCallback ||
						string(rows[0].GetButtons()[0].GetType().GetData()) != "go" {
						t.Errorf("keyboard at 233 from layer %d: %v", layer, rm)
					}
				}
				return true
			})
		}
	}
}

// newestDialogMessageID is the id of the last message of the chat with peer as
// u sees it: the reply below must use the receiver's own id.
func newestDialogMessageID(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer) int32 {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetHistory{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getHistory),
		Peer:        peer, Limit: 1,
	})
	if err != nil {
		t.Fatalf("messages.getHistory: %v", err)
	}
	msgs := o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	}).To_Messages_Messages().GetMessages()
	if len(msgs) == 0 {
		t.Fatal("messages.getHistory: no messages")
	}
	return msgs[0].GetId()
}

func TestMixedLayersChannelAndForum(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	old := loginAtLayer(ctx, t, phones[0], "Old", 228)
	cur := loginAtLayer(ctx, t, phones[1], "New", 233)
	getState(ctx, t, old)
	getState(ctx, t, cur)

	_, ch := createChannel(ctx, t, cur, "mixed layers", true)
	channel := &mtproto.InputChannel{Constructor: cur.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}
	if _, err := cur.c.Invoke(ctx, &mtproto.TLChannelsInviteToChannelC9E33D54{
		Constructor: cur.c.Constructor(mtproto.Predicate_channels_inviteToChannelC9E33D54),
		Channel:     channel,
		Users:       []*mtproto.InputUser{{Constructor: cur.c.Constructor(mtproto.Predicate_inputUser), UserId: old.self.GetId(), AccessHash: old.self.GetAccessHash().GetValue()}},
	}); err != nil {
		t.Fatalf("channels.inviteToChannel: %v", err)
	}
	peerCur := channelInputPeer(ch)
	// The old member's view of the channel: its access hash for that user.
	peerOld := mtproto.MakeTLInputPeerChannel(&mtproto.InputPeer{ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}).To_InputPeer()

	// The channel of layer 232 and later for the new member.
	sendMessageID(ctx, t, cur, peerCur, "post at 233", randomID())
	waitUpdate(ctx, t, old, mtproto.Predicate_updateNewChannelMessage, func(u *mtproto.Update) bool {
		return u.GetMessage_MESSAGE().GetMessage() == "post at 233"
	})
	sendMessageID(ctx, t, old, peerOld, "post at 228", randomID())
	waitUpdate(ctx, t, cur, mtproto.Predicate_updateNewChannelMessage, func(u *mtproto.Update) bool {
		return u.GetMessage_MESSAGE().GetMessage() == "post at 228"
	})
	o, err := cur.c.Invoke(ctx, &mtproto.TLChannelsGetChannels{
		Constructor: cur.c.Constructor(mtproto.Predicate_channels_getChannels),
		Id:          []*mtproto.InputChannel{channel},
	})
	if err != nil {
		t.Fatalf("channels.getChannels at 233: %v", err)
	}
	if ch2 := o.(interface {
		To_Messages_Chats() *mtproto.Messages_Chats
	}).To_Messages_Chats().GetChats()[0]; ch2.GetId() != ch.GetId() || ch2.GetPredicateName() != mtproto.Predicate_channel {
		t.Fatalf("channel: %v", ch2)
	}

	// channels.toggleForum in the form of layer 233, topics through both.
	o, err = cur.c.Invoke(ctx, &mtproto.TLChannelsToggleForum{
		Constructor: cur.c.Constructor(mtproto.Predicate_channels_toggleForum),
		Channel:     channel, Enabled: mtproto.BoolTrue, Tabs: mtproto.BoolFalse, Sidebar: mtproto.BoolTrue,
	})
	if err != nil {
		t.Fatalf("channels.toggleForum with sidebar: %v", err)
	}
	if c := o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetChats(); len(c) == 0 || !c[0].GetForum() || !c[0].GetForumSidebar() {
		t.Fatalf("the channel of the answer: %v", c)
	}
	o, err = cur.c.Invoke(ctx, &mtproto.TLMessagesCreateForumTopic{
		Constructor: cur.c.Constructor(mtproto.Predicate_messages_createForumTopic),
		Peer:        peerCur, Title: "A topic", RandomId: randomID(),
	})
	if err != nil {
		t.Fatalf("messages.createForumTopic: %v", err)
	}
	var topic int32
	for _, u := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateNewChannelMessage {
			topic = u.GetMessage_MESSAGE().GetId()
		}
	}
	if topic == 0 {
		t.Fatal("no message of the topic")
	}
	inTopic := func(u *user, peer *mtproto.InputPeer, text string) {
		t.Helper()
		if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_sendMessage),
			Peer:        peer,
			Message:     text,
			RandomId:    randomID(),
			ReplyTo:     mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{ReplyToMsgId: topic}).To_InputReplyTo(),
		}); err != nil {
			t.Fatalf("a message in the topic: %v", err)
		}
	}
	inTopic(old, peerOld, "in the topic at 228")
	waitUpdate(ctx, t, cur, mtproto.Predicate_updateNewChannelMessage, func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		return m.GetMessage() == "in the topic at 228" && m.GetReplyTo().GetForumTopic()
	})
	inTopic(cur, peerCur, "in the topic at 233")
	waitUpdate(ctx, t, old, mtproto.Predicate_updateNewChannelMessage, func(u *mtproto.Update) bool {
		m := u.GetMessage_MESSAGE()
		return m.GetMessage() == "in the topic at 233" && m.GetReplyTo().GetForumTopic()
	})
}
