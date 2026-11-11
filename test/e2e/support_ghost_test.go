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
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// The support ghost (task 080, ADR 0048) as the clients meet it.

// makeGhost gives u a username and makes it the support ghost with
// pluma-admin, as the administrator does.
func makeGhost(ctx context.Context, t *testing.T, u *user) string {
	t.Helper()
	name := fmt.Sprintf("support_e2e_%d", u.self.GetId())
	if _, err := u.c.Invoke(ctx, &mtproto.TLAccountUpdateUsername{
		Constructor: u.c.Constructor(mtproto.Predicate_account_updateUsername),
		Username:    name,
	}); err != nil {
		t.Fatalf("account.updateUsername: %v", err)
	}
	out := admin(t, "ghost", "set", "@"+name, "on")
	if !strings.Contains(out, "is a ghost now") || strings.Contains(out, u.phone) || strings.Contains(out, strings.TrimPrefix(u.phone, "+")) {
		t.Fatalf("pluma-admin ghost set: %q", out)
	}
	return name
}

// memberView is what a member is shown of a supergroup's members.
type memberView struct {
	ids        []int64
	count      int32
	fullCount  int32
	onlines    int32
	topMessage int32
}

func viewOfMembers(ctx context.Context, t *testing.T, u *user, ch *mtproto.InputChannel, peer *mtproto.InputPeer) memberView {
	t.Helper()
	var v memberView
	o, err := u.c.Invoke(ctx, &mtproto.TLChannelsGetParticipants{
		Constructor: u.c.Constructor(mtproto.Predicate_channels_getParticipants),
		Channel:     ch,
		Filter:      &mtproto.ChannelParticipantsFilter{Constructor: u.c.Constructor(mtproto.Predicate_channelParticipantsRecent)},
		Limit:       100,
	})
	if err != nil {
		t.Fatalf("channels.getParticipants: %v", err)
	}
	ps := o.(interface {
		To_Channels_ChannelParticipants() *mtproto.Channels_ChannelParticipants
	}).To_Channels_ChannelParticipants()
	for _, p := range ps.GetParticipants() {
		v.ids = append(v.ids, p.GetUserId())
	}
	v.count = ps.GetCount()
	o, err = u.c.Invoke(ctx, &mtproto.TLChannelsGetFullChannel{Constructor: u.c.Constructor(mtproto.Predicate_channels_getFullChannel), Channel: ch})
	if err != nil {
		t.Fatalf("channels.getFullChannel: %v", err)
	}
	v.fullCount = o.(interface {
		To_Messages_ChatFull() *mtproto.Messages_ChatFull
	}).To_Messages_ChatFull().GetFullChat().GetParticipantsCount().GetValue()
	o, err = u.c.Invoke(ctx, &mtproto.TLMessagesGetOnlines{Constructor: u.c.Constructor(mtproto.Predicate_messages_getOnlines), Peer: peer})
	if err != nil {
		t.Fatalf("messages.getOnlines: %v", err)
	}
	v.onlines = o.(interface{ To_ChatOnlines() *mtproto.ChatOnlines }).To_ChatOnlines().GetOnlines()
	ds := peerDialogs(ctx, t, u, peer).GetDialogs()
	if len(ds) == 1 {
		v.topMessage = ds[0].GetTopMessage()
	}
	return v
}

// pushedChannelMessage waits on u for updateNewChannelMessage of the
// channel with text, and returns it with the channel the push carried.
func pushedChannelMessage(ctx context.Context, t *testing.T, u *user, channelId int64, text string) (*mtproto.Message, *mtproto.Chat) {
	t.Helper()
	var (
		m  *mtproto.Message
		ch *mtproto.Chat
	)
	if _, err := u.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, x := range pushedUpdates(us) {
			got := x.GetMessage_MESSAGE()
			if x.GetPredicateName() == mtproto.Predicate_updateNewChannelMessage && got.GetPeerId().GetChannelId() == channelId && got.GetMessage() == text {
				m = got
				for _, c := range us.GetChats() {
					if c.GetId() == channelId {
						ch = c
					}
				}
				return true
			}
		}
		return false
	}); err != nil {
		t.Fatalf("no push of %q: %v", text, err)
	}
	return m, ch
}

// TestSupportGhostMentioned: Bob mentions @support in a private supergroup
// support is not in. Support is notified (the message pushed, mentioned)
// and finds the chat joined, with its whole history; it answers and reacts.
// Bob sees its answer and reaction and nothing else: the members, their
// count, the online count, the service messages and support's membership
// (channels.getParticipant) are as before.
func TestSupportGhostMentioned(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	support := login(ctx, t, phones[2], "Support")
	name := makeGhost(ctx, t, support)

	_, gr := createChannel(ctx, t, alice, "ghost group", true)
	hash := gr.GetAccessHash_FLAGINT64().GetValue()
	inputOf := func(u *user) *mtproto.InputChannel {
		return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: gr.GetId(), AccessHash: hash}
	}
	peer := func(u *user) *mtproto.InputPeer {
		return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: gr.GetId(), AccessHash: hash}
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsInviteToChannelC9E33D54{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_inviteToChannelC9E33D54),
		Channel:     inputOf(alice),
		Users:       []*mtproto.InputUser{inputUserOf(ctx, t, alice, bob)},
	}); err != nil {
		t.Fatalf("channels.inviteToChannel: %v", err)
	}
	// Nobody adds support: it is called in by a mention.
	_, err := alice.c.Invoke(ctx, &mtproto.TLChannelsInviteToChannelC9E33D54{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_inviteToChannelC9E33D54),
		Channel:     inputOf(alice),
		Users:       []*mtproto.InputUser{inputUserOf(ctx, t, alice, support)},
	})
	if !mtclient.IsRPCError(err, "USER_PRIVACY_RESTRICTED") {
		t.Errorf("support invited: %v, want USER_PRIVACY_RESTRICTED", err)
	}
	sendMessageID(ctx, t, alice, peer(alice), "the history before", randomID())
	before := viewOfMembers(ctx, t, bob, inputOf(bob), peer(bob))

	getState(ctx, t, support)
	text := "@" + name + " the app crashes"
	asked := sendMessageID(ctx, t, bob, peer(bob), text, randomID())

	m, seen := pushedChannelMessage(ctx, t, support, gr.GetId(), text)
	if !m.GetMentioned() {
		t.Error("the push to support does not say it mentions it")
	}
	if seen == nil || seen.GetLeft() {
		t.Errorf("the push shows support the group as %v, want joined", seen)
	}
	if texts := historyTexts(ctx, t, support, peer(support)); !slices.Contains(texts, "the history before") || !slices.Contains(texts, text) {
		t.Errorf("support's history %q lacks what came before the mention", texts)
	}
	if n := unreadMentions(ctx, t, support, peer(support)); n != 1 {
		t.Errorf("support's unread mentions: %d, want 1", n)
	}

	after := viewOfMembers(ctx, t, bob, inputOf(bob), peer(bob))
	switch {
	case slices.Contains(after.ids, support.self.GetId()):
		t.Errorf("Bob sees support among the members: %v", after.ids)
	case after.count != before.count || after.fullCount != before.fullCount:
		t.Errorf("counts %d/%d after the mention, want %d/%d", after.count, after.fullCount, before.count, before.fullCount)
	case after.onlines != before.onlines:
		t.Errorf("onlines %d, want %d", after.onlines, before.onlines)
	case after.topMessage != asked:
		t.Errorf("the group's last message %d, want Bob's %d: a service message told of support", after.topMessage, asked)
	}
	_, err = bob.c.Invoke(ctx, &mtproto.TLChannelsGetParticipant{
		Constructor: bob.c.Constructor(mtproto.Predicate_channels_getParticipant),
		Channel:     inputOf(bob),
		Participant: peerOf(ctx, t, bob, support),
	})
	if !mtclient.IsRPCError(err, "USER_NOT_PARTICIPANT") {
		t.Errorf("channels.getParticipant of support to Bob: %v, want USER_NOT_PARTICIPANT", err)
	}

	getState(ctx, t, bob)
	if _, err = support.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: support.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        peer(support), Message: "we are on it", RandomId: randomID(),
		ReplyTo: mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{ReplyToMsgId: asked}).To_InputReplyTo(),
	}); err != nil {
		t.Fatalf("support's answer: %v", err)
	}
	answer, _ := pushedChannelMessage(ctx, t, bob, gr.GetId(), "we are on it")
	if answer.GetFromId().GetUserId() != support.self.GetId() || !answer.GetMentioned() {
		t.Errorf("Bob gets the answer from %v, mentioned %v", answer.GetFromId(), answer.GetMentioned())
	}
	if _, err = support.c.Invoke(ctx, &mtproto.TLMessagesSendReaction{
		Constructor:                 support.c.Constructor(mtproto.Predicate_messages_sendReaction),
		Peer:                        peer(support),
		MsgId:                       asked,
		Reaction_FLAGVECTORREACTION: []*mtproto.Reaction{{Constructor: support.c.Constructor(mtproto.Predicate_reactionEmoji), Emoticon: "👍"}},
	}); err != nil {
		t.Errorf("support's reaction: %v", err)
	}
	if final := viewOfMembers(ctx, t, bob, inputOf(bob), peer(bob)); final.count != before.count || slices.Contains(final.ids, support.self.GetId()) {
		t.Errorf("after support answered Bob sees %v of %d", final.ids, final.count)
	}

	if out := admin(t, "ghost", "list", "@"+name); !strings.Contains(out, fmt.Sprint(gr.GetId())) {
		t.Errorf("pluma-admin ghost list: %q", out)
	}
}

// TestSupportGhostComments: support comments under a post of a public
// channel it has not subscribed to, and a reply to its comment reaches it;
// the subscribers and the group's members do not see it among the members.
func TestSupportGhostComments(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	support := login(ctx, t, phones[1], "Support")
	makeGhost(ctx, t, support)

	_, ch := createChannel(ctx, t, alice, "ghost news", false)
	_, gr := createChannel(ctx, t, alice, "ghost news chat", true)
	inputOf := func(u *user, c *mtproto.Chat) *mtproto.InputChannel {
		return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: c.GetId(), AccessHash: c.GetAccessHash_FLAGINT64().GetValue()}
	}
	peerOf := func(u *user, c *mtproto.Chat) *mtproto.InputPeer {
		return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: c.GetId(), AccessHash: c.GetAccessHash_FLAGINT64().GetValue()}
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsUpdateUsername{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_updateUsername),
		Channel:     inputOf(alice, ch),
		Username:    fmt.Sprintf("ghost_news_%d", ch.GetId()),
	}); err != nil {
		t.Fatalf("channels.updateUsername: %v", err)
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsSetDiscussionGroup{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_setDiscussionGroup),
		Broadcast:   inputOf(alice, ch),
		Group:       inputOf(alice, gr),
	}); err != nil {
		t.Fatalf("channels.setDiscussionGroup: %v", err)
	}
	post := sendMessageID(ctx, t, alice, peerOf(alice, ch), "release 1.2", randomID())
	before := viewOfMembers(ctx, t, alice, inputOf(alice, gr), peerOf(alice, gr))

	o, err := support.c.Invoke(ctx, &mtproto.TLMessagesGetDiscussionMessage{
		Constructor: support.c.Constructor(mtproto.Predicate_messages_getDiscussionMessage),
		Peer:        peerOf(support, ch), MsgId: post,
	})
	if err != nil {
		t.Fatalf("messages.getDiscussionMessage by support: %v", err)
	}
	dm := o.(interface {
		To_Messages_DiscussionMessage() *mtproto.Messages_DiscussionMessage
	}).To_Messages_DiscussionMessage()
	if len(dm.GetMessages()) != 1 {
		t.Fatalf("the post's copy: %v", dm.GetMessages())
	}
	copyId := dm.GetMessages()[0].GetId()
	getState(ctx, t, support)
	if _, err = support.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: support.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        peerOf(support, gr), Message: "a comment by support", RandomId: randomID(),
		ReplyTo: mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{ReplyToMsgId: copyId, TopMsgId: mtproto.MakeFlagsInt32(copyId)}).To_InputReplyTo(),
	}); err != nil {
		t.Fatalf("support's comment: %v", err)
	}
	if after := viewOfMembers(ctx, t, alice, inputOf(alice, gr), peerOf(alice, gr)); slices.Contains(after.ids, support.self.GetId()) || after.count != before.count || after.fullCount != before.fullCount {
		t.Errorf("Alice sees the group's members %v of %d/%d, want %v of %d/%d", after.ids, after.count, after.fullCount, before.ids, before.count, before.fullCount)
	}
	_, err = support.c.Invoke(ctx, &mtproto.TLChannelsGetParticipant{
		Constructor: support.c.Constructor(mtproto.Predicate_channels_getParticipant),
		Channel:     inputOf(support, ch),
		Participant: &mtproto.InputPeer{Constructor: support.c.Constructor(mtproto.Predicate_inputPeerSelf)},
	})
	if !mtclient.IsRPCError(err, "USER_NOT_PARTICIPANT") {
		t.Errorf("support subscribed to the channel: %v, want USER_NOT_PARTICIPANT", err)
	}

	var supportsComment int32
	for _, m := range historyMessages(ctx, t, alice, peerOf(alice, gr)) {
		if m.GetMessage() == "a comment by support" && m.GetFromId().GetUserId() == support.self.GetId() {
			supportsComment = m.GetId()
		}
	}
	if supportsComment == 0 {
		t.Fatal("Alice does not see support's comment")
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        peerOf(alice, gr), Message: "thanks, support", RandomId: randomID(),
		ReplyTo: mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{ReplyToMsgId: supportsComment, TopMsgId: mtproto.MakeFlagsInt32(copyId)}).To_InputReplyTo(),
	}); err != nil {
		t.Fatalf("Alice's reply: %v", err)
	}
	if m, _ := pushedChannelMessage(ctx, t, support, gr.GetId(), "thanks, support"); !m.GetMentioned() {
		t.Error("the reply to support's comment does not say it is for support")
	}
}

// TestSupportGhostReacts: support reacts to a message of a public
// supergroup it is not in; the reaction is there for everyone, the group
// shows up in support's dialogs, and the members see support nowhere else.
func TestSupportGhostReacts(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	support := login(ctx, t, phones[1], "Support")
	makeGhost(ctx, t, support)

	_, gr := createChannel(ctx, t, alice, "ghost public group", true)
	hash := gr.GetAccessHash_FLAGINT64().GetValue()
	inputOf := func(u *user) *mtproto.InputChannel {
		return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: gr.GetId(), AccessHash: hash}
	}
	peer := func(u *user) *mtproto.InputPeer {
		return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: gr.GetId(), AccessHash: hash}
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsUpdateUsername{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_updateUsername),
		Channel:     inputOf(alice),
		Username:    fmt.Sprintf("ghost_group_%d", gr.GetId()),
	}); err != nil {
		t.Fatalf("channels.updateUsername: %v", err)
	}
	msgId := sendMessageID(ctx, t, alice, peer(alice), "is it a bug?", randomID())
	before := viewOfMembers(ctx, t, alice, inputOf(alice), peer(alice))

	if _, err := support.c.Invoke(ctx, &mtproto.TLMessagesSendReaction{
		Constructor:                 support.c.Constructor(mtproto.Predicate_messages_sendReaction),
		Peer:                        peer(support),
		MsgId:                       msgId,
		Reaction_FLAGVECTORREACTION: []*mtproto.Reaction{{Constructor: support.c.Constructor(mtproto.Predicate_reactionEmoji), Emoticon: "👍"}},
	}); err != nil {
		t.Fatalf("support's reaction without joining: %v", err)
	}
	var reacted bool
	for _, m := range historyMessages(ctx, t, alice, peer(alice)) {
		if m.GetId() == msgId {
			for _, r := range m.GetReactions().GetResults() {
				reacted = reacted || r.GetReaction_REACTION().GetEmoticon() == "👍" && r.GetCount() == 1
			}
		}
	}
	if !reacted {
		t.Error("Alice does not see support's reaction")
	}
	if ds := peerDialogs(ctx, t, support, peer(support)).GetDialogs(); len(ds) != 1 {
		t.Errorf("support's dialogs with the group: %v", ds)
	}
	if after := viewOfMembers(ctx, t, alice, inputOf(alice), peer(alice)); slices.Contains(after.ids, support.self.GetId()) || after.count != before.count || after.fullCount != before.fullCount || after.topMessage != before.topMessage {
		t.Errorf("Alice sees %v of %d/%d, last message %d, want %v of %d/%d, %d", after.ids, after.count, after.fullCount, after.topMessage,
			before.ids, before.count, before.fullCount, before.topMessage)
	}
}

// historyMessages are the messages of u's history with peer.
func historyMessages(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer) []*mtproto.Message {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetHistory{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getHistory),
		Peer:        peer,
		Limit:       100,
	})
	if err != nil {
		t.Fatalf("messages.getHistory: %v", err)
	}
	return o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	}).To_Messages_Messages().GetMessages()
}
