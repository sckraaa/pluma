//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// TestBusinessBot: the connected bot of Telegram Business through a day of
// an account (task 105, parts 5-8 together). The bot hears the chats it
// serves, the account's away message among them (an outgoing message of
// the chat, offline); a chat taken away from it (disablePeerConnectedBot)
// tells it of its changed connection and then nothing more, with no gap
// in its qts, and refuses its answers, while another chat still takes
// them; pluma-admin turning its business mode off disconnects it, tells it
// so, and its connection stops working.
func TestBusinessBot(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 3)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	carol := login(ctx, t, phones[2], "Carol")
	username := fmt.Sprintf("e2e%dbot", randomID()&0xffffff)
	bot, _ := botLoggedInWithToken(ctx, t, phones[0], username, mtclient.DefaultLayer)
	if out := admin(t, "bot", "set", username, "-business", "on"); !strings.Contains(out, "updated") {
		t.Fatalf("bot set -business on: %q", out)
	}
	getState(ctx, t, bot)
	getState(ctx, t, alice)
	getState(ctx, t, bob)
	getState(ctx, t, carol)
	botPeer := resolveBot(ctx, t, alice, username)
	bobId, carolId := bob.self.GetId(), carol.self.GetId()
	bobToAlice := inputPeerOf(ctx, t, bob, alice)
	carolToAlice := inputPeerOf(ctx, t, carol, alice)
	// The bot names the users by their ids, as it learns them from its
	// updates.
	forBot := func(id int64) *mtproto.InputPeer {
		return &mtproto.InputPeer{Constructor: bot.c.Constructor(mtproto.Predicate_inputPeerUser), UserId: id}
	}

	// Connected for all chats with the right to reply.
	if _, err := alice.c.Invoke(ctx, &mtproto.TLAccountUpdateConnectedBot{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_updateConnectedBot),
		Rights:      &mtproto.BusinessBotRights{Constructor: alice.c.Constructor(mtproto.Predicate_businessBotRights), Reply: true},
		Bot:         inputUserOfPeer(alice.c, botPeer),
		Recipients_INPUTBUSINESSBOTRECIPIENTS: &mtproto.InputBusinessBotRecipients{
			Constructor:   alice.c.Constructor(mtproto.Predicate_inputBusinessBotRecipients),
			ExistingChats: true,
			NewChats:      true,
		},
	}); err != nil {
		t.Fatalf("account.updateConnectedBot: %v", err)
	}
	connected := waitBusinessUpdate(ctx, t, bot, mtproto.Predicate_updateBotBusinessConnect, func(u *mtproto.Update) bool {
		return !u.GetConnection().GetDisabled()
	})
	id := connected.GetConnection().GetConnectionId()
	qts := connected.GetQts()
	// next waits for the update of the connection that match takes and
	// checks it came with a qts after every one before. A message names
	// the connection by connection_id, updateBotBusinessConnect inside its
	// botBusinessConnection.
	ofConnection := func(u *mtproto.Update) bool {
		if c := u.GetConnection(); c != nil {
			return c.GetConnectionId() == id
		}
		return u.GetConnectionId() == id
	}
	next := func(predicate string, match func(*mtproto.Update) bool) *mtproto.Update {
		t.Helper()
		u := waitBusinessUpdate(ctx, t, bot, predicate, func(u *mtproto.Update) bool { return ofConnection(u) && match(u) })
		if u.GetQts() <= qts {
			t.Fatalf("%s with qts %d after %d", predicate, u.GetQts(), qts)
		}
		qts = u.GetQts()
		return u
	}
	text := func(s string) func(*mtproto.Update) bool {
		return func(u *mtproto.Update) bool { return u.GetMessage_MESSAGE().GetMessage() == s }
	}

	// The away message for Bob.
	const away = "We are closed, the bot will help you."
	shortcutId := bizShortcut(ctx, t, alice, "away", away)
	bizInvokeTrue(ctx, t, alice, "account.updateBusinessAwayMessage", &mtproto.TLAccountUpdateBusinessAwayMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_updateBusinessAwayMessage),
		Message: &mtproto.InputBusinessAwayMessage{
			Constructor: alice.c.Constructor(mtproto.Predicate_inputBusinessAwayMessage),
			ShortcutId:  shortcutId,
			Schedule:    &mtproto.BusinessAwayMessageSchedule{Constructor: alice.c.Constructor(mtproto.Predicate_businessAwayMessageScheduleAlways)},
			Recipients: &mtproto.InputBusinessRecipients{
				Constructor: alice.c.Constructor(mtproto.Predicate_inputBusinessRecipients),
				Users:       []*mtproto.InputUser{inputUserOf(ctx, t, alice, bob)},
			},
		},
	})

	// Bob writes: the bot hears his message and the away message that
	// answers it, Alice's, outgoing and offline.
	bizSend(ctx, t, bob, bobToAlice, "Is the shop open?")
	if m := next(mtproto.Predicate_updateBotNewBusinessMessage, text("Is the shop open?")).GetMessage_MESSAGE(); m.GetOut() || m.GetPeerId().GetUserId() != bobId {
		t.Fatalf("Bob's message to the bot: %v", m)
	}
	if m := next(mtproto.Predicate_updateBotNewBusinessMessage, text(away)).GetMessage_MESSAGE(); !m.GetOut() || !m.GetOffline() || m.GetPeerId().GetUserId() != bobId {
		t.Fatalf("the away message to the bot: %v", m)
	}
	waitNewMessage(ctx, t, bob, away)
	bizSend(ctx, t, carol, carolToAlice, "Do you ship abroad?")
	next(mtproto.Predicate_updateBotNewBusinessMessage, text("Do you ship abroad?"))

	// The chat with Bob is taken from the bot: it is told of its
	// connection, the same one, still on.
	bizInvokeTrue(ctx, t, alice, "account.disablePeerConnectedBot", &mtproto.TLAccountDisablePeerConnectedBot{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_disablePeerConnectedBot),
		Peer:        inputPeerOf(ctx, t, alice, bob),
	})
	if c := next(mtproto.Predicate_updateBotBusinessConnect, func(*mtproto.Update) bool { return true }).GetConnection(); c.GetDisabled() || c.GetUserId() != alice.self.GetId() {
		t.Fatalf("the connection after the chat was taken: %v", c)
	}

	// Bob's next message reaches Alice and not the bot: Carol's, after it,
	// takes the very next qts.
	before := qts
	bizSend(ctx, t, bob, bobToAlice, "Hello again")
	waitNewMessage(ctx, t, alice, "Hello again")
	bizSend(ctx, t, carol, carolToAlice, "And to Canada?")
	if u := next(mtproto.Predicate_updateBotNewBusinessMessage, text("And to Canada?")); u.GetQts() != before+1 {
		t.Errorf("Carol's message with qts %d after %d: the bot was told of something else", u.GetQts(), before)
	}
	quiet, stop := context.WithTimeout(ctx, 2*time.Second)
	_, err := bot.c.WaitUpdate(quiet, func(us *mtproto.Updates) bool {
		for _, u := range us.GetUpdates() {
			if u.GetMessage_MESSAGE().GetMessage() == "Hello again" {
				return true
			}
		}
		return false
	})
	stop()
	if err == nil {
		t.Errorf("the bot heard a message of the chat taken from it")
	}

	// It may not answer Bob any more; Carol it may.
	answer := func(to int64, s string) error {
		_, err := viaConnection(ctx, bot, id, &mtproto.TLMessagesSendMessage{
			Constructor: bot.c.Constructor(mtproto.Predicate_messages_sendMessage),
			Peer:        forBot(to),
			Message:     s,
			RandomId:    randomID(),
		})
		return err
	}
	if err = answer(bobId, "Yes, until six"); !strings.Contains(fmt.Sprint(err), "BOT_ACCESS_FORBIDDEN") {
		t.Errorf("an answer in the chat taken from the bot: %v, want BOT_ACCESS_FORBIDDEN", err)
	}
	if err = answer(carolId, "Yes, we ship to Canada"); err != nil {
		t.Fatalf("an answer to Carol through the connection: %v", err)
	}
	if m, _ := waitNewMessage(ctx, t, carol, "Yes, we ship to Canada"); m.GetOut() || m.GetPeerId().GetUserId() != alice.self.GetId() {
		t.Errorf("Carol's copy of the bot's answer: %v", m)
	}

	// The business mode off: disconnected, told so; the connection is gone.
	if out := admin(t, "bot", "set", username, "-business", "off"); !strings.Contains(out, "disconnected from 1 accounts") {
		t.Fatalf("bot set -business off: %q", out)
	}
	off := waitBusinessUpdate(ctx, t, bot, mtproto.Predicate_updateBotBusinessConnect, func(u *mtproto.Update) bool {
		return u.GetConnection().GetDisabled()
	}).GetConnection()
	if off.GetConnectionId() != id || off.GetUserId() != alice.self.GetId() {
		t.Fatalf("the disconnection: %v", off)
	}
	if err = answer(carolId, "Still here"); !strings.Contains(fmt.Sprint(err), "BUSINESS_CONNECTION_INVALID") {
		t.Errorf("an answer through a connection that is gone: %v, want BUSINESS_CONNECTION_INVALID", err)
	}
	o, err := alice.c.Invoke(ctx, &mtproto.TLAccountGetConnectedBots{Constructor: alice.c.Constructor(mtproto.Predicate_account_getConnectedBots)})
	if err != nil {
		t.Fatalf("account.getConnectedBots: %v", err)
	}
	if bots := o.(interface {
		To_Account_ConnectedBots() *mtproto.Account_ConnectedBots
	}).To_Account_ConnectedBots().GetConnectedBots(); len(bots) != 0 {
		t.Errorf("connected after the business mode went off: %v", bots)
	}
}
