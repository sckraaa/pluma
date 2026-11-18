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
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/urlauth"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// oauthHost is the host of the provider on the stand's links service, which
// tells it by the Host header as behind Caddy.
const oauthHost = "oauth.localhost"

var (
	oauthSecretRe = regexp.MustCompile(`(?m)^client_secret: (\S+)$`)
	oauthTokenRe  = regexp.MustCompile(`data-token="([A-Za-z0-9_-]+)"`)
	oauthCodeRe   = regexp.MustCompile(`class="match-code">([^<]+)<`)
)

// oauthReply is what the provider answered, read in full.
type oauthReply struct {
	StatusCode int
	Header     http.Header
	cookies    []*http.Cookie
}

// Cookies are the cookies the answer sets.
func (r oauthReply) Cookies() []*http.Cookie { return r.cookies }

// oauthDo sends a request to the provider of the stand.
func oauthDo(ctx context.Context, t *testing.T, method, target string, body url.Values, header http.Header) (oauthReply, string) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		rd = strings.NewReader(body.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://"+linksAddr+target, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = oauthHost
	// As Caddy passes the visitor's address.
	req.Header.Set("X-Forwarded-For", "203.0.113.50")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:128.0) Gecko/20100101 Firefox/128.0")
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range header {
		req.Header[k] = v
	}
	// The provider's redirects go to the site, which the stand has not.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return oauthReply{StatusCode: res.StatusCode, Header: res.Header, cookies: res.Cookies()}, string(b)
}

// oauthPage opens the page of a login request and returns its token, the
// match code it shows and the cookie that binds its state to the browser.
func oauthPage(ctx context.Context, t *testing.T, target string) (token, code string, cookie *http.Cookie) {
	t.Helper()
	res, body := oauthDo(ctx, t, http.MethodGet, target, nil, nil)
	tm, cm := oauthTokenRe.FindStringSubmatch(body), oauthCodeRe.FindStringSubmatch(body)
	if res.StatusCode != http.StatusOK || tm == nil || cm == nil {
		t.Fatalf("the page of %s: %d\n%s", target, res.StatusCode, body)
	}
	for _, c := range res.Cookies() {
		if c.Path == "/auth/state" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatalf("the page set no cookie: %v", res.Header)
	}
	return tm[1], cm[1], cookie
}

// oauthState polls the request of the page as its script does.
func oauthState(ctx context.Context, t *testing.T, token string, cookie *http.Cookie) (status, redirect string) {
	t.Helper()
	h := http.Header{}
	if cookie != nil {
		h.Set("Cookie", cookie.Name+"="+cookie.Value)
	}
	res, body := oauthDo(ctx, t, http.MethodGet, "/auth/state?token="+token, nil, h)
	var s struct{ Status, Redirect string }
	if res.StatusCode != http.StatusOK || json.Unmarshal([]byte(body), &s) != nil {
		t.Fatalf("the state: %d %s", res.StatusCode, body)
	}
	return s.Status, s.Redirect
}

// TestOAuthSite: a site logs a user in with OpenID Connect through the
// provider of the stand (task 106, part I2). The page of /auth shows a
// request; the app opens its link and is asked with the match codes, a
// wrong one is false; the acceptance with the right one turns the page's
// state into the site's address with a code, which the site exchanges with
// its secret and PKCE verifier for an ID token the JWKS checks; the code
// works once. A declined request sends the browser back with access_denied.
func TestOAuthSite(t *testing.T) {
	if linksAddr == "" {
		t.Skip("PLUMA_E2E_LINKS is not set (run.sh test sets it)")
	}
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	phone := testPhones(t, 1)[0]
	alice := login(ctx, t, phone, "Alice")
	getState(ctx, t, alice)
	username := fmt.Sprintf("e2e%dbot", randomID()&0xffffff)
	domain := fmt.Sprintf("o%d.example", randomID()&0xffffff)
	redirect := "https://" + domain + "/cb"
	bot, _ := botLoggedInWithToken(ctx, t, phone, username, 233)
	botId := fmt.Sprint(bot.self.GetId())
	m := oauthSecretRe.FindStringSubmatch(admin(t, "bot", "login", "set", username, "-redirect", redirect))
	if m == nil {
		t.Fatal("bot login set printed no secret")
	}
	secret := m[1]

	res, body := oauthDo(ctx, t, http.MethodGet, "/.well-known/openid-configuration", nil, nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(body, `"issuer":"https://`+oauthHost+`"`) {
		t.Fatalf("the discovery document: %d %s", res.StatusCode, body)
	}

	verifier := strings.Repeat("e2eVerifier-", 5)
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"client_id": {botId}, "redirect_uri": {redirect}, "response_type": {"code"},
		"scope": {"openid profile"}, "state": {"st"}, "nonce": {"nc"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
	}
	token, code, cookie := oauthPage(ctx, t, "/auth?"+q.Encode())

	// The link of the page's QR code, as Android passes it.
	link := urlauth.AppLink("pluma.chat", token)
	o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesRequestUrlAuth{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_requestUrlAuth),
		Url:         wrapperspb.String(link),
	})
	r := urlAuthResult(t, o, err, "messages.requestUrlAuth")
	if r.GetPredicateName() != mtproto.Predicate_urlAuthResultRequest || r.GetDomain() != domain || r.GetBot().GetId() != bot.self.GetId() ||
		!slices.Contains(r.GetMatchCodes(), code) || r.GetBrowser().GetValue() != "Firefox 128" || r.GetPlatform().GetValue() != "Windows" ||
		r.GetIp().GetValue() != "203.0.113.50" {
		t.Fatalf("the request in the app: %v (the page shows %q)", r, code)
	}
	for _, c := range r.GetMatchCodes() {
		if c == code {
			continue
		}
		o, err := alice.c.Invoke(ctx, &mtproto.TLMessagesCheckUrlAuthMatchCode{
			Constructor: alice.c.Constructor(mtproto.Predicate_messages_checkUrlAuthMatchCode),
			Url:         link, MatchCode: c,
		})
		if err != nil || mtproto.FromBool(o.(interface{ To_Bool() *mtproto.Bool }).To_Bool()) {
			t.Fatalf("a wrong code: %v, %v", o, err)
		}
		break
	}
	if s, _ := oauthState(ctx, t, token, cookie); s != "pending" {
		t.Fatalf("before the answer: %q", s)
	}
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesAcceptUrlAuth{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_acceptUrlAuth),
		Url:         wrapperspb.String(link),
		MatchCode:   wrapperspb.String(code),
	})
	if r = urlAuthResult(t, o, err, "messages.acceptUrlAuth"); r.GetPredicateName() != mtproto.Predicate_urlAuthResultAccepted {
		t.Fatalf("accepted: %v", r)
	}

	if s, _ := oauthState(ctx, t, token, nil); s != "expired" {
		t.Fatalf("a browser without the page's cookie: %q", s)
	}
	s, back := oauthState(ctx, t, token, cookie)
	u, err := url.Parse(back)
	if s != "done" || err != nil || u.Host != domain || u.Query().Get("state") != "st" || u.Query().Get("code") == "" {
		t.Fatalf("the state after the answer: %q %q", s, back)
	}
	form := url.Values{
		"grant_type": {"authorization_code"}, "code": {u.Query().Get("code")}, "redirect_uri": {redirect},
		"client_id": {botId}, "client_secret": {secret}, "code_verifier": {verifier},
	}
	res, body = oauthDo(ctx, t, http.MethodPost, "/token", form, nil)
	var reply struct {
		IdToken   string `json:"id_token"`
		TokenType string `json:"token_type"`
	}
	if res.StatusCode != http.StatusOK || json.Unmarshal([]byte(body), &reply) != nil || reply.IdToken == "" || reply.TokenType != "Bearer" {
		t.Fatalf("the exchange: %d %s", res.StatusCode, body)
	}
	claims := checkIDToken(ctx, t, reply.IdToken)
	if claims["iss"] != "https://"+oauthHost || claims["aud"] != botId || claims["sub"] != fmt.Sprint(alice.self.GetId()) ||
		claims["nonce"] != "nc" || claims["given_name"] != "Alice" {
		t.Fatalf("the claims: %v", claims)
	}
	res, body = oauthDo(ctx, t, http.MethodPost, "/token", form, nil)
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(body, "invalid_grant") {
		t.Fatalf("the code twice: %d %s", res.StatusCode, body)
	}

	// Declined in the app: the browser goes back with access_denied.
	token, _, cookie = oauthPage(ctx, t, "/auth?"+q.Encode())
	o, err = alice.c.Invoke(ctx, &mtproto.TLMessagesDeclineUrlAuth{
		Constructor: alice.c.Constructor(mtproto.Predicate_messages_declineUrlAuth),
		Url:         urlauth.AppLink("pluma.chat", token),
	})
	if err != nil || !mtproto.FromBool(o.(interface{ To_Bool() *mtproto.Bool }).To_Bool()) {
		t.Fatalf("messages.declineUrlAuth: %v, %v", o, err)
	}
	s, back = oauthState(ctx, t, token, cookie)
	if u, err = url.Parse(back); s != "done" || err != nil || u.Query().Get("error") != "access_denied" || u.Query().Get("state") != "st" {
		t.Fatalf("declined: %q %q", s, back)
	}
}

// checkIDToken checks an ID token with the provider's JWKS as a site does
// and returns its claims.
func checkIDToken(ctx context.Context, t *testing.T, idToken string) map[string]any {
	t.Helper()
	_, body := oauthDo(ctx, t, http.MethodGet, "/.well-known/jwks.json", nil, nil)
	var set struct {
		Keys []struct{ Kid, N, E string }
	}
	if err := json.Unmarshal([]byte(body), &set); err != nil {
		t.Fatalf("the JWKS: %v %s", err, body)
	}
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		t.Fatalf("an ID token of %d parts", len(parts))
	}
	var header struct{ Alg, Kid string }
	if hb, err := base64.RawURLEncoding.DecodeString(parts[0]); err != nil || json.Unmarshal(hb, &header) != nil || header.Alg != "RS256" {
		t.Fatalf("the header: %v", err)
	}
	var key *rsa.PublicKey
	for _, k := range set.Keys {
		if k.Kid != header.Kid {
			continue
		}
		n, errN := base64.RawURLEncoding.DecodeString(k.N)
		e, errE := base64.RawURLEncoding.DecodeString(k.E)
		if errN != nil || errE != nil {
			t.Fatalf("a key of the JWKS: %v %v", errN, errE)
		}
		key = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
	}
	if key == nil {
		t.Fatalf("no key %q in the JWKS", header.Kid)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig); err != nil {
		t.Fatalf("the signature of the ID token: %v", err)
	}
	pb, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims map[string]any
	if err := json.Unmarshal(pb, &claims); err != nil {
		t.Fatal(err)
	}
	return claims
}
