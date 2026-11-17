//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// The rules of the automatic replies of Telegram Business (task 105, part
// 5) that depend on time: the greeting's period without activity, the
// line between existing and new chats, the away message's offline_only
// and how often it goes. The stand cannot wait days or ten minutes, so a
// test moves the facts the rule reads back in time in the stand's MySQL:
// the dates of the messages of the owner's box (business.ChatHistoryTx,
// business.OwnerWroteSinceTx), the owner's last seen (user_presences, the
// value users.getUsers shows) and the log of the away messages
// (business_auto_reply_log).

// bizStandSQL runs query in the stand's MySQL. The password stays in the
// container's environment.
func bizStandSQL(t *testing.T, query string) {
	t.Helper()
	stack(t, "exec", "-T", "mysql", "sh", "-c", fmt.Sprintf(
		`MYSQL_PWD="$MYSQL_PASSWORD" mysql -u "$MYSQL_USER" "$MYSQL_DATABASE" -e %q`, query))
}

// bizAgeChat moves the messages of owner's box in the private chat with
// peer back by age, as if the chat had been quiet so long. The chat is
// named by its dialog ids, as business.ChatHistoryTx reads it: an incoming
// copy has the owner as its peer_id.
func bizAgeChat(t *testing.T, owner, peer *user, age time.Duration) {
	t.Helper()
	did := mtproto.MakeDialogId(owner.self.GetId(), mtproto.PEER_USER, peer.self.GetId())
	bizStandSQL(t, fmt.Sprintf("UPDATE messages SET date2 = date2 - %d WHERE user_id = %d AND dialog_id1 = %d AND dialog_id2 = %d",
		int64(age/time.Second), owner.self.GetId(), did.A, did.B))
}

// bizSetLastSeen makes u last seen ago, offline since: the row of
// user_presences that account.updateStatus writes, and its cache in Redis
// (the biz service reads the cache first).
func bizSetLastSeen(t *testing.T, u *user, ago time.Duration) {
	t.Helper()
	bizStandSQL(t, fmt.Sprintf("UPDATE user_presences SET last_seen_at = UNIX_TIMESTAMP() - %d, expires = 0 WHERE user_id = %d",
		int64(ago/time.Second), u.self.GetId()))
	stack(t, "exec", "-T", "redis", "redis-cli", "del", fmt.Sprintf("user_presences_%d", u.self.GetId()))
}

// bizNoMore waits for the worker of the automatic replies to have taken
// what it was given (it looks every second) and checks that peer's chat
// with owner, as peer sees it, holds text n times.
func bizNoMore(ctx context.Context, t *testing.T, peer *user, toOwner *mtproto.InputPeer, text string, n int, what string) {
	t.Helper()
	time.Sleep(3 * time.Second)
	if got := bizCountText(ctx, t, peer, toOwner, text); got != n {
		t.Errorf("%s: %q %d times, want %d", what, text, got, n)
	}
}

// TestBusinessGreetingInactivity: the greeting goes again only after the
// chat was quiet for no_activity_days, counted from its last message
// either way (Android BusinessGreetPeriodInfo, tdesktop
// lng_greeting_period_about): six quiet days of seven are not enough,
// eight are.
func TestBusinessGreetingInactivity(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, alice)
	getState(ctx, t, bob)

	const welcome = "Welcome back to the shop."
	shortcutId := bizShortcut(ctx, t, alice, "hello", welcome)
	bizInvokeTrue(ctx, t, alice, "account.updateBusinessGreetingMessage", &mtproto.TLAccountUpdateBusinessGreetingMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_updateBusinessGreetingMessage),
		Message: &mtproto.InputBusinessGreetingMessage{
			Constructor: alice.c.Constructor(mtproto.Predicate_inputBusinessGreetingMessage),
			ShortcutId:  shortcutId,
			Recipients: &mtproto.InputBusinessRecipients{
				Constructor:   alice.c.Constructor(mtproto.Predicate_inputBusinessRecipients),
				ExistingChats: true,
				NewChats:      true,
			},
			NoActivityDays: 7,
		},
	})
	toAlice := peerOf(ctx, t, bob, alice)

	bizSend(ctx, t, bob, toAlice, "First visit")
	waitNewMessage(ctx, t, bob, welcome)

	// Six days quiet: not greeted.
	bizAgeChat(t, alice, bob, 6*24*time.Hour)
	bizSend(ctx, t, bob, toAlice, "Back after six days")
	waitNewMessage(ctx, t, alice, "Back after six days")
	bizNoMore(ctx, t, bob, toAlice, welcome, 1, "after six quiet days")

	// Eight days quiet since that message: greeted again.
	bizAgeChat(t, alice, bob, 8*24*time.Hour)
	bizSend(ctx, t, bob, toAlice, "Back after eight days")
	m, _ := waitNewMessage(ctx, t, bob, welcome)
	if !m.GetOffline() || m.GetOut() || m.GetPeerId().GetUserId() != alice.self.GetId() {
		t.Errorf("the second greeting: %v", m)
	}
	bizNoMore(ctx, t, bob, toAlice, welcome, 2, "after eight quiet days")
}

// TestBusinessGreetingNewChats: a chat begun before the greeting was turned
// on is an existing one, a chat begun after it a new one (task 105,
// decisions to confirm, 2); a change of the setting keeps that line. The
// existing chat has been quiet past the period, so only the recipients
// keep the greeting from it.
func TestBusinessGreetingNewChats(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	getState(ctx, t, alice)
	getState(ctx, t, bob)
	getState(ctx, t, carol)
	bobToAlice := peerOf(ctx, t, bob, alice)
	carolToAlice := peerOf(ctx, t, carol, alice)

	// Carol's chat, older than the greeting and quiet for eight days.
	bizSend(ctx, t, carol, carolToAlice, "Carol long ago")
	waitNewMessage(ctx, t, alice, "Carol long ago")
	bizAgeChat(t, alice, carol, 8*24*time.Hour)

	const welcome = "Hello, new customer!"
	shortcutId := bizShortcut(ctx, t, alice, "hello", welcome)
	greeting := func(existing, fresh bool) *mtproto.TLAccountUpdateBusinessGreetingMessage {
		return &mtproto.TLAccountUpdateBusinessGreetingMessage{
			Constructor: alice.c.Constructor(mtproto.Predicate_account_updateBusinessGreetingMessage),
			Message: &mtproto.InputBusinessGreetingMessage{
				Constructor: alice.c.Constructor(mtproto.Predicate_inputBusinessGreetingMessage),
				ShortcutId:  shortcutId,
				Recipients: &mtproto.InputBusinessRecipients{
					Constructor:   alice.c.Constructor(mtproto.Predicate_inputBusinessRecipients),
					ExistingChats: existing,
					NewChats:      fresh,
				},
				NoActivityDays: 7,
			},
		}
	}

	// New chats only: Bob, who never wrote, is greeted; Carol is not.
	bizInvokeTrue(ctx, t, alice, "account.updateBusinessGreetingMessage", greeting(false, true))
	bizSend(ctx, t, bob, bobToAlice, "Hi, first time here")
	waitNewMessage(ctx, t, bob, welcome)
	bizSend(ctx, t, carol, carolToAlice, "Carol again")
	waitNewMessage(ctx, t, alice, "Carol again")
	bizNoMore(ctx, t, carol, carolToAlice, welcome, 0, "an existing chat with new chats chosen")

	// Changed to existing chats only: the line stays where it was, so
	// Carol's chat, quiet again, is greeted.
	bizInvokeTrue(ctx, t, alice, "account.updateBusinessGreetingMessage", greeting(true, false))
	bizAgeChat(t, alice, carol, 8*24*time.Hour)
	bizSend(ctx, t, carol, carolToAlice, "Carol once more")
	waitNewMessage(ctx, t, carol, welcome)
	bizNoMore(ctx, t, carol, carolToAlice, welcome, 1, "an existing chat with existing chats chosen")
}

// TestBusinessAwayOffline: the away message with offline_only waits until
// the account has been away for ten minutes ("not sent if the account was
// online in the last 10 minutes", core.telegram.org/constructor/
// inputBusinessAwayMessage); then it goes to a user once, and again only
// when a day has passed and the account wrote to the user since (task 105,
// decisions to confirm, 3). The setting is the owner's alone: another
// user's userFull of the owner has none.
func TestBusinessAwayOffline(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, alice)
	getState(ctx, t, bob)

	const away = "Closed for today, back tomorrow."
	shortcutId := bizShortcut(ctx, t, alice, "away", away)
	bizInvokeTrue(ctx, t, alice, "account.updateBusinessAwayMessage", &mtproto.TLAccountUpdateBusinessAwayMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_updateBusinessAwayMessage),
		Message: &mtproto.InputBusinessAwayMessage{
			Constructor: alice.c.Constructor(mtproto.Predicate_inputBusinessAwayMessage),
			OfflineOnly: true,
			ShortcutId:  shortcutId,
			Schedule:    &mtproto.BusinessAwayMessageSchedule{Constructor: alice.c.Constructor(mtproto.Predicate_businessAwayMessageScheduleAlways)},
			Recipients: &mtproto.InputBusinessRecipients{
				Constructor:     alice.c.Constructor(mtproto.Predicate_inputBusinessRecipients),
				ExcludeSelected: true,
			},
		},
	})
	self := &mtproto.InputUser{Constructor: alice.c.Constructor(mtproto.Predicate_inputUserSelf)}
	if a := bizFullUser(ctx, t, alice, self).GetBusinessAwayMessage(); a.GetShortcutId() != shortcutId || !a.GetOfflineOnly() ||
		a.GetSchedule().GetPredicateName() != mtproto.Predicate_businessAwayMessageScheduleAlways {
		t.Errorf("the owner's away message: %v", a)
	}
	if a := bizFullUser(ctx, t, bob, inputUserOf(ctx, t, bob, alice)).GetBusinessAwayMessage(); a != nil {
		t.Errorf("Bob sees Alice's away message: %v", a)
	}
	toAlice := peerOf(ctx, t, bob, alice)
	toBob := peerOf(ctx, t, alice, bob)

	// Offline just now: still within the ten minutes.
	bizInvokeTrue(ctx, t, alice, "account.updateStatus", &mtproto.TLAccountUpdateStatus{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_updateStatus),
		Offline:     mtproto.BoolTrue,
	})
	bizSend(ctx, t, bob, toAlice, "Are you open?")
	waitNewMessage(ctx, t, alice, "Are you open?")
	bizNoMore(ctx, t, bob, toAlice, away, 0, "the owner offline for less than ten minutes")

	// Offline for eleven minutes: it goes, marked offline.
	bizSetLastSeen(t, alice, 11*time.Minute)
	bizSend(ctx, t, bob, toAlice, "Hello?")
	m, _ := waitNewMessage(ctx, t, bob, away)
	if !m.GetOffline() || m.GetOut() || m.GetPeerId().GetUserId() != alice.self.GetId() {
		t.Errorf("the away message Bob got: %v", m)
	}
	bizSend(ctx, t, bob, toAlice, "Anyone?")
	waitNewMessage(ctx, t, alice, "Anyone?")
	bizNoMore(ctx, t, bob, toAlice, away, 1, "the same user again at once")

	// A day later, with the account silent since: not again. The chat is
	// moved back a minute more than the log, so that the away message
	// itself, written in the transaction that logged it, is not taken for
	// the account writing after it.
	bizStandSQL(t, fmt.Sprintf("UPDATE business_auto_reply_log SET sent_at = sent_at - %d WHERE user_id = %d AND peer_id = %d",
		25*3600, alice.self.GetId(), bob.self.GetId()))
	bizAgeChat(t, alice, bob, 25*time.Hour+time.Minute)
	bizSend(ctx, t, bob, toAlice, "Next day, still nobody?")
	waitNewMessage(ctx, t, alice, "Next day, still nobody?")
	bizNoMore(ctx, t, bob, toAlice, away, 1, "a day later, unanswered")

	// The account answered: the next message after the day gets it again.
	bizSend(ctx, t, alice, toBob, "Sorry, I was out.")
	waitNewMessage(ctx, t, bob, "Sorry, I was out.")
	bizSend(ctx, t, bob, toAlice, "No problem, are you there now?")
	waitNewMessage(ctx, t, bob, away)
	bizNoMore(ctx, t, bob, toAlice, away, 2, "a day later, answered")
}
