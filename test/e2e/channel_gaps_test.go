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
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// TestForumTopicGaps (task 067): notification settings of a topic are kept
// and sent back with it; a reaction counts unread in its topic until it is
// read; typing reaches the others with the topic.
func TestForumTopicGaps(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, alice)
	getState(ctx, t, bob)

	_, ch := createChannel(ctx, t, alice, "gaps", true)
	hash := ch.GetAccessHash_FLAGINT64().GetValue()
	input := func(u *user) *mtproto.InputChannel {
		return &mtproto.InputChannel{Constructor: u.c.Constructor(mtproto.Predicate_inputChannel), ChannelId: ch.GetId(), AccessHash: hash}
	}
	peer := func(u *user) *mtproto.InputPeer {
		return &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerChannel), ChannelId: ch.GetId(), AccessHash: hash}
	}
	if _, err := bob.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
		Hash:        channelInviteHash(ctx, t, alice, input(alice)),
	}); err != nil {
		t.Fatalf("messages.importChatInvite: %v", err)
	}
	if _, err := alice.c.Invoke(ctx, &mtproto.TLChannelsToggleForum{
		Constructor: alice.c.Constructor(mtproto.Predicate_channels_toggleForum),
		Channel:     input(alice), Enabled: mtproto.BoolTrue, Tabs: mtproto.BoolFalse,
	}); err != nil {
		t.Fatalf("channels.toggleForum: %v", err)
	}
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesCreateForumTopic{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_createForumTopic),
		Peer:        peer(alice), Title: "Topic", RandomId: randomID(),
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
		t.Fatal("no topic in the answer")
	}
	topicOf := func() *mtproto.ForumTopic {
		t.Helper()
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetForumTopicsByID{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_getForumTopicsByID),
			Peer:        peer(alice), Topics: []int32{topic},
		})
		if err != nil {
			t.Fatalf("messages.getForumTopicsByID: %v", err)
		}
		list := o.(interface {
			To_Messages_ForumTopics() *mtproto.Messages_ForumTopics
		}).To_Messages_ForumTopics().GetTopics()
		if len(list) != 1 {
			t.Fatalf("the topics: %v", list)
		}
		return list[0]
	}

	// The settings of the topic: stored, shown with it, listed as exceptions.
	const muted = int32(2147483647)
	notifyTopic := &mtproto.InputNotifyPeer{
		Constructor: alice.c.Constructor(mtproto.Predicate_inputNotifyForumTopic),
		Peer:        peer(alice),
		TopMsgId:    topic,
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLAccountUpdateNotifySettings{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_updateNotifySettings),
		Peer:        notifyTopic,
		Settings: &mtproto.InputPeerNotifySettings{
			Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerNotifySettings),
			MuteUntil:   wrapperspb.Int32(muted),
		},
	}); err != nil {
		t.Fatalf("account.updateNotifySettings: %v", err)
	}
	if got := topicOf().GetNotifySettings().GetMuteUntil().GetValue(); got != muted {
		t.Errorf("the topic's mute_until: %d", got)
	}
	o, err = alice.c.Invoke(ctx, &mtproto.TLAccountGetNotifySettings{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_getNotifySettings),
		Peer:        notifyTopic,
	})
	if err != nil {
		t.Fatalf("account.getNotifySettings: %v", err)
	}
	if got := o.(interface {
		To_PeerNotifySettings() *mtproto.PeerNotifySettings
	}).To_PeerNotifySettings().GetMuteUntil().GetValue(); got != muted {
		t.Errorf("account.getNotifySettings mute_until: %d", got)
	}
	o, err = alice.c.Invoke(ctx, &mtproto.TLAccountGetNotifyExceptions{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_getNotifyExceptions),
		Peer: &mtproto.InputNotifyPeer{
			Constructor: alice.c.Constructor(mtproto.Predicate_inputNotifyPeer),
			Peer:        peer(alice),
		},
	})
	if err != nil {
		t.Fatalf("account.getNotifyExceptions: %v", err)
	}
	us := o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates()
	if len(us) != 1 || us[0].GetPeer_NOTIFYPEER().GetPredicateName() != mtproto.Predicate_notifyForumTopic || us[0].GetPeer_NOTIFYPEER().GetTopMsgId() != topic {
		t.Errorf("the exceptions of the forum: %v", us)
	}

	// A reaction of another to a message of the topic is unread in it.
	reply := func(u *user) *mtproto.InputReplyTo {
		return &mtproto.InputReplyTo{Constructor: u.c.Constructor(mtproto.Predicate_inputReplyToMessage), ReplyToMsgId: topic, TopMsgId: wrapperspb.Int32(topic)}
	}
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        peer(alice), ReplyTo: reply(alice), Message: "in the topic", RandomId: randomID(),
	})
	if err != nil {
		t.Fatalf("messages.sendMessage: %v", err)
	}
	var msgId int32
	for _, u := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateMessageID {
			msgId = u.GetId_INT32()
		}
	}
	if _, err = bob.c.Invoke(ctx, &mtproto.TLMessagesSendReaction{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_sendReaction),
		Peer:        peer(bob), MsgId: msgId,
		Reaction_FLAGVECTORREACTION: []*mtproto.Reaction{{Constructor: bob.c.Constructor(mtproto.Predicate_reactionEmoji), Emoticon: "👍"}},
	}); err != nil {
		t.Fatalf("messages.sendReaction: %v", err)
	}
	if got := topicOf().GetUnreadReactionsCount(); got != 1 {
		t.Errorf("unread reactions of the topic: %d, want 1", got)
	}
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesGetUnreadReactions{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_getUnreadReactions),
		Peer:        peer(alice), TopMsgId: wrapperspb.Int32(topic), Limit: 10,
	})
	if err != nil {
		t.Fatalf("messages.getUnreadReactions: %v", err)
	}
	if ms := o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	}).To_Messages_Messages().GetMessages(); len(ms) != 1 || ms[0].GetId() != msgId {
		t.Errorf("the unread reactions of the topic: %v", ms)
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLMessagesReadReactions{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_readReactions),
		Peer:        peer(alice), TopMsgId: wrapperspb.Int32(topic),
	}); err != nil {
		t.Fatalf("messages.readReactions: %v", err)
	}
	if got := topicOf().GetUnreadReactionsCount(); got != 0 {
		t.Errorf("unread reactions after reading: %d", got)
	}

	// Typing in the topic.
	getState(ctx, t, bob)
	if _, err = alice.c.Invoke(ctx, &mtproto.TLMessagesSetTyping{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_setTyping),
		Peer:        peer(alice), TopMsgId: wrapperspb.Int32(topic),
		Action: &mtproto.SendMessageAction{Constructor: alice.c.Constructor(mtproto.Predicate_sendMessageTypingAction)},
	}); err != nil {
		t.Fatalf("messages.setTyping: %v", err)
	}
	// It comes as updateShort, as in a basic group.
	if _, err = bob.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		u := us.GetUpdate()
		return u.GetPredicateName() == mtproto.Predicate_updateChannelUserTyping && u.GetChannelId() == ch.GetId() && u.GetTopMsgId_FLAGINT32().GetValue() == topic
	}); err != nil {
		t.Fatalf("no updateChannelUserTyping: %v", err)
	}

	// The counters of a topic of a forum.
	if _, err = alice.c.InvokeRaw(ctx, &mtproto.TLMessagesGetSearchCounters{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_getSearchCounters),
		Peer:        peer(alice), TopMsgId: wrapperspb.Int32(topic),
		Filters: []*mtproto.MessagesFilter{{Constructor: alice.c.Constructor(mtproto.Predicate_inputMessagesFilterPhotoVideo)}},
	}); err != nil {
		t.Fatalf("messages.getSearchCounters: %v", err)
	}
}

// TestMigratedGroupLink (task 067): an invite link made while a chat was a
// basic group leads to its supergroup after the migration.
func TestMigratedGroupLink(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	chatId := createChat(ctx, t, alice, "with a link", bob)

	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesExportChatInvite{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_exportChatInvite),
		Peer:        &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerChat), ChatId: chatId},
	})
	if err != nil {
		t.Fatalf("messages.exportChatInvite: %v", err)
	}
	link := o.(interface {
		To_ExportedChatInvite() *mtproto.ExportedChatInvite
	}).To_ExportedChatInvite().GetLink()
	i := strings.LastIndex(link, "/+")
	if i < 0 {
		t.Fatalf("invite link %q", link)
	}
	hash := link[i+2:]

	if _, err = alice.c.Invoke(ctx, &mtproto.TLMessagesMigrateChat{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_migrateChat),
		ChatId:      chatId,
	}); err != nil {
		t.Fatalf("messages.migrateChat: %v", err)
	}

	o, err = carol.c.Invoke(ctx, &mtproto.TLMessagesCheckChatInvite{
		Constructor: carol.c.Constructor(mtproto.Predicate_messages_checkChatInvite),
		Hash:        hash,
	})
	if err != nil {
		t.Fatalf("messages.checkChatInvite: %v", err)
	}
	if ci := o.(interface{ To_ChatInvite() *mtproto.ChatInvite }).To_ChatInvite(); ci.GetPredicateName() != mtproto.Predicate_chatInvite || !ci.GetMegagroup() {
		t.Errorf("the link after the migration: %v", ci)
	}
	if _, err = carol.c.Invoke(ctx, &mtproto.TLMessagesImportChatInviteDE91436E{
		Constructor: carol.c.Constructor(mtproto.Predicate_messages_importChatInviteDE91436E),
		Hash:        hash,
	}); err != nil {
		t.Fatalf("messages.importChatInvite: %v", err)
	}
}
