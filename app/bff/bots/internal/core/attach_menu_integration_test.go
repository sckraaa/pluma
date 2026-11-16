//go:build integration

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

package core

import (
	"context"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/svc"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	mediapb "github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/outbox"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/logx"
)

// iconDocs are the documents the media service has.
type iconDocs struct {
	media_client.MediaClient
	docs map[int64]*mtproto.Document
}

func (m iconDocs) MediaGetDocumentList(_ context.Context, in *mediapb.TLMediaGetDocumentList) (*mediapb.Vector_Document, error) {
	out := &mediapb.Vector_Document{}
	for _, id := range in.IdList {
		if d, ok := m.docs[id]; ok {
			out.Datas = append(out.Datas, d)
		}
	}
	return out, nil
}

// TestAttachMenuOnMySQL: the administrator gives a bot an attachment menu
// entry; the user sees it inactive, adds it with the permission to be
// written to, and every session of the user gets updateAttachMenuBots in
// the journal; the chat with the bot gets messageActionBotAllowed with
// attach_menu once; the list has the bot until the user removes it, its
// hash answers attachMenuBotsNotModified and changes with the entry.
func TestAttachMenuOnMySQL(t *testing.T) {
	ctx := context.Background()
	mysql, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mysql.Terminate(context.Background()) })
	conf := mysql.Config()
	db := sqlx.NewMySQL(&conf)
	store := botstore.New(db)
	boxDB, err := userbox.Open(mysql.DSN())
	if err != nil {
		t.Fatal(err)
	}
	const topic = "Sync-AttachMenu-T"
	if err = outbox.Register(outbox.Topics{Push: []string{topic}}); err != nil {
		t.Fatal(err)
	}

	const user = int64(1000002)
	bot, _, err := store.Create(ctx, "shop_attach_bot", "Shop bot", 0)
	if err != nil {
		t.Fatal(err)
	}
	plain, _, err := store.Create(ctx, "plain_attach_bot", "Plain bot", 0)
	if err != nil {
		t.Fatal(err)
	}
	entry := botstore.AttachMenu{
		BotId: bot, ShortName: "Shop", URL: "https://shop.example/app",
		ShowInAttachMenu: true, RequestWriteAccess: true,
		PeerTypes: botstore.AttachPeerPM | botstore.AttachPeerChat,
		Icons: []botstore.AttachIcon{
			{Name: "default_static", DocumentId: 701, Colors: []botstore.AttachIconColor{{Name: "light_icon", Color: 0x2481cc}}},
			{Name: "android_animated", DocumentId: 702}, // gone from the media service
		},
	}
	if _, err = store.SetAttachMenu(ctx, entry); err != nil {
		t.Fatal(err)
	}

	sent := &sentOnce{seen: map[int64]bool{}}
	svcCtx := &svc.ServiceContext{
		Dao: &dao.Dao{
			UserClient: loginUsers{user: user, bots: map[int64]bool{bot: true, plain: true}},
			MediaClient: iconDocs{docs: map[int64]*mtproto.Document{
				701: mtproto.MakeTLDocument(&mtproto.Document{Id: 701, AccessHash: 3, FileReference: []byte{}, MimeType: "image/svg+xml", Size2: 300, DcId: 1, Attributes: []*mtproto.DocumentAttribute{}}).To_Document(),
			}},
			MsgClient: sent,
			Bots:      store,
			UserSeq:   dao.BoxPush{DB: boxDB, Topic: topic},
		},
	}
	c := &BotsCore{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx), MD: &metadata.RpcMetadata{UserId: user, PermAuthKeyId: 78}}
	inputBot := func(id int64) *mtproto.InputUser {
		return mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: id, AccessHash: 1}).To_InputUser()
	}
	menuUpdates := func() int {
		t.Helper()
		var n []int
		if err := db.QueryRowsPartial(ctx, &n, "SELECT COUNT(*) FROM user_seq_updates WHERE user_id = ? AND update_type = ?", user, mtproto.Predicate_updateAttachMenuBots); err != nil {
			t.Fatal(err)
		}
		return n[0]
	}
	list := func(hash int64) *mtproto.AttachMenuBots {
		t.Helper()
		r, err := c.MessagesGetAttachMenuBots(&mtproto.TLMessagesGetAttachMenuBots{Hash: hash})
		if err != nil {
			t.Fatal(err)
		}
		x := mtproto.NewEncodeBuf(512)
		if err = r.Encode(x, 233); err != nil {
			t.Fatalf("encode: %v", err)
		}
		return r
	}
	checkEntry := func(t *testing.T, b *mtproto.AttachMenuBot, inactive bool) {
		t.Helper()
		if b.GetBotId() != bot || b.GetShortName() != "Shop" || b.GetInactive() != inactive || !b.GetShowInAttachMenu() || b.GetShowInSideMenu() ||
			!b.GetRequestWriteAccess() || len(b.GetPeerTypes()) != 2 {
			t.Errorf("entry %v, want inactive %v", b, inactive)
		}
		if icons := b.GetIcons(); len(icons) != 1 || icons[0].GetName() != "default_static" || icons[0].GetIcon().GetId() != 701 ||
			len(icons[0].GetColors()) != 1 || icons[0].GetColors()[0].GetColor() != 0x2481cc {
			t.Errorf("icons %v", icons)
		}
	}

	// Nothing added yet: an empty list with hash 0, whatever the client's
	// hash.
	for _, hash := range []int64{0, 1, -5} {
		if r := list(hash); r.GetPredicateName() != mtproto.Predicate_attachMenuBots || r.GetHash() != 0 || len(r.GetBots()) != 0 || len(r.GetUsers()) != 0 {
			t.Fatalf("hash %d: %v, want an empty list", hash, r)
		}
	}

	// The entry, inactive, with the bot among the users (iOS takes it from
	// there).
	got, err := c.MessagesGetAttachMenuBot(&mtproto.TLMessagesGetAttachMenuBot{Bot: inputBot(bot)})
	if err != nil {
		t.Fatal(err)
	}
	checkEntry(t, got.GetBot(), true)
	if users := got.GetUsers(); len(users) != 1 || users[0].GetId() != bot {
		t.Errorf("users %v", users)
	}

	// A bot without an entry can be neither shown nor added; removing it
	// is no error.
	if _, err = c.MessagesGetAttachMenuBot(&mtproto.TLMessagesGetAttachMenuBot{Bot: inputBot(plain)}); err != mtproto.ErrBotInvalid {
		t.Errorf("entry of a bot without one: %v", err)
	}
	if _, err = c.MessagesToggleBotInAttachMenu(&mtproto.TLMessagesToggleBotInAttachMenu{Bot: inputBot(plain), Enabled: mtproto.BoolTrue}); err != mtproto.ErrBotInvalid {
		t.Errorf("adding a bot without an entry: %v", err)
	}
	if n := menuUpdates(); n != 0 {
		t.Fatalf("%d menu updates after a refused addition", n)
	}

	// Added with the permission: one update in the journal, one
	// messageActionBotAllowed{attach_menu} to the bot, the permission kept.
	for range 2 {
		r, err := c.MessagesToggleBotInAttachMenu(&mtproto.TLMessagesToggleBotInAttachMenu{WriteAllowed: true, Bot: inputBot(bot), Enabled: mtproto.BoolTrue})
		if err != nil || !mtproto.FromBool(r) {
			t.Fatalf("toggleBotInAttachMenu: %v, %v", r, err)
		}
	}
	if n := menuUpdates(); n != 2 {
		t.Errorf("%d menu updates after two additions, want 2", n)
	}
	if len(sent.sent) != 1 {
		t.Fatalf("%d messages sent, want one", len(sent.sent))
	}
	m := sent.sent[0]
	if a := m.GetMessage()[0].GetMessage().GetAction(); m.GetPeerId() != bot || a.GetPredicateName() != mtproto.Predicate_messageActionBotAllowed || !a.GetAttachMenu() || a.GetDomain_FLAGSTRING() != nil {
		t.Errorf("message to %d: %v", m.GetPeerId(), a)
	}
	if allowed, err := store.WriteAllowed(ctx, user, bot); err != nil || !allowed {
		t.Errorf("permission: %v, %v", allowed, err)
	}

	// The list has the bot, active; its own hash is not modified.
	r := list(0)
	if r.GetHash() == 0 || len(r.GetBots()) != 1 || len(r.GetUsers()) != 1 || r.GetUsers()[0].GetId() != bot {
		t.Fatalf("list %v", r)
	}
	checkEntry(t, r.GetBots()[0], false)
	hash := r.GetHash()
	if again := list(hash); again.GetPredicateName() != mtproto.Predicate_attachMenuBotsNotModified {
		t.Errorf("the list's own hash: %s", again.GetPredicateName())
	}
	if got, err = c.MessagesGetAttachMenuBot(&mtproto.TLMessagesGetAttachMenuBot{Bot: inputBot(bot)}); err != nil || got.GetBot().GetInactive() {
		t.Errorf("an added entry: %v, %v", got, err)
	}

	// A change of the entry changes the hash.
	entry.ShortName = "Shop2"
	if _, err = store.SetAttachMenu(ctx, entry); err != nil {
		t.Fatal(err)
	}
	if r = list(hash); r.GetPredicateName() != mtproto.Predicate_attachMenuBots || r.GetHash() == hash || r.GetBots()[0].GetShortName() != "Shop2" {
		t.Errorf("after a change of the entry: %v", r)
	}

	// Removed: the list is empty again, and the sessions learn it.
	if _, err = c.MessagesToggleBotInAttachMenu(&mtproto.TLMessagesToggleBotInAttachMenu{Bot: inputBot(bot), Enabled: mtproto.BoolFalse}); err != nil {
		t.Fatal(err)
	}
	if r = list(hash); r.GetHash() != 0 || len(r.GetBots()) != 0 {
		t.Errorf("after removal: %v", r)
	}
	if n := menuUpdates(); n != 3 {
		t.Errorf("%d menu updates after the removal, want 3", n)
	}
	if len(sent.sent) != 1 {
		t.Errorf("%d messages sent after the removal, want one", len(sent.sent))
	}
}
