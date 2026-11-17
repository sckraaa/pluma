//go:build e2e

package e2e

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// bizShortcut adds text to the quick reply shortcut name of u, by name
// as tdesktop adds it, and returns the shortcut's id.
func bizShortcut(ctx context.Context, t *testing.T, u *user, name, text string) int32 {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor:        u.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:               &mtproto.InputPeer{Constructor: u.c.Constructor(mtproto.Predicate_inputPeerSelf)},
		Message:            text,
		RandomId:           randomID(),
		QuickReplyShortcut: &mtproto.InputQuickReplyShortcut{Constructor: u.c.Constructor(mtproto.Predicate_inputQuickReplyShortcut), Shortcut: name},
	})
	if err != nil {
		t.Fatalf("messages.sendMessage to shortcut %q: %v", name, err)
	}
	for _, x := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if x.GetPredicateName() == mtproto.Predicate_updateQuickReplyMessage {
			return x.GetMessage_MESSAGE().GetQuickReplyShortcutId().GetValue()
		}
	}
	t.Fatalf("no updateQuickReplyMessage for shortcut %q", name)
	return 0
}

// bizSend sends text from u to peer.
func bizSend(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, text string) {
	t.Helper()
	if _, err := u.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: u.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        peer,
		Message:     text,
		RandomId:    randomID(),
	}); err != nil {
		t.Fatalf("messages.sendMessage %q: %v", text, err)
	}
}

// bizFullUser is users.getFullUser of who as u sees it.
func bizFullUser(ctx context.Context, t *testing.T, u *user, who *mtproto.InputUser) *mtproto.UserFull {
	t.Helper()
	o, err := u.c.Invoke(ctx, &mtproto.TLUsersGetFullUser{Constructor: u.c.Constructor(mtproto.Predicate_users_getFullUser), Id: who})
	if err != nil {
		t.Fatalf("users.getFullUser: %v", err)
	}
	return o.(interface {
		To_Users_UserFull() *mtproto.Users_UserFull
	}).To_Users_UserFull().GetFullUser()
}

func bizInvokeTrue(ctx context.Context, t *testing.T, u *user, method string, req mtproto.TLObject) {
	t.Helper()
	o, err := u.c.Invoke(ctx, req)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	if b, ok := o.(interface{ To_Bool() *mtproto.Bool }); !ok || !mtproto.FromBool(b.To_Bool()) {
		t.Fatalf("%s: %v", method, o)
	}
}

// bizCountText is how many messages of u's chat with peer read text.
func bizCountText(ctx context.Context, t *testing.T, u *user, peer *mtproto.InputPeer, text string) int {
	t.Helper()
	n := 0
	for _, s := range historyTexts(ctx, t, u, peer) {
		if s == text {
			n++
		}
	}
	return n
}

// TestBusinessGreeting: a greeting of Telegram Business (task 105) goes,
// marked offline, from the owner to a new chat once; a user the owner
// left out of the recipients gets none. The owner's other device hears of
// the setting (updateUser) and the owner's userFull carries it; another
// user's view of the owner does not.
func TestBusinessGreeting(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	laptop := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	getState(ctx, t, alice)
	getState(ctx, t, laptop)
	getState(ctx, t, bob)
	getState(ctx, t, carol)

	const welcome = "Welcome! We answer within a day."
	shortcutId := bizShortcut(ctx, t, alice, "hello", welcome)
	carolForAlice := inputUserOf(ctx, t, alice, carol)
	recipients := &mtproto.InputBusinessRecipients{
		Constructor:     alice.c.Constructor(mtproto.Predicate_inputBusinessRecipients),
		ExcludeSelected: true,
		Users:           []*mtproto.InputUser{carolForAlice},
	}
	greeting := func(days int32) *mtproto.TLAccountUpdateBusinessGreetingMessage {
		return &mtproto.TLAccountUpdateBusinessGreetingMessage{
			Constructor: alice.c.Constructor(mtproto.Predicate_account_updateBusinessGreetingMessage),
			Message: &mtproto.InputBusinessGreetingMessage{
				Constructor:    alice.c.Constructor(mtproto.Predicate_inputBusinessGreetingMessage),
				ShortcutId:     shortcutId,
				Recipients:     recipients,
				NoActivityDays: days,
			},
		}
	}
	if _, err := alice.c.Invoke(ctx, greeting(10)); !mtclient.IsRPCError(err, "NO_ACTIVITY_DAYS_INVALID") {
		t.Errorf("10 days: %v, want NO_ACTIVITY_DAYS_INVALID", err)
	}
	bizInvokeTrue(ctx, t, alice, "account.updateBusinessGreetingMessage", greeting(7))
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updateUser, func(u *mtproto.Update) bool { return u.GetUserId() == alice.self.GetId() })

	self := &mtproto.InputUser{Constructor: alice.c.Constructor(mtproto.Predicate_inputUserSelf)}
	g := bizFullUser(ctx, t, alice, self).GetBusinessGreetingMessage()
	if g.GetShortcutId() != shortcutId || g.GetNoActivityDays() != 7 || !g.GetRecipients().GetExcludeSelected() ||
		!slices.Equal(g.GetRecipients().GetUsers(), []int64{carol.self.GetId()}) {
		t.Errorf("the owner's greeting: %v", g)
	}
	if g := bizFullUser(ctx, t, bob, inputUserOf(ctx, t, bob, alice)).GetBusinessGreetingMessage(); g != nil {
		t.Errorf("Bob sees Alice's greeting: %v", g)
	}

	aliceForBob := peerOf(ctx, t, bob, alice)
	aliceForCarol := peerOf(ctx, t, carol, alice)
	bizSend(ctx, t, bob, aliceForBob, "Hi, are you open?")
	m, _ := waitNewMessage(ctx, t, bob, welcome)
	if !m.GetOffline() || m.GetOut() || m.GetPeerId().GetUserId() != alice.self.GetId() {
		t.Errorf("the greeting Bob got: %v", m)
	}
	waitNewMessage(ctx, t, laptop, welcome)

	// The second message of the chat is not greeted; nor is Carol, left
	// out. Alice's own message after both shows they were taken.
	bizSend(ctx, t, bob, aliceForBob, "Hello?")
	bizSend(ctx, t, carol, aliceForCarol, "Hi from Carol")
	waitNewMessage(ctx, t, alice, "Hello?")
	waitNewMessage(ctx, t, alice, "Hi from Carol")
	time.Sleep(3 * time.Second)
	if n := bizCountText(ctx, t, bob, aliceForBob, welcome); n != 1 {
		t.Errorf("Bob has %d greetings, want 1", n)
	}
	if n := bizCountText(ctx, t, carol, aliceForCarol, welcome); n != 0 {
		t.Errorf("Carol, left out, has %d greetings", n)
	}

	// Turned off: the owner's userFull has none.
	bizInvokeTrue(ctx, t, alice, "account.updateBusinessGreetingMessage", &mtproto.TLAccountUpdateBusinessGreetingMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_updateBusinessGreetingMessage),
	})
	if g := bizFullUser(ctx, t, alice, self).GetBusinessGreetingMessage(); g != nil {
		t.Errorf("a greeting turned off: %v", g)
	}
}

// TestBusinessAway: the away message with offline_only is not sent while
// the owner is online; without it, it goes once, marked offline, and not
// again the same day.
func TestBusinessAway(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, alice)
	getState(ctx, t, bob)

	const away = "We are away until Monday."
	shortcutId := bizShortcut(ctx, t, alice, "away", away)
	set := func(offlineOnly bool) {
		t.Helper()
		bizInvokeTrue(ctx, t, alice, "account.updateBusinessAwayMessage", &mtproto.TLAccountUpdateBusinessAwayMessage{
			Constructor: alice.c.Constructor(mtproto.Predicate_account_updateBusinessAwayMessage),
			Message: &mtproto.InputBusinessAwayMessage{
				Constructor: alice.c.Constructor(mtproto.Predicate_inputBusinessAwayMessage),
				OfflineOnly: offlineOnly,
				ShortcutId:  shortcutId,
				Schedule:    &mtproto.BusinessAwayMessageSchedule{Constructor: alice.c.Constructor(mtproto.Predicate_businessAwayMessageScheduleAlways)},
				Recipients: &mtproto.InputBusinessRecipients{
					Constructor:     alice.c.Constructor(mtproto.Predicate_inputBusinessRecipients),
					ExcludeSelected: true,
				},
			},
		})
	}
	aliceForBob := peerOf(ctx, t, bob, alice)

	// Online: nothing goes.
	set(true)
	bizInvokeTrue(ctx, t, alice, "account.updateStatus", &mtproto.TLAccountUpdateStatus{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_updateStatus),
		Offline:     mtproto.BoolFalse,
	})
	bizSend(ctx, t, bob, aliceForBob, "Are you there?")
	waitNewMessage(ctx, t, alice, "Are you there?")
	time.Sleep(3 * time.Second)
	if n := bizCountText(ctx, t, bob, aliceForBob, away); n != 0 {
		t.Fatalf("an away message went to Bob while Alice is online: %d", n)
	}

	// Always: it goes once.
	set(false)
	bizSend(ctx, t, bob, aliceForBob, "Hello?")
	m, _ := waitNewMessage(ctx, t, bob, away)
	if !m.GetOffline() || m.GetOut() || m.GetPeerId().GetUserId() != alice.self.GetId() {
		t.Errorf("the away message Bob got: %v", m)
	}
	bizSend(ctx, t, bob, aliceForBob, "Anyone?")
	waitNewMessage(ctx, t, alice, "Anyone?")
	time.Sleep(3 * time.Second)
	if n := bizCountText(ctx, t, bob, aliceForBob, away); n != 1 {
		t.Errorf("Bob has %d away messages, want 1", n)
	}
}
