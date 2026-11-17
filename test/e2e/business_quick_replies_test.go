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

// bizQuickReplyUpdates is the answer of an add to a shortcut: the messages
// of updateQuickReplyMessage in order, and the id updateMessageID gave
// each random id.
func bizQuickReplyUpdates(t *testing.T, method string, o mtproto.TLObject, err error) ([]*mtproto.Message, map[int64]int32) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	u, ok := o.(interface{ To_Updates() *mtproto.Updates })
	if !ok {
		t.Fatalf("%s: %T", method, o)
	}
	var messages []*mtproto.Message
	ids := map[int64]int32{}
	for _, x := range u.To_Updates().GetUpdates() {
		switch x.GetPredicateName() {
		case mtproto.Predicate_updateQuickReplyMessage:
			messages = append(messages, x.GetMessage_MESSAGE())
		case mtproto.Predicate_updateMessageID:
			ids[x.GetRandomId()] = x.GetId_INT32()
		}
	}
	return messages, ids
}

// bizQuickReplyMessagesHash is the hash of messages.getQuickReplyMessages
// as tdesktop counts it (data/business/data_shortcut_messages.cpp:763-781,
// countListHash): for each message, newest first, its id and its edit
// date. messages are newest first, as the method answers.
func bizQuickReplyMessagesHash(messages []*mtproto.Message) int64 {
	var h uint64
	add := func(v uint64) {
		h ^= h >> 21
		h ^= h << 35
		h ^= h >> 4
		h += v
	}
	for _, m := range messages {
		add(uint64(m.GetId()))
		add(uint64(m.GetEditDate().GetValue()))
	}
	return int64(h)
}

// TestBusinessQuickReplyLimits: the limits and the repeats of quick
// replies (task 105): a shortcut takes quick_reply_messages_limit (20)
// messages and refuses the next with REPLY_MESSAGES_TOO_MUCH, while a send
// repeated with a random id already taken answers the message it added;
// checkQuickReplyShortcut tells whether a name can take one more; the
// messages come back NotModified for the clients' hash; a shortcut has no
// schedule date; sendQuickReplyMessages wants a random id per message and
// a person to send to.
func TestBusinessQuickReplyLimits(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, bob)

	self := &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerSelf)}
	faq := &mtproto.InputQuickReplyShortcut{Constructor: alice.c.Constructor(mtproto.Predicate_inputQuickReplyShortcut), Shortcut: "faq"}
	add := func(text string, randomId int64) (mtproto.TLObject, error) {
		return alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
			Constructor:        alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
			Peer:               self,
			Message:            text,
			RandomId:           randomId,
			QuickReplyShortcut: faq,
		})
	}
	check := func(name string, want bool) {
		t.Helper()
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesCheckQuickReplyShortcut{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_checkQuickReplyShortcut),
			Shortcut:    name,
		})
		if err != nil {
			t.Fatalf("messages.checkQuickReplyShortcut %q: %v", name, err)
		}
		if got := mtproto.FromBool(o.(interface{ To_Bool() *mtproto.Bool }).To_Bool()); got != want {
			t.Errorf("messages.checkQuickReplyShortcut %q: %v, want %v", name, got, want)
		}
	}

	check("faq", true)
	check("not a name", false)

	firstRandom := randomID()
	o, err := add("Question 1", firstRandom)
	messages, ids := bizQuickReplyUpdates(t, "messages.sendMessage to a shortcut", o, err)
	if len(messages) != 1 || ids[firstRandom] != messages[0].GetId() {
		t.Fatalf("the first message of the shortcut: %v, ids %v", messages, ids)
	}
	first := messages[0]
	shortcutId := first.GetQuickReplyShortcutId().GetValue()
	for i := 2; i <= 20; i++ {
		o, err := add(fmt.Sprintf("Question %d", i), randomID())
		bizQuickReplyUpdates(t, "messages.sendMessage to a shortcut", o, err)
	}

	// Full: the 21st is refused, a repeat is answered with what it added.
	if _, err = add("Question 21", randomID()); !mtclient.IsRPCError(err, "REPLY_MESSAGES_TOO_MUCH") {
		t.Errorf("the 21st message: %v, want REPLY_MESSAGES_TOO_MUCH", err)
	}
	o, err = add("Question 1", firstRandom)
	messages, ids = bizQuickReplyUpdates(t, "a repeated send to a shortcut", o, err)
	if len(messages) != 1 || messages[0].GetId() != first.GetId() || ids[firstRandom] != first.GetId() {
		t.Errorf("the repeat: %v, ids %v, want message %d", messages, ids, first.GetId())
	}
	check("faq", false)
	check("FAQ", false)
	check("other", true)

	// The messages and their hash.
	get := func(hash int64) *mtproto.Messages_Messages {
		t.Helper()
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetQuickReplyMessages{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_getQuickReplyMessages),
			ShortcutId:  shortcutId,
			Hash:        hash,
		})
		if err != nil {
			t.Fatalf("messages.getQuickReplyMessages: %v", err)
		}
		return o.(interface {
			To_Messages_Messages() *mtproto.Messages_Messages
		}).To_Messages_Messages()
	}
	all := get(0)
	if len(all.GetMessages()) != 20 || all.GetMessages()[19].GetId() != first.GetId() || all.GetMessages()[0].GetMessage() != "Question 20" {
		t.Fatalf("the messages of a full shortcut: %d, newest %q", len(all.GetMessages()), all.GetMessages()[0].GetMessage())
	}
	if nm := get(bizQuickReplyMessagesHash(all.GetMessages())); nm.GetPredicateName() != mtproto.Predicate_messages_messagesNotModified || nm.GetCount() != 20 {
		t.Errorf("the same hash: %s, count %d", nm.GetPredicateName(), nm.GetCount())
	}

	// No schedule date for a shortcut.
	if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor:        alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:               self,
		Message:            "later",
		RandomId:           randomID(),
		ScheduleDate:       wrapperspb.Int32(int32(time.Now().Add(time.Hour).Unix())),
		QuickReplyShortcut: &mtproto.InputQuickReplyShortcut{Constructor: alice.c.Constructor(mtproto.Predicate_inputQuickReplyShortcut), Shortcut: "later"},
	}); !mtclient.IsRPCError(err, "SCHEDULE_DATE_INVALID") {
		t.Errorf("a shortcut with a schedule date: %v, want SCHEDULE_DATE_INVALID", err)
	}

	// sendQuickReplyMessages: a random id per message, to a person.
	send := func(peer *mtproto.InputPeer, ids []int32, randomIds []int64) error {
		_, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendQuickReplyMessages{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendQuickReplyMessages),
			Peer:        peer,
			ShortcutId:  shortcutId,
			Id:          ids,
			RandomId:    randomIds,
		})
		return err
	}
	toBob := inputPeerOf(ctx, t, alice, bob)
	two := []int32{all.GetMessages()[1].GetId(), all.GetMessages()[0].GetId()}
	if err := send(toBob, two, []int64{randomID()}); !mtclient.IsRPCError(err, "RANDOM_ID_INVALID") {
		t.Errorf("two messages with one random id: %v, want RANDOM_ID_INVALID", err)
	}
	username := fmt.Sprintf("e2e%dbot", randomID()&0xffffff)
	admin(t, "bot", "create", username, "Shop Bot", "-owner", phones[0])
	if err := send(resolveBot(ctx, t, alice, username), nil, nil); !mtclient.IsRPCError(err, "PEER_ID_INVALID") {
		t.Errorf("a shortcut sent to a bot: %v, want PEER_ID_INVALID", err)
	}
	if err := send(toBob, two, []int64{randomID(), randomID()}); err != nil {
		t.Fatalf("messages.sendQuickReplyMessages: %v", err)
	}
	waitNewMessages(ctx, t, bob, "Question 19", "Question 20")
}

// TestBusinessQuickReplyAlbum: an album added to a shortcut
// (messages.sendMultiMedia with quick_reply_shortcut) keeps its messages
// together under one grouped_id, a photo added by id (messages.sendMedia)
// stays alone, and every send of the shortcut gives the recipient the album
// under a new grouped_id of its own, the photos as photos, in the order of
// the shortcut.
func TestBusinessQuickReplyAlbum(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	laptop := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, laptop)
	getState(ctx, t, bob)

	self := &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerSelf)}
	tag := fmt.Sprint(randomID() & 0xffffff)
	front, back, alone := "front "+tag, "back "+tag, "detail "+tag
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendMultiMedia{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMultiMedia),
		Peer:        self,
		MultiMedia: []*mtproto.InputSingleMedia{
			mtproto.MakeTLInputSingleMedia(&mtproto.InputSingleMedia{Media: uploadPhoto(ctx, t, alice), RandomId: randomID(), Message: front}).To_InputSingleMedia(),
			mtproto.MakeTLInputSingleMedia(&mtproto.InputSingleMedia{Media: uploadPhoto(ctx, t, alice), RandomId: randomID(), Message: back}).To_InputSingleMedia(),
		},
		QuickReplyShortcut: &mtproto.InputQuickReplyShortcut{Constructor: alice.c.Constructor(mtproto.Predicate_inputQuickReplyShortcut), Shortcut: "catalog"},
	})
	album, _ := bizQuickReplyUpdates(t, "messages.sendMultiMedia to a shortcut", o, err)
	if len(album) != 2 || album[0].GetMessage() != front || album[1].GetMessage() != back {
		t.Fatalf("the album in the shortcut: %v", album)
	}
	group := album[0].GetGroupedId().GetValue()
	if group == 0 || album[1].GetGroupedId().GetValue() != group {
		t.Errorf("the album's grouped_id: %v and %v", album[0].GetGroupedId(), album[1].GetGroupedId())
	}
	for _, m := range album {
		if m.GetMedia().GetPredicateName() != mtproto.Predicate_messageMediaPhoto {
			t.Errorf("a photo of the shortcut: %v", m.GetMedia())
		}
	}
	shortcutId := album[0].GetQuickReplyShortcutId().GetValue()
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateQuickReplyMessage, func(u *mtproto.Update) bool {
		return u.GetMessage_MESSAGE().GetMessage() == back
	})

	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesSendMedia{
		Constructor:        alice.c.Constructor(mtproto.Predicate_messages_sendMedia),
		Peer:               self,
		Media:              uploadPhoto(ctx, t, alice),
		Message:            alone,
		RandomId:           randomID(),
		QuickReplyShortcut: &mtproto.InputQuickReplyShortcut{Constructor: alice.c.Constructor(mtproto.Predicate_inputQuickReplyShortcutId), ShortcutId: shortcutId},
	})
	single, _ := bizQuickReplyUpdates(t, "messages.sendMedia to a shortcut", o, err)
	if len(single) != 1 || single[0].GetGroupedId() != nil || single[0].GetQuickReplyShortcutId().GetValue() != shortcutId {
		t.Fatalf("the photo added by id: %v", single)
	}

	toBob := inputPeerOf(ctx, t, alice, bob)
	sendAll := func() map[string]*mtproto.Message {
		t.Helper()
		if _, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendQuickReplyMessages{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendQuickReplyMessages),
			Peer:        toBob,
			ShortcutId:  shortcutId,
		}); err != nil {
			t.Fatalf("messages.sendQuickReplyMessages: %v", err)
		}
		got, _ := waitNewMessages(ctx, t, bob, front, back, alone)
		return got
	}
	got := sendAll()
	sent := got[front].GetGroupedId().GetValue()
	if sent == 0 || sent == group || got[back].GetGroupedId().GetValue() != sent || got[alone].GetGroupedId() != nil {
		t.Errorf("the grouped_id Bob got: %v, %v, %v; of the shortcut %d", got[front].GetGroupedId(), got[back].GetGroupedId(), got[alone].GetGroupedId(), group)
	}
	if ordered := got[front].GetId() < got[back].GetId() && got[back].GetId() < got[alone].GetId(); !ordered {
		t.Errorf("the order Bob got: %d, %d, %d", got[front].GetId(), got[back].GetId(), got[alone].GetId())
	}
	for text, m := range got {
		if m.GetMedia().GetPredicateName() != mtproto.Predicate_messageMediaPhoto || m.GetQuickReplyShortcutId() != nil || m.GetOut() || m.GetPeerId().GetUserId() != alice.self.GetId() {
			t.Errorf("Bob's %q: %v", text, m)
		}
	}
	again := sendAll()
	if g := again[front].GetGroupedId().GetValue(); g == 0 || g == sent || g == group || again[back].GetGroupedId().GetValue() != g {
		t.Errorf("the second send's grouped_id: %v, %v; the first %d", again[front].GetGroupedId(), again[back].GetGroupedId(), sent)
	}
}

// TestBusinessShortcutDeleteTurnsOffGreeting: deleting the shortcut a
// greeting sends turns the greeting off with it, as Android does itself
// before it deletes one (ui/Business/QuickRepliesController.java:467-482)
// and tdesktop and iOS do not: the other device gets updateDeleteQuickReply
// and updateUser, on which it reads its userFull again; the owner's
// userFull has no greeting, and a new chat gets none.
func TestBusinessShortcutDeleteTurnsOffGreeting(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	laptop := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, laptop)
	getState(ctx, t, bob)

	const welcome = "Welcome, the greeting that is gone."
	shortcutId := bizShortcut(ctx, t, alice, "hello", welcome)
	bizInvokeTrue(ctx, t, alice, "account.updateBusinessGreetingMessage", &mtproto.TLAccountUpdateBusinessGreetingMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_updateBusinessGreetingMessage),
		Message: &mtproto.InputBusinessGreetingMessage{
			Constructor: alice.c.Constructor(mtproto.Predicate_inputBusinessGreetingMessage),
			ShortcutId:  shortcutId,
			Recipients: &mtproto.InputBusinessRecipients{
				Constructor:     alice.c.Constructor(mtproto.Predicate_inputBusinessRecipients),
				ExcludeSelected: true,
			},
			NoActivityDays: 7,
		},
	})
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateUser, func(u *mtproto.Update) bool { return u.GetUserId() == alice.self.GetId() })

	bizInvokeTrue(ctx, t, alice, "messages.deleteQuickReplyShortcut", &mtproto.TLMessagesDeleteQuickReplyShortcut{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_deleteQuickReplyShortcut),
		ShortcutId:  shortcutId,
	})
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateDeleteQuickReply, func(u *mtproto.Update) bool { return u.GetShortcutId() == shortcutId })
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateUser, func(u *mtproto.Update) bool { return u.GetUserId() == alice.self.GetId() })
	self := &mtproto.InputUser{Constructor: alice.c.Constructor(mtproto.Predicate_inputUserSelf)}
	if g := bizFullUser(ctx, t, alice, self).GetBusinessGreetingMessage(); g != nil {
		t.Fatalf("the greeting of a deleted shortcut: %v", g)
	}

	toAlice := peerOf(ctx, t, bob, alice)
	bizSend(ctx, t, bob, toAlice, "Hi, anyone greeting?")
	// The laptop is Alice's session that asked for its state and so gets
	// the pushes.
	waitNewMessage(ctx, t, laptop, "Hi, anyone greeting?")
	bizNoMore(ctx, t, bob, toAlice, welcome, 0, "a new chat after the shortcut was deleted")
}
