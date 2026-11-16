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
	"testing"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/svc"
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/urlauth"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/types/known/wrapperspb"

	_ "github.com/go-sql-driver/mysql"
)

// TestOAuthSiteOnMySQL: a site's page asks for a login with
// telegram:bot_access and phone; a web app of an origin the bot's login
// client does not list cannot ask for it, one it lists can; the user
// matches the code and accepts with the permission and the phone; the page
// sees the request accepted with the consents, the site is listed among the
// sites logged in on with the visitor's browser, and the bot's chat gets
// messageActionBotAllowed once, also when the acceptance is sent again.
func TestOAuthSiteOnMySQL(t *testing.T) {
	ctx := context.Background()
	mysql, err := testinfra.StartMySQL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mysql.Terminate(context.Background()) })
	conf := mysql.Config()
	bots := botstore.New(sqlx.NewMySQL(&conf))

	const user, other = int64(1000001), int64(1000002)
	bot, _, err := bots.Create(ctx, "oauth_site_bot", "Site bot", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = bots.SetLoginClient(ctx, bot, []string{"https://site.example/cb"}, []string{"https://app.site.example"}, 1); err != nil {
		t.Fatal(err)
	}
	requests := urlauth.NewStore(testKV(t))
	sent := &sentOnce{seen: map[int64]bool{}}
	svcCtx := &svc.ServiceContext{
		Dao: &dao.Dao{
			UserClient: loginUsers{user: user, bots: map[int64]bool{bot: true}},
			MsgClient:  sent,
			Bots:       bots,
		},
		OAuth: requests,
	}
	coreOf := func(id int64) *BotsCore {
		return &BotsCore{
			ctx: ctx, svcCtx: svcCtx, Logger: logx.WithContext(ctx),
			MD: &metadata.RpcMetadata{UserId: id, PermAuthKeyId: 77, ClientAddr: "193.17.47.1"},
		}
	}
	c := coreOf(user)

	e, err := requests.Create(ctx, urlauth.Request{
		Kind: urlauth.KindOIDC, BotId: bot, Domain: "site.example", RedirectURI: "https://site.example/cb", State: "st",
		Scopes:  []string{urlauth.ScopeOpenID, urlauth.ScopeProfile, urlauth.ScopePhone, urlauth.ScopeBotAccess},
		Browser: "Chrome 120", Platform: "Windows", IP: "198.51.100.4", Region: "Berlin, Germany",
	})
	if err != nil {
		t.Fatal(err)
	}
	link := urlauth.AppLink("pluma.chat", e.Token)

	// From a web app: only an origin of the login client.
	if _, err = c.MessagesRequestUrlAuth(&mtproto.TLMessagesRequestUrlAuth{Url: wrapperspb.String(link), InAppOrigin: wrapperspb.String("https://evil.example")}); !errors.Is(err, mtproto.ErrUrlInvalid) {
		t.Fatalf("a foreign origin: %v", err)
	}
	r, err := c.MessagesRequestUrlAuth(&mtproto.TLMessagesRequestUrlAuth{Url: wrapperspb.String(link), InAppOrigin: wrapperspb.String("https://app.site.example")})
	if err != nil {
		t.Fatal(err)
	}
	if r.GetPredicateName() != mtproto.Predicate_urlAuthResultRequest || !r.GetRequestWriteAccess() || !r.GetRequestPhoneNumber() ||
		!r.GetMatchCodesFirst() || r.GetBot().GetId() != bot || r.GetDomain() != "site.example" || r.GetRegion().GetValue() != "Berlin, Germany" {
		t.Fatalf("request: %v", r)
	}

	if ok, err := c.MessagesCheckUrlAuthMatchCode(&mtproto.TLMessagesCheckUrlAuthMatchCode{Url: link, MatchCode: e.Code}); err != nil || !mtproto.FromBool(ok) {
		t.Fatalf("check: %v, %v", ok, err)
	}
	accept := &mtproto.TLMessagesAcceptUrlAuth{Url: wrapperspb.String(link), MatchCode: wrapperspb.String(e.Code), WriteAllowed: true, SharePhoneNumber: true}
	for i := 0; i < 2; i++ {
		r, err = c.MessagesAcceptUrlAuth(accept)
		if err != nil {
			t.Fatalf("accept %d: %v", i, err)
		}
		if r.GetPredicateName() != mtproto.Predicate_urlAuthResultAccepted || r.GetUrl_FLAGSTRING() != nil {
			t.Fatalf("accept %d: %v", i, r)
		}
	}
	if _, err = coreOf(other).MessagesAcceptUrlAuth(accept); !errors.Is(err, mtproto.ErrUrlExpired) {
		t.Errorf("another user's acceptance: %v", err)
	}
	if _, err = c.MessagesRequestUrlAuth(&mtproto.TLMessagesRequestUrlAuth{Url: wrapperspb.String(link)}); !errors.Is(err, mtproto.ErrUrlExpired) {
		t.Errorf("an accepted request asked again: %v", err)
	}

	got, err := requests.Take(ctx, e.Token)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != urlauth.StatusAccepted || got.UserId != user || !got.WriteAllowed || !got.SharePhone {
		t.Fatalf("the page sees %+v", got)
	}

	if len(sent.sent) != 1 {
		t.Fatalf("%d messages sent, want the permission's one", len(sent.sent))
	}
	if a := sent.sent[0].GetMessage()[0].GetMessage().GetAction(); a.GetPredicateName() != mtproto.Predicate_messageActionBotAllowed || a.GetDomain_FLAGSTRING().GetValue() != "site.example" {
		t.Errorf("action %v", a)
	}
	if ok, err := bots.WriteAllowed(ctx, user, bot); err != nil || !ok {
		t.Errorf("the bot may not write: %v, %v", ok, err)
	}
	sites, err := bots.WebAuthorizations(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 1 || sites[0].BotId != bot || sites[0].Domain != "site.example" || sites[0].Browser != "Chrome 120" ||
		sites[0].Platform != "Windows" || sites[0].IP != "198.51.100.4" || sites[0].Region != "Berlin, Germany" || !sites[0].WriteAllowed {
		t.Errorf("sites %+v", sites)
	}

	// A user the bot may write to already is not asked again, and the
	// site gets the permission all the same.
	e2, err := requests.Create(ctx, urlauth.Request{
		Kind: urlauth.KindOIDC, BotId: bot, Domain: "site.example", RedirectURI: "https://site.example/cb",
		Scopes: []string{urlauth.ScopeOpenID, urlauth.ScopeBotAccess},
	})
	if err != nil {
		t.Fatal(err)
	}
	link2 := urlauth.AppLink("pluma.chat", e2.Token)
	if r, err = c.MessagesRequestUrlAuth(&mtproto.TLMessagesRequestUrlAuth{Url: wrapperspb.String(link2)}); err != nil || r.GetRequestWriteAccess() || r.GetRequestPhoneNumber() {
		t.Fatalf("second request: %v, %v", r, err)
	}
	if _, err = c.MessagesAcceptUrlAuth(&mtproto.TLMessagesAcceptUrlAuth{Url: wrapperspb.String(link2), MatchCode: wrapperspb.String(e2.Code), SharePhoneNumber: true}); err != nil {
		t.Fatal(err)
	}
	if got, err = requests.Get(ctx, e2.Token); err != nil || !got.WriteAllowed || got.SharePhone {
		t.Fatalf("second acceptance %+v, %v", got, err)
	}
	if len(sent.sent) != 1 {
		t.Errorf("%d messages sent, want one", len(sent.sent))
	}
}
