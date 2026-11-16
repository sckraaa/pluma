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
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"

	"google.golang.org/protobuf/types/known/wrapperspb"
)

// serverWebAppKey is the public Ed25519 key of the stand, with which a third
// party checks the signature of initData (pluma-admin bot webapp-key).
func serverWebAppKey(t *testing.T) ed25519.PublicKey {
	t.Helper()
	keyHex := strings.TrimSpace(admin(t, "bot", "webapp-key"))
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) != ed25519.PublicKeySize {
		t.Fatalf("the server's web app key %q: %v", keyHex, err)
	}
	return key
}

// TestBotAppDirectLink: the administrator gives a bot a direct link mini
// app (task 106, ADR 0068 p. 5; core.telegram.org/api/bots/webapps#direct-link-mini-apps).
// The user's client reads it by the bot and the short name: inactive, asking
// for the permission to write; opens it by its id with that permission, and
// gets initData signed for the bot with chat_type and start_param; the chat
// with the bot gets messageActionBotAllowed with the app. Read again with
// its hash, the app is botAppNotModified and no longer inactive; an edit by
// the administrator changes the hash; a removed app or a wrong access hash
// is BOT_APP_INVALID.
func TestBotAppDirectLink(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]
	alice := login(ctx, t, phone, "Alice")
	getState(ctx, t, alice)
	username := fmt.Sprintf("e2e%dbot", randomID()&0xffffff)
	bot, token := botLoggedInWithToken(ctx, t, phone, username, 228)
	getState(ctx, t, bot)
	publicKey := serverWebAppKey(t)

	picture := filepath.Join(t.TempDir(), "shop.png")
	if err := os.WriteFile(picture, gamePicture(t), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := fmt.Sprintf("/tmp/app-%d.png", uint64(randomID()))
	stack(t, "cp", picture, "bff:"+dest)
	defer stack(t, "exec", "-T", "bff", "rm", "-f", dest)
	const appURL = "https://shop.example/direct"
	admin(t, "bot", "app", "add", username, "shop", "-title", "Shop", "-description", "Buy things", "-url", appURL, "-photo", dest, "-write-access", "on")

	toBot := resolveBot(ctx, t, alice, username)
	botUser := inputUserOfPeer(alice.c, toBot)
	byName := func(name string) *mtproto.InputBotApp {
		return &mtproto.InputBotApp{Constructor: alice.c.Constructor(mtproto.Predicate_inputBotAppShortName), BotId: botUser, ShortName: name}
	}
	getApp := func(in *mtproto.InputBotApp, hash int64) (*mtproto.Messages_BotApp, error) {
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetBotApp{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_getBotApp),
			App:         in, Hash: hash,
		})
		if err != nil {
			return nil, err
		}
		return o.(interface {
			To_Messages_BotApp() *mtproto.Messages_BotApp
		}).To_Messages_BotApp(), nil
	}
	open := func(in *mtproto.InputBotApp, write bool) (mtproto.TLObject, error) {
		return alice.c.Invoke(ctx, &mtproto.TLMessagesRequestAppWebView53618BCE{
			Constructor:  alice.c.Constructor(mtproto.Predicate_messages_requestAppWebView53618BCE),
			WriteAllowed: write, Peer: toBot, App: in,
			StartParam: wrapperspb.String("ref1"), Platform: "ios",
		})
	}

	if _, err := getApp(byName("nope"), 0); !strings.Contains(fmt.Sprint(err), "BOT_APP_INVALID") {
		t.Fatalf("an app the bot does not have: %v", err)
	}
	first, err := getApp(byName("shop"), 0)
	if err != nil {
		t.Fatalf("messages.getBotApp: %v", err)
	}
	app := first.GetApp()
	if !first.GetInactive() || !first.GetRequestWriteAccess() || app.GetPredicateName() != mtproto.Predicate_botApp ||
		app.GetShortName() != "shop" || app.GetTitle() != "Shop" || app.GetDescription() != "Buy things" ||
		app.GetPhoto().GetPredicateName() != mtproto.Predicate_photo || app.GetHash() == 0 {
		t.Fatalf("the app before its first launch: %v", first)
	}
	byId := &mtproto.InputBotApp{Constructor: alice.c.Constructor(mtproto.Predicate_inputBotAppID), Id: app.GetId(), AccessHash: app.GetAccessHash()}
	wrongHash := &mtproto.InputBotApp{Constructor: alice.c.Constructor(mtproto.Predicate_inputBotAppID), Id: app.GetId(), AccessHash: app.GetAccessHash() ^ 1}
	if _, err = open(wrongHash, false); !strings.Contains(fmt.Sprint(err), "BOT_APP_INVALID") {
		t.Fatalf("a wrong access hash: %v", err)
	}

	o, err := open(byId, true)
	r := webViewResult(t, o, err, "messages.requestAppWebView")
	params, data := checkInitData(t, r.GetUrl(), appURL, token, bot.self.GetId(), publicKey)
	if params.Get("tgWebAppPlatform") != "ios" || params.Get("tgWebAppStartParam") != "ref1" || !r.GetFullsize() {
		t.Errorf("launch parameters %v of %v", params, r)
	}
	if data.Get("start_param") != "ref1" || data.Get("chat_type") != "sender" || data.Get("chat_instance") == "" ||
		!strings.Contains(data.Get("user"), fmt.Sprintf(`"id":%d`, alice.self.GetId())) || !strings.Contains(data.Get("user"), `"allows_write_to_pm":true`) {
		t.Errorf("initData %v", data)
	}
	waitUpdate(ctx, t, bot, mtproto.Predicate_updateNewMessage, func(u *mtproto.Update) bool {
		a := u.GetMessage_MESSAGE().GetAction()
		return a.GetPredicateName() == mtproto.Predicate_messageActionBotAllowed && a.GetApp().GetShortName() == "shop"
	})

	same, err := getApp(byName("shop"), app.GetHash())
	if err != nil {
		t.Fatalf("messages.getBotApp with its hash: %v", err)
	}
	if same.GetInactive() || same.GetApp().GetPredicateName() != mtproto.Predicate_botAppNotModified {
		t.Fatalf("the app after its launch, with its hash: %v", same)
	}

	admin(t, "bot", "app", "edit", username, "shop", "-title", "Shop 2")
	edited, err := getApp(byName("shop"), app.GetHash())
	if err != nil {
		t.Fatalf("messages.getBotApp after an edit: %v", err)
	}
	if edited.GetApp().GetPredicateName() != mtproto.Predicate_botApp || edited.GetApp().GetTitle() != "Shop 2" || edited.GetApp().GetHash() == app.GetHash() {
		t.Fatalf("the edited app: %v", edited)
	}

	admin(t, "bot", "app", "rm", username, "shop")
	if _, err = open(byId, false); !strings.Contains(fmt.Sprint(err), "BOT_APP_INVALID") {
		t.Fatalf("a removed app: %v", err)
	}
}

// TestSimpleWebView: a mini app with no chat to answer into
// (messages.requestSimpleWebView; core.telegram.org/api/bots/webapps): the
// web_app button of a keyboard opens its address with initData and no
// query_id; an address that is not https and a malformed platform are
// refused. The switch_webview button of inline results opens only the
// address the bot gave with the results the user got, with
// tgWebAppBotInline.
func TestSimpleWebView(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]
	alice := login(ctx, t, phone, "Alice")
	getState(ctx, t, alice)
	username := fmt.Sprintf("e2e%dbot", randomID()&0xffffff)
	bot, token := botLoggedInWithToken(ctx, t, phone, username, 228)
	getState(ctx, t, bot)
	publicKey := serverWebAppKey(t)
	admin(t, "bot", "set", username, "-inline", "Shop...")

	toBot := resolveBot(ctx, t, alice, username)
	botUser := inputUserOfPeer(alice.c, toBot)
	simple := func(link, platform string, fromSwitch bool) (mtproto.TLObject, error) {
		return alice.c.Invoke(ctx, &mtproto.TLMessagesRequestSimpleWebView413A3E73{
			Constructor:       alice.c.Constructor(mtproto.Predicate_messages_requestSimpleWebView413A3E73),
			FromSwitchWebview: fromSwitch, Bot: botUser, Url: wrapperspb.String(link), Platform: platform,
		})
	}

	const keyboardURL = "https://shop.example/keyboard"
	o, err := simple(keyboardURL, "android", false)
	r := webViewResult(t, o, err, "messages.requestSimpleWebView")
	params, data := checkInitData(t, r.GetUrl(), keyboardURL, token, bot.self.GetId(), publicKey)
	if params.Get("tgWebAppPlatform") != "android" || params.Get("tgWebAppBotInline") != "" ||
		r.GetQueryId_FLAGINT64() != nil || data.Get("query_id") != "" ||
		!strings.Contains(data.Get("user"), fmt.Sprintf(`"id":%d`, alice.self.GetId())) {
		t.Errorf("the keyboard's mini app: %v, %v, %v", r, params, data)
	}
	if _, err = simple("http://shop.example/keyboard", "android", false); !strings.Contains(fmt.Sprint(err), "URL_INVALID") {
		t.Errorf("an address that is not https: %v", err)
	}
	if _, err = simple(keyboardURL, "and roid", false); !strings.Contains(fmt.Sprint(err), "PLATFORM_INVALID") {
		t.Errorf("a malformed platform: %v", err)
	}

	// switch_webview opens only the address of the results the user got.
	const inlineURL = "https://shop.example/inline"
	if _, err = simple(inlineURL, "tdesktop", true); !strings.Contains(fmt.Sprint(err), "URL_INVALID") {
		t.Fatalf("switch_webview before any results: %v", err)
	}
	asked := make(chan error, 1)
	var results *mtproto.Messages_BotResults
	go func() {
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesGetInlineBotResults{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_getInlineBotResults),
			Bot:         botUser, Peer: toBot, Query: "shoes",
		})
		if err == nil {
			results = o.(interface {
				To_Messages_BotResults() *mtproto.Messages_BotResults
			}).To_Messages_BotResults()
		}
		asked <- err
	}()
	var queryId int64
	waitUpdate(ctx, t, bot, mtproto.Predicate_updateBotInlineQuery, func(u *mtproto.Update) bool {
		if u.GetQuery() != "shoes" {
			return false
		}
		queryId = u.GetQueryId_INT64()
		return true
	})
	if _, err = bot.c.Invoke(ctx, &mtproto.TLMessagesSetInlineBotResults{
		Constructor: bot.c.Constructor(mtproto.Predicate_messages_setInlineBotResults),
		QueryId:     queryId,
		Results: []*mtproto.InputBotInlineResult{{
			Constructor: bot.c.Constructor(mtproto.Predicate_inputBotInlineResult),
			Id:          "a", Type: "article", Title: wrapperspb.String("Shoes"),
			SendMessage: &mtproto.InputBotInlineMessage{Constructor: bot.c.Constructor(mtproto.Predicate_inputBotInlineMessageText), Message: "Shoes"},
		}},
		SwitchWebview: &mtproto.InlineBotWebView{Constructor: bot.c.Constructor(mtproto.Predicate_inlineBotWebView), Text: "Open the shop", Url: inlineURL},
	}); err != nil {
		t.Fatalf("messages.setInlineBotResults with switch_webview: %v", err)
	}
	if err = <-asked; err != nil {
		t.Fatalf("messages.getInlineBotResults: %v", err)
	}
	if results.GetSwitchWebview().GetUrl() != inlineURL {
		t.Fatalf("the results' switch_webview: %v", results)
	}
	if _, err = simple("https://shop.example/other", "tdesktop", true); !strings.Contains(fmt.Sprint(err), "URL_INVALID") {
		t.Errorf("switch_webview with another address: %v", err)
	}
	o, err = simple(inlineURL, "tdesktop", true)
	r = webViewResult(t, o, err, "messages.requestSimpleWebView from switch_webview")
	if params, _ = checkInitData(t, r.GetUrl(), inlineURL, token, bot.self.GetId(), publicKey); params.Get("tgWebAppBotInline") != "1" {
		t.Errorf("the switch_webview mini app may not switch to inline mode: %v", params)
	}
}

// TestUrlAuthLinksAndResetAll: links to the login domains of bots
// (url_auth_domains; core.telegram.org/api/url-authorization#link-url-authorization)
// log in as the login button does: the first opening asks, without asking
// for the permission to write, which only a button may ask for; the
// accepted one gives the address with the data signed with that bot's
// token. A link to a domain of no bot is URL_INVALID, which the clients
// open as it is.
// Both sites are listed among the sites logged in on; resetting all of them
// empties the list, and the link asks again.
func TestUrlAuthLinksAndResetAll(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]
	alice := login(ctx, t, phone, "Alice")
	getState(ctx, t, alice)

	type site struct {
		bot    *user
		token  string
		domain string
	}
	sites := make([]site, 2)
	for i := range sites {
		username := fmt.Sprintf("e2e%dbot", randomID()&0xffffff)
		domain := fmt.Sprintf("l%d.example", randomID()&0xffffff)
		bot, token := botLoggedInWithToken(ctx, t, phone, username, 228)
		admin(t, "bot", "set", username, "-domain", domain)
		getState(ctx, t, bot)
		sites[i] = site{bot: bot, token: token, domain: domain}
	}
	link := func(s site) string { return "https://" + s.domain + "/login?next=%2Fhome" }
	request := func(l string) (*mtproto.UrlAuthResult, error) {
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesRequestUrlAuth{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_requestUrlAuth),
			Url:         wrapperspb.String(l),
		})
		if err != nil {
			return nil, err
		}
		return o.(interface{ To_UrlAuthResult() *mtproto.UrlAuthResult }).To_UrlAuthResult(), nil
	}

	if _, err := request("https://nobody.example/login"); !strings.Contains(fmt.Sprint(err), "URL_INVALID") {
		t.Fatalf("a link to a domain of no bot: %v", err)
	}
	for i, s := range sites {
		r, err := request(link(s))
		if err != nil {
			t.Fatalf("messages.requestUrlAuth of site %d: %v", i, err)
		}
		if r.GetPredicateName() != mtproto.Predicate_urlAuthResultRequest || r.GetDomain() != s.domain || r.GetBot().GetId() != s.bot.self.GetId() ||
			r.GetRequestWriteAccess() {
			t.Fatalf("the first opening of site %d: %v", i, r)
		}
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesAcceptUrlAuth{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_acceptUrlAuth),
			Url:         wrapperspb.String(link(s)),
		})
		accepted := urlAuthResult(t, o, err, "messages.acceptUrlAuth")
		if accepted.GetPredicateName() != mtproto.Predicate_urlAuthResultAccepted {
			t.Fatalf("site %d accepted: %v", i, accepted)
		}
		checkLoginData(t, acceptedURL(accepted), s.token, alice.self.GetId())
	}
	logins := func() *mtproto.Account_WebAuthorizations {
		t.Helper()
		o, err := alice.c.Invoke(ctx, &mtproto.TLAccountGetWebAuthorizations{Constructor: alice.c.Constructor(mtproto.Predicate_account_getWebAuthorizations)})
		if err != nil {
			t.Fatalf("account.getWebAuthorizations: %v", err)
		}
		return o.(interface {
			To_Account_WebAuthorizations() *mtproto.Account_WebAuthorizations
		}).To_Account_WebAuthorizations()
	}
	listed := map[string]int64{}
	for _, a := range logins().GetAuthorizations() {
		listed[a.GetDomain()] = a.GetBotId()
	}
	if len(listed) != 2 || listed[sites[0].domain] != sites[0].bot.self.GetId() || listed[sites[1].domain] != sites[1].bot.self.GetId() {
		t.Fatalf("the sites logged in on: %v", listed)
	}

	o, err := alice.c.Invoke(ctx, &mtproto.TLAccountResetWebAuthorizations{Constructor: alice.c.Constructor(mtproto.Predicate_account_resetWebAuthorizations)})
	if err != nil || !mtproto.FromBool(o.(interface{ To_Bool() *mtproto.Bool }).To_Bool()) {
		t.Fatalf("account.resetWebAuthorizations: %v, %v", o, err)
	}
	if left := logins(); len(left.GetAuthorizations()) != 0 {
		t.Fatalf("the sites after the reset of all: %v", left)
	}
	r, err := request(link(sites[1]))
	if err != nil || r.GetPredicateName() != mtproto.Predicate_urlAuthResultRequest {
		t.Fatalf("the link after the reset: %v, %v", r, err)
	}
}
