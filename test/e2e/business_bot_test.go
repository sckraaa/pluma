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
)

// The settings of the connected bot of Telegram Business (task 105, part
// 6): a bot without the business mode is refused; with it the account
// connects it, reads it back, sees its bar in the chat it serves (in
// messages.getPeerSettings and in userFull.settings), pauses it there and
// takes the chat away from it, the other device of the account learning
// both by updatePeerSettings; confirmBotConnection names the connected
// bot; the account disconnects it with inputUserEmpty, as iOS does, and
// pluma-admin turning the business mode off disconnects it everywhere.
func TestBusinessConnectedBotSettings(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phones := testPhones(t, 2)
	alice := login(ctx, t, phones[0], "Alice")
	laptop := login(ctx, t, phones[0], "Alice")
	bob := login(ctx, t, phones[1], "Bob")
	getState(ctx, t, laptop)
	username := fmt.Sprintf("e2e%dbot", randomID()&0xffffff)
	admin(t, "bot", "create", username, "Shop Bot", "-owner", phones[0])
	botPeer := resolveBot(ctx, t, alice, username)
	botUser := inputUserOfPeer(alice.c, botPeer)
	bobPeer := inputPeerOf(ctx, t, alice, bob)
	bobUser := inputUserOf(ctx, t, alice, bob)

	// A chat of Bob with Alice, begun before the bot is connected.
	if _, err := bob.c.Invoke(ctx, &mtproto.TLMessagesSendMessage{
		Constructor: bob.c.Constructor(mtproto.Predicate_messages_sendMessage),
		Peer:        inputPeerOf(ctx, t, bob, alice),
		Message:     "hello",
		RandomId:    randomID(),
	}); err != nil {
		t.Fatalf("messages.sendMessage: %v", err)
	}

	connect := func(r *mtproto.InputBusinessBotRecipients) (*mtproto.Updates, error) {
		o, err := alice.c.Invoke(ctx, &mtproto.TLAccountUpdateConnectedBot{
			Constructor:                           alice.c.Constructor(mtproto.Predicate_account_updateConnectedBot),
			Rights:                                &mtproto.BusinessBotRights{Constructor: alice.c.Constructor(mtproto.Predicate_businessBotRights), Reply: true, ReadMessages: true},
			Bot:                                   botUser,
			Recipients_INPUTBUSINESSBOTRECIPIENTS: r,
		})
		if err != nil {
			return nil, err
		}
		return o.(interface{ To_Updates() *mtproto.Updates }).To_Updates(), nil
	}
	all := &mtproto.InputBusinessBotRecipients{
		Constructor:   alice.c.Constructor(mtproto.Predicate_inputBusinessBotRecipients),
		ExistingChats: true,
		NewChats:      true,
	}
	connected := func() *mtproto.Account_ConnectedBots {
		t.Helper()
		o, err := alice.c.Invoke(ctx, &mtproto.TLAccountGetConnectedBots{
			Constructor: alice.c.Constructor(mtproto.Predicate_account_getConnectedBots),
		})
		if err != nil {
			t.Fatalf("account.getConnectedBots: %v", err)
		}
		return o.(interface {
			To_Account_ConnectedBots() *mtproto.Account_ConnectedBots
		}).To_Account_ConnectedBots()
	}
	settings := func(peer *mtproto.InputPeer) *mtproto.Messages_PeerSettings {
		t.Helper()
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetPeerSettings{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_getPeerSettings),
			Peer:        peer,
		})
		if err != nil {
			t.Fatalf("messages.getPeerSettings: %v", err)
		}
		return o.(interface {
			To_Messages_PeerSettings() *mtproto.Messages_PeerSettings
		}).To_Messages_PeerSettings()
	}
	toggle := func(peer *mtproto.InputPeer, paused bool) error {
		_, err := alice.c.Invoke(ctx, &mtproto.TLAccountToggleConnectedBotPaused{
			Constructor: alice.c.Constructor(mtproto.Predicate_account_toggleConnectedBotPaused),
			Peer:        peer,
			Paused:      mtproto.ToBool(paused),
		})
		return err
	}
	confirm := func(u *mtproto.InputUser) error {
		_, err := alice.c.Invoke(ctx, &mtproto.TLAccountConfirmBotConnection{
			Constructor: alice.c.Constructor(mtproto.Predicate_account_confirmBotConnection),
			BotId:       u,
		})
		return err
	}
	botId := botPeer.GetUserId()

	if _, err := connect(all); !strings.Contains(fmt.Sprint(err), "BOT_BUSINESS_MISSING") {
		t.Fatalf("a bot without the business mode: %v", err)
	}
	if out := admin(t, "bot", "set", username, "-business", "on"); !strings.Contains(out, "updated") {
		t.Fatalf("bot set -business on: %q", out)
	}
	if _, err := connect(&mtproto.InputBusinessBotRecipients{Constructor: alice.c.Constructor(mtproto.Predicate_inputBusinessBotRecipients)}); !strings.Contains(fmt.Sprint(err), "BUSINESS_RECIPIENTS_EMPTY") {
		t.Fatalf("no recipients: %v", err)
	}
	if err := confirm(botUser); !strings.Contains(fmt.Sprint(err), "BOT_INVALID") {
		t.Fatalf("confirm a bot not connected: %v", err)
	}

	updates, err := connect(all)
	if err != nil {
		t.Fatalf("account.updateConnectedBot: %v", err)
	}
	if !slices.ContainsFunc(updates.GetUsers(), func(u *mtproto.User) bool { return u.GetId() == botId && u.GetBot() }) {
		t.Fatalf("the answer has no bot: %v", updates.GetUsers())
	}
	bots := connected()
	if len(bots.GetConnectedBots()) != 1 {
		t.Fatalf("connected bots: %v", bots.GetConnectedBots())
	}
	cb := bots.GetConnectedBots()[0]
	if cb.GetBotId() != botId || !cb.GetRights().GetReply() || !cb.GetRights().GetReadMessages() || cb.GetRights().GetEditBio() ||
		!cb.GetRecipients_BUSINESSBOTRECIPIENTS().GetExistingChats() || !cb.GetRecipients_BUSINESSBOTRECIPIENTS().GetNewChats() || cb.GetRecipients_BUSINESSBOTRECIPIENTS().GetExcludeSelected() {
		t.Fatalf("connected bot: %v", cb)
	}
	if err := confirm(botUser); err != nil {
		t.Fatalf("confirm the connected bot: %v", err)
	}
	if err := confirm(bobUser); !strings.Contains(fmt.Sprint(err), "BOT_INVALID") {
		t.Fatalf("confirm a user: %v", err)
	}

	// The bar in the chat with Bob.
	ps := settings(bobPeer)
	bar := ps.GetSettings()
	if bar.GetBusinessBotId().GetValue() != botId || !strings.HasSuffix(bar.GetBusinessBotManageUrl().GetValue(), "/"+username) ||
		!bar.GetBusinessBotCanReply() || bar.GetBusinessBotPaused() {
		t.Fatalf("the bar: %v", bar)
	}
	if !slices.ContainsFunc(ps.GetUsers(), func(u *mtproto.User) bool { return u.GetId() == botId }) {
		t.Fatalf("no bot in the users of the settings: %v", ps.GetUsers())
	}
	o, err := alice.c.Invoke(ctx, &mtproto.TLUsersGetFullUser{
		Constructor: alice.c.Constructor(mtproto.Predicate_users_getFullUser),
		Id:          bobUser,
	})
	if err != nil {
		t.Fatalf("users.getFullUser: %v", err)
	}
	full := o.(interface {
		To_Users_UserFull() *mtproto.Users_UserFull
	}).To_Users_UserFull()
	if full.GetFullUser().GetSettings().GetBusinessBotId().GetValue() != botId ||
		!slices.ContainsFunc(full.GetUsers(), func(u *mtproto.User) bool { return u.GetId() == botId }) {
		t.Fatalf("userFull.settings: %v, users %v", full.GetFullUser().GetSettings(), full.GetUsers())
	}
	if bar := settings(botPeer).GetSettings(); bar.GetBusinessBotId() != nil {
		t.Fatalf("the chat with the bot itself has the bar: %v", bar)
	}

	// Pause and resume; the laptop learns both.
	if err := toggle(bobPeer, true); err != nil {
		t.Fatalf("account.toggleConnectedBotPaused: %v", err)
	}
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updatePeerSettings, func(u *mtproto.Update) bool {
		return u.GetPeer_PEER().GetUserId() == bob.self.GetId() && u.GetSettings().GetBusinessBotPaused() &&
			!u.GetSettings().GetBusinessBotCanReply() && u.GetSettings().GetBusinessBotId().GetValue() == botId
	})
	if bar := settings(bobPeer).GetSettings(); !bar.GetBusinessBotPaused() || bar.GetBusinessBotCanReply() {
		t.Fatalf("paused: %v", bar)
	}
	if err := toggle(bobPeer, false); err != nil {
		t.Fatalf("account.toggleConnectedBotPaused: %v", err)
	}
	if bar := settings(bobPeer).GetSettings(); bar.GetBusinessBotPaused() || !bar.GetBusinessBotCanReply() {
		t.Fatalf("resumed: %v", bar)
	}
	selfPeer := &mtproto.InputPeer{Constructor: alice.c.Constructor(mtproto.Predicate_inputPeerSelf)}
	if err := toggle(selfPeer, true); !strings.Contains(fmt.Sprint(err), "PEER_ID_INVALID") {
		t.Fatalf("pause in Saved Messages: %v", err)
	}
	if err := toggle(botPeer, true); !strings.Contains(fmt.Sprint(err), "PEER_ID_INVALID") {
		t.Fatalf("pause in the chat with a bot: %v", err)
	}

	// The chat with Bob is taken away from the bot.
	if _, err := alice.c.Invoke(ctx, &mtproto.TLAccountDisablePeerConnectedBot{
		Constructor: alice.c.Constructor(mtproto.Predicate_account_disablePeerConnectedBot),
		Peer:        bobPeer,
	}); err != nil {
		t.Fatalf("account.disablePeerConnectedBot: %v", err)
	}
	waitUpdate(ctx, t, laptop, mtproto.Predicate_updatePeerSettings, func(u *mtproto.Update) bool {
		return u.GetPeer_PEER().GetUserId() == bob.self.GetId() && u.GetSettings().GetBusinessBotId() == nil
	})
	if bar := settings(bobPeer).GetSettings(); bar.GetBusinessBotId() != nil {
		t.Fatalf("the bar stays after the disable: %v", bar)
	}
	if r := connected().GetConnectedBots()[0].GetRecipients_BUSINESSBOTRECIPIENTS(); !slices.Contains(r.GetExcludeUsers(), bob.self.GetId()) || !r.GetExistingChats() {
		t.Fatalf("recipients after the disable: %v", r)
	}
	if err := toggle(bobPeer, true); !strings.Contains(fmt.Sprint(err), "PEER_ID_INVALID") {
		t.Fatalf("pause in a chat the bot does not serve: %v", err)
	}

	// Disconnected as iOS does it.
	if _, err := alice.c.Invoke(ctx, &mtproto.TLAccountUpdateConnectedBot{
		Constructor:                           alice.c.Constructor(mtproto.Predicate_account_updateConnectedBot),
		Deleted:                               true,
		Bot:                                   &mtproto.InputUser{Constructor: alice.c.Constructor(mtproto.Predicate_inputUserEmpty)},
		Recipients_INPUTBUSINESSBOTRECIPIENTS: &mtproto.InputBusinessBotRecipients{Constructor: alice.c.Constructor(mtproto.Predicate_inputBusinessBotRecipients)},
	}); err != nil {
		t.Fatalf("account.updateConnectedBot deleted: %v", err)
	}
	if bots := connected().GetConnectedBots(); len(bots) != 0 {
		t.Fatalf("still connected: %v", bots)
	}

	// Connected again, then the business mode goes off for the bot.
	if _, err := connect(all); err != nil {
		t.Fatalf("account.updateConnectedBot again: %v", err)
	}
	if out := admin(t, "bot", "set", username, "-business", "off"); !strings.Contains(out, "disconnected from 1 accounts") {
		t.Fatalf("bot set -business off: %q", out)
	}
	if bots := connected().GetConnectedBots(); len(bots) != 0 {
		t.Fatalf("connected after the business mode went off: %v", bots)
	}
	if _, err := connect(all); !strings.Contains(fmt.Sprint(err), "BOT_BUSINESS_MISSING") {
		t.Fatalf("connect after the business mode went off: %v", err)
	}
}
