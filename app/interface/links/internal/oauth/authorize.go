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
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/teamgram/teamgram-server/pkg/botkeys"
	"github.com/teamgram/teamgram-server/pkg/urlauth"

	"github.com/zeromicro/go-zero/core/logx"
)

// Limits of the parameters of a request a site makes; they bound what a
// request keeps in Redis.
const (
	maxState = 1024
	maxNonce = 512
	// An S256 code challenge is the base64url of a SHA-256 digest, 43
	// characters (RFC 7636 4.2).
	challengeLen = 43
)

// knownScopes are the scopes a site may ask for (core.telegram.org/bots/telegram-login,
// pkg/urlauth); others are left out of the request, as OpenID Connect asks:
// "Scope values used that are not understood by an implementation SHOULD
// be ignored" (OpenID Connect Core 1.0, 3.1.2.1).
var knownScopes = []string{urlauth.ScopeOpenID, urlauth.ScopeProfile, urlauth.ScopePhone, urlauth.ScopeBotAccess}

// oneValue is the single value of name in q; ok is false when the
// parameter is there more than once ("Request and response parameters MUST
// NOT be included more than once", RFC 6749 3.1).
func oneValue(q url.Values, name string) (string, bool) {
	v := q[name]
	if len(v) > 1 {
		return "", false
	}
	if len(v) == 0 {
		return "", true
	}
	return v[0], true
}

// scopesOf is the known scopes of a scope parameter, each once, in the
// order of knownScopes.
func scopesOf(scope string) []string {
	asked := strings.Fields(scope)
	var out []string
	for _, s := range knownScopes {
		if slices.Contains(asked, s) {
			out = append(out, s)
		}
	}
	return out
}

// withQuery is link with v added after its own query; the fragment stays.
func withQuery(link string, v url.Values) string {
	u, err := url.Parse(link)
	if err != nil {
		return link
	}
	if u.RawQuery != "" {
		u.RawQuery += "&" + v.Encode()
	} else {
		u.RawQuery = v.Encode()
	}
	return u.String()
}

// authorize is GET /auth, the authorization endpoint of OpenID Connect
// (OpenID Connect Core 1.0, 3.1.2; RFC 6749 4.1.1; RFC 7636). A request of
// an unknown client or with a redirect_uri the client does not list is an
// error page, never a redirect (RFC 6749 4.1.2.1); any other error goes
// back to the site's redirect_uri.
func (p *Provider) authorize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ip := visitorIP(r)
	if !p.limits.allow(limitAuthorize, ip, p.now()) {
		p.errorPage(w, r, http.StatusTooManyRequests, "oauth_error_busy")
		return
	}
	q := r.URL.Query()
	clientId, ok1 := oneValue(q, "client_id")
	redirect, ok2 := oneValue(q, "redirect_uri")
	if !ok1 || !ok2 {
		p.errorPage(w, r, http.StatusBadRequest, "oauth_error_client")
		return
	}
	botId, err := strconv.ParseInt(clientId, 10, 64)
	if err != nil || botId <= 0 {
		p.errorPage(w, r, http.StatusBadRequest, "oauth_error_client")
		return
	}
	client, err := p.Bots.LoginClient(ctx, botId)
	if err != nil {
		p.serverError(w, r, "a login client", err)
		return
	}
	if client == nil || !slices.Contains(client.RedirectURIs, redirect) {
		p.errorPage(w, r, http.StatusBadRequest, "oauth_error_client")
		return
	}
	bot, err := p.bot(ctx, botId)
	if err != nil {
		p.serverError(w, r, "the bot of a login client", err)
		return
	}
	if bot == nil {
		p.errorPage(w, r, http.StatusBadRequest, "oauth_error_client")
		return
	}

	state, okState := oneValue(q, "state")
	fail := func(code, desc string) {
		v := url.Values{"error": {code}, "error_description": {desc}}
		if okState && state != "" && len(state) <= maxState {
			v.Set("state", state)
		}
		http.Redirect(w, r, withQuery(redirect, v), http.StatusFound)
	}
	for _, name := range []string{"response_type", "scope", "state", "nonce", "code_challenge", "code_challenge_method"} {
		if _, ok := oneValue(q, name); !ok {
			fail("invalid_request", name+" is given more than once")
			return
		}
	}
	if q.Get("response_type") != "code" {
		fail("unsupported_response_type", "only the authorization code flow is supported")
		return
	}
	scopes := scopesOf(q.Get("scope"))
	if !slices.Contains(scopes, urlauth.ScopeOpenID) {
		fail("invalid_scope", "the openid scope is required")
		return
	}
	nonce := q.Get("nonce")
	if len(state) > maxState || len(nonce) > maxNonce {
		fail("invalid_request", "state or nonce is too long")
		return
	}
	challenge, method := q.Get("code_challenge"), q.Get("code_challenge_method")
	switch {
	case challenge == "" && method != "":
		fail("invalid_request", "code_challenge_method without code_challenge")
		return
	case challenge != "" && method != "S256":
		// Without a method the challenge is "plain" (RFC 7636 4.3), which
		// the provider does not take.
		fail("invalid_request", "only the S256 code challenge method is supported")
		return
	case challenge != "" && (len(challenge) != challengeLen || !base64url(challenge)):
		fail("invalid_request", "code_challenge is not a base64url SHA-256 digest")
		return
	}
	u, err := url.Parse(redirect)
	if err != nil {
		p.errorPage(w, r, http.StatusBadRequest, "oauth_error_client")
		return
	}
	browser, platform := urlauth.UserAgent(r.UserAgent())
	p.start(w, r, bot, urlauth.Request{
		Kind:          urlauth.KindOIDC,
		BotId:         botId,
		Domain:        strings.ToLower(u.Hostname()),
		RedirectURI:   redirect,
		State:         state,
		CodeChallenge: challenge,
		Scopes:        scopes,
		Nonce:         nonce,
		Browser:       browser,
		Platform:      platform,
		IP:            ip,
		Region:        p.region(ctx, ip),
	}, "")
}

// base64url tells whether s is of the base64url alphabet without padding.
func base64url(s string) bool {
	_, err := base64.RawURLEncoding.DecodeString(s)
	return err == nil
}

// authorizeLegacy is GET /auth/legacy?bot_id&origin&return_to[&request_access=write],
// the login in the format of the login widget
// (core.telegram.org/widgets/login-legacy): the site is the bot's login
// domain, and the browser goes back to return_to with the fields of the
// login signed with the bot's login key (task 106, «Провайдер входа»).
func (p *Provider) authorizeLegacy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ip := visitorIP(r)
	if !p.limits.allow(limitAuthorize, ip, p.now()) {
		p.errorPage(w, r, http.StatusTooManyRequests, "oauth_error_busy")
		return
	}
	q := r.URL.Query()
	for _, name := range []string{"bot_id", "origin", "return_to", "request_access"} {
		if _, ok := oneValue(q, name); !ok {
			p.errorPage(w, r, http.StatusBadRequest, "oauth_error_client")
			return
		}
	}
	botId, err := strconv.ParseInt(q.Get("bot_id"), 10, 64)
	if err != nil || botId <= 0 {
		p.errorPage(w, r, http.StatusBadRequest, "oauth_error_client")
		return
	}
	bot, err := p.bot(ctx, botId)
	if err != nil {
		p.serverError(w, r, "the bot of a login", err)
		return
	}
	if bot == nil {
		p.errorPage(w, r, http.StatusBadRequest, "oauth_error_client")
		return
	}
	domain, err := p.Bots.LoginDomain(ctx, botId)
	if err != nil {
		p.serverError(w, r, "the login domain of a bot", err)
		return
	}
	origin, okOrigin := siteURL(q.Get("origin"), domain)
	returnTo, okReturn := siteURL(q.Get("return_to"), domain)
	if domain == "" || !okOrigin || !okReturn {
		p.errorPage(w, r, http.StatusBadRequest, "oauth_error_client")
		return
	}
	// The data of the login is signed with the bot's login key: a bot
	// without one (no BOT_SECRET_ENCRYPTION_KEY, or no login since the keys
	// were introduced) cannot log anybody in this way.
	if _, ok, err := p.loginKey(ctx, botId); err != nil {
		p.serverError(w, r, "the keys of a bot", err)
		return
	} else if !ok {
		p.errorPage(w, r, http.StatusServiceUnavailable, "oauth_error_unavailable")
		return
	}
	browser, platform := urlauth.UserAgent(r.UserAgent())
	p.start(w, r, bot, urlauth.Request{
		Kind:         urlauth.KindLegacy,
		BotId:        botId,
		Domain:       domain,
		Origin:       origin.Scheme + "://" + origin.Host,
		ReturnTo:     returnTo.String(),
		RequestWrite: q.Get("request_access") == "write",
		Browser:      browser,
		Platform:     platform,
		IP:           ip,
		Region:       p.region(ctx, ip),
	}, origin.Scheme+"://"+origin.Host+"/")
}

// siteURL is s parsed when it is an https address on domain (the host
// without regard to case, any port), the bot's login domain
// (core.telegram.org/bots/api#loginurl: "The url's domain must be the same
// as the domain linked with the bot").
func siteURL(s, domain string) (*url.URL, bool) {
	if s == "" || domain == "" || len(s) > 2048 {
		return nil, false
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.User != nil || !strings.EqualFold(u.Hostname(), domain) {
		return nil, false
	}
	return u, true
}

// loginKey is the login key of the bot's current token; ok is false for a
// bot without keys. Keys sealed under another encryption key count as none
// after a log line: the bot's next login stores them again
// (botkeys.Store.Get).
func (p *Provider) loginKey(ctx context.Context, botId int64) ([]byte, bool, error) {
	if p.BotKeys == nil {
		return nil, false, nil
	}
	keys, ok, err := p.BotKeys.Get(ctx, botId)
	if errors.Is(err, botkeys.ErrSealedInvalid) {
		logx.WithContext(ctx).Errorf("oauth: the keys of bot %d do not open: %v", botId, err)
		return nil, false, nil
	}
	if err != nil || !ok {
		return nil, false, err
	}
	return keys.Login, true, nil
}

// start stores the request and shows its page; back is the site, offered
// when the user declines a login of the widget format.
func (p *Provider) start(w http.ResponseWriter, r *http.Request, bot *Account, req urlauth.Request, back string) {
	ctx := r.Context()
	e, err := p.Requests.Create(ctx, req)
	if err != nil {
		p.serverError(w, r, "store a login request", err)
		return
	}
	link := urlauth.AppLink(siteHost(p.Site), e.Token)
	code, err := qrSVG(link)
	if err != nil {
		p.serverError(w, r, "draw the QR code", err)
		return
	}
	pg := p.newPage(r, "oauth_page_title", req.Domain)
	pg.Heading = p.Texts.Text(pg.Lang, "oauth_heading", req.Domain)
	p.botFace(pg, bot)
	pg.QR = code
	pg.AppLink = link
	pg.Code = e.Code
	pg.Token = e.Token
	pg.Retry = r.URL.RequestURI()
	pg.Back = back
	http.SetCookie(w, &http.Cookie{
		Name:     bindingCookie(e.Token),
		Value:    p.binding(e.Token),
		Path:     "/auth/state",
		MaxAge:   int(urlauth.RequestTTL.Seconds()),
		Secure:   true, // the issuer is an https:// origin (server.go checks it)
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
	p.render(ctx, w, http.StatusOK, pg)
}

// siteHost is the host of the site's address.
func siteHost(site string) string {
	u, err := url.Parse(site)
	if err != nil {
		return site
	}
	return u.Host
}

// The page of a request polls its state with a cookie only the browser that
// was shown the page has: the token of the request is in its QR code and
// link, which anybody who sees the screen may read, and the state of an
// accepted request carries the authorization code, or the signed login of
// the widget format.

// bindingCookie is the name of the cookie of the page of token; one per
// request, so that pages of several requests in one browser do not
// replace each other's.
func bindingCookie(token string) string {
	n := len(token)
	if n > 16 {
		n = 16
	}
	return "oauth_" + token[:n]
}

// binding is the value of the cookie of the page of token.
func (p *Provider) binding(token string) string {
	mac := hmac.New(sha256.New, p.Keys.binding)
	mac.Write([]byte(token))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// boundTo tells whether r comes from the browser shown the page of token.
func (p *Provider) boundTo(r *http.Request, token string) bool {
	c, err := r.Cookie(bindingCookie(token))
	if err != nil {
		return false
	}
	return hmac.Equal([]byte(c.Value), []byte(p.binding(token)))
}

// serverError logs err, which is the server's, and shows the error page.
func (p *Provider) serverError(w http.ResponseWriter, r *http.Request, what string, err error) {
	logx.WithContext(r.Context()).Errorf("oauth: %s: %v", what, err)
	p.errorPage(w, r, http.StatusServiceUnavailable, "oauth_error_server")
}
