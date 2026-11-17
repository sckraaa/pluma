//go:build e2e

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

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/urlauth"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// putOAuthRequest stores a login request of a site in the stand's Redis as
// the provider of oauth.pluma.chat stores it (pkg/urlauth, oauth_req) and
// returns its token and right match code.
func putOAuthRequest(t *testing.T, r urlauth.Request) (token, code string) {
	t.Helper()
	token, err := urlauth.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	codes, right, err := urlauth.NewMatchCodes()
	if err != nil {
		t.Fatal(err)
	}
	r.MatchCodes, r.CreatedAt = codes, time.Now().Unix()
	data, err := json.Marshal(&r)
	if err != nil {
		t.Fatal(err)
	}
	key := "oauth_req:" + token
	for _, args := range [][]string{
		{"HSET", key, "req", string(data), "code", right, "state", "pending", "attempts", "0", "user_id", "0", "write", "0", "phone", "0", "accepted_at", "0"},
		{"EXPIRE", key, "300"},
	} {
		cmd := append([]string{"compose", "exec", "-T", "redis", "redis-cli"}, args...)
		if out, err := exec.Command(runScript, cmd...).CombinedOutput(); err != nil {
			t.Fatalf("redis-cli %s: %v\n%s", args[0], err, out)
		}
	}
	return token, right
}

// oauthRequestField reads a field of a stored login request.
func oauthRequestField(t *testing.T, token, field string) string {
	t.Helper()
	out, err := exec.Command(runScript, "compose", "exec", "-T", "redis", "redis-cli", "HGET", "oauth_req:"+token, field).CombinedOutput()
	if err != nil {
		t.Fatalf("redis-cli HGET: %v\n%s", err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestOAuthRequest: the app opens the link of a site's login request and is
// asked with the site's visitor and the match codes first; a wrong code is
// false, the right one true; the acceptance with the code and the
// permission turns the request accepted for the user, without an address,
// posts messageActionBotAllowed to both sides and lists the site among the
// sites logged in on. A second request declined is gone for the app.
func TestOAuthRequest(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]
	alice := login(ctx, t, phone, "Alice")
	getState(ctx, t, alice)
	username := fmt.Sprintf("e2e%dbot", randomID()&0xffffff)
	domain := fmt.Sprintf("o%d.example", randomID()&0xffffff)
	bot, _ := botLoggedInWithToken(ctx, t, phone, username, 233)
	getState(ctx, t, bot)

	token, code := putOAuthRequest(t, urlauth.Request{
		Kind: urlauth.KindOIDC, BotId: bot.self.GetId(), Domain: domain, RedirectURI: "https://" + domain + "/cb",
		Scopes:  []string{urlauth.ScopeOpenID, urlauth.ScopeProfile, urlauth.ScopeBotAccess},
		Browser: "Firefox 121", Platform: "Linux", IP: "192.0.2.7", Region: "Amsterdam, Netherlands",
	})
	// tdesktop's form of the link (core/local_url_handlers.cpp:707-717).
	link := "tg://resolve?domain=oauth&startapp=" + token

	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesRequestUrlAuth{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_requestUrlAuth),
		Url:         wrapperspb.String(link),
	})
	r := urlAuthResult(t, o, err, "messages.requestUrlAuth")
	if r.GetPredicateName() != mtproto.Predicate_urlAuthResultRequest || r.GetBot().GetId() != bot.self.GetId() || r.GetDomain() != domain ||
		!r.GetMatchCodesFirst() || !r.GetRequestWriteAccess() || r.GetRequestPhoneNumber() || len(r.GetMatchCodes()) != urlauth.MatchCodeCount ||
		r.GetBrowser().GetValue() != "Firefox 121" || r.GetPlatform().GetValue() != "Linux" || r.GetIp().GetValue() != "192.0.2.7" || r.GetRegion().GetValue() != "Amsterdam, Netherlands" {
		t.Fatalf("the request: %v", r)
	}

	check := func(c string) bool {
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesCheckUrlAuthMatchCode{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_checkUrlAuthMatchCode),
			Url:         link, MatchCode: c,
		})
		if err != nil {
			t.Fatalf("messages.checkUrlAuthMatchCode: %v", err)
		}
		return mtproto.FromBool(o.(interface{ To_Bool() *mtproto.Bool }).To_Bool())
	}
	for _, c := range r.GetMatchCodes() {
		if c != code {
			if check(c) {
				t.Fatalf("a wrong code %q is right", c)
			}
			break
		}
	}
	if !check(code) {
		t.Fatal("the right code is wrong")
	}

	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesAcceptUrlAuth{
		Constructor:  alice.c.Constructor(mtproto.Predicate_messages_acceptUrlAuth),
		WriteAllowed: true,
		Url:          wrapperspb.String(link),
		MatchCode:    wrapperspb.String(code),
	})
	r = urlAuthResult(t, o, err, "messages.acceptUrlAuth")
	if r.GetPredicateName() != mtproto.Predicate_urlAuthResultAccepted || acceptedURL(r) != "" {
		t.Fatalf("accepted: %v", r)
	}
	if s, u, w := oauthRequestField(t, token, "state"), oauthRequestField(t, token, "user_id"), oauthRequestField(t, token, "write"); s != "accepted" || u != fmt.Sprint(alice.self.GetId()) || w != "1" {
		t.Fatalf("the stored request: state %q, user %q, write %q", s, u, w)
	}
	allowed := func(u *mtproto.Update) bool {
		a := u.GetMessage_MESSAGE().GetAction()
		if a.GetPredicateName() != mtproto.Predicate_messageActionBotAllowed {
			return false
		}
		if d := a.GetDomain_FLAGSTRING(); d != nil {
			return d.GetValue() == domain
		}
		return a.GetDomain_STRING() == domain
	}
	waitUpdate(ctx, t, alice, mtproto.Predicate_updateNewMessage, allowed)
	waitUpdate(ctx, t, bot, mtproto.Predicate_updateNewMessage, allowed)

	o, err = alice.c.Invoke(ctx, &mtproto.TLAccountGetWebAuthorizations{Constructor: alice.c.Constructor(mtproto.Predicate_account_getWebAuthorizations)})
	if err != nil {
		t.Fatalf("account.getWebAuthorizations: %v", err)
	}
	sites := o.(interface {
		To_Account_WebAuthorizations() *mtproto.Account_WebAuthorizations
	}).To_Account_WebAuthorizations()
	if len(sites.GetAuthorizations()) != 1 || sites.GetAuthorizations()[0].GetDomain() != domain || sites.GetAuthorizations()[0].GetBrowser() != "Firefox 121" {
		t.Fatalf("the sites logged in on: %v", sites)
	}

	// A declined request is gone for the app.
	token2, _ := putOAuthRequest(t, urlauth.Request{Kind: urlauth.KindOIDC, BotId: bot.self.GetId(), Domain: domain, Scopes: []string{urlauth.ScopeOpenID}})
	link2 := urlauth.AppLink("pluma.chat", token2)
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesDeclineUrlAuth{Constructor: alice.c.Constructor(mtproto.Predicate_messages_declineUrlAuth), Url: link2})
	if err != nil || !mtproto.FromBool(o.(interface{ To_Bool() *mtproto.Bool }).To_Bool()) {
		t.Fatalf("messages.declineUrlAuth: %v, %v", o, err)
	}
	if s := oauthRequestField(t, token2, "state"); s != "declined" {
		t.Fatalf("the declined request: %q", s)
	}
	_, err = alice.c.Invoke(ctx, &mtproto.TLMessagesRequestUrlAuth{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_requestUrlAuth),
		Url:         wrapperspb.String(link2),
	})
	if !strings.Contains(fmt.Sprint(err), "URL_EXPIRED") {
		t.Fatalf("a declined request asked: %v", err)
	}
}
