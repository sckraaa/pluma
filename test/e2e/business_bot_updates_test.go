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
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/mtclient"
)

// waitBusinessUpdate waits for the push to the bot of an update of
// predicate that match takes and returns it.
func waitBusinessUpdate(ctx context.Context, t *testing.T, bot *user, predicate string, match func(*mtproto.Update) bool) *mtproto.Update {
	t.Helper()
	var found *mtproto.Update
	if _, err := bot.c.WaitUpdate(ctx, func(us *mtproto.Updates) bool {
		for _, u := range us.GetUpdates() {
			if u.GetPredicateName() == predicate && match(u) {
				found = u
				return true
			}
		}
		return false
	}); err != nil {
		t.Fatalf("no %s: %v", predicate, err)
	}
	return found
}

// What the connected bot of Telegram Business is told (task 105, part 7):
// its connection (updateBotBusinessConnect) and the messages of the chat
// it serves, new, replied to, edited and deleted, each with the next qts of
// its authorization; nothing of a chat it is paused in. A bot that missed
// the pushes gets the same updates from updates.getDifference by qts, which
// confirms them; account.getBotBusinessConnection gives it the connection
// and refuses anyone else; the disconnection is told with disabled.
func TestBusinessBotUpdates(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	username := fmt.Sprintf("e2e%dbot", randomID()&0xffffff)
	bot, _ := botLoggedInWithToken(ctx, t, phones[0], username, mtclient.DefaultLayer)
	if out := admin(t, "bot", "set", username, "-business", "on"); !strings.Contains(out, "updated") {
		t.Fatalf("bot set -business on: %q", out)
	}
	start := getState(ctx, t, bot)
	botPeer := resolveBot(ctx, t, alice, username)
	toBob := inputPeerOf(ctx, t, alice, bob)
	toAlice := inputPeerOf(ctx, t, bob, alice)
	aliceId, bobId := alice.self.GetId(), bob.self.GetId()

	connect := func(deleted bool) {
		t.Helper()
		in := &mtproto.TLAccountUpdateConnectedBot{
			Constructor: alice.c.Constructor(mtproto.Predicate_account_updateConnectedBot),
			Deleted:     deleted,
			Rights:      &mtproto.BusinessBotRights{Constructor: alice.c.Constructor(mtproto.Predicate_businessBotRights), Reply: true},
			Bot:         inputUserOfPeer(alice.c, botPeer),
			Recipients_INPUTBUSINESSBOTRECIPIENTS: &mtproto.InputBusinessBotRecipients{
				Constructor:   alice.c.Constructor(mtproto.Predicate_inputBusinessBotRecipients),
				ExistingChats: true,
				NewChats:      true,
			},
		}
		if _, err := alice.c.Invoke(ctx, in); err != nil {
			t.Fatalf("account.updateConnectedBot (deleted %v): %v", deleted, err)
		}
	}

	connect(false)
	qts := start.GetQts()
	// next checks that u, the update waited for, came with the next qts.
	next := func(u *mtproto.Update) *mtproto.Update {
		t.Helper()
		if u.GetQts() != qts+1 {
			t.Fatalf("%s with qts %d after %d", u.GetPredicateName(), u.GetQts(), qts)
		}
		qts = u.GetQts()
		return u
	}
	anyUpdate := func(*mtproto.Update) bool { return true }
	conn := next(waitBusinessUpdate(ctx, t, bot, mtproto.Predicate_updateBotBusinessConnect, anyUpdate)).GetConnection()
	if conn.GetUserId() != aliceId || conn.GetDisabled() || conn.GetConnectionId() == "" || conn.GetDcId() != 1 || !conn.GetRights().GetReply() {
		t.Fatalf("the connection: %v", conn)
	}
	id := conn.GetConnectionId()
	ofConnection := func(u *mtproto.Update) bool { return u.GetConnectionId() == id }

	// Bob writes, Alice answers him with a reply, edits and deletes it.
	sendMessageID(ctx, t, bob, toAlice, "hello shop", randomID())
	in := next(waitBusinessUpdate(ctx, t, bot, mtproto.Predicate_updateBotNewBusinessMessage, ofConnection)).GetMessage_MESSAGE()
	if in.GetMessage() != "hello shop" || in.GetOut() || in.GetPeerId().GetUserId() != bobId {
		t.Fatalf("the message of Bob: %v", in)
	}
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        toBob,
		Message:     "we are open",
		RandomId:    randomID(),
		ReplyTo:     &mtproto.InputReplyTo{Constructor: alice.c.Constructor(mtproto.Predicate_inputReplyToMessage), ReplyToMsgId: in.GetId()},
	})
	if err != nil {
		t.Fatalf("messages.sendMessage: %v", err)
	}
	var answer int32
	for _, u := range o.(interface{ To_Updates() *mtproto.Updates }).To_Updates().GetUpdates() {
		if u.GetPredicateName() == mtproto.Predicate_updateMessageID {
			answer = u.GetId_INT32()
		}
	}
	out := next(waitBusinessUpdate(ctx, t, bot, mtproto.Predicate_updateBotNewBusinessMessage, ofConnection))
	if m := out.GetMessage_MESSAGE(); m.GetMessage() != "we are open" || !m.GetOut() || m.GetId() != answer || m.GetPeerId().GetUserId() != bobId {
		t.Fatalf("the answer of Alice: %v", m)
	}
	if r := out.GetReplyToMessage(); r.GetId() != in.GetId() || r.GetMessage() != "hello shop" {
		t.Fatalf("reply_to_message: %v", r)
	}
	editText(ctx, t, alice, toBob, answer, "we are open till 6")
	edited := next(waitBusinessUpdate(ctx, t, bot, mtproto.Predicate_updateBotEditBusinessMessage, ofConnection)).GetMessage_MESSAGE()
	if edited.GetId() != answer || edited.GetMessage() != "we are open till 6" {
		t.Fatalf("the edit: %v", edited)
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLMessagesDeleteMessages{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_deleteMessages), Revoke: true, Id: []int32{answer},
	}); err != nil {
		t.Fatalf("messages.deleteMessages: %v", err)
	}
	gone := next(waitBusinessUpdate(ctx, t, bot, mtproto.Predicate_updateBotDeleteBusinessMessage, ofConnection))
	if gone.GetPeer_PEER().GetUserId() != bobId || len(gone.GetMessages()) != 1 || gone.GetMessages()[0] != answer {
		t.Fatalf("the deletion: %v", gone)
	}
	last := qts

	// Paused in the chat with Bob, the bot is told nothing of it: the
	// message reaches Alice's box, whose transaction would have told the
	// bot.
	if _, err = alice.c.Invoke(ctx, &mtproto.TLAccountToggleConnectedBotPaused{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_toggleConnectedBotPaused),
		Peer:        toBob,
		Paused:      mtproto.BoolTrue,
	}); err != nil {
		t.Fatalf("account.toggleConnectedBotPaused: %v", err)
	}
	before := getState(ctx, t, alice)
	sendMessageID(ctx, t, bob, toAlice, "are you there", randomID())
	waitDifference(ctx, t, alice, before, "are you there", 1)

	// The bot that missed the pushes gets them by qts from its start; the
	// difference confirms them, and the next one from its qts has none.
	o, err = bot.c.Invoke(ctx, &mtproto.TLUpdatesGetDifference{
		Constructor: bot.c.Constructor(mtproto.Predicate_updates_getDifference),
		Pts:         start.GetPts(),
		Date:        start.GetDate(),
		Qts:         start.GetQts(),
	})
	if err != nil {
		t.Fatalf("updates.getDifference: %v", err)
	}
	d := o.(interface {
		To_Updates_Difference() *mtproto.Updates_Difference
	}).To_Updates_Difference()
	var got []int32
	for _, u := range d.GetOtherUpdates() {
		if u.GetQts() != 0 {
			got = append(got, u.GetQts())
			if m := u.GetMessage_MESSAGE(); m != nil && m.GetMessage() == "are you there" {
				t.Fatalf("a message of a paused chat in the difference: %v", u)
			}
		}
	}
	if len(got) != int(last-start.GetQts()) || got[0] != start.GetQts()+1 || got[len(got)-1] != last {
		t.Fatalf("qts of the difference %v, want %d..%d", got, start.GetQts()+1, last)
	}
	if d.GetState().GetQts() != last {
		t.Fatalf("state qts %d, want %d", d.GetState().GetQts(), last)
	}
	hasUser := func(users []*mtproto.User, id int64) bool {
		for _, u := range users {
			if u.GetId() == id {
				return true
			}
		}
		return false
	}
	if !hasUser(d.GetUsers(), aliceId) || !hasUser(d.GetUsers(), bobId) {
		t.Fatalf("users of the difference: %v", d.GetUsers())
	}
	o, err = bot.c.Invoke(ctx, &mtproto.TLUpdatesGetDifference{
		Constructor: bot.c.Constructor(mtproto.Predicate_updates_getDifference),
		Pts:         d.GetState().GetPts(),
		Date:        d.GetState().GetDate(),
		Qts:         last,
	})
	if err != nil {
		t.Fatalf("updates.getDifference: %v", err)
	}
	for _, u := range o.(interface {
		To_Updates_Difference() *mtproto.Updates_Difference
	}).To_Updates_Difference().GetOtherUpdates() {
		if u.GetQts() != 0 {
			t.Fatalf("a confirmed update again: %v", u)
		}
	}

	// The connection asked for by its bot, and by someone else.
	o, err = bot.c.Invoke(ctx, &mtproto.TLAccountGetBotBusinessConnection{
		Constructor:  bot.c.Constructor(mtproto.Predicate_account_getBotBusinessConnection),
		ConnectionId: id,
	})
	if err != nil {
		t.Fatalf("account.getBotBusinessConnection: %v", err)
	}
	got1 := o.(interface{ To_Updates() *mtproto.Updates }).To_Updates()
	if len(got1.GetUpdates()) != 1 || got1.GetUpdates()[0].GetConnection().GetConnectionId() != id ||
		got1.GetUpdates()[0].GetQts() != last || !hasUser(got1.GetUsers(), aliceId) {
		t.Fatalf("account.getBotBusinessConnection: %v", got1)
	}
	if _, err = alice.c.Invoke(ctx, &mtproto.TLAccountGetBotBusinessConnection{
		Constructor:  alice.c.Constructor(mtproto.Predicate_account_getBotBusinessConnection),
		ConnectionId: id,
	}); !strings.Contains(fmt.Sprint(err), "BUSINESS_CONNECTION_INVALID") {
		t.Fatalf("account.getBotBusinessConnection by the account: %v", err)
	}

	// Disconnected: the bot is told so, with the same connection.
	connect(true)
	off := waitBusinessUpdate(ctx, t, bot, mtproto.Predicate_updateBotBusinessConnect, func(u *mtproto.Update) bool {
		return u.GetConnection().GetDisabled() && u.GetQts() == last+1
	}).GetConnection()
	if off.GetConnectionId() != id || off.GetUserId() != aliceId {
		t.Fatalf("the disconnection: %v", off)
	}
}
