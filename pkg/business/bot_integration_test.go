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
	"slices"
	"sync"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/userbox"
)

// fakeUsers are the users of the bar: people, a bot with a username and
// the contacts of the accounts.
type fakeUsers struct {
	users    map[int64]*mtproto.ImmutableUser
	contacts map[[2]int64]bool
}

func (f *fakeUsers) UserGetImmutableUser(_ context.Context, in *userpb.TLUserGetImmutableUser) (*mtproto.ImmutableUser, error) {
	if u, ok := f.users[in.GetId()]; ok {
		return u, nil
	}
	return nil, mtproto.ErrUserIdInvalid
}

func (f *fakeUsers) UserCheckContact(_ context.Context, in *userpb.TLUserCheckContact) (*mtproto.Bool, error) {
	return mtproto.ToBool(f.contacts[[2]int64{in.GetUserId(), in.GetId()}]), nil
}

// The connected bot on the production MySQL with the whole schema: the
// same bot keeps its connection, another replaces it with a new id and no
// pauses, a removal names the bot or not, the recipients change by
// disablePeerConnectedBot, and the bar of a chat follows the recipients
// (new and existing chats, contacts), the pause and the rights.
func TestConnectedBotStore(t *testing.T) {
	ctx := context.Background()
	db, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Terminate(ctx) })
	conf := db.Config()
	conn := sqlx.NewMySQL(&conf)
	s := NewStore(conn)
	boxDB, err := userbox.Open(conf.DSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = boxDB.Close() })

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	tx := func(f func(tx *sqlx.Tx) error) {
		t.Helper()
		must(conn.Transact(ctx, f))
	}
	connect := func(user, bot int64, rights Rights, r Recipients, now int32) (c, replaced *Connection) {
		t.Helper()
		tx(func(tx *sqlx.Tx) error {
			var err error
			c, replaced, err = ConnectTx(tx, user, bot, rights, r, now)
			return err
		})
		return
	}
	read := func(user int64) *Connection {
		t.Helper()
		c, err := s.Connection(ctx, user)
		must(err)
		return c
	}
	paused := func(user, peer int64) bool {
		t.Helper()
		p, err := s.Paused(ctx, user, peer)
		must(err)
		return p
	}
	// message puts a message of the chat of user with peer in user's box
	// at date.
	box := int32(0)
	message := func(user, peer int64, date int64) {
		t.Helper()
		box++
		did := mtproto.MakeDialogId(user, mtproto.PEER_USER, peer)
		_, err := conn.Exec(ctx, "INSERT INTO messages (user_id, user_message_box_id, dialog_id1, dialog_id2, dialog_message_id, sender_user_id,"+
			" peer_type, peer_id, message_data, message, date2) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'null', '', ?)",
			user, box, did.A, did.B, int64(box), peer, mtproto.PEER_USER, peer, date)
		must(err)
	}

	const (
		owner    = 1
		bot      = 50
		otherBot = 51
		old      = 2 // a chat before the connection, a contact
		fresh    = 3 // a chat after it
		quiet    = 4 // no chat yet
		listed   = 5
	)
	if c := read(owner); c != nil {
		t.Fatalf("a connection nobody made: %+v", c)
	}

	// A new connection.
	c1, replaced := connect(owner, bot, RightReply|RightReadMessages, Recipients{ExistingChats: true, Users: []int64{listed}}, 1000)
	if replaced != nil || c1.ConnectionID == "" || c1.CreatedAt != 1000 {
		t.Fatalf("new connection %+v, replaced %+v", c1, replaced)
	}
	if got := read(owner); !got.Same(c1) || got.CreatedAt != 1000 || got.UpdatedAt != 1000 {
		t.Fatalf("read back %+v, want %+v", got, c1)
	}
	tx(func(tx *sqlx.Tx) error { return SetPausedTx(tx, owner, old, true, 1001) })
	tx(func(tx *sqlx.Tx) error { return SetPausedTx(tx, owner, old, true, 1002) }) // again: one row
	if !paused(owner, old) || paused(owner, fresh) {
		t.Fatal("the pause is not kept")
	}

	// The same bot with new settings keeps the id, the time and the pauses.
	c2, replaced := connect(owner, bot, RightReply, Recipients{ExistingChats: true, NewChats: true, Users: []int64{listed}}, 2000)
	if replaced != nil || c2.ConnectionID != c1.ConnectionID || c2.CreatedAt != 1000 || c2.UpdatedAt != 2000 || !paused(owner, old) {
		t.Fatalf("edited %+v, replaced %+v, paused %v", c2, replaced, paused(owner, old))
	}
	if got := read(owner); !got.Same(c2) || got.Rights != RightReply {
		t.Fatalf("read back %+v", got)
	}

	// Another bot replaces it.
	c3, replaced := connect(owner, otherBot, RightReply, Recipients{Contacts: true}, 3000)
	if replaced == nil || replaced.BotID != bot || c3.ConnectionID == c1.ConnectionID || c3.CreatedAt != 3000 || paused(owner, old) {
		t.Fatalf("replaced by %+v, replaced %+v", c3, replaced)
	}
	accounts, err := s.AccountsOfBot(ctx, otherBot)
	must(err)
	if !slices.Equal(accounts, []int64{owner}) {
		t.Fatalf("accounts of the bot: %v", accounts)
	}
	if accounts, _ = s.AccountsOfBot(ctx, bot); len(accounts) != 0 {
		t.Fatalf("the replaced bot is still connected: %v", accounts)
	}

	// Removal of another bot keeps it; removal of it or of any bot removes
	// it with its pauses.
	tx(func(tx *sqlx.Tx) error { return SetPausedTx(tx, owner, fresh, true, 3001) })
	tx(func(tx *sqlx.Tx) error {
		removed, err := DisconnectTx(tx, owner, bot)
		if removed != nil {
			t.Errorf("removed another bot: %+v", removed)
		}
		return err
	})
	if read(owner) == nil {
		t.Fatal("the connection went with the removal of another bot")
	}
	tx(func(tx *sqlx.Tx) error {
		removed, err := DisconnectTx(tx, owner, 0)
		if removed == nil || removed.ConnectionID != c3.ConnectionID {
			t.Errorf("removed %+v", removed)
		}
		return err
	})
	if read(owner) != nil || paused(owner, fresh) {
		t.Fatal("the connection or its pause stays after the removal")
	}
	tx(func(tx *sqlx.Tx) error {
		removed, err := DisconnectTx(tx, owner, 0)
		if removed != nil {
			t.Errorf("removed twice: %+v", removed)
		}
		return err
	})

	// The bar of a chat.
	users := &fakeUsers{
		users: map[int64]*mtproto.ImmutableUser{
			old:      {User: &mtproto.UserData{Id: old}},
			fresh:    {User: &mtproto.UserData{Id: fresh}},
			quiet:    {User: &mtproto.UserData{Id: quiet}},
			listed:   {User: &mtproto.UserData{Id: listed}},
			bot:      {User: &mtproto.UserData{Id: bot, Username: "shopbot", Bot: &mtproto.BotData{BotBusiness: true}}},
			otherBot: {User: &mtproto.UserData{Id: otherBot, Username: "otherbot", Bot: &mtproto.BotData{}}},
		},
		contacts: map[[2]int64]bool{{owner, old}: true},
	}
	message(owner, old, 100)
	message(owner, old, 5000)
	message(owner, fresh, 5000)
	connect(owner, bot, RightReply, Recipients{NewChats: true}, 4000)
	bar := func(peer int64) *PeerBar {
		t.Helper()
		b, err := s.PeerBar(ctx, users, "pluma.chat", owner, peer)
		must(err)
		return b
	}
	if b := bar(old); b != nil {
		t.Errorf("an existing chat has the bar of new chats: %+v", b)
	}
	// Its first message deleted, the chat still began before the bot.
	_, err = conn.Exec(ctx, "UPDATE messages SET deleted = 1 WHERE user_id = ? AND date2 = 100", owner)
	must(err)
	if b := bar(old); b != nil {
		t.Errorf("an existing chat with its first message deleted has the bar of new chats: %+v", b)
	}
	if b := bar(fresh); b == nil || b.BotID != bot || b.ManageURL != "https://pluma.chat/shopbot" || !b.CanReply || b.Paused {
		t.Errorf("a new chat: %+v", b)
	}
	if b := bar(quiet); b == nil {
		t.Error("a chat with no message yet is not new")
	}
	for _, peer := range []int64{owner, otherBot, 999} {
		if b := bar(peer); b != nil {
			t.Errorf("the chat with %d has a bar: %+v", peer, b)
		}
	}

	// Contacts, a pause, no right to reply.
	connect(owner, bot, RightReadMessages, Recipients{Contacts: true}, 4100)
	if b := bar(old); b == nil || b.CanReply {
		t.Errorf("a contact, read only: %+v", b)
	}
	if b := bar(fresh); b != nil {
		t.Errorf("not a contact: %+v", b)
	}
	c, _ := connect(owner, bot, RightReply, Recipients{Contacts: true}, 4200)
	tx(func(tx *sqlx.Tx) error { return SetPausedTx(tx, owner, old, true, 4201) })
	if b := bar(old); b == nil || !b.Paused || b.CanReply {
		t.Errorf("paused: %+v", b)
	}

	// disablePeerConnectedBot: the chat leaves the recipients in the
	// transaction of the account.
	peer, served, err := s.ChatPeer(ctx, users, c, old)
	must(err)
	if !served || !peer.Contact || peer.New {
		t.Fatalf("chat peer %+v, served %v", peer, served)
	}
	if _, err = userbox.Run(ctx, boxDB, owner, func(b *userbox.Box) error {
		locked, err := ConnectionTx(b.Tx(), owner)
		if err != nil {
			return err
		}
		if !locked.Same(c) {
			t.Errorf("locked %+v, read %+v", locked, c)
		}
		if err = SetRecipientsTx(b.Tx(), locked, locked.Recipients.WithoutPeer(peer), 4300); err != nil {
			return err
		}
		return SetPausedTx(b.Tx(), owner, old, false, 4300)
	}); err != nil {
		t.Fatal(err)
	}
	if b := bar(old); b != nil || paused(owner, old) {
		t.Errorf("disabled chat: %+v, paused %v", b, paused(owner, old))
	}
	if got := read(owner); !slices.Equal(got.Recipients.ExcludeUsers, []int64{old}) || !got.Recipients.Contacts || got.UpdatedAt != 4300 {
		t.Errorf("recipients after the disable: %+v", got)
	}

	// Two connections at once of the same account, by two bots, in its box
	// transactions: one of them stays, whole.
	var wg sync.WaitGroup
	for _, b := range []int64{bot, otherBot} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := userbox.Run(ctx, boxDB, owner, func(box *userbox.Box) error {
				_, _, err := ConnectTx(box.Tx(), owner, b, RightReply, Recipients{NonContacts: true}, 5000)
				return err
			}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got := read(owner)
	if got == nil || (got.BotID != bot && got.BotID != otherBot) || !got.Recipients.NonContacts {
		t.Errorf("after two connections at once: %+v", got)
	}
	var n []struct {
		N int `db:"n"`
	}
	must(conn.QueryRowsPartial(ctx, &n, "SELECT COUNT(*) AS n FROM business_bot_connections WHERE user_id = ?", owner))
	if n[0].N != 1 {
		t.Errorf("%d connections of one account", n[0].N)
	}
}
