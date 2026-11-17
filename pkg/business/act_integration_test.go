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

//go:build integration

package business

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// A connected bot's request on the production MySQL with the whole schema:
// who may make it, through which connection, in which chats, with which
// rights, within 24 hours of an incoming message, and what it may delete.
func TestAct(t *testing.T) {
	ctx := context.Background()
	db, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Terminate(ctx) })
	conf := db.Config()
	conn := sqlx.NewMySQL(&conf)
	s := NewStore(conn)

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	const (
		owner    = 1
		peer     = 2  // writes to the owner
		silent   = 3  // never wrote
		excluded = 4  // left out of the recipients
		paused   = 5  // the bot is paused there
		bot      = 50 // business mode, connected
		plainBot = 51 // no business mode
		person   = 60 // not a bot
		otherBot = 52 // business mode, connected to another account
		other    = 9  // the account of otherBot
	)
	now := time.Unix(1_800_000_000, 0)
	box := int32(0)
	// message puts in the box of user a message of the chat with peer,
	// sent by from, at date.
	message := func(user, peerID, from int64, date time.Time) int32 {
		t.Helper()
		box++
		did := mtproto.MakeDialogId(user, mtproto.PEER_USER, peerID)
		_, err := conn.Exec(ctx, "INSERT INTO messages (user_id, user_message_box_id, dialog_id1, dialog_id2, dialog_message_id, sender_user_id,"+
			" peer_type, peer_id, message_data, message, date2) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'null', '', ?)",
			user, box, did.A, did.B, int64(box), from, mtproto.PEER_USER, peerID, date.Unix())
		must(err)
		return box
	}
	var c *Connection
	must(conn.Transact(ctx, func(tx *sqlx.Tx) error {
		var err error
		if c, _, err = ConnectTx(tx, owner, bot, RightReply|RightReadMessages|RightDeleteSentMessages,
			Recipients{ExistingChats: true, NewChats: true, ExcludeUsers: []int64{excluded}}, int32(now.Add(-72*time.Hour).Unix())); err != nil {
			return err
		}
		if _, _, err = ConnectTx(tx, other, otherBot, RightReply, Recipients{NewChats: true}, int32(now.Unix())); err != nil {
			return err
		}
		return SetPausedTx(tx, owner, paused, true, int32(now.Unix()))
	}))

	users := &fakeUsers{users: map[int64]*mtproto.ImmutableUser{
		peer:     {User: &mtproto.UserData{Id: peer}},
		silent:   {User: &mtproto.UserData{Id: silent}},
		excluded: {User: &mtproto.UserData{Id: excluded}},
		paused:   {User: &mtproto.UserData{Id: paused}},
		person:   {User: &mtproto.UserData{Id: person}},
		bot:      {User: &mtproto.UserData{Id: bot, Username: "shopbot", Bot: &mtproto.BotData{BotBusiness: true}}},
		plainBot: {User: &mtproto.UserData{Id: plainBot, Username: "plainbot", Bot: &mtproto.BotData{}}},
		otherBot: {User: &mtproto.UserData{Id: otherBot, Username: "otherbot", Bot: &mtproto.BotData{BotBusiness: true}}},
	}}
	// The peer wrote 23 hours ago, the excluded and the paused one just now.
	message(owner, peer, peer, now.Add(-30*time.Hour))
	message(owner, peer, owner, now.Add(-time.Minute))
	message(owner, peer, peer, now.Add(-23*time.Hour))
	message(owner, excluded, excluded, now)
	message(owner, paused, paused, now)

	user := func(id int64) *mtproto.PeerUtil { return &mtproto.PeerUtil{PeerType: mtproto.PEER_USER, PeerId: id} }
	act := func(botID int64, connID string, m Method, p *mtproto.PeerUtil, at time.Time) (*Act, error) {
		return s.Act(ctx, users, botID, connID, m, p, at)
	}
	for _, tc := range []struct {
		name   string
		bot    int64
		connID string
		m      Method
		peer   *mtproto.PeerUtil
		at     time.Time
		want   error
	}{
		{"a person", person, c.ConnectionID, MethodSendMessage, user(peer), now, mtproto.ErrBusinessConnectionNotAllowed},
		{"an unknown connection", bot, "nope", MethodSendMessage, user(peer), now, mtproto.ErrBusinessConnectionInvalid},
		{"another bot's connection", otherBot, c.ConnectionID, MethodSendMessage, user(peer), now, mtproto.ErrBusinessConnectionInvalid},
		{"a method of no connection", bot, c.ConnectionID, Method(99), user(peer), now, mtproto.ErrBusinessConnectionNotAllowed},
		{"a chat that is not private", bot, c.ConnectionID, MethodSendMessage, &mtproto.PeerUtil{PeerType: mtproto.PEER_CHAT, PeerId: 7}, now, mtproto.ErrBotAccessForbidden},
		{"the account itself", bot, c.ConnectionID, MethodSendMessage, user(owner), now, mtproto.ErrBotAccessForbidden},
		{"a chat left out", bot, c.ConnectionID, MethodSendMessage, user(excluded), now, mtproto.ErrBotAccessForbidden},
		{"a paused chat", bot, c.ConnectionID, MethodSendMessage, user(paused), now, mtproto.ErrBotAccessForbidden},
		{"a chat with no incoming message", bot, c.ConnectionID, MethodSendMessage, user(silent), now, mtproto.ErrBotAccessForbidden},
		{"after 24 hours", bot, c.ConnectionID, MethodSendMessage, user(peer), now.Add(2 * time.Hour), mtproto.ErrBotAccessForbidden},
		{"within 24 hours", bot, c.ConnectionID, MethodSendMessage, user(peer), now, nil},
		{"an edit within 24 hours", bot, c.ConnectionID, MethodEditMessage, user(peer), now, nil},
		{"reading needs no 24 hours", bot, c.ConnectionID, MethodReadHistory, user(peer), now.Add(48 * time.Hour), nil},
		{"the profile has no chat", bot, c.ConnectionID, MethodUpdateProfile, nil, now, nil},
	} {
		a, err := act(tc.bot, tc.connID, tc.m, tc.peer, tc.at)
		if !errors.Is(err, tc.want) && err != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, err, tc.want)
			continue
		}
		if err == nil && (a.Owner() != owner || a.BotID() != bot || (tc.peer != nil && a.PeerID != tc.peer.PeerId)) {
			t.Errorf("%s: act %+v", tc.name, a)
		}
	}

	// Without business mode the connection is not the bot's to use.
	users.users[bot] = &mtproto.ImmutableUser{User: &mtproto.UserData{Id: bot, Username: "shopbot", Bot: &mtproto.BotData{}}}
	if _, err := act(bot, c.ConnectionID, MethodSendMessage, user(peer), now); err != mtproto.ErrBusinessConnectionNotAllowed {
		t.Errorf("no business mode: %v", err)
	}
	users.users[bot] = &mtproto.ImmutableUser{User: &mtproto.UserData{Id: bot, Username: "shopbot", Bot: &mtproto.BotData{BotBusiness: true}}}

	// Read only: no reply.
	must(conn.Transact(ctx, func(tx *sqlx.Tx) error {
		_, _, err := ConnectTx(tx, owner, bot, RightReadMessages, c.Recipients, int32(now.Unix()))
		return err
	}))
	if _, err := act(bot, c.ConnectionID, MethodSendMessage, user(peer), now); err != mtproto.ErrBotAccessForbidden {
		t.Errorf("no right to reply: %v", err)
	}
	if _, err := act(bot, c.ConnectionID, MethodReadHistory, user(peer), now); err != nil {
		t.Errorf("read only, reading: %v", err)
	}

	// Deletions: the bot's own with delete_sent_messages, the incoming ones
	// with delete_received_messages, never the account's own.
	a, err := act(bot, c.ConnectionID, MethodDeleteMessages, nil, now)
	must(err)
	viaBot := func(id int64) *wrapperspb.Int64Value {
		if id == 0 {
			return nil
		}
		return wrapperspb.Int64(id)
	}
	outBox := func(to, via int64) *mtproto.MessageBox {
		return &mtproto.MessageBox{UserId: owner, SenderUserId: owner, PeerType: mtproto.PEER_USER, PeerId: to,
			Message: mtproto.MakeTLMessage(&mtproto.Message{Out: true, PeerId: mtproto.MakePeerUser(to), ViaBusinessBotId: viaBot(via)}).To_Message()}
	}
	inBox := func(from int64) *mtproto.MessageBox {
		return &mtproto.MessageBox{UserId: owner, SenderUserId: from, PeerType: mtproto.PEER_USER, PeerId: owner,
			Message: mtproto.MakeTLMessage(&mtproto.Message{PeerId: mtproto.MakePeerUser(from)}).To_Message()}
	}
	setRights := func(r Rights) {
		t.Helper()
		a.Connection.Rights = r
	}
	for _, tc := range []struct {
		name   string
		rights Rights
		boxes  []*mtproto.MessageBox
		ok     bool
	}{
		{"the bot's own", RightDeleteSentMessages, []*mtproto.MessageBox{outBox(peer, bot)}, true},
		{"the bot's own without the right", RightDeleteReceivedMessages, []*mtproto.MessageBox{outBox(peer, bot)}, false},
		{"the account's own", RightDeleteSentMessages | RightDeleteReceivedMessages, []*mtproto.MessageBox{outBox(peer, 0)}, false},
		{"another bot's", RightDeleteSentMessages, []*mtproto.MessageBox{outBox(peer, otherBot)}, false},
		{"incoming", RightDeleteReceivedMessages, []*mtproto.MessageBox{inBox(peer)}, true},
		{"incoming without the right", RightDeleteSentMessages, []*mtproto.MessageBox{inBox(peer)}, false},
		{"in a chat left out", RightDeleteReceivedMessages, []*mtproto.MessageBox{inBox(peer), inBox(excluded)}, false},
		{"in a paused chat", RightDeleteSentMessages, []*mtproto.MessageBox{outBox(paused, bot)}, false},
		{"of a group", RightDeleteSentMessages | RightDeleteReceivedMessages, []*mtproto.MessageBox{{UserId: owner, SenderUserId: peer, PeerType: mtproto.PEER_CHAT, PeerId: 7,
			Message: mtproto.MakeTLMessage(&mtproto.Message{PeerId: mtproto.MakePeerChat(7)}).To_Message()}}, false},
	} {
		setRights(tc.rights)
		err := s.CheckDelete(ctx, users, a, tc.boxes)
		if tc.ok != (err == nil) {
			t.Errorf("%s: %v", tc.name, err)
		}
		if err != nil && err != mtproto.ErrBotAccessForbidden {
			t.Errorf("%s: %v, want BOT_ACCESS_FORBIDDEN", tc.name, err)
		}
	}

	// The last incoming message counts the deleted ones too.
	last, err := s.LastIncomingAt(ctx, owner, peer)
	must(err)
	if want := now.Add(-23 * time.Hour).Unix(); last != want {
		t.Errorf("last incoming %d, want %d", last, want)
	}
	_, err = conn.Exec(ctx, "UPDATE messages SET deleted = 1 WHERE user_id = ? AND sender_user_id = ?", owner, peer)
	must(err)
	if last2, err := s.LastIncomingAt(ctx, owner, peer); err != nil || last2 != last {
		t.Errorf("deleted, last incoming %d, %v", last2, err)
	}
	if last, err := s.LastIncomingAt(ctx, owner, silent); err != nil || last != 0 {
		t.Errorf("none: %d, %v", last, err)
	}
}
