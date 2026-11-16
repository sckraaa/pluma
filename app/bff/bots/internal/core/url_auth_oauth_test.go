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
	"slices"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/svc"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/urlauth"

	"github.com/zeromicro/go-zero/core/logx"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// oauthUsers are the users of a login request: the bots of the sites, a
// deleted bot, and people for the rest.
type oauthUsers struct {
	user_client.UserClient
	bots, deleted map[int64]bool
}

func (u oauthUsers) immutable(id int64) *mtproto.ImmutableUser {
	data := &mtproto.UserData{Id: id, AccessHash: 1, FirstName: "Person"}
	if u.bots[id] || u.deleted[id] {
		data.FirstName = "Site bot"
		data.Bot = mtproto.MakeTLBotData(&mtproto.BotData{}).To_BotData()
	}
	data.Deleted = u.deleted[id]
	return mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{User: mtproto.MakeTLUserData(data).To_UserData()}).To_ImmutableUser()
}

func (u oauthUsers) UserGetImmutableUser(_ context.Context, in *userpb.TLUserGetImmutableUser) (*mtproto.ImmutableUser, error) {
	return u.immutable(in.Id), nil
}

func (u oauthUsers) UserGetMutableUsers(_ context.Context, in *userpb.TLUserGetMutableUsers) (*userpb.Vector_ImmutableUser, error) {
	out := &userpb.Vector_ImmutableUser{}
	for _, id := range in.Id {
		out.Datas = append(out.Datas, u.immutable(id))
	}
	return out, nil
}

const oauthBot, oauthGoneBot, oauthUser = int64(9001), int64(9002), int64(1000010)

func oauthCore(t *testing.T) (*BotsCore, *urlauth.Store) {
	t.Helper()
	store := urlauth.NewStore(testKV(t))
	ctx := context.Background()
	return &BotsCore{
		ctx: ctx,
		svcCtx: &svc.ServiceContext{
			Dao:   &dao.Dao{UserClient: oauthUsers{bots: map[int64]bool{oauthBot: true}, deleted: map[int64]bool{oauthGoneBot: true}}},
			OAuth: store,
		},
		Logger: logx.WithContext(ctx),
		MD:     &metadata.RpcMetadata{UserId: oauthUser},
	}, store
}

// siteRequest is a request of OpenID Connect without telegram:bot_access,
// which needs nothing of the database to be answered.
func siteRequest(t *testing.T, s *urlauth.Store, scopes ...string) *urlauth.Entry {
	t.Helper()
	e, err := s.Create(context.Background(), urlauth.Request{
		Kind: urlauth.KindOIDC, BotId: oauthBot, Domain: "site.example", RedirectURI: "https://site.example/cb",
		Scopes:  append([]string{urlauth.ScopeOpenID}, scopes...),
		Browser: "Firefox 121", Platform: "Linux", IP: "192.0.2.7", Region: "",
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func wrongCode(e *urlauth.Entry) string {
	for _, c := range e.MatchCodes {
		if c != e.Code {
			return c
		}
	}
	panic("no wrong code")
}

// A request names a login request only by its link, not by a button.
func TestOAuthTokenOf(t *testing.T) {
	link := urlauth.AppLink("pluma.chat", "AbCdEfGhIjKlMnOpQrStUvWxYz012345")
	msgId, buttonId := int32(5), int32(1)
	peer := mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: 20}).To_InputPeer()
	if tok, ok := oauthTokenOf(nil, nil, nil, &link); !ok || tok != "AbCdEfGhIjKlMnOpQrStUvWxYz012345" {
		t.Errorf("link: %q, %v", tok, ok)
	}
	if _, ok := oauthTokenOf(peer, &msgId, &buttonId, &link); ok {
		t.Error("a button is no login request")
	}
	if _, ok := oauthTokenOf(nil, nil, nil, nil); ok {
		t.Error("no link")
	}
	plain := "https://site.example/login"
	if _, ok := oauthTokenOf(nil, nil, nil, &plain); ok {
		t.Error("a link of url_auth_domains")
	}
}

// The app is asked with the bot, the site, the visitor and the match codes
// to be matched first, for every form of the link.
func TestOAuthRequestAsks(t *testing.T) {
	c, s := oauthCore(t)
	e := siteRequest(t, s, urlauth.ScopeProfile, urlauth.ScopePhone)
	for _, link := range []string{
		urlauth.AppLink("pluma.chat", e.Token),
		"tg://resolve?domain=oauth&startapp=" + e.Token,
		"tg://oauth?token=" + e.Token,
	} {
		r, err := c.MessagesRequestUrlAuth(&mtproto.TLMessagesRequestUrlAuth{Url: wrapperspb.String(link)})
		if err != nil {
			t.Fatalf("%s: %v", link, err)
		}
		if r.GetPredicateName() != mtproto.Predicate_urlAuthResultRequest || r.GetBot().GetId() != oauthBot || r.GetDomain() != "site.example" ||
			!r.GetMatchCodesFirst() || !r.GetRequestPhoneNumber() || r.GetRequestWriteAccess() || r.GetIsApp() {
			t.Fatalf("%s: %v", link, r)
		}
		if r.GetBrowser().GetValue() != "Firefox 121" || r.GetPlatform().GetValue() != "Linux" || r.GetIp().GetValue() != "192.0.2.7" || r.GetRegion() == nil {
			t.Errorf("visitor %v", r)
		}
		if !slices.Equal(r.GetMatchCodes(), e.MatchCodes) {
			t.Errorf("codes %q, want %q", r.GetMatchCodes(), e.MatchCodes)
		}
		// The answer goes out in the layer of the clients, all four fields
		// of the visitor under one flag.
		x := mtproto.NewEncodeBuf(512)
		if err := r.Encode(x, 233); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
}

// What a request names that is not a pending request of a live bot.
func TestOAuthRequestErrors(t *testing.T) {
	c, s := oauthCore(t)
	request := func(link string, origin *string) error {
		_, err := c.MessagesRequestUrlAuth(&mtproto.TLMessagesRequestUrlAuth{Url: wrapperspb.String(link), InAppOrigin: stringPtr(origin)})
		return err
	}
	if err := request(urlauth.AppLink("pluma.chat", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"), nil); !errors.Is(err, mtproto.ErrUrlExpired) {
		t.Errorf("no such request: %v", err)
	}
	gone, err := s.Create(context.Background(), urlauth.Request{Kind: urlauth.KindOIDC, BotId: oauthGoneBot, Domain: "gone.example"})
	if err != nil {
		t.Fatal(err)
	}
	if err := request(urlauth.AppLink("pluma.chat", gone.Token), nil); !errors.Is(err, mtproto.ErrOauthRequestInvalid) {
		t.Errorf("deleted bot: %v", err)
	}
	declined := siteRequest(t, s)
	if _, err := s.Decline(context.Background(), declined.Token); err != nil {
		t.Fatal(err)
	}
	if err := request(urlauth.AppLink("pluma.chat", declined.Token), nil); !errors.Is(err, mtproto.ErrUrlExpired) {
		t.Errorf("declined: %v", err)
	}
	empty := ""
	if err := request(urlauth.AppLink("pluma.chat", siteRequest(t, s).Token), &empty); !errors.Is(err, mtproto.ErrUrlInvalid) {
		t.Errorf("empty in_app_origin: %v", err)
	}
	// A page of the widget format asks from the origin it was made for.
	legacy, err := s.Create(context.Background(), urlauth.Request{
		Kind: urlauth.KindLegacy, BotId: oauthBot, Domain: "site.example", Origin: "https://site.example", ReturnTo: "https://site.example/back",
	})
	if err != nil {
		t.Fatal(err)
	}
	origin := "https://SITE.example"
	if err := request(urlauth.AppLink("pluma.chat", legacy.Token), &origin); err != nil {
		t.Errorf("the widget's own origin: %v", err)
	}
	// in_app_origin with a link of no login request stays URL_INVALID.
	if err := request("https://site.example/login", &origin); !errors.Is(err, mtproto.ErrUrlInvalid) {
		t.Errorf("in_app_origin of a plain link: %v", err)
	}
}

func stringPtr(s *string) *wrapperspb.StringValue {
	if s == nil {
		return nil
	}
	return wrapperspb.String(*s)
}

// A match code is checked against the request; three wrong ones end it.
func TestOAuthCheckMatchCode(t *testing.T) {
	c, s := oauthCore(t)
	e := siteRequest(t, s)
	link := urlauth.AppLink("pluma.chat", e.Token)
	check := func(code string) (bool, error) {
		r, err := c.MessagesCheckUrlAuthMatchCode(&mtproto.TLMessagesCheckUrlAuthMatchCode{Url: link, MatchCode: code})
		if err != nil {
			return false, err
		}
		return mtproto.FromBool(r), nil
	}
	if ok, err := check(e.Code); err != nil || !ok {
		t.Fatalf("right code: %v, %v", ok, err)
	}
	for i := 0; i < urlauth.MaxWrongCodes; i++ {
		if ok, err := check(wrongCode(e)); err != nil || ok {
			t.Fatalf("wrong code %d: %v, %v", i, ok, err)
		}
	}
	if _, err := check(e.Code); !errors.Is(err, mtproto.ErrUrlExpired) {
		t.Fatalf("after the wrong codes: %v", err)
	}
	if _, err := c.MessagesCheckUrlAuthMatchCode(&mtproto.TLMessagesCheckUrlAuthMatchCode{Url: "https://site.example/", MatchCode: e.Code}); !errors.Is(err, mtproto.ErrUrlInvalid) {
		t.Errorf("a link of no login request: %v", err)
	}
}

// A declined request is gone for the apps and declined for its page.
func TestOAuthDecline(t *testing.T) {
	c, s := oauthCore(t)
	e := siteRequest(t, s)
	link := urlauth.AppLink("pluma.chat", e.Token)
	for range 2 {
		if r, err := c.MessagesDeclineUrlAuth(&mtproto.TLMessagesDeclineUrlAuth{Url: link}); err != nil || !mtproto.FromBool(r) {
			t.Fatalf("decline: %v, %v", r, err)
		}
	}
	if _, err := c.MessagesAcceptUrlAuth(&mtproto.TLMessagesAcceptUrlAuth{Url: wrapperspb.String(link), MatchCode: wrapperspb.String(e.Code)}); !errors.Is(err, mtproto.ErrUrlExpired) {
		t.Errorf("accept after decline: %v", err)
	}
	got, err := s.Take(context.Background(), e.Token)
	if err != nil || got.Status != urlauth.StatusDeclined {
		t.Fatalf("the page sees %+v, %v", got, err)
	}
	if _, err := c.MessagesDeclineUrlAuth(&mtproto.TLMessagesDeclineUrlAuth{Url: link}); !errors.Is(err, mtproto.ErrUrlExpired) {
		t.Errorf("decline of a request gone: %v", err)
	}
	// A login button keeps nothing to decline.
	if r, err := c.MessagesDeclineUrlAuth(&mtproto.TLMessagesDeclineUrlAuth{Url: "https://site.example/login"}); err != nil || !mtproto.FromBool(r) {
		t.Errorf("decline of a button: %v, %v", r, err)
	}
}

// Without the right match code nothing is accepted.
func TestOAuthAcceptNeedsTheCode(t *testing.T) {
	c, s := oauthCore(t)
	e := siteRequest(t, s)
	link := wrapperspb.String(urlauth.AppLink("pluma.chat", e.Token))
	if _, err := c.MessagesAcceptUrlAuth(&mtproto.TLMessagesAcceptUrlAuth{Url: link}); !errors.Is(err, mtproto.ErrUrlInvalid) {
		t.Errorf("no code: %v", err)
	}
	if _, err := c.MessagesAcceptUrlAuth(&mtproto.TLMessagesAcceptUrlAuth{Url: link, MatchCode: wrapperspb.String(wrongCode(e))}); !errors.Is(err, mtproto.ErrUrlInvalid) {
		t.Errorf("wrong code: %v", err)
	}
	got, err := s.Get(context.Background(), e.Token)
	if err != nil || got.Status != urlauth.StatusPending || got.Attempts != 1 {
		t.Fatalf("%+v, %v", got, err)
	}
}
