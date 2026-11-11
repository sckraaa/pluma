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
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// chatView is what a member is shown of a basic group's members.
type chatView struct {
	ids        []int64
	count      int32
	onlines    int32
	topMessage int32
}

func viewOfChat(ctx context.Context, t *testing.T, u *user, chatId int64) chatView {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesGetFullChat{Constructor: u.c.Constructor(mtproto.Predicate_messages_getFullChat), ChatId: chatId})
	if err != nil {
		t.Fatalf("messages.getFullChat: %v", err)
	}
	full := o.(interface {
		To_Messages_ChatFull() *mtproto.Messages_ChatFull
	}).To_Messages_ChatFull()
	var v chatView
	for _, p := range full.GetFullChat().GetParticipants().GetParticipants() {
		v.ids = append(v.ids, p.GetUserId())
	}
	for _, c := range full.GetChats() {
		if c.GetId() == chatId {
			v.count = c.GetParticipantsCount_INT32()
		}
	}
	o, err = u.c.Invoke(ctx, &mtproto.TLMessagesGetOnlines{Constructor: u.c.Constructor(mtproto.Predicate_messages_getOnlines), Peer: chatPeer(u, chatId)})
	if err != nil {
		t.Fatalf("messages.getOnlines: %v", err)
	}
	v.onlines = o.(interface{ To_ChatOnlines() *mtproto.ChatOnlines }).To_ChatOnlines().GetOnlines()
	if ds := peerDialogs(ctx, t, u, chatPeer(u, chatId)).GetDialogs(); len(ds) == 1 {
		v.topMessage = ds[0].GetTopMessage()
	}
	return v
}

// pushedChatMessage waits on u for updateNewMessage of the basic group with
// text, and returns it with the chat the push carried.
func pushedChatMessage(ctx context.Context, t *testing.T, u *user, chatId int64, text string) (*mtproto.Message, *mtproto.Chat) {
	t.Helper()
	var (
		m  *mtproto.Message
		ch *mtproto.Chat
	)
	if _, err := u.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, x := range pushedUpdates(us) {
			got := x.GetMessage_MESSAGE()
			if x.GetPredicateName() == mtproto.Predicate_updateNewMessage && got.GetPeerId().GetChatId() == chatId && got.GetMessage() == text {
				m = got
				for _, c := range us.GetChats() {
					if c.GetId() == chatId {
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

func readersOf(ctx context.Context, t *testing.T, u *user, chatId int64, msgId int32) []int64 {
	t.Helper()
	raw, err := u.c.InvokeRaw(ctx, &mtproto.TLMessagesGetMessageReadParticipants31C1C44F{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_getMessageReadParticipants31C1C44F),
		Peer:        chatPeer(u, chatId),
		MsgId:       msgId,
	})
	if err != nil {
		t.Fatalf("messages.getMessageReadParticipants: %v", err)
	}
	list, err := mtclient.DecodeVector(raw)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, x := range list {
		ids = append(ids, x.(interface {
			To_ReadParticipantDate() *mtproto.ReadParticipantDate
		}).To_ReadParticipantDate().GetUserId())
	}
	return ids
}

func readChat(ctx context.Context, t *testing.T, u *user, chatId int64) {
	t.Helper()
	if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesReadHistory{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_readHistory),
		Peer:        chatPeer(u, chatId),
		MaxId:       1 << 30,
	}); err != nil {
		t.Fatalf("messages.readHistory: %v", err)
	}
}

// TestSupportGhostBasicGroup: nobody adds support to a basic group (a new
// group lists it as a missing invitee, an add is USER_PRIVACY_RESTRICTED).
// Bob mentions @support in his group: support is notified, finds the group
// joined with the whole history, the mention unread, and answers. Bob sees
// the answer and nothing else: the members, their count and the online
// count, the last message (no service message), who read Alice's message.
// Support leaves unseen too.
func TestSupportGhostBasicGroup(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	support := login(ctx, t, phones[2], "Support")
	name := makeGhost(ctx, t, support)
	supportUser := inputUserOfPeer(alice.c, ghostPeer(ctx, t, alice, name))

	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesCreateChat92CEDDD4{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_createChat92CEDDD4),
		Users:       []*mtproto.InputUser{inputUserOf(ctx, t, alice, bob), supportUser},
		Title:       "ghost basic group",
	})
	if err != nil {
		t.Fatalf("messages.createChat: %v", err)
	}
	invited := o.(interface {
		To_Messages_InvitedUsers() *mtproto.Messages_InvitedUsers
	}).To_Messages_InvitedUsers()
	if m := invited.GetMissingInvitees(); len(m) != 1 || m[0].GetUserId() != support.self.GetId() {
		t.Errorf("missing invitees %v, want support", m)
	}
	var chatId int64
	for _, c := range invited.GetUpdates().GetChats() {
		if c.GetTitle() == "ghost basic group" {
			chatId = c.GetId()
		}
	}
	if chatId == 0 {
		t.Fatal("messages.createChat reply without the chat")
	}
	_, err = alice.c.Invoke(ctx, &mtproto.TLMessagesAddChatUserCBC6D107{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_addChatUserCBC6D107),
		ChatId:      chatId, UserId: supportUser, FwdLimit: 100,
	})
	if !mtclient.IsRPCError(err, "USER_PRIVACY_RESTRICTED") {
		t.Errorf("support added: %v, want USER_PRIVACY_RESTRICTED", err)
	}

	early := sendMessageID(ctx, t, alice, chatPeer(alice, chatId), "the history before", randomID())
	before := viewOfChat(ctx, t, bob, chatId)

	getState(ctx, t, support)
	text := "@" + name + " the app crashes"
	asked := sendMessageID(ctx, t, bob, chatPeer(bob, chatId), text, randomID())
	m, seen := pushedChatMessage(ctx, t, support, chatId, text)
	if !m.GetMentioned() {
		t.Error("the push to support does not say it mentions it")
	}
	if seen == nil || seen.GetLeft() || seen.GetPredicateName() != mtproto.Predicate_chat {
		t.Errorf("the push shows support the group as %v, want joined", seen)
	}
	if texts := historyTexts(ctx, t, support, chatPeer(support, chatId)); !slices.Contains(texts, "the history before") || !slices.Contains(texts, text) {
		t.Errorf("support's history %q lacks what came before the mention", texts)
	}
	if n := unreadMentions(ctx, t, support, chatPeer(support, chatId)); n != 1 {
		t.Errorf("support's unread mentions: %d, want 1", n)
	}
	if full := viewOfChat(ctx, t, support, chatId); !slices.Contains(full.ids, support.self.GetId()) {
		t.Errorf("support does not see itself among the members: %v", full.ids)
	}

	after := viewOfChat(ctx, t, bob, chatId)
	switch {
	case slices.Contains(after.ids, support.self.GetId()):
		t.Errorf("Bob sees support among the members: %v", after.ids)
	case after.count != before.count || len(after.ids) != len(before.ids):
		t.Errorf("Bob sees %d members of %d, want %d of %d", len(after.ids), after.count, len(before.ids), before.count)
	case after.onlines != before.onlines:
		t.Errorf("onlines %d, want %d", after.onlines, before.onlines)
	case after.topMessage != asked:
		t.Errorf("the group's last message %d, want Bob's %d: a service message told of support", after.topMessage, asked)
	}

	// Support reads; Alice's message shows no reader but Bob.
	readChat(ctx, t, support, chatId)
	readChat(ctx, t, bob, chatId)
	for deadline := time.Now().Add(20 * time.Second); !slices.Contains(readersOf(ctx, t, alice, chatId, early), bob.self.GetId()); {
		if time.Now().After(deadline) {
			t.Fatal("Bob's read does not show")
		}
		time.Sleep(200 * time.Millisecond)
	}
	if r := readersOf(ctx, t, alice, chatId, early); slices.Contains(r, support.self.GetId()) {
		t.Errorf("readers %v include support", r)
	}

	getState(ctx, t, bob)
	if _, err = support.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: support.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        chatPeer(support, chatId), Message: "we are on it", RandomId: randomID(),
		ReplyTo: mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{ReplyToMsgId: m.GetId()}).To_InputReplyTo(),
	}); err != nil {
		t.Fatalf("support's answer: %v", err)
	}
	answer, _ := pushedChatMessage(ctx, t, bob, chatId, "we are on it")
	if answer.GetFromId().GetUserId() != support.self.GetId() || !answer.GetMentioned() {
		t.Errorf("Bob gets the answer from %v, mentioned %v", answer.GetFromId(), answer.GetMentioned())
	}

	if out := admin(t, "ghost", "list", "@"+name); !strings.Contains(out, fmt.Sprint(chatId)) || !strings.Contains(out, "group") {
		t.Errorf("pluma-admin ghost list: %q", out)
	}

	// Support leaves: the members see nothing of it either.
	top := viewOfChat(ctx, t, bob, chatId).topMessage
	if _, err = support.c.Invoke(ctx, &mtproto.TLMessagesDeleteChatUser{
		Constructor: support.c.Constructor(mtproto.Predicate_messages_deleteChatUser),
		ChatId:      chatId,
		UserId:      &mtproto.InputUser{Constructor: support.c.Constructor(mtproto.Predicate_inputUserSelf)},
	}); err != nil {
		t.Fatalf("support leaves: %v", err)
	}
	if final := viewOfChat(ctx, t, bob, chatId); final.topMessage != top || final.count != before.count {
		t.Errorf("after support left Bob sees the last message %d (want %d), %d members", final.topMessage, top, final.count)
	}
	if out := admin(t, "ghost", "list", "@"+name); strings.Contains(out, fmt.Sprint(chatId)) {
		t.Errorf("pluma-admin ghost list after the leave: %q", out)
	}
	if out := admin(t, "ghost", "join", "@"+name, fmt.Sprint(chatId)); !strings.Contains(out, "joined group") {
		t.Errorf("pluma-admin ghost join of a basic group: %q", out)
	}
	if final := viewOfChat(ctx, t, bob, chatId); final.topMessage != top || slices.Contains(final.ids, support.self.GetId()) {
		t.Errorf("after the operator's join Bob sees %v, last message %d", final.ids, final.topMessage)
	}

	// The creator removes support: the answer is the same whether it is in
	// the group (here, after the operator's join) or not (a group it never
	// came into), and no service message tells of it.
	removeSupport := func(by *user, id int64) error {
		_, err := by.c.Invoke(ctx, &mtproto.TLMessagesDeleteChatUser{
			Constructor: by.c.Constructor(mtproto.Predicate_messages_deleteChatUser),
			ChatId:      id,
			UserId:      supportUser,
		})
		return err
	}
	if err = removeSupport(alice, chatId); err != nil {
		t.Errorf("Alice removes support from the group: %v", err)
	}
	if final := viewOfChat(ctx, t, bob, chatId); final.topMessage != top || final.count != before.count {
		t.Errorf("after support's removal Bob sees the last message %d (want %d), %d members", final.topMessage, top, final.count)
	}
	// Bob creates the second group: Alice has just created one, and a new
	// group within createChatFlood is FLOOD_WAIT.
	o, err = bob.c.Invoke(ctx, &mtproto.TLMessagesCreateChat92CEDDD4{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_createChat92CEDDD4),
		Users:       []*mtproto.InputUser{inputUserOf(ctx, t, bob, alice)},
		Title:       "no ghost here",
	})
	if err != nil {
		t.Fatalf("messages.createChat: %v", err)
	}
	var otherId int64
	for _, c := range o.(interface {
		To_Messages_InvitedUsers() *mtproto.Messages_InvitedUsers
	}).To_Messages_InvitedUsers().GetUpdates().GetChats() {
		if c.GetTitle() == "no ghost here" {
			otherId = c.GetId()
		}
	}
	if otherId == 0 {
		t.Fatal("messages.createChat reply without the chat")
	}
	if err = removeSupport(bob, otherId); err != nil {
		t.Errorf("Bob removes support from a group it was never in: %v, want the answer of a removal", err)
	}
}

// TestSupportGhostPhoneAndPrivateChat: support's number is shown to nobody,
// not even to Alice, who keeps it and whom support's privacy would let see
// it; nobody finds support by it (contacts.resolvePhone, an import answer
// as for a number without an account, a contact card that links to no
// account). It is found by its username, with its verified badge, and
// anyone writes to it: its setting that only contacts may write closes
// nothing.
func TestSupportGhostPhoneAndPrivateChat(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	support := login(ctx, t, phones[2], "Support")
	number := strings.TrimPrefix(support.phone, "+")

	for _, key := range []string{mtproto.Predicate_inputPrivacyKeyPhoneNumber, mtproto.Predicate_inputPrivacyKeyAddedByPhone} {
		mustSetPrivacy(ctx, t, support, key, ruleOf(support, mtproto.Predicate_inputPrivacyValueAllowAll))
	}
	if _, err := support.c.Invoke(ctx, &mtproto.TLAccountSetGlobalPrivacySettings{
		Constructor: support.c.Constructor(mtproto.Predicate_account_setGlobalPrivacySettings),
		Settings: &mtproto.GlobalPrivacySettings{
			Constructor:                      support.c.Constructor(mtproto.Predicate_globalPrivacySettings),
			NewNoncontactPeersRequirePremium: true,
		},
	}); err != nil {
		t.Fatalf("account.setGlobalPrivacySettings: %v", err)
	}
	// Alice keeps the number from before.
	if r := importContacts(ctx, t, alice, phoneContact(alice.c, 1, number, "Help", "")); len(r.GetUsers()) != 1 || r.GetUsers()[0].GetPhone().GetValue() != number {
		t.Fatalf("Alice's import before: %v", r.GetUsers())
	}
	name := makeGhost(ctx, t, support)
	admin(t, "user", "verify", "@"+name, "on")

	phoneOf := func(u *user) (string, bool) {
		t.Helper()
		raw, err := u.c.InvokeRaw(ctx, &mtproto.TLUsersGetUsers{
			Constructor: u.c.Constructor(mtproto.Predicate_users_getUsers),
			Id:          []*mtproto.InputUser{inputUserOfPeer(u.c, ghostPeer(ctx, t, u, name))},
		})
		if err != nil {
			t.Fatalf("users.getUsers: %v", err)
		}
		us, err := mtclient.DecodeVector(raw)
		if err != nil || len(us) != 1 {
			t.Fatalf("users.getUsers: %d users, %v", len(us), err)
		}
		x := us[0].(interface{ To_User() *mtproto.User }).To_User()
		return x.GetPhone().GetValue(), x.GetContactRequirePremium()
	}
	for _, u := range []*user{alice, bob} {
		if p, premiumOnly := phoneOf(u); p != "" || premiumOnly {
			t.Errorf("%s sees support's phone %q, contact_require_premium %v", u.self.GetFirstName().GetValue(), p, premiumOnly)
		}
	}
	if g := resolveGhost(ctx, t, bob, name); g.GetPhone() != nil || !g.GetVerified() {
		t.Errorf("support found by its username: phone %v, verified %v", g.GetPhone(), g.GetVerified())
	}
	raw, err := support.c.InvokeRaw(ctx, &mtproto.TLUsersGetUsers{
		Constructor: support.c.Constructor(mtproto.Predicate_users_getUsers),
		Id:          []*mtproto.InputUser{{Constructor: support.c.Constructor(mtproto.Predicate_inputUserSelf)}},
	})
	if err != nil {
		t.Fatalf("users.getUsers of itself: %v", err)
	}
	if us, err := mtclient.DecodeVector(raw); err != nil || len(us) != 1 || us[0].(interface{ To_User() *mtproto.User }).To_User().GetPhone().GetValue() != number {
		t.Errorf("support's own user: %v, %v", us, err)
	}

	_, err = bob.c.Invoke(ctx, &mtproto.TLContactsResolvePhone{
		Constructor: bob.c.Constructor(mtproto.Predicate_contacts_resolvePhone),
		Phone:       number,
	})
	if !mtclient.IsRPCError(err, "PHONE_NOT_OCCUPIED") {
		t.Errorf("contacts.resolvePhone of support's number: %v, want PHONE_NOT_OCCUPIED", err)
	}
	r := importContacts(ctx, t, bob, phoneContact(bob.c, 7, number, "Help", ""))
	if len(r.GetImported()) != 0 || len(r.GetUsers()) != 0 || len(r.GetPopularInvites()) != 1 {
		t.Errorf("Bob's import of support's number: imported %v, users %v, popular %v", r.GetImported(), r.GetUsers(), r.GetPopularInvites())
	}

	// A contact card with support's number links to no account, but its
	// own card does; a card of an ordinary number links as before.
	contactCard := func(u *user, phone string) *mtproto.InputMedia {
		return &mtproto.InputMedia{
			Constructor: u.c.Constructor(mtproto.Predicate_inputMediaContact),
			PhoneNumber: phone,
			FirstName:   "Help",
			Vcard:       "",
		}
	}
	uploaded := func(u *user, phone string) int64 {
		t.Helper()
		o, err := u.c.Invoke(ctx, &mtproto.TLMessagesUploadMedia{
			Constructor: u.c.Constructor(mtproto.Predicate_messages_uploadMedia),
			Peer:        &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerSelf)},
			Media:       contactCard(u, phone),
		})
		if err != nil {
			t.Fatalf("messages.uploadMedia: %v", err)
		}
		return o.(interface{ To_MessageMedia() *mtproto.MessageMedia }).To_MessageMedia().GetUserId()
	}
	if id := uploaded(bob, number); id != 0 {
		t.Errorf("Bob's card of support's number links to %d", id)
	}
	if id := uploaded(support, number); id != support.self.GetId() {
		t.Errorf("support's own card links to %d, want %d", id, support.self.GetId())
	}
	if id := uploaded(bob, strings.TrimPrefix(alice.phone, "+")); id != alice.self.GetId() {
		t.Errorf("Bob's card of Alice's number links to %d, want %d", id, alice.self.GetId())
	}
	r2, _ := sendPollMedia(ctx, t, bob, &mtproto.InputPeer{Constructor: bob.c.Constructor(mtproto.Predicate_inputPeerSelf)}, contactCard(bob, number))
	for _, x := range r2.GetUpdates() {
		if x.GetPredicateName() == mtproto.Predicate_updateNewMessage {
			if id := x.GetMessage_MESSAGE().GetMedia().GetUserId(); id != 0 {
				t.Errorf("the sent card of support's number links to %d", id)
			}
		}
	}

	getState(ctx, t, support)
	sendMessageID(ctx, t, bob, ghostPeer(ctx, t, bob, name), "hello, support", randomID())
	if _, err = support.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, x := range pushedUpdates(us) {
			if x.GetPredicateName() == mtproto.Predicate_updateNewMessage && x.GetMessage_MESSAGE().GetMessage() == "hello, support" {
				return true
			}
		}
		return us.GetPredicateName() == mtproto.Predicate_updateShortMessage && us.GetMessage() == "hello, support"
	}); err != nil {
		t.Errorf("support did not get Bob's message: %v", err)
	}
}
