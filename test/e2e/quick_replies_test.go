//go:build e2e

package e2e

import (
	"context"
	"crypto/md5"
	"encoding/binary"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// Quick replies of Telegram Business (task 105): a message sent to Saved
// Messages with quick_reply_shortcut by name creates the shortcut, as
// tdesktop sends it, and by id adds to it, as iOS does once it knows the
// id; the other device learns both with seq; the list comes back with the
// clients' hash; renaming, ordering and deleting reach the other device.
func TestQuickReplies(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	laptop := login(ctx, t, phones[0], "Alice")
	getState(ctx, t, laptop)

	self := &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerSelf)}
	byName := func(name string) *mtproto.InputQuickReplyShortcut {
		return &mtproto.InputQuickReplyShortcut{Constructor: alice.c.Constructor(mtproto.Predicate_inputQuickReplyShortcut), Shortcut: name}
	}
	byId := func(id int32) *mtproto.InputQuickReplyShortcut {
		return &mtproto.InputQuickReplyShortcut{Constructor: alice.c.Constructor(mtproto.Predicate_inputQuickReplyShortcutId), ShortcutId: id}
	}
	send := func(peer *mtproto.InputPeer, shortcut *mtproto.InputQuickReplyShortcut, text string, randomId int64) (*mtproto.Updates, error) {
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
			Constructor:        alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
			Peer:               peer,
			Message:            text,
			RandomId:           randomId,
			QuickReplyShortcut: shortcut,
		})
		if err != nil {
			return nil, err
		}
		u, ok := o.(interface{ To_Updates() *mtproto.Updates })
		if !ok {
			t.Fatalf("messages.sendMessage to a shortcut: %T", o)
		}
		return u.To_Updates(), nil
	}
	// added reads the answer to an add: the shortcut it created, if any, and
	// the message with the id updateMessageID gave the random id.
	added := func(r *mtproto.Updates, randomId int64) (*mtproto.QuickReply, *mtproto.Message) {
		t.Helper()
		var (
			created *mtproto.QuickReply
			message *mtproto.Message
			id      int32
		)
		for _, u := range r.GetUpdates() {
			switch u.GetPredicateName() {
			case mtproto.Predicate_updateNewQuickReply:
				created = u.GetQuickReply()
			case mtproto.Predicate_updateMessageID:
				if u.GetRandomId() == randomId {
					id = u.GetId_INT32()
				}
			case mtproto.Predicate_updateQuickReplyMessage:
				message = u.GetMessage_MESSAGE()
			}
		}
		if message == nil || message.GetId() != id || message.GetQuickReplyShortcutId() == nil {
			t.Fatalf("the answer to an add: %v", r.GetUpdates())
		}
		return created, message
	}
	quickReplies := func(hash int64) *mtproto.Messages_QuickReplies {
		t.Helper()
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetQuickReplies{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_getQuickReplies),
			Hash:        hash,
		})
		if err != nil {
			t.Fatalf("messages.getQuickReplies: %v", err)
		}
		return o.(interface {
			To_Messages_QuickReplies() *mtproto.Messages_QuickReplies
		}).To_Messages_QuickReplies()
	}
	names := func(r *mtproto.Messages_QuickReplies) []string {
		var out []string
		for _, q := range r.GetQuickReplies() {
			out = append(out, q.GetShortcut())
		}
		return out
	}
	boolOK := func(method string, o mtproto.TLObject, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		if b, _ := o.(interface{ To_Bool() *mtproto.Bool }); b == nil || !mtproto.FromBool(b.To_Bool()) {
			t.Fatalf("%s: %v, want boolTrue", method, o)
		}
	}

	// Created by name: updateNewQuickReply first in the answer and on the
	// other device.
	rid := randomID()
	r, err := send(self, byName("hello"), "Hello! How can I help?", rid)
	if err != nil {
		t.Fatalf("messages.sendMessage to a new shortcut: %v", err)
	}
	created, first := added(r, rid)
	if created == nil || created.GetShortcut() != "hello" || created.GetCount() != 1 || created.GetTopMessage() != first.GetId() {
		t.Fatalf("the shortcut created: %v", created)
	}
	hello := created.GetShortcutId()
	if first.GetQuickReplyShortcutId().GetValue() != hello || !first.GetOut() || first.GetPeerId().GetUserId() != alice.self.GetId() {
		t.Errorf("the message of the shortcut: %v", first)
	}
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateNewQuickReply, func(u *mtproto.Update) bool {
		return u.GetQuickReply().GetShortcutId() == hello
	})
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateQuickReplyMessage, func(u *mtproto.Update) bool {
		return u.GetMessage_MESSAGE().GetId() == first.GetId()
	})

	// Added to by id, then by the name in another case.
	rid = randomID()
	if r, err = send(self, byId(hello), "We are open 9 to 18.", rid); err != nil {
		t.Fatalf("messages.sendMessage to a shortcut by id: %v", err)
	}
	if again, second := added(r, rid); again != nil || second.GetQuickReplyShortcutId().GetValue() != hello {
		t.Errorf("an add by id created %v, message %v", again, second)
	}
	rid = randomID()
	if r, err = send(self, byName("HELLO"), "Bye.", rid); err != nil {
		t.Fatalf("messages.sendMessage to a shortcut by name: %v", err)
	}
	if again, _ := added(r, rid); again != nil {
		t.Errorf("the same name in another case created %v", again)
	}

	// Only to the account itself.
	bob := login(ctx, t, phones[1], "Bob")
	bobForAlice := inputUserOf(ctx, t, alice, bob)
	toBob := &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerUser), UserId: bobForAlice.GetUserId(), AccessHash: bobForAlice.GetAccessHash()}
	if _, err = send(toBob, byName("hello"), "x", randomID()); !mtclient.IsRPCError(err, "PEER_ID_INVALID") {
		t.Errorf("a shortcut message to another user: %v, want PEER_ID_INVALID", err)
	}
	if _, err = send(self, byName("hello there"), "x", randomID()); !mtclient.IsRPCError(err, "SHORTCUT_INVALID") {
		t.Errorf("an invalid name: %v, want SHORTCUT_INVALID", err)
	}

	// The list, its hash and the messages.
	list := quickReplies(0)
	if len(list.GetQuickReplies()) != 1 || list.GetQuickReplies()[0].GetCount() != 3 || list.GetQuickReplies()[0].GetTopMessage() != first.GetId() {
		t.Fatalf("messages.getQuickReplies: %v", list.GetQuickReplies())
	}
	if len(list.GetMessages()) != 1 || list.GetMessages()[0].GetId() != first.GetId() {
		t.Errorf("the top messages: %v", list.GetMessages())
	}
	if nm := quickReplies(quickRepliesHash(list)); nm.GetPredicateName() != mtproto.Predicate_messages_quickRepliesNotModified {
		t.Errorf("the same hash: %s", nm.GetPredicateName())
	}
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetQuickReplyMessages{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_getQuickReplyMessages),
		ShortcutId:  hello,
	})
	if err != nil {
		t.Fatalf("messages.getQuickReplyMessages: %v", err)
	}
	if msgs := o.(interface {
		To_Messages_Messages() *mtproto.Messages_Messages
	}).To_Messages_Messages().GetMessages(); len(msgs) != 3 || msgs[2].GetId() != first.GetId() {
		t.Errorf("the messages of the shortcut: %v", msgs)
	}

	// A second shortcut; renaming onto a name taken, renaming, ordering.
	rid = randomID()
	if r, err = send(self, byName("away"), "I am away.", rid); err != nil {
		t.Fatal(err)
	}
	away, _ := added(r, rid)
	edit := func(id int32, name string) (mtproto.TLObject, error) {
		return alice.c.Invoke(ctx, &mtproto.TLMessagesEditQuickReplyShortcut{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_editQuickReplyShortcut),
			ShortcutId:  id,
			Shortcut:    name,
		})
	}
	if _, err = edit(away.GetShortcutId(), "Hello"); !mtclient.IsRPCError(err, "SHORTCUT_OCCUPIED") {
		t.Errorf("a name taken: %v, want SHORTCUT_OCCUPIED", err)
	}
	o, err = edit(away.GetShortcutId(), "later")
	boolOK("messages.editQuickReplyShortcut", o, err)
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateQuickReplies, func(u *mtproto.Update) bool {
		for _, q := range u.GetQuickReplies() {
			if q.GetShortcut() == "later" {
				return true
			}
		}
		return false
	})
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesReorderQuickReplies{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_reorderQuickReplies),
		Order:       []int32{away.GetShortcutId(), hello},
	})
	boolOK("messages.reorderQuickReplies", o, err)
	if got := names(quickReplies(0)); len(got) != 2 || got[0] != "later" || got[1] != "hello" {
		t.Errorf("the order: %v", got)
	}

	// Deleting: updateDeleteQuickReply alone on the other device.
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesDeleteQuickReplyShortcut{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_deleteQuickReplyShortcut),
		ShortcutId:  hello,
	})
	boolOK("messages.deleteQuickReplyShortcut", o, err)
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateDeleteQuickReply, func(u *mtproto.Update) bool {
		return u.GetShortcutId() == hello
	})
	if got := names(quickReplies(0)); len(got) != 1 || got[0] != "later" {
		t.Errorf("after the delete: %v", got)
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLMessagesGetQuickReplyMessages{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_getQuickReplyMessages),
		ShortcutId:  hello,
	}); !mtclient.IsRPCError(err, "SHORTCUT_INVALID") {
		t.Errorf("the messages of a deleted shortcut: %v, want SHORTCUT_INVALID", err)
	}
}

// quickRepliesHash is the hash of messages.getQuickReplies as Android
// counts it (ui/Business/QuickRepliesController.java:203-215): for each
// shortcut its id, the first 8 bytes of the MD5 of its name, its top
// message and that message's edit date.
func quickRepliesHash(r *mtproto.Messages_QuickReplies) int64 {
	var h uint64
	add := func(v uint64) {
		h ^= h >> 21
		h ^= h << 35
		h ^= h >> 4
		h += v
	}
	for _, q := range r.GetQuickReplies() {
		var edit int32
		for _, m := range r.GetMessages() {
			if m.GetId() == q.GetTopMessage() {
				edit = m.GetEditDate().GetValue()
			}
		}
		add(uint64(q.GetShortcutId()))
		add(md5Prefix(q.GetShortcut()))
		add(uint64(q.GetTopMessage()))
		add(uint64(edit))
	}
	return int64(h)
}

// md5Prefix is the first 8 bytes of the MD5 of s as a big-endian number.
func md5Prefix(s string) uint64 {
	sum := md5.Sum([]byte(s))
	return binary.BigEndian.Uint64(sum[:8])
}
