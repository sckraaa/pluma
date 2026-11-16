//go:build e2e

package e2e

import (
	"context"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// The messages of a quick reply shortcut once added (task 105): an edit
// with quick_reply_shortcut_id and a delete reach the other device, a
// forward is added to the shortcut, and
// messages.sendQuickReplyMessages sends the shortcut to a chat as tdesktop
// and iOS ask (no ids) and as Android asks (ids newest first, each with a
// random id); the recipient gets the messages from the owner, and every
// device of the owner, the calling one too, is pushed them.
func TestQuickReplyMessages(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	laptop := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, alice)
	getState(ctx, t, laptop)
	getState(ctx, t, bob)

	self := &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerSelf)}
	bobForAlice := inputUserOf(ctx, t, alice, bob)
	toBob := &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerUser), UserId: bobForAlice.GetUserId(), AccessHash: bobForAlice.GetAccessHash()}
	shortcut := &mtproto.InputQuickReplyShortcut{Constructor: alice.c.Constructor(mtproto.Predicate_inputQuickReplyShortcut), Shortcut: "promo"}
	updatesOf := func(method string, o mtproto.TLObject, err error) *mtproto.Updates {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		u, ok := o.(interface{ To_Updates() *mtproto.Updates })
		if !ok {
			t.Fatalf("%s: %T", method, o)
		}
		return u.To_Updates()
	}
	// quickReplyMessage is the message of updateQuickReplyMessage in r with
	// text.
	quickReplyMessage := func(r *mtproto.Updates, text string) *mtproto.Message {
		t.Helper()
		for _, u := range r.GetUpdates() {
			if u.GetPredicateName() == mtproto.Predicate_updateQuickReplyMessage && u.GetMessage_MESSAGE().GetMessage() == text {
				return u.GetMessage_MESSAGE()
			}
		}
		t.Fatalf("no updateQuickReplyMessage %q in %v", text, r.GetUpdates())
		return nil
	}

	// Two texts, by name as tdesktop adds them.
	var texts []*mtproto.Message
	for _, text := range []string{"Spring sale", "typo here"} {
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
			Constructor:        alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
			Peer:               self,
			Message:            text,
			RandomId:           randomID(),
			QuickReplyShortcut: shortcut,
		})
		texts = append(texts, quickReplyMessage(updatesOf("messages.sendMessage to a shortcut", o, err), text))
	}
	shortcutId := texts[0].GetQuickReplyShortcutId().GetValue()

	// A message of the chat with Bob forwarded into it, as iOS forwards.
	rid := randomID()
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        toBob,
		Message:     "Our address: Main St 1",
		RandomId:    rid,
	})
	var chatId int32
	for _, u := range updatesOf("messages.sendMessage", o, err).GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateMessageID && u.GetRandomId() == rid {
			chatId = u.GetId_INT32()
		}
	}
	if chatId == 0 {
		t.Fatal("no id for the message to Bob")
	}
	waitNewMessage(ctx, t, bob, "Our address: Main St 1")
	waitNewMessage(ctx, t, laptop, "Our address: Main St 1")
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesForwardMessages{
		Constructor:        alice.c.Constructor(mtproto.Predicate_messages_forwardMessages),
		FromPeer:           toBob,
		Id:                 []int32{chatId},
		RandomId:           []int64{randomID()},
		ToPeer:             self,
		QuickReplyShortcut: &mtproto.InputQuickReplyShortcut{Constructor: alice.c.Constructor(mtproto.Predicate_inputQuickReplyShortcutId), ShortcutId: shortcutId},
	})
	forwarded := quickReplyMessage(updatesOf("messages.forwardMessages to a shortcut", o, err), "Our address: Main St 1")
	if forwarded.GetQuickReplyShortcutId().GetValue() != shortcutId || forwarded.GetFwdFrom() == nil || forwarded.GetFwdFrom().GetSavedFromPeer() != nil {
		t.Errorf("the forward in the shortcut: %v", forwarded)
	}

	// The typo fixed: the answer and the other device.
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesEditMessage{
		Constructor:          alice.c.Constructor(mtproto.Predicate_messages_editMessage),
		Peer:                 self,
		Id:                   texts[1].GetId(),
		Message:              mtproto.MakeFlagsString("Free delivery"),
		QuickReplyShortcutId: mtproto.MakeFlagsInt32(shortcutId),
	})
	edited := quickReplyMessage(updatesOf("messages.editMessage of a shortcut", o, err), "Free delivery")
	if edited.GetId() != texts[1].GetId() || edited.GetEditDate() == nil {
		t.Errorf("the edited message: %v", edited)
	}
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateQuickReplyMessage, func(u *mtproto.Update) bool {
		return u.GetMessage_MESSAGE().GetMessage() == "Free delivery"
	})

	// A message of the shortcut deleted.
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor:        alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:               self,
		Message:            "to delete",
		RandomId:           randomID(),
		QuickReplyShortcut: shortcut,
	})
	extra := quickReplyMessage(updatesOf("messages.sendMessage to a shortcut", o, err), "to delete")
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesDeleteQuickReplyMessages{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_deleteQuickReplyMessages),
		ShortcutId:  shortcutId,
		Id:          []int32{extra.GetId()},
	})
	r := updatesOf("messages.deleteQuickReplyMessages", o, err)
	if len(r.GetUpdates()) != 1 || r.GetUpdates()[0].GetPredicateName() != mtproto.Predicate_updateDeleteQuickReplyMessages {
		t.Errorf("the answer to the delete: %v", r.GetUpdates())
	}
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateDeleteQuickReplyMessages, func(u *mtproto.Update) bool {
		return u.GetShortcutId() == shortcutId && len(u.GetMessages()) == 1 && u.GetMessages()[0] == extra.GetId()
	})

	send := func(peer *mtproto.InputPeer, ids []int32, randomIds []int64) (mtproto.TLObject, error) {
		return alice.c.Invoke(ctx, &mtproto.TLMessagesSendQuickReplyMessages{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendQuickReplyMessages),
			Peer:        peer,
			ShortcutId:  shortcutId,
			Id:          ids,
			RandomId:    randomIds,
		})
	}
	if _, err = send(self, nil, nil); !mtclient.IsRPCError(err, "PEER_ID_INVALID") {
		t.Errorf("a shortcut sent to Saved Messages: %v, want PEER_ID_INVALID", err)
	}

	// tdesktop and iOS: the whole shortcut, without ids. Bob gets the three
	// messages in order; Alice's calling session is pushed them too, as
	// Android reads no answer.
	o, err = send(toBob, nil, nil)
	updatesOf("messages.sendQuickReplyMessages", o, err)
	// The three may come in one update: they are waited for together.
	sent3 := []string{"Spring sale", "Free delivery", "Our address: Main St 1"}
	got, _ := waitNewMessages(ctx, t, bob, sent3...)
	for i, text := range sent3 {
		m := got[text]
		if m.GetOut() || m.GetPeerId().GetUserId() != alice.self.GetId() || m.GetQuickReplyShortcutId() != nil || m.GetFwdFrom() != nil {
			t.Errorf("Bob's %q: %v", text, m)
		}
		if i > 0 && m.GetId() <= got[sent3[i-1]].GetId() {
			t.Errorf("Bob's %q is not after %q", text, sent3[i-1])
		}
	}
	waitNewMessages(ctx, t, alice, sent3...)
	waitNewMessages(ctx, t, laptop, sent3...)

	// Android: the ids newest first, each with its random id.
	ids := []int32{forwarded.GetId(), texts[1].GetId(), texts[0].GetId()}
	o, err = send(toBob, ids, []int64{randomID(), randomID(), randomID()})
	r = updatesOf("messages.sendQuickReplyMessages", o, err)
	var sent []string
	for _, u := range r.GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateNewMessage {
			sent = append(sent, u.GetMessage_MESSAGE().GetMessage())
		}
	}
	if len(sent) != 3 || sent[0] != "Spring sale" || sent[2] != "Our address: Main St 1" {
		t.Errorf("sent with ids: %v, want the order the shortcut has", sent)
	}
	waitNewMessage(ctx, t, bob, "Our address: Main St 1")
}
