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

package oauth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/teamgram/teamgram-server/app/interface/links/internal/links"
	"github.com/teamgram/teamgram-server/pkg/botkeys"
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/urlauth"
	"github.com/teamgram/teamgram-server/pkg/webappdata"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

const (
	testBot      = int64(7001)
	testUser     = int64(42)
	testSecret   = "s3cret-of-the-site"
	testRedirect = "https://shop.example/cb?x=1"
	testDomain   = "shop.example"
)

var testRSA = sync.OnceValues(func() (*rsa.PrivateKey, error) { return rsa.GenerateKey(rand.Reader, 2048) })

func rsaPEM(t *testing.T, k *rsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

type fakeBots struct{ domain string }

func (f fakeBots) LoginClient(_ context.Context, botId int64) (*botstore.LoginClient, error) {
	if botId != testBot {
		return nil, nil
	}
	return &botstore.LoginClient{BotId: botId, RedirectURIs: []string{testRedirect}}, nil
}

func (f fakeBots) CheckLoginClientSecret(_ context.Context, botId int64, secret string) (bool, error) {
	return botId == testBot && secret == testSecret, nil
}

func (f fakeBots) LoginDomain(_ context.Context, botId int64) (string, error) {
	if botId != testBot {
		return "", nil
	}
	return f.domain, nil
}

type fakeKeys struct{ keys *botkeys.Keys }

func (f fakeKeys) Get(_ context.Context, botId int64) (botkeys.Keys, bool, error) {
	if f.keys == nil || botId != testBot {
		return botkeys.Keys{}, false, nil
	}
	return *f.keys, true, nil
}

type fakeAccounts map[int64]*Account

func (f fakeAccounts) Account(_ context.Context, id int64) (*Account, error) {
	return f[id], nil
}

type fakeRegions struct{}

func (fakeRegions) Region(context.Context, string) (string, error) {
	return "Amsterdam, Netherlands", nil
}

// testProvider is a provider on miniredis with a bot whose site is
// shop.example and a user with a username, photo and phone.
func testProvider(t *testing.T) (*Provider, *miniredis.Miniredis) {
	t.Helper()
	k, err := testRSA()
	if err != nil {
		t.Fatal(err)
	}
	keys, err := ParseKeys(rsaPEM(t, k))
	if err != nil {
		t.Fatal(err)
	}
	mr := miniredis.RunT(t)
	store := kv.NewStore(kv.KvConf{cache.NodeConf{
		RedisConf: redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType, DisableIdentity: true},
		Weight:    100,
	}})
	wordmark, err := links.Wordmark()
	if err != nil {
		t.Fatal(err)
	}
	botKeys := botkeys.Derive("7001:token-of-the-bot")
	p := &Provider{
		Issuer:  "https://oauth.pluma.chat",
		Site:    "https://pluma.chat",
		Keys:    keys,
		KV:      store,
		Bots:    fakeBots{domain: testDomain},
		BotKeys: fakeKeys{keys: &botKeys},
		Accounts: fakeAccounts{
			testBot:  {Id: testBot, Bot: true, FirstName: "Shop", Username: "shopbot", PhotoId: 5},
			testUser: {Id: testUser, FirstName: "Alice", LastName: "Liddell", Username: "alice", Phone: "15550100", PhotoId: 9},
		},
		Regions:  fakeRegions{},
		Texts:    links.SiteTexts{},
		Wordmark: wordmark,
	}
	// Each request a minute after the last, so that the limits of one
	// address never refuse a test's requests (TestLimits checks them).
	clock := time.Now()
	p.now = func() time.Time {
		clock = clock.Add(time.Minute)
		return clock
	}
	if err := p.Init(); err != nil {
		t.Fatal(err)
	}
	return p, mr
}

func get(p *Provider, target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "https://oauth.pluma.chat"+target, nil)
	r.RemoteAddr = "172.18.0.5:41000"
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	r.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:121.0) Gecko/20100101 Firefox/121.0")
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	return w
}

var tokenRe = regexp.MustCompile(`data-token="([A-Za-z0-9_-]+)"`)

// open opens the page of a request and returns its token and cookie.
func open(t *testing.T, p *Provider, target string) (string, *http.Cookie) {
	t.Helper()
	w := get(p, target)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", target, w.Code, w.Body.String())
	}
	m := tokenRe.FindStringSubmatch(w.Body.String())
	if m == nil {
		t.Fatalf("no token on the page: %s", w.Body.String())
	}
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self'") || strings.Contains(csp, "unsafe-inline") {
		t.Errorf("the page's policy: %q", csp)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].Path != "/auth/state" {
		t.Fatalf("the page's cookie: %v", cookies)
	}
	return m[1], cookies[0]
}

func pollState(t *testing.T, p *Provider, token string, c *http.Cookie) stateReply {
	t.Helper()
	var cs []*http.Cookie
	if c != nil {
		cs = append(cs, c)
	}
	w := get(p, "/auth/state?token="+token, cs...)
	if w.Code != http.StatusOK {
		t.Fatalf("state: %d %s", w.Code, w.Body.String())
	}
	var s stateReply
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// accept answers the request of token as the app does (bff, pkg/urlauth).
func accept(t *testing.T, p *Provider, token string, phone bool) {
	t.Helper()
	e, err := p.Requests.Get(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.Requests.Accept(context.Background(), token, urlauth.Acceptance{UserId: testUser, Code: e.Code, SharePhone: phone})
	if err != nil || res != urlauth.CodeRight {
		t.Fatalf("accept: %v, %v", res, err)
	}
}

func verifier() (v, challenge string) {
	v = strings.Repeat("aB3-", 12) // 48 unreserved characters
	sum := sha256.Sum256([]byte(v))
	return v, base64.RawURLEncoding.EncodeToString(sum[:])
}

func exchange(p *Provider, form url.Values, basic bool) *httptest.ResponseRecorder {
	body := url.Values{}
	for k, v := range form {
		body[k] = v
	}
	if basic {
		body.Del("client_secret")
	}
	r := httptest.NewRequest(http.MethodPost, "https://oauth.pluma.chat/token", strings.NewReader(body.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr = "198.51.100.3:5000"
	if basic {
		r.SetBasicAuth(url.QueryEscape(form.Get("client_id")), url.QueryEscape(form.Get("client_secret")))
	}
	w := httptest.NewRecorder()
	p.ServeHTTP(w, r)
	return w
}

// verifyJWT checks an ID token against the provider's JWKS as a site does
// and returns its claims.
func verifyJWT(t *testing.T, p *Provider, token string) map[string]any {
	t.Helper()
	w := get(p, "/.well-known/jwks.json")
	var set struct {
		Keys []jwk `json:"keys"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &set); err != nil || len(set.Keys) == 0 {
		t.Fatalf("jwks: %v %s", err, w.Body.String())
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("an ID token of %d parts", len(parts))
	}
	hb, _ := base64.RawURLEncoding.DecodeString(parts[0])
	var header map[string]string
	if err := json.Unmarshal(hb, &header); err != nil || header["alg"] != "RS256" {
		t.Fatalf("header %s: %v", hb, err)
	}
	var key *rsa.PublicKey
	for _, k := range set.Keys {
		if k.Kid == header["kid"] {
			n, _ := base64.RawURLEncoding.DecodeString(k.N)
			e, _ := base64.RawURLEncoding.DecodeString(k.E)
			key = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
		}
	}
	if key == nil {
		t.Fatalf("no key %q in the JWKS", header["kid"])
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig); err != nil {
		t.Fatalf("the signature: %v", err)
	}
	pb, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims map[string]any
	if err := json.Unmarshal(pb, &claims); err != nil {
		t.Fatal(err)
	}
	return claims
}

// The whole login of OpenID Connect: the page, the answer in the app, the
// poll bound to the browser, the code with PKCE, an ID token the JWKS
// checks, and a code that works once.
func TestOIDCLogin(t *testing.T) {
	p, _ := testProvider(t)
	v, challenge := verifier()
	q := url.Values{
		"client_id": {"7001"}, "redirect_uri": {testRedirect}, "response_type": {"code"},
		"scope": {"openid profile phone email"}, "state": {"st-1"}, "nonce": {"n-1"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"},
	}
	token, cookie := open(t, p, "/auth?"+q.Encode())
	e, err := p.Requests.Get(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	if e.Kind != urlauth.KindOIDC || e.Domain != testDomain || e.IP != "203.0.113.9" || e.Region != "Amsterdam, Netherlands" ||
		e.Browser != "Firefox 121" || e.Platform != "Linux" || strings.Join(e.Scopes, " ") != "openid profile phone" {
		t.Fatalf("the stored request: %+v", e.Request)
	}

	if s := pollState(t, p, token, cookie); s.Status != "pending" {
		t.Fatalf("before the answer: %+v", s)
	}
	accept(t, p, token, true)
	if s := pollState(t, p, token, nil); s.Status != "expired" || s.Redirect != "" {
		t.Fatalf("another browser got %+v", s)
	}
	forged := &http.Cookie{Name: cookie.Name, Value: p.binding(token + "x")}
	if s := pollState(t, p, token, forged); s.Status != "expired" || s.Redirect != "" {
		t.Fatalf("a forged cookie got %+v", s)
	}
	s := pollState(t, p, token, cookie)
	if s.Status != "done" {
		t.Fatalf("after the answer: %+v", s)
	}
	back, err := url.Parse(s.Redirect)
	if err != nil || back.Host != testDomain || back.Query().Get("state") != "st-1" || back.Query().Get("x") != "1" {
		t.Fatalf("the redirect: %s", s.Redirect)
	}
	code := back.Query().Get("code")
	if again := pollState(t, p, token, cookie); again.Redirect != s.Redirect {
		t.Fatalf("a second poll: %+v", again)
	}

	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {testRedirect}, "client_id": {"7001"}, "client_secret": {testSecret}}
	// A wrong verifier, a wrong secret and a wrong redirect_uri leave the
	// code as it was.
	bad := url.Values{}
	for k, v := range form {
		bad[k] = v
	}
	bad.Set("code_verifier", strings.Repeat("x", 43))
	if w := exchange(p, bad, false); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_grant") {
		t.Fatalf("a wrong verifier: %d %s", w.Code, w.Body.String())
	}
	bad.Set("code_verifier", v)
	bad.Set("client_secret", "nope")
	if w := exchange(p, bad, true); w.Code != http.StatusUnauthorized || w.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("a wrong secret: %d %v", w.Code, w.Header())
	}
	bad.Set("client_secret", testSecret)
	bad.Set("redirect_uri", "https://shop.example/other")
	if w := exchange(p, bad, false); !strings.Contains(w.Body.String(), "invalid_grant") {
		t.Fatalf("a wrong redirect_uri: %d %s", w.Code, w.Body.String())
	}

	form.Set("code_verifier", v)
	w := exchange(p, form, true)
	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("the exchange: %d %s", w.Code, w.Body.String())
	}
	var reply tokenReply
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || reply.TokenType != "Bearer" || reply.ExpiresIn != 600 {
		t.Fatalf("the reply: %s", w.Body.String())
	}
	c := verifyJWT(t, p, reply.IdToken)
	if c["iss"] != "https://oauth.pluma.chat" || c["aud"] != "7001" || c["sub"] != "42" || c["nonce"] != "n-1" ||
		c["name"] != "Alice Liddell" || c["preferred_username"] != "alice" || c["picture"] != "https://pluma.chat/_l/avatar/alice?v=9" ||
		c["phone_number"] != "+15550100" || c["phone_number_verified"] != true {
		t.Fatalf("the claims: %v", c)
	}
	if exp, iat := c["exp"].(float64), c["iat"].(float64); exp-iat != 600 {
		t.Fatalf("the token lives %v s", exp-iat)
	}

	form.Set("client_secret", testSecret)
	if w := exchange(p, form, false); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_grant") {
		t.Fatalf("the code twice: %d %s", w.Code, w.Body.String())
	}
}

// The phone number goes to the site only with the user's consent, and the
// profile only with the scope profile.
func TestIDTokenScopes(t *testing.T) {
	p, _ := testProvider(t)
	q := url.Values{"client_id": {"7001"}, "redirect_uri": {testRedirect}, "response_type": {"code"}, "scope": {"openid phone"}}
	token, cookie := open(t, p, "/auth?"+q.Encode())
	accept(t, p, token, false)
	back, _ := url.Parse(pollState(t, p, token, cookie).Redirect)
	form := url.Values{"grant_type": {"authorization_code"}, "code": {back.Query().Get("code")}, "redirect_uri": {testRedirect}, "client_id": {"7001"}, "client_secret": {testSecret}}
	w := exchange(p, form, false)
	var reply tokenReply
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || reply.IdToken == "" {
		t.Fatalf("the exchange: %d %s", w.Code, w.Body.String())
	}
	c := verifyJWT(t, p, reply.IdToken)
	for _, k := range []string{"phone_number", "name", "preferred_username", "picture", "nonce"} {
		if _, ok := c[k]; ok {
			t.Errorf("claim %s without its scope or consent: %v", k, c)
		}
	}
	// A verifier where the request had no challenge.
	if checkVerifier("", strings.Repeat("a", 43)) == nil {
		t.Error("a verifier without a challenge passed")
	}
}

// A declined login goes back to the site with access_denied.
func TestOIDCDeclined(t *testing.T) {
	p, _ := testProvider(t)
	q := url.Values{"client_id": {"7001"}, "redirect_uri": {testRedirect}, "response_type": {"code"}, "scope": {"openid"}, "state": {"z"}}
	token, cookie := open(t, p, "/auth?"+q.Encode())
	if ok, err := p.Requests.Decline(context.Background(), token); !ok || err != nil {
		t.Fatalf("decline: %v %v", ok, err)
	}
	back, err := url.Parse(pollState(t, p, token, cookie).Redirect)
	if err != nil || back.Query().Get("error") != "access_denied" || back.Query().Get("state") != "z" || back.Query().Get("code") != "" {
		t.Fatalf("the redirect: %v", back)
	}
}

// The errors of an authorization request: an unknown client or address is
// a page, never a redirect; anything else goes back to the site.
func TestAuthorizeErrors(t *testing.T) {
	p, _ := testProvider(t)
	base := func() url.Values {
		return url.Values{"client_id": {"7001"}, "redirect_uri": {testRedirect}, "response_type": {"code"}, "scope": {"openid"}, "state": {"s"}}
	}
	for name, edit := range map[string]func(url.Values){
		"unknown client":   func(q url.Values) { q.Set("client_id", "7002") },
		"user as client":   func(q url.Values) { q.Set("client_id", "42") },
		"other address":    func(q url.Values) { q.Set("redirect_uri", "https://evil.example/cb") },
		"address twice":    func(q url.Values) { q.Add("redirect_uri", testRedirect) },
		"no client":        func(q url.Values) { q.Del("client_id") },
		"client not a num": func(q url.Values) { q.Set("client_id", "shopbot") },
	} {
		q := base()
		edit(q)
		w := get(p, "/auth?"+q.Encode())
		if w.Code != http.StatusBadRequest || w.Header().Get("Location") != "" || strings.Contains(w.Body.String(), "data-token") {
			t.Errorf("%s: %d, location %q", name, w.Code, w.Header().Get("Location"))
		}
	}
	for name, c := range map[string]struct {
		edit func(url.Values)
		want string
	}{
		"implicit flow": {func(q url.Values) { q.Set("response_type", "token") }, "unsupported_response_type"},
		"no openid":     {func(q url.Values) { q.Set("scope", "profile") }, "invalid_scope"},
		"plain pkce":    {func(q url.Values) { q.Set("code_challenge", strings.Repeat("a", 43)) }, "invalid_request"},
		"bad challenge": {func(q url.Values) { q.Set("code_challenge", "short"); q.Set("code_challenge_method", "S256") }, "invalid_request"},
		"method only":   {func(q url.Values) { q.Set("code_challenge_method", "S256") }, "invalid_request"},
		"scope twice":   {func(q url.Values) { q.Add("scope", "openid") }, "invalid_request"},
		"long nonce":    {func(q url.Values) { q.Set("nonce", strings.Repeat("n", maxNonce+1)) }, "invalid_request"},
	} {
		q := base()
		c.edit(q)
		w := get(p, "/auth?"+q.Encode())
		loc, err := url.Parse(w.Header().Get("Location"))
		if w.Code != http.StatusFound || err != nil || loc.Host != testDomain || loc.Query().Get("error") != c.want || loc.Query().Get("state") != "s" {
			t.Errorf("%s: %d, location %q", name, w.Code, w.Header().Get("Location"))
		}
	}
}

// The format of the login widget: the browser goes back to return_to with
// the fields of the login signed with the bot's login key.
func TestLegacyLogin(t *testing.T) {
	p, _ := testProvider(t)
	q := url.Values{"bot_id": {"7001"}, "origin": {"https://shop.example"}, "return_to": {"https://shop.example/login?next=/cart"}, "request_access": {"write"}}
	token, cookie := open(t, p, "/auth/legacy?"+q.Encode())
	e, err := p.Requests.Get(context.Background(), token)
	if err != nil || e.Kind != urlauth.KindLegacy || !e.WantsWrite() || e.Domain != testDomain {
		t.Fatalf("the stored request: %+v, %v", e, err)
	}
	accept(t, p, token, false)
	s := pollState(t, p, token, cookie)
	back, err := url.Parse(s.Redirect)
	if s.Status != "done" || err != nil || back.Host != testDomain || back.Path != "/login" || back.Query().Get("next") != "/cart" {
		t.Fatalf("the redirect: %+v", s)
	}
	fields := back.Query()
	fields.Del("next")
	if err := webappdata.VerifyLogin(fields, botkeys.Derive("7001:token-of-the-bot").Login); err != nil {
		t.Fatalf("the site cannot check the login: %v (%s)", err, s.Redirect)
	}
	if fields.Get("id") != "42" || fields.Get("username") != "alice" || fields.Get("photo_url") != "https://pluma.chat/_l/avatar/alice" {
		t.Fatalf("the fields: %v", fields)
	}

	// A declined one stays on the page.
	token2, cookie2 := open(t, p, "/auth/legacy?"+q.Encode())
	if _, err := p.Requests.Decline(context.Background(), token2); err != nil {
		t.Fatal(err)
	}
	if s := pollState(t, p, token2, cookie2); s.Status != "declined" || s.Redirect != "" {
		t.Fatalf("declined: %+v", s)
	}

	for name, edit := range map[string]func(url.Values){
		"other return_to": func(q url.Values) { q.Set("return_to", "https://evil.example/") },
		"other origin":    func(q url.Values) { q.Set("origin", "https://evil.example") },
		"http return_to":  func(q url.Values) { q.Set("return_to", "http://shop.example/") },
		"unknown bot":     func(q url.Values) { q.Set("bot_id", "7002") },
	} {
		q2 := url.Values{}
		for k, v := range q {
			q2[k] = v
		}
		edit(q2)
		if w := get(p, "/auth/legacy?"+q2.Encode()); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", name, w.Code)
		}
	}
	p.BotKeys = fakeKeys{}
	if w := get(p, "/auth/legacy?"+q.Encode()); w.Code != http.StatusServiceUnavailable {
		t.Errorf("a bot without keys: %d", w.Code)
	}
}

// A request gone from Redis (expired, or lost with Redis) starts again.
func TestStateExpired(t *testing.T) {
	p, mr := testProvider(t)
	q := url.Values{"client_id": {"7001"}, "redirect_uri": {testRedirect}, "response_type": {"code"}, "scope": {"openid"}}
	token, cookie := open(t, p, "/auth?"+q.Encode())
	mr.FastForward(urlauth.RequestTTL + time.Second)
	if s := pollState(t, p, token, cookie); s.Status != "expired" {
		t.Fatalf("an expired request: %+v", s)
	}
	// The code of an accepted request lives CodeTTL from its issue.
	token, cookie = open(t, p, "/auth?"+q.Encode())
	accept(t, p, token, false)
	back, _ := url.Parse(pollState(t, p, token, cookie).Redirect)
	mr.FastForward(CodeTTL + time.Second)
	form := url.Values{"grant_type": {"authorization_code"}, "code": {back.Query().Get("code")}, "redirect_uri": {testRedirect}, "client_id": {"7001"}, "client_secret": {testSecret}}
	if w := exchange(p, form, false); !strings.Contains(w.Body.String(), "invalid_grant") {
		t.Fatalf("an expired code: %d %s", w.Code, w.Body.String())
	}
}

func TestParseKeys(t *testing.T) {
	if k, err := ParseKeys([]byte("\n")); k != nil || err != nil {
		t.Fatalf("an empty file: %v %v", k, err)
	}
	a, err := testRSA()
	if err != nil {
		t.Fatal(err)
	}
	b, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pkcs1 := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(b)})
	keys, err := ParseKeys(append(rsaPEM(t, a), pkcs1...))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := keys.JWKS()
	if err != nil {
		t.Fatal(err)
	}
	var set struct{ Keys []jwk }
	if err := json.Unmarshal(doc, &set); err != nil || len(set.Keys) != 2 || set.Keys[0].Kid == set.Keys[1].Kid || set.Keys[0].E != "AQAB" {
		t.Fatalf("the JWKS: %s", doc)
	}
	if want, _ := keyId(&a.PublicKey); set.Keys[0].Kid != want {
		t.Fatalf("the first key signs: %s", doc)
	}
	small, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"1024 bits": rsaPEM(t, small),
		"garbage":   []byte("not a key"),
		"twice":     append(rsaPEM(t, a), rsaPEM(t, a)...),
		"cert":      pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte{1}}),
	} {
		if _, err := ParseKeys(data); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestDiscovery(t *testing.T) {
	p, _ := testProvider(t)
	w := get(p, "/.well-known/openid-configuration")
	var d discoveryDoc
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if d.Issuer != "https://oauth.pluma.chat" || d.TokenEndpoint != "https://oauth.pluma.chat/token" || d.JwksURI != "https://oauth.pluma.chat/.well-known/jwks.json" ||
		strings.Join(d.CodeChallengeMethodsSupported, ",") != "S256" {
		t.Fatalf("the document: %+v", d)
	}
	if w := get(p, "/token"); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /token: %d", w.Code)
	}
	if w := get(p, "/nothing"); w.Code != http.StatusNotFound {
		t.Errorf("an unknown path: %d", w.Code)
	}
}

func TestVisitorIP(t *testing.T) {
	for _, c := range []struct{ remote, xff, want string }{
		{"172.18.0.5:1", "203.0.113.9", "203.0.113.9"},
		{"172.18.0.5:1", "10.0.0.1, 203.0.113.9", "203.0.113.9"},
		{"198.51.100.1:1", "203.0.113.9", "198.51.100.1"}, // not past Caddy
		{"172.18.0.5:1", "", "172.18.0.5"},
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.RemoteAddr = c.remote
		if c.xff != "" {
			r.Header.Set("X-Forwarded-For", c.xff)
		}
		if got := visitorIP(r); got != c.want {
			t.Errorf("%s %q: %q, want %q", c.remote, c.xff, got, c.want)
		}
	}
}

func TestLimits(t *testing.T) {
	l := newLimits()
	now := time.Unix(1_700_000_000, 0)
	n := 0
	for l.allow(limitAuthorize, "203.0.113.9", now) {
		n++
		if n > 100 {
			break
		}
	}
	if n != limitRates[limitAuthorize].burst {
		t.Fatalf("%d requests at once, want %d", n, limitRates[limitAuthorize].burst)
	}
	if !l.allow(limitAuthorize, "203.0.113.10", now) || !l.allow(limitAuthorize, "203.0.113.9", now.Add(limitRates[limitAuthorize].every)) {
		t.Fatal("another address, or the same one later, is refused")
	}
	if subject("2001:db8::1") != subject("2001:db8::2") {
		t.Fatal("one IPv6 /64 counts as several")
	}
}

func TestCodeFormat(t *testing.T) {
	token, err := urlauth.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	code, err := newCode(token)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := tokenOfCode(code); !ok || got != token {
		t.Fatalf("tokenOfCode(%q) = %q, %v", code, got, ok)
	}
	for _, bad := range []string{"", token, token + ".", "x." + code[len(token)+1:], code + "x"} {
		if _, ok := tokenOfCode(bad); ok {
			t.Errorf("tokenOfCode(%q) took it", bad)
		}
	}
}
