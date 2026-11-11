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
)

func getSendAs(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer) []*mtproto.Peer {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLChannelsGetSendAs{
		Constructor: u.c.Constructor(mtproto.Predicate_channels_getSendAs),
		Peer:        peer,
	})
	if err != nil {
		t.Fatalf("channels.getSendAs: %v", err)
	}
	r := o.(interface {
		To_Channels_SendAsPeers() *mtproto.Channels_SendAsPeers
	}).To_Channels_SendAsPeers()
	out := make([]*mtproto.Peer, 0, len(r.GetPeers_VECTORSENDASPEER()))
	for _, p := range r.GetPeers_VECTORSENDASPEER() {
		if p.GetPremiumRequired() {
			t.Errorf("peer %v asks for Premium", p.GetPeer())
		}
		out = append(out, p.GetPeer())
	}
	for _, p := range out {
		if id := p.GetChannelId(); id != 0 && !hasChat(r.GetChats(), id) {
			t.Errorf("channel %d of the list without its chat", id)
		}
	}
	return out
}

func sendAsText(ctx context.Context, u *user, peer, as *mtproto.InputPeer, text string) (*mtproto.Updates, error) {
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        peer, Message: text, RandomId: randomID(), SendAs: as,
	})
	if err != nil {
		return nil, err
	}
	return o.(interface{ To_Updates() *mtproto.Updates }).To_Updates(), nil
}

func defaultSendAs(ctx context.Context, t *testing.T, u *user, ch *mtproto.Chat) *mtproto.Peer {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLChannelsGetFullChannel{
		Constructor: u.c.Constructor(mtproto.Predicate_channels_getFullChannel),
		Channel: &mtproto.InputChannel{
			Constructor: u.c.Constructor(mtproto.Predicate_inputChannel),
			ChannelId:   ch.GetId(),
			AccessHash:  ch.GetAccessHash_FLAGINT64().GetValue(),
		},
	})
	if err != nil {
		t.Fatalf("channels.getFullChannel: %v", err)
	}
	return o.(interface {
		To_Messages_ChatFull() *mtproto.Messages_ChatFull
	}).To_Messages_ChatFull().GetFullChat().GetDefaultSendAs()
}

// The creator of a supergroup writes to it as her channel: the members get
// the message from the channel, with the channel's chat, and it is out for
// her alone; a member may not write as a channel that is not his, and the
// chosen peer comes back as default_send_as.
func TestSendAsChannel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	_, mine := createChannel(ctx, t, alice, fmt.Sprintf("mine %d", randomID()), false)
	_, group := createChannel(ctx, t, alice, fmt.Sprintf("send as %d", randomID()), true)
	if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsInviteToChannelC9E33D54{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_inviteToChannelC9E33D54),
		Channel: &mtproto.InputChannel{
			Constructor: alice.c.Constructor(mtproto.Predicate_inputChannel),
			ChannelId:   group.GetId(),
			AccessHash:  group.GetAccessHash_FLAGINT64().GetValue(),
		},
		Users: []*mtproto.InputUser{inputUserOf(ctx, t, alice, bob)},
	}); err != nil {
		t.Fatalf("channels.inviteToChannel: %v", err)
	}
	groupPeer, minePeer := channelInputPeer(group), channelInputPeer(mine)
	getState(ctx, t, bob)

	// The creator is an admin of the private group: herself first, then
	// her channel. Bob, a member who owns no channel, only himself.
	peers := getSendAs(ctx, t, alice, groupPeer)
	if len(peers) != 2 || peers[0].GetUserId() != alice.self.GetId() || peers[1].GetChannelId() != mine.GetId() {
		t.Fatalf("Alice's peers %v: want herself and channel %d", peers, mine.GetId())
	}
	if peers = getSendAs(ctx, t, bob, groupPeer); len(peers) != 1 || peers[0].GetUserId() != bob.self.GetId() {
		t.Fatalf("Bob's peers %v: want himself", peers)
	}

	// Alice writes as her channel.
	reply, err := sendAsText(ctx, alice, groupPeer, minePeer, "as my channel")
	if err != nil {
		t.Fatalf("messages.sendMessage with send_as: %v", err)
	}
	var sent int32
	for _, u := range reply.GetUpdates() {
		if m := u.GetMessage_MESSAGE(); u.GetPredicateName() == mtproto.Predicate_updateNewChannelMessage {
			sent = m.GetId()
			if m.GetFromId().GetChannelId() != mine.GetId() || !m.GetOut() {
				t.Errorf("Alice's answer: from %v out %v, want her channel and out", m.GetFromId(), m.GetOut())
			}
		}
	}
	if sent == 0 || !hasChat(reply.GetChats(), mine.GetId()) {
		t.Fatalf("answer %v: want the message and the channel's chat", reply)
	}
	_, err = bob.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, u := range us.GetUpdates() {
			m := u.GetMessage_MESSAGE()
			if u.GetPredicateName() != mtproto.Predicate_updateNewChannelMessage || m.GetId() != sent {
				continue
			}
			if m.GetFromId().GetChannelId() != mine.GetId() || m.GetOut() {
				t.Errorf("Bob got from %v out %v, want Alice's channel, not out", m.GetFromId(), m.GetOut())
			}
			if !hasChat(us.GetChats(), mine.GetId()) {
				t.Error("Bob got the message without the channel's chat")
			}
			return true
		}
		return false
	})
	if err != nil {
		t.Fatalf("Bob: no message from the channel: %v", err)
	}
	history := func(u *user) *mtproto.Message {
		o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetHistory{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_getHistory),
			Peer:        groupPeer, Limit: 1,
		})
		if err != nil {
			t.Fatalf("messages.getHistory: %v", err)
		}
		h := o.(interface {
			To_Messages_Messages() *mtproto.Messages_Messages
		}).To_Messages_Messages()
		if len(h.GetMessages()) == 0 || h.GetMessages()[0].GetId() != sent {
			t.Fatalf("history %v: want message %d first", h.GetMessages(), sent)
		}
		if !hasChat(h.GetChats(), mine.GetId()) {
			t.Error("the history comes without the channel's chat")
		}
		return h.GetMessages()[0]
	}
	if !history(alice).GetOut() || history(bob).GetOut() {
		t.Error("the message in the history is not out for Alice alone")
	}

	// Bob may not write as Alice's channel, nor in her name.
	if _, err = sendAsText(ctx, bob, groupPeer, minePeer, "not mine"); !rpcErrorIs(err, "SEND_AS_PEER_INVALID") {
		t.Errorf("Bob as Alice's channel: %v, want SEND_AS_PEER_INVALID", err)
	}

	// A broadcast channel offers itself alone, and a post names it as
	// send_as, as Android sends every post of a channel without signatures
	// (ChatObject.getSendAsPeerId, SendMessagesHelper.java:5420-5421).
	if peers = getSendAs(ctx, t, alice, minePeer); len(peers) != 1 || peers[0].GetChannelId() != mine.GetId() {
		t.Errorf("the channel's peers %v: want the channel alone", peers)
	}
	post, err := sendAsText(ctx, alice, minePeer, minePeer, "a post")
	if err != nil {
		t.Fatalf("a post with the channel as send_as: %v", err)
	}
	for _, u := range post.GetUpdates() {
		if m := u.GetMessage_MESSAGE(); u.GetPredicateName() == mtproto.Predicate_updateNewChannelMessage && (!m.GetPost() || m.GetFromId() != nil) {
			t.Errorf("the post: post %v from %v, want a post from the channel", m.GetPost(), m.GetFromId())
		}
	}

	// The choice is remembered, and given up.
	save := func(as *mtproto.InputPeer) error {
		_, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSaveDefaultSendAs{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_saveDefaultSendAs),
			Peer:        groupPeer, SendAs: as,
		})
		return err
	}
	if err = save(minePeer); err != nil {
		t.Fatalf("messages.saveDefaultSendAs: %v", err)
	}
	if got := defaultSendAs(ctx, t, alice, group); got.GetChannelId() != mine.GetId() {
		t.Errorf("default_send_as %v, want channel %d", got, mine.GetId())
	}
	if got := defaultSendAs(ctx, t, bob, group); got != nil {
		t.Errorf("Bob's default_send_as %v, want none", got)
	}
	if err = save(mtproto.MakeTLInputPeerSelf(nil).To_InputPeer()); err != nil {
		t.Fatalf("messages.saveDefaultSendAs self: %v", err)
	}
	if got := defaultSendAs(ctx, t, alice, group); got != nil {
		t.Errorf("default_send_as %v after choosing herself, want none", got)
	}
	if err = save(groupPeer); !rpcErrorIs(err, "SEND_AS_PEER_INVALID") {
		t.Errorf("the group as the default of an admin who is not anonymous: %v, want SEND_AS_PEER_INVALID", err)
	}
}
