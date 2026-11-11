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
)

// Methods of channels not served yet answer ERR_ENTERPRISE_IS_BLOCKED
// (400) through the session's routing, never an internal error, which the
// clients' transports retry without end: those of RPCChannels from the bff
// module channels, those the schema puts in other services (anti-spam here)
// from the proxy.
func TestChannelsNotServed(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	alice := login(ctx, t, testPhones(t, 1)[0], "Alice")
	channel := &mtproto.InputChannel{
		Constructor: alice.c.Constructor(mtproto.Predicate_inputChannel),
		ChannelId:   1,
		AccessHash:  1,
	}
	for name, req := range map[string]mtproto.TLObject{
		"channels.editLocation": &mtproto.TLChannelsEditLocation{
			Constructor: alice.c.Constructor(mtproto.Predicate_channels_editLocation),
			Channel:     channel,
			GeoPoint:    &mtproto.InputGeoPoint{Constructor: alice.c.Constructor(mtproto.Predicate_inputGeoPointEmpty)},
		},
		"channels.toggleAntiSpam": &mtproto.TLChannelsToggleAntiSpam{
			Constructor: alice.c.Constructor(mtproto.Predicate_channels_toggleAntiSpam),
			Channel:     channel,
			Enabled:     mtproto.BoolTrue,
		},
	} {
		if _, err := alice.c.Invoke(ctx, req); !mtclient.IsRPCError(err, "ERR_ENTERPRISE_IS_BLOCKED") {
			t.Errorf("%s: %v, want ERR_ENTERPRISE_IS_BLOCKED", name, err)
		}
	}
}

func createChannel(ctx context.Context, t *testing.T, u *user, title string, megagroup bool) (*mtproto.Updates, *mtproto.Chat) {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLChannelsCreateChannel{
		Constructor: u.c.Constructor(mtproto.Predicate_channels_createChannel),
		Broadcast:   !megagroup,
		Megagroup:   megagroup,
		Title:       title,
		About:       "about " + title,
	})
	if err != nil {
		t.Fatalf("channels.createChannel: %v", err)
	}
	r := o.(interface{ To_Updates() *mtproto.Updates }).To_Updates()
	if len(r.GetChats()) == 0 || r.GetChats()[0].GetPredicateName() != mtproto.Predicate_channel {
		t.Fatalf("channels.createChannel: chats %v", r.GetChats())
	}
	return r, r.GetChats()[0]
}

// A channel and a supergroup are made and opened (task 038): the answer
// has the channel first in chats and its first message with the channel's
// pts; the full channel, the dialogs and the dialog of the channel give a
// pts above 0; the creator has the permanent link, which Android's creation
// screen takes unchecked; the creator's other device learns the channel.
func TestChannelCreate(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]
	alice := login(ctx, t, phone, "Alice")
	laptop := login(ctx, t, phone, "Alice")
	getState(ctx, t, laptop)

	for _, megagroup := range []bool{false, true} {
		r, ch := createChannel(ctx, t, alice, fmt.Sprintf("kind %v", megagroup), megagroup)
		if ch.GetMegagroup() != megagroup || ch.GetBroadcast() == megagroup || !ch.GetCreator() || ch.GetAccessHash_FLAGINT64().GetValue() == 0 {
			t.Errorf("the channel %v", ch)
		}
		var first *mtproto.Update
		for _, u := range r.GetUpdates() {
			if u.GetPredicateName() == mtproto.Predicate_updateNewChannelMessage {
				first = u
			}
		}
		if first == nil || first.GetMessage_MESSAGE().GetId() != 1 || first.GetPts_INT32() != 2 ||
			first.GetMessage_MESSAGE().GetAction().GetPredicateName() != mtproto.Predicate_messageActionChannelCreate ||
			first.GetMessage_MESSAGE().GetAction().GetTitle_STRING() != fmt.Sprintf("kind %v", megagroup) {
			t.Fatalf("the first message: %v", r.GetUpdates())
		}
		input := &mtproto.InputChannel{Constructor: alice.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}
		inputPeer := &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}

		o, err := alice.c.Invoke(ctx, &mtproto.TLChannelsGetFullChannel{
			Constructor: alice.c.Constructor(mtproto.Predicate_channels_getFullChannel),
			Channel:     input,
		})
		if err != nil {
			t.Fatalf("channels.getFullChannel: %v", err)
		}
		full := o.(interface {
			To_Messages_ChatFull() *mtproto.Messages_ChatFull
		}).To_Messages_ChatFull().GetFullChat()
		if full.GetPts() < 2 || full.GetReadInboxMaxId() != 1 || full.GetParticipantsCount().GetValue() != 1 || full.GetExportedInvite() == nil {
			t.Errorf("the full channel: pts %d, read %d, members %v, invite %v", full.GetPts(), full.GetReadInboxMaxId(), full.GetParticipantsCount(), full.GetExportedInvite())
		}

		d := peerDialogs(ctx, t, alice, inputPeer).GetDialogs()
		if len(d) != 1 || d[0].GetPts().GetValue() < 2 || d[0].GetTopMessage() != 1 {
			t.Errorf("the dialog of the channel: %v", d)
		}

		o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesGetExportedChatInvites{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_getExportedChatInvites),
			Peer:        inputPeer,
			AdminId:     &mtproto.InputUser{Constructor: alice.c.Constructor(mtproto.Predicate_inputUserSelf)},
			Limit:       1,
		})
		if err != nil {
			t.Fatalf("messages.getExportedChatInvites: %v", err)
		}
		if n := len(o.(interface {
			To_Messages_ExportedChatInvites() *mtproto.Messages_ExportedChatInvites
		}).To_Messages_ExportedChatInvites().GetInvites()); n != 1 {
			t.Errorf("%d invite links, want the permanent one", n)
		}

		o, err = alice.c.Invoke(ctx, &mtproto.TLChannelsGetChannels{
			Constructor: alice.c.Constructor(mtproto.Predicate_channels_getChannels),
			Id:          []*mtproto.InputChannel{input},
		})
		if err != nil || len(o.(interface {
			To_Messages_Chats() *mtproto.Messages_Chats
		}).To_Messages_Chats().GetChats()) != 1 {
			t.Errorf("channels.getChannels: %v, %v", o, err)
		}
		bad := &mtproto.InputChannel{Constructor: input.Constructor, ChannelId: input.ChannelId, AccessHash: input.AccessHash + 1}
		if _, err = alice.c.Invoke(ctx, &mtproto.TLChannelsGetChannels{
			Constructor: alice.c.Constructor(mtproto.Predicate_channels_getChannels),
			Id:          []*mtproto.InputChannel{bad},
		}); !mtclient.IsRPCError(err, "CHANNEL_INVALID") {
			t.Errorf("a wrong access_hash: %v, want CHANNEL_INVALID", err)
		}

		o, err = alice.c.Invoke(ctx, &mtproto.TLChannelsGetParticipant{
			Constructor: alice.c.Constructor(mtproto.Predicate_channels_getParticipant),
			Channel:     input,
			Participant: &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerSelf)},
		})
		if err != nil {
			t.Fatalf("channels.getParticipant: %v", err)
		}
		if p := o.(interface {
			To_Channels_ChannelParticipant() *mtproto.Channels_ChannelParticipant
		}).To_Channels_ChannelParticipant().GetParticipant(); p.GetPredicateName() != mtproto.Predicate_channelParticipantCreator {
			t.Errorf("the creator's participant: %v", p)
		}

		waitUpdate(ctx, t, laptop, mtproto.Predicate_updateChannel, func(u *mtproto.Update) bool { return u.GetChannelId() == ch.GetId() })
	}

	// Both are in the list of dialogs, with their pts.
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetDialogs{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_getDialogs),
		OffsetPeer:  &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerEmpty)},
		Limit:       50,
	})
	if err != nil {
		t.Fatalf("messages.getDialogs: %v", err)
	}
	ds := o.(interface {
		To_Messages_Dialogs() *mtproto.Messages_Dialogs
	}).To_Messages_Dialogs()
	channels := 0
	for _, d := range ds.GetDialogs() {
		if d.GetPeer().GetPredicateName() == mtproto.Predicate_peerChannel {
			channels++
			if d.GetPts().GetValue() < 2 {
				t.Errorf("a channel dialog with pts %v", d.GetPts())
			}
		}
	}
	if channels != 2 {
		t.Errorf("%d channel dialogs, want 2", channels)
	}
}

// Messages to a supergroup and posts to a channel (task 039): each gets the
// channel's next id and pts; the sender's other device gets them pushed;
// the history is messages.channelMessages with the channel's pts; a send
// repeated with the same random_id gets the first answer, no second copy.
func TestChannelSend(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]
	alice := login(ctx, t, phone, "Alice")
	laptop := login(ctx, t, phone, "Alice")
	getState(ctx, t, laptop)

	for _, megagroup := range []bool{true, false} {
		_, ch := createChannel(ctx, t, alice, fmt.Sprintf("send %v", megagroup), megagroup)
		peer := &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()}
		send := func(text string, randomId int64) *mtproto.Update {
			t.Helper()
			o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
				Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
				Peer:        peer,
				Message:     text,
				RandomId:    randomId,
			})
			if err != nil {
				t.Fatalf("messages.sendMessage to a channel: %v", err)
			}
			for _, u := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
				if u.GetPredicateName() == mtproto.Predicate_updateNewChannelMessage {
					return u
				}
			}
			t.Fatalf("no updateNewChannelMessage in the answer")
			return nil
		}
		first, second := randomID(), randomID()
		u1 := send("one", first)
		u2 := send("two", second)
		if u1.GetMessage_MESSAGE().GetId() != 2 || u2.GetMessage_MESSAGE().GetId() != 3 || u1.GetPts_INT32() != 3 || u2.GetPts_INT32() != 4 || !u2.GetMessage_MESSAGE().GetOut() {
			t.Fatalf("ids %d, %d and pts %d, %d", u1.GetMessage_MESSAGE().GetId(), u2.GetMessage_MESSAGE().GetId(), u1.GetPts_INT32(), u2.GetPts_INT32())
		}
		if post := u1.GetMessage_MESSAGE().GetPost(); post == megagroup {
			t.Errorf("post %v in a megagroup %v", post, megagroup)
		}
		// The same random_id again: the first answer.
		if again := send("one", first); again.GetMessage_MESSAGE().GetId() != 2 || again.GetPts_INT32() != 3 {
			t.Errorf("the repeat made %d with pts %d", again.GetMessage_MESSAGE().GetId(), again.GetPts_INT32())
		}
		waitUpdate(ctx, t, laptop, mtproto.Predicate_updateNewChannelMessage, func(u *mtproto.Update) bool {
			return u.GetMessage_MESSAGE().GetPeerId().GetChannelId() == ch.GetId() && u.GetPts_INT32() == 4
		})

		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetHistory{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_getHistory),
			Peer:        peer,
			Limit:       20,
		})
		if err != nil {
			t.Fatalf("messages.getHistory of a channel: %v", err)
		}
		h := o.(interface {
			To_Messages_Messages() *mtproto.Messages_Messages
		}).To_Messages_Messages()
		if h.GetPredicateName() != mtproto.Predicate_messages_channelMessages || h.GetPts() != 4 || h.GetCount() != 3 || len(h.GetMessages()) != 3 ||
			h.GetMessages()[0].GetId() != 3 || len(h.GetChats()) != 1 {
			t.Fatalf("history: %s pts %d count %d messages %d chats %d", h.GetPredicateName(), h.GetPts(), h.GetCount(), len(h.GetMessages()), len(h.GetChats()))
		}

		o, err = alice.c.Invoke(ctx, &mtproto.TLChannelsGetMessages{
			Constructor: alice.c.Constructor(mtproto.Predicate_channels_getMessages),
			Channel:     &mtproto.InputChannel{Constructor: alice.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: ch.GetAccessHash_FLAGINT64().GetValue()},
			Id_VECTORINPUTMESSAGE: []*mtproto.InputMessage{
				{Constructor: alice.c.Constructor(mtproto.Predicate_inputMessageID), Id: 2},
				{Constructor: alice.c.Constructor(mtproto.Predicate_inputMessageID), Id: 99},
			},
		})
		if err != nil {
			t.Fatalf("channels.getMessages: %v", err)
		}
		if ms := o.(interface {
			To_Messages_Messages() *mtproto.Messages_Messages
		}).To_Messages_Messages().GetMessages(); len(ms) != 1 || ms[0].GetMessage() != "one" {
			t.Errorf("channels.getMessages: %v", ms)
		}

		// The dialog follows the last message.
		if d := peerDialogs(ctx, t, alice, peer).GetDialogs(); len(d) != 1 || d[0].GetTopMessage() != 3 || d[0].GetPts().GetValue() != 4 {
			t.Errorf("the dialog after sending: %v", d)
		}
	}
}
