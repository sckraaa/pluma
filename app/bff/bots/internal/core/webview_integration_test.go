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
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/svc"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	media_client "github.com/teamgram/teamgram-server/app/service/media/client"
	mediapb "github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/botkeys"
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/sendas"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/webappdata"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/types/known/wrapperspb"

	_ "github.com/go-sql-driver/mysql"
)

// webViewUsers are loginUsers nobody of whom blocked anybody.
type webViewUsers struct {
	loginUsers
}

func (webViewUsers) UserBlockedByUser(context.Context, *userpb.TLUserBlockedByUser) (*mtproto.Bool, error) {
	return mtproto.BoolFalse, nil
}

// appPhotos has every photo the apps name.
type appPhotos struct {
	media_client.MediaClient
}

func (appPhotos) MediaGetPhoto(_ context.Context, in *mediapb.TLMediaGetPhoto) (*mtproto.Photo, error) {
	return mtproto.MakeTLPhoto(&mtproto.Photo{Id: in.PhotoId, AccessHash: 3}).To_Photo(), nil
}

// launch reads the launch parameters of a web view's address and checks
// its initData as the bot and a third party would.
func launch(t *testing.T, r *mtproto.WebViewResult, appURL string, botId int64, keys botkeys.Keys, signer *webappdata.Signer) (url.Values, url.Values) {
	t.Helper()
	base, frag, ok := strings.Cut(r.GetUrl(), "#")
	if !ok || base != appURL {
		t.Fatalf("address %q, want %s with a fragment", r.GetUrl(), appURL)
	}
	params, err := url.ParseQuery(frag)
	if err != nil {
		t.Fatal(err)
	}
	data, err := webappdata.Verify(params.Get("tgWebAppData"), botId, keys.WebApp, signer.PublicKey())
	if err != nil {
		t.Fatalf("initData %q: %v", params.Get("tgWebAppData"), err)
	}
	return params, data
}

// TestWebViewsOnMySQL: the mini apps of a bot open with initData signed for
// the bot; a web view the bot may answer is prolonged until it is gone; a
// bot without keys opens none; a direct link app is inactive until the
// first launch, which with write_allowed lets the bot write; the switch
// button of inline results opens only the address the user got.
func TestWebViewsOnMySQL(t *testing.T) {
	ctx := context.Background()
	mysql, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mysql.Terminate(context.Background()) })
	conf := mysql.Config()
	db := sqlx.NewMySQL(&conf)
	store := botstore.New(db)

	const (
		user    = int64(1000001)
		other   = int64(1000002)
		menuURL = "https://shop.example/app"
		appURL  = "https://shop.example/direct#/home"
		mainURL = "https://shop.example/main"
	)
	bot, token, err := store.Create(ctx, "shop_bot", "Shop bot", 0)
	if err != nil {
		t.Fatal(err)
	}
	appBot, appToken, err := store.Create(ctx, "games_bot", "Games bot", 0)
	if err != nil {
		t.Fatal(err)
	}
	keyless, _, err := store.Create(ctx, "keyless_shop_bot", "Keyless bot", 0)
	if err != nil {
		t.Fatal(err)
	}
	keys, appKeys := botkeys.Derive(token), botkeys.Derive(appToken)
	signer, err := webappdata.NewSigner(strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	// The user let the shop bot write: allows_write_to_pm.
	if _, err = store.AllowWrite(ctx, user, bot, botstore.WriteAllowedDomain, int32(time.Now().Unix())); err != nil {
		t.Fatal(err)
	}

	sent := &sentOnce{seen: map[int64]bool{}}
	svcCtx := &svc.ServiceContext{
		Config: config.Config{Avatars: "https://pluma.chat/_l/avatar/"},
		Dao: &dao.Dao{
			UserClient:  webViewUsers{loginUsers{user: user, bots: map[int64]bool{bot: true, appBot: true, keyless: true}}},
			MediaClient: appPhotos{},
			MsgClient:   sent,
			Bots:        store,
			KV:          testKV(t),
			SendAs:      sendas.New(db),
		},
		BotKeys:      keysOf{bot: keys, appBot: appKeys},
		WebAppSigner: signer,
	}
	c := &BotsCore{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx), MD: &metadata.RpcMetadata{UserId: user, LangCode: "de"}}
	peerOf := func(id int64) *mtproto.InputPeer {
		return mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: id, AccessHash: 1}).To_InputPeer()
	}
	userOf := func(id int64) *mtproto.InputUser {
		return mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: id, AccessHash: 1}).To_InputUser()
	}
	theme := &mtproto.DataJSON{Data: `{"bg_color":-1,"text_color":"#000000"}`}

	t.Run("menu button", func(t *testing.T) {
		in := &mtproto.TLMessagesRequestWebView{
			FromBotMenu: true, Peer: peerOf(bot), Bot: userOf(bot), Url: wrapperspb.String(menuURL),
			ThemeParams: theme, Platform: "android", StartParam: wrapperspb.String("promo"),
			ReplyTo: mtproto.MakeTLInputReplyToMessage(&mtproto.InputReplyTo{ReplyToMsgId: 7}).To_InputReplyTo(),
		}
		r, err := c.MessagesRequestWebView(in)
		if err != nil {
			t.Fatal(err)
		}
		params, data := launch(t, r, menuURL, bot, keys, signer)
		if params.Get("tgWebAppVersion") != "8.0" || params.Get("tgWebAppPlatform") != "android" || params.Get("tgWebAppStartParam") != "promo" ||
			params.Get("tgWebAppThemeParams") != `{"bg_color":"#ffffff","text_color":"#000000"}` {
			t.Errorf("launch parameters %v", params)
		}
		queryId := r.GetQueryId_FLAGINT64().GetValue()
		if queryId == 0 || data.Get("query_id") != strconv.FormatInt(queryId, 10) || data.Get("start_param") != "promo" || data.Get("auth_date") == "" {
			t.Errorf("initData %v, query %d", data, queryId)
		}
		var u webappdata.User
		if err := json.Unmarshal([]byte(data.Get("user")), &u); err != nil {
			t.Fatal(err)
		}
		if u.Id != user || u.FirstName != "Ann" || u.Username != "ann" || u.LanguageCode != "de" || !u.IsPremium || !u.AllowsWriteToPm ||
			u.AddedToAttachmentMenu || u.PhotoUrl != "https://pluma.chat/_l/avatar/ann" {
			t.Errorf("user %+v", u)
		}
		if data.Get("chat_type") != "" || data.Get("receiver") != "" || data.Get("chat") != "" {
			t.Errorf("a menu button has no chat: %v", data)
		}

		// Stored for the bot's answer, prolonged while it is there.
		w, err := svcCtx.GetWebView(ctx, queryId)
		if err != nil || w == nil || w.User != user || w.Bot != bot || w.PeerId != bot || w.ReplyTo.GetReplyToMsgId() != 7 {
			t.Fatalf("stored web view %+v, %v", w, err)
		}
		prolong := &mtproto.TLMessagesProlongWebView{Peer: peerOf(bot), Bot: userOf(bot), QueryId: queryId, Silent: true}
		if ok, err := c.MessagesProlongWebView(prolong); err != nil || !mtproto.FromBool(ok) {
			t.Fatalf("prolong: %v, %v", ok, err)
		}
		if w, _ = svcCtx.GetWebView(ctx, queryId); !w.Silent || w.ReplyTo != nil {
			t.Errorf("the prolonged web view keeps what the last request says: %+v", w)
		}
		if _, err := c.MessagesProlongWebView(&mtproto.TLMessagesProlongWebView{Peer: peerOf(bot), Bot: userOf(appBot), QueryId: queryId}); err != mtproto.ErrQueryIdInvalid {
			t.Errorf("another bot's prolong: %v", err)
		}
		stranger := &BotsCore{ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx), MD: &metadata.RpcMetadata{UserId: other}}
		if _, err := stranger.MessagesProlongWebView(prolong); err != mtproto.ErrQueryIdInvalid {
			t.Errorf("another user's prolong: %v", err)
		}
		if _, err := svcCtx.KV.DelCtx(ctx, "bot_webview:"+strconv.FormatInt(queryId, 10)); err != nil {
			t.Fatal(err)
		}
		if _, err := c.MessagesProlongWebView(prolong); err != mtproto.ErrQueryIdInvalid {
			t.Errorf("a web view gone: %v", err)
		}
	})

	t.Run("refused", func(t *testing.T) {
		if _, err := c.MessagesRequestWebView(&mtproto.TLMessagesRequestWebView{
			Peer: peerOf(keyless), Bot: userOf(keyless), Url: wrapperspb.String(menuURL), Platform: "ios",
		}); err != mtproto.ErrBotWebviewDisabled {
			t.Errorf("a bot without keys: %v", err)
		}
		if _, err := c.MessagesRequestWebView(&mtproto.TLMessagesRequestWebView{
			Peer: peerOf(appBot), Bot: userOf(bot), Url: wrapperspb.String(menuURL), Platform: "ios",
		}); err != mtproto.ErrBotWebviewDisabled {
			t.Errorf("a button of the bot in another chat: %v", err)
		}
		if _, err := c.MessagesRequestWebView(&mtproto.TLMessagesRequestWebView{
			Peer: peerOf(bot), Bot: userOf(bot), Url: wrapperspb.String("http://shop.example/"), Platform: "ios",
		}); err != mtproto.ErrUrlInvalid {
			t.Errorf("an http address: %v", err)
		}
		if _, err := c.MessagesRequestWebView(&mtproto.TLMessagesRequestWebView{Peer: peerOf(bot), Bot: userOf(bot), Platform: "ios"}); err != mtproto.ErrBotInvalid {
			t.Errorf("the attachment menu of a bot without one: %v", err)
		}
		if _, err := c.MessagesRequestWebView(&mtproto.TLMessagesRequestWebView{Peer: peerOf(bot), Bot: userOf(other), Url: wrapperspb.String(menuURL)}); err != mtproto.ErrBotInvalid {
			t.Errorf("a user that is not a bot: %v", err)
		}
	})

	t.Run("switch button", func(t *testing.T) {
		in := &mtproto.TLMessagesRequestSimpleWebView413A3E73{FromSwitchWebview: true, Bot: userOf(bot), Url: wrapperspb.String(menuURL), Platform: "tdesktop"}
		if _, err := c.MessagesRequestSimpleWebView413A3E73(in); err != mtproto.ErrUrlInvalid {
			t.Fatalf("a switch button the user did not get: %v", err)
		}
		if err := svcCtx.RememberSwitchWebview(ctx, bot, user, menuURL, 0); err != nil {
			t.Fatal(err)
		}
		r, err := c.MessagesRequestSimpleWebView413A3E73(in)
		if err != nil {
			t.Fatal(err)
		}
		params, data := launch(t, r, menuURL, bot, keys, signer)
		if params.Get("tgWebAppBotInline") != "1" || r.GetQueryId_FLAGINT64() != nil || data.Get("query_id") != "" {
			t.Errorf("a simple web view: %v, query %v", params, r.GetQueryId_FLAGINT64())
		}
	})

	t.Run("main app", func(t *testing.T) {
		in := &mtproto.TLMessagesRequestMainWebView{Peer: peerOf(bot), Bot: userOf(bot), Platform: "weba"}
		if _, err := c.MessagesRequestMainWebView(in); err != mtproto.ErrBotWebviewDisabled {
			t.Fatalf("no main app: %v", err)
		}
		if err := store.SetMainApp(ctx, bot, mainURL); err != nil {
			t.Fatal(err)
		}
		r, err := c.MessagesRequestMainWebView(in)
		if err != nil {
			t.Fatal(err)
		}
		_, data := launch(t, r, mainURL, bot, keys, signer)
		if !r.GetFullsize() || r.GetQueryId_FLAGINT64() == nil || data.Get("chat_type") != "sender" || data.Get("chat_instance") == "" {
			t.Errorf("the main app from the chat with the bot: %v, %v", r, data)
		}
		in.Peer, in.Compact = mtproto.MakeTLInputPeerEmpty(nil).To_InputPeer(), true
		if r, err = c.MessagesRequestMainWebView(in); err != nil {
			t.Fatal(err)
		}
		if _, data = launch(t, r, mainURL, bot, keys, signer); r.GetFullsize() || r.GetQueryId_FLAGINT64() != nil || data.Get("chat_type") != "" {
			t.Errorf("the main app from no chat, compact: %v, %v", r, data)
		}
	})

	t.Run("direct link app", func(t *testing.T) {
		app, err := store.AddApp(ctx, botstore.App{BotId: appBot, ShortName: "chess", Title: "Chess", URL: appURL, PhotoId: 77, RequestWriteAccess: true})
		if err != nil {
			t.Fatal(err)
		}
		byName := mtproto.MakeTLInputBotAppShortName(&mtproto.InputBotApp{BotId: userOf(appBot), ShortName: "chess"}).To_InputBotApp()
		got, err := c.MessagesGetBotApp(&mtproto.TLMessagesGetBotApp{App: byName})
		if err != nil {
			t.Fatal(err)
		}
		a := got.GetApp()
		if !got.GetInactive() || !got.GetRequestWriteAccess() || a.GetPredicateName() != mtproto.Predicate_botApp || a.GetId() != app.Id ||
			a.GetHash() != int64(app.Version) || a.GetPhoto().GetId() != 77 || a.GetTitle() != "Chess" {
			t.Fatalf("botApp %v", got)
		}
		if got, err = c.MessagesGetBotApp(&mtproto.TLMessagesGetBotApp{App: byName, Hash: a.GetHash()}); err != nil || got.GetApp().GetPredicateName() != mtproto.Predicate_botAppNotModified {
			t.Fatalf("with the hash: %v, %v", got, err)
		}
		if _, err = c.MessagesGetBotApp(&mtproto.TLMessagesGetBotApp{App: mtproto.MakeTLInputBotAppID(&mtproto.InputBotApp{Id: app.Id, AccessHash: app.AccessHash + 1}).To_InputBotApp()}); err != mtproto.ErrBotAppInvalid {
			t.Errorf("a wrong access hash: %v", err)
		}

		if may, err := c.BotsCanSendMessage(&mtproto.TLBotsCanSendMessage{Bot: userOf(bot)}); err != nil || !mtproto.FromBool(may) {
			t.Errorf("canSendMessage of the allowed bot: %v, %v", may, err)
		}
		r, err := c.MessagesRequestAppWebView53618BCE(&mtproto.TLMessagesRequestAppWebView53618BCE{
			WriteAllowed: true, Peer: peerOf(appBot), Platform: "ios",
			App: mtproto.MakeTLInputBotAppID(&mtproto.InputBotApp{Id: app.Id, AccessHash: app.AccessHash}).To_InputBotApp(),
		})
		if err != nil {
			t.Fatal(err)
		}
		base, frag, _ := strings.Cut(r.GetUrl(), "#/home?")
		if base != "https://shop.example/direct" {
			t.Fatalf("the app's own fragment is lost: %s", r.GetUrl())
		}
		params, err := url.ParseQuery(frag)
		if err != nil {
			t.Fatal(err)
		}
		data, err := webappdata.Verify(params.Get("tgWebAppData"), appBot, appKeys.WebApp, signer.PublicKey())
		if err != nil {
			t.Fatal(err)
		}
		if data.Get("chat_type") != "sender" || data.Get("query_id") == "" || !strings.Contains(data.Get("user"), `"allows_write_to_pm":true`) {
			t.Errorf("initData %v", data)
		}
		if len(sent.sent) != 1 {
			t.Fatalf("%d messages sent, want the permission's one", len(sent.sent))
		}
		if a := sent.sent[0].GetMessage()[0].GetMessage().GetAction(); a.GetPredicateName() != mtproto.Predicate_messageActionBotAllowed || a.GetApp().GetId() != app.Id {
			t.Errorf("action %v", a)
		}
		if got, err = c.MessagesGetBotApp(&mtproto.TLMessagesGetBotApp{App: byName}); err != nil || got.GetInactive() {
			t.Errorf("after the launch: %v, %v", got, err)
		}
		// Let before: nothing more to post.
		if u, err := c.BotsAllowSendMessage(&mtproto.TLBotsAllowSendMessage{Bot: userOf(appBot)}); err != nil || len(u.GetUpdates()) != 0 || len(sent.sent) != 1 {
			t.Errorf("allowSendMessage again: %v, %v", u, err)
		}
	})
}
