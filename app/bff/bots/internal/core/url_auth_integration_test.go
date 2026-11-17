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
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/svc"
	msg_client "github.com/teamgram/teamgram-server/app/messenger/msg/msg/client"
	msgpb "github.com/teamgram/teamgram-server/app/messenger/msg/msg/msg"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	authsession_client "github.com/teamgram/teamgram-server/app/service/authsession/client"
	message_client "github.com/teamgram/teamgram-server/app/service/biz/message/client"
	"github.com/teamgram/teamgram-server/app/service/biz/message/message"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/botkeys"
	"github.com/teamgram/teamgram-server/pkg/botmarkup"
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/webappdata"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/types/known/wrapperspb"

	_ "github.com/go-sql-driver/mysql"
)

// loginUsers are the user who logs in (Ann, @ann) and the bots.
type loginUsers struct {
	user_client.UserClient
	user int64
	bots map[int64]bool
}

func (u loginUsers) immutable(id int64) *mtproto.ImmutableUser {
	data := &mtproto.UserData{Id: id, AccessHash: 1, FirstName: "Site bot"}
	if u.bots[id] {
		data.Bot = mtproto.MakeTLBotData(&mtproto.BotData{}).To_BotData()
	} else if id == u.user {
		data.FirstName, data.LastName, data.Username = "Ann", "Lee", "ann"
	}
	return mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{User: mtproto.MakeTLUserData(data).To_UserData()}).To_ImmutableUser()
}

func (u loginUsers) UserGetImmutableUser(_ context.Context, in *userpb.TLUserGetImmutableUser) (*mtproto.ImmutableUser, error) {
	if in.Id != u.user && !u.bots[in.Id] {
		return nil, mtproto.ErrUserIdInvalid
	}
	return u.immutable(in.Id), nil
}

func (u loginUsers) UserGetMutableUsers(_ context.Context, in *userpb.TLUserGetMutableUsers) (*userpb.Vector_ImmutableUser, error) {
	out := &userpb.Vector_ImmutableUser{}
	for _, id := range in.Id {
		out.Datas = append(out.Datas, u.immutable(id))
	}
	return out, nil
}

// botMessage is the user's copy of message 5 of the private chat with the
// bot, with the markup.
type botMessage struct {
	message_client.MessageClient
	user, bot int64
	markup    *mtproto.ReplyMarkup
}

func (b *botMessage) MessageGetUserMessage(_ context.Context, in *message.TLMessageGetUserMessage) (*mtproto.MessageBox, error) {
	if in.UserId != b.user || in.Id != 5 {
		return nil, mtproto.ErrMessageIdInvalid
	}
	did := mtproto.MakeDialogId(b.user, mtproto.PEER_USER, b.bot)
	return &mtproto.MessageBox{UserId: b.user, MessageId: 5, DialogId1: did.A, DialogId2: did.B, Message: mtproto.MakeTLMessage(&mtproto.Message{
		Id: 5, FromId: mtproto.MakePeerUser(b.bot), PeerId: mtproto.MakePeerUser(b.user), ReplyMarkup: b.markup,
	}).To_Message()}, nil
}

// sentOnce answers a send with a random_id already sent as msg does: with
// the message sent (dao.SentBefore), not a new one.
type sentOnce struct {
	msg_client.MsgClient
	sent []*msgpb.TLMsgSendMessageV2
	seen map[int64]bool
}

func (s *sentOnce) MsgSendMessageV2(_ context.Context, in *msgpb.TLMsgSendMessageV2) (*mtproto.Updates, error) {
	id := in.GetMessage()[0].GetRandomId()
	if !s.seen[id] {
		s.seen[id] = true
		s.sent = append(s.sent, in)
	}
	return mtproto.MakeEmptyUpdates(), nil
}

// desktopSession is a Desktop session on Windows in Prague.
type desktopSession struct {
	authsession_client.AuthsessionClient
}

func (desktopSession) AuthsessionGetAuthorization(context.Context, *authsession.TLAuthsessionGetAuthorization) (*mtproto.Authorization, error) {
	return &mtproto.Authorization{AppName: "Pluma Desktop", AppVersion: "6.2", DeviceModel: "Desktop PC", Platform: "Windows"}, nil
}

func (desktopSession) AuthsessionGetIpRegion(_ context.Context, in *authsession.TLAuthsessionGetIpRegion) (*mtproto.String, error) {
	if in.Ip != "193.17.47.1" {
		return mtproto.MakeTLString(&mtproto.String{}).To_String(), nil
	}
	return mtproto.MakeTLString(&mtproto.String{V: "Prague, Czechia"}).To_String(), nil
}

// keysOf has the keys of the bots' tokens.
type keysOf map[int64]botkeys.Keys

func (k keysOf) Get(_ context.Context, botId int64) (botkeys.Keys, bool, error) {
	keys, ok := k[botId]
	return keys, ok, nil
}

// TestUrlAuthOnMySQL: a bot with a login domain sends a login button; the
// user is asked, accepts with the permission to be written to, and gets the
// address with the signed data; the bot's chat gets messageActionBotAllowed
// once; the site is listed among the sites logged in on and the next press
// logs in at once; after the site is reset the press asks again.
func TestUrlAuthOnMySQL(t *testing.T) {
	ctx := context.Background()
	mysql, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mysql.Terminate(context.Background()) })
	conf := mysql.Config()
	store := botstore.New(sqlx.NewMySQL(&conf))

	const user = int64(1000001)
	bot, token, err := store.Create(ctx, "site_login_bot", "Site bot", 0)
	if err != nil {
		t.Fatal(err)
	}
	keyless, _, err := store.Create(ctx, "keyless_login_bot", "Keyless bot", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SetLoginDomain(ctx, bot, "site.example"); err != nil {
		t.Fatal(err)
	}
	if err = store.SetLoginDomain(ctx, keyless, "keyless.example"); err != nil {
		t.Fatal(err)
	}
	keys := botkeys.Derive(token)

	users := loginUsers{user: user, bots: map[int64]bool{bot: true, keyless: true}}
	messages := &botMessage{user: user, bot: bot}
	sent := &sentOnce{seen: map[int64]bool{}}
	svcCtx := &svc.ServiceContext{
		Config: config.Config{Avatars: "https://pluma.chat/_l/avatar/"},
		Dao: &dao.Dao{
			UserClient: users, MessageClient: messages, MsgClient: sent, AuthsessionClient: desktopSession{},
			Bots: store,
		},
		BotKeys: keysOf{bot: keys},
	}
	coreOf := func(id int64) *BotsCore {
		return &BotsCore{
			ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx),
			MD: &metadata.RpcMetadata{UserId: id, PermAuthKeyId: 77, ClientAddr: "193.17.47.1"},
		}
	}

	// The bot's markup: a link, then the login button asking for the
	// permission to write.
	markup, err := botmarkup.Normalize(mtproto.MakeTLReplyInlineMarkup(&mtproto.ReplyMarkup{
		Rows: []*mtproto.KeyboardButtonRow{mtproto.MakeTLKeyboardButtonRow(&mtproto.KeyboardButtonRow{Buttons: []*mtproto.KeyboardButton{
			mtproto.MakeTLKeyboardButtonUrl(&mtproto.KeyboardButton{Text: "About", Url: "https://site.example/about"}).To_KeyboardButton(),
			mtproto.MakeTLInputKeyboardButtonUrlAuth(&mtproto.KeyboardButton{
				Text: "Log in", Url: "https://site.example/login?next=%2Fhome", Bot: mtproto.MakeTLInputUserSelf(nil).To_InputUser(), RequestWriteAccess: true,
			}).To_KeyboardButton(),
		}}).To_KeyboardButtonRow()},
	}).To_ReplyMarkup(), coreOf(bot).markupEnv())
	if err != nil {
		t.Fatal(err)
	}
	messages.markup = markup
	button := &mtproto.TLMessagesRequestUrlAuth{
		Peer: mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: bot, AccessHash: 1}).To_InputPeer(), MsgId: wrapperspb.Int32(5), ButtonId: wrapperspb.Int32(2),
	}
	accept := &mtproto.TLMessagesAcceptUrlAuth{WriteAllowed: true, Peer: button.Peer, MsgId: button.MsgId, ButtonId: button.ButtonId}
	c := coreOf(user)

	checkLogin := func(t *testing.T, r *mtproto.UrlAuthResult) {
		t.Helper()
		if r.GetPredicateName() != mtproto.Predicate_urlAuthResultAccepted {
			t.Fatalf("got %s, want urlAuthResultAccepted", r.GetPredicateName())
		}
		u, err := url.Parse(r.GetUrl_FLAGSTRING().GetValue())
		if err != nil || !strings.EqualFold(u.Host, "site.example") || u.Path != "/login" {
			t.Fatalf("address %q: %v", r.GetUrl_FLAGSTRING().GetValue(), err)
		}
		q := u.Query()
		if q.Get("next") != "/home" {
			t.Errorf("the site's own field is lost: %v", q)
		}
		q.Del("next")
		if err := webappdata.VerifyLogin(q, keys.Login); err != nil {
			t.Fatalf("the data do not verify with the bot's token: %v", err)
		}
		if q.Get("id") != "1000001" || q.Get("first_name") != "Ann" || q.Get("last_name") != "Lee" || q.Get("username") != "ann" ||
			q.Get("photo_url") != "https://pluma.chat/_l/avatar/ann" || q.Get("auth_date") == "" {
			t.Errorf("data %v", q)
		}
	}

	r, err := c.MessagesRequestUrlAuth(button)
	if err != nil {
		t.Fatal(err)
	}
	if r.GetPredicateName() != mtproto.Predicate_urlAuthResultRequest || !r.GetRequestWriteAccess() || r.GetDomain() != "site.example" || r.GetBot().GetId() != bot {
		t.Fatalf("first press: %v", r)
	}

	r, err = c.MessagesAcceptUrlAuth(accept)
	if err != nil {
		t.Fatal(err)
	}
	checkLogin(t, r)
	if len(sent.sent) != 1 {
		t.Fatalf("%d messages sent, want the permission's one", len(sent.sent))
	}
	m := sent.sent[0]
	if m.UserId != user || m.PeerType != mtproto.PEER_USER || m.PeerId != bot || m.AuthKeyId != 0 ||
		m.GetMessage()[0].GetRandomId() != botAllowedRandomId(user, bot) {
		t.Errorf("permission message %+v", m)
	}
	if a := m.GetMessage()[0].GetMessage().GetAction(); a.GetPredicateName() != mtproto.Predicate_messageActionBotAllowed || a.GetDomain_FLAGSTRING().GetValue() != "site.example" {
		t.Errorf("action %v", a)
	}
	if ok, err := store.WriteAllowed(ctx, user, bot); err != nil || !ok {
		t.Errorf("the bot may not write: %v, %v", ok, err)
	}

	// Logged in and nothing new to ask: in at once, and no second message.
	r, err = c.MessagesRequestUrlAuth(button)
	if err != nil {
		t.Fatal(err)
	}
	checkLogin(t, r)
	if r, err = c.MessagesAcceptUrlAuth(accept); err != nil {
		t.Fatal(err)
	}
	checkLogin(t, r)
	if len(sent.sent) != 1 {
		t.Errorf("%d messages sent, want one", len(sent.sent))
	}

	sites, err := c.AccountGetWebAuthorizations(&mtproto.TLAccountGetWebAuthorizations{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sites.GetAuthorizations()) != 1 || len(sites.GetUsers()) != 1 || sites.GetUsers()[0].GetId() != bot {
		t.Fatalf("sites %v", sites)
	}
	w := sites.GetAuthorizations()[0]
	if w.GetBotId() != bot || w.GetDomain() != "site.example" || w.GetBrowser() != "Pluma Desktop 6.2" || w.GetPlatform() != "Desktop PC, Windows" ||
		w.GetIp() != "193.17.47.1" || w.GetRegion() != "Prague, Czechia" || w.GetHash() == 0 || w.GetDateActive() < w.GetDateCreated() {
		t.Errorf("site %v", w)
	}

	// Reset: the press asks again, without the permission given already.
	if ok, err := c.AccountResetWebAuthorization(&mtproto.TLAccountResetWebAuthorization{Hash: w.GetHash()}); err != nil || !mtproto.FromBool(ok) {
		t.Fatalf("reset: %v, %v", ok, err)
	}
	if ok, err := c.AccountResetWebAuthorization(&mtproto.TLAccountResetWebAuthorization{Hash: w.GetHash()}); err != nil || !mtproto.FromBool(ok) {
		t.Fatalf("reset again: %v, %v", ok, err)
	}
	if r, err = c.MessagesRequestUrlAuth(button); err != nil {
		t.Fatal(err)
	}
	if r.GetPredicateName() != mtproto.Predicate_urlAuthResultRequest || r.GetRequestWriteAccess() {
		t.Errorf("after the reset: %v", r)
	}

	// A link of url_auth_domains logs in the same way, at the link's
	// address; a link to a domain of no bot is URL_INVALID.
	link := &mtproto.TLMessagesAcceptUrlAuth{Url: wrapperspb.String("https://SITE.example/login?next=%2Fhome")}
	if r, err = c.MessagesAcceptUrlAuth(link); err != nil {
		t.Fatal(err)
	}
	checkLogin(t, r)
	if _, err = c.MessagesRequestUrlAuth(&mtproto.TLMessagesRequestUrlAuth{Url: wrapperspb.String("https://elsewhere.example/")}); !errors.Is(err, mtproto.ErrUrlInvalid) {
		t.Errorf("a link of no bot: %v", err)
	}
	if ok, err := c.AccountResetWebAuthorizations(&mtproto.TLAccountResetWebAuthorizations{}); err != nil || !mtproto.FromBool(ok) {
		t.Fatalf("reset all: %v, %v", ok, err)
	}
	if sites, err = c.AccountGetWebAuthorizations(&mtproto.TLAccountGetWebAuthorizations{}); err != nil || len(sites.GetAuthorizations()) != 0 || len(sites.GetUsers()) != 0 {
		t.Errorf("sites after reset all: %v, %v", sites, err)
	}

	// A bot without keys logs nobody in: the link opens as it is.
	if r, err = c.MessagesRequestUrlAuth(&mtproto.TLMessagesRequestUrlAuth{Url: wrapperspb.String("https://keyless.example/")}); err != nil || r.GetPredicateName() != mtproto.Predicate_urlAuthResultDefault {
		t.Errorf("a bot without keys: %v, %v", r, err)
	}
	// A button whose bot has another domain by now opens its link as it is.
	if err = store.SetLoginDomain(ctx, bot, "new.example"); err != nil {
		t.Fatal(err)
	}
	if r, err = c.MessagesRequestUrlAuth(button); err != nil || r.GetPredicateName() != mtproto.Predicate_urlAuthResultDefault {
		t.Errorf("the bot's domain changed: %v, %v", r, err)
	}
	// A button that is not a login button of the message.
	if _, err = c.MessagesRequestUrlAuth(&mtproto.TLMessagesRequestUrlAuth{Peer: button.Peer, MsgId: button.MsgId, ButtonId: wrapperspb.Int32(1)}); !errors.Is(err, mtproto.ErrButtonIdInvalid) {
		t.Errorf("the plain link button: %v", err)
	}
}
