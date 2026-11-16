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
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/teamgram/teamgram-server/pkg/urlauth"
	"github.com/teamgram/teamgram-server/pkg/webappdata"

	"github.com/zeromicro/go-zero/core/logx"
)

// CodeTTL is how long an authorization code may be exchanged (task 106,
// «Пределы и ошибки»; RFC 6749 4.1.2: "A maximum authorization code
// lifetime of 10 minutes is RECOMMENDED").
const CodeTTL = 60 * time.Second

// codeBytes is the random part of an authorization code.
const codeBytes = 32

// The authorization code of an accepted request lives in the request
// itself (oauth_req:<token>, pkg/urlauth), so that every step of it is one
// script on one key, as the scripts of pkg/urlauth are:
//
//	issued    the code, <token>.<random>, given out once; a page that
//	          polls again gets the same one
//	redeemed  set when a site exchanged it
//
// Issuing the code cuts the life of the request to CodeTTL.
const (
	// KEYS[1]; ARGV code, ttl. {state, code}: the code issued now or
	// before for an accepted request, "" for another state; {"", ""} for no
	// request.
	issueScript = `
local v = redis.call('HMGET', KEYS[1], 'state', 'issued')
if not v[1] then return {'', ''} end
if v[1] ~= 'accepted' then return {v[1], ''} end
if v[2] then return {v[1], v[2]} end
redis.call('HSET', KEYS[1], 'issued', ARGV[1])
redis.call('EXPIRE', KEYS[1], ARGV[2])
return {v[1], ARGV[1]}`

	// KEYS[1]; ARGV code. 1 redeemed now, 0 no such code, -1 redeemed
	// before.
	redeemScript = `
local v = redis.call('HMGET', KEYS[1], 'state', 'issued', 'redeemed')
if not v[1] or v[1] ~= 'accepted' or not v[2] or v[2] ~= ARGV[1] then return 0 end
if v[3] then return -1 end
redis.call('HSET', KEYS[1], 'redeemed', '1')
return 1`
)

// requestKey is the key of the request of token (pkg/urlauth).
func requestKey(token string) string { return "oauth_req:" + token }

// newCode makes the authorization code of the request of token.
func newCode(token string) (string, error) {
	b := make([]byte, codeBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("oauth: read random: %w", err)
	}
	return token + "." + base64.RawURLEncoding.EncodeToString(b), nil
}

// tokenOfCode is the token of the request of an authorization code.
func tokenOfCode(code string) (string, bool) {
	token, random, ok := strings.Cut(code, ".")
	if !ok || !urlauth.ValidToken(token) || len(random) != base64.RawURLEncoding.EncodedLen(codeBytes) || !base64url(random) {
		return "", false
	}
	return token, true
}

// issueCode returns the authorization code of the accepted request of
// token: made now, or the one made for it before.
func (p *Provider) issueCode(ctx context.Context, token string) (string, error) {
	code, err := newCode(token)
	if err != nil {
		return "", err
	}
	res, err := p.KV.EvalCtx(ctx, issueScript, requestKey(token), code, int64(CodeTTL/time.Second))
	if err != nil {
		return "", err
	}
	list, ok := res.([]any)
	if !ok || len(list) != 2 {
		return "", fmt.Errorf("oauth: issue: unexpected reply %T", res)
	}
	state, _ := list[0].(string)
	issued, _ := list[1].(string)
	switch {
	case state == "":
		return "", urlauth.ErrExpired
	case state != string(urlauth.StatusAccepted) || issued == "":
		return "", fmt.Errorf("oauth: issue: the request is %q", state)
	}
	return issued, nil
}

// redeemCode marks code exchanged: true the first time, false for a code
// there is no request of, or one exchanged before.
func (p *Provider) redeemCode(ctx context.Context, token, code string) (bool, error) {
	res, err := p.KV.EvalCtx(ctx, redeemScript, requestKey(token), code)
	if err != nil {
		return false, err
	}
	n, ok := res.(int64)
	if !ok {
		return false, fmt.Errorf("oauth: redeem: unexpected reply %T", res)
	}
	return n == 1, nil
}

// stateReply is the answer of /auth/state to the page's script
// (assets/poll.js).
type stateReply struct {
	// Status: pending, done (go to Redirect), declined (the widget format
	// stays on the page), expired (start again).
	Status   string `json:"status"`
	Redirect string `json:"redirect,omitempty"`
}

// state is GET /auth/state?token: where the request of the page is and,
// once the user has answered it, the address the browser goes back to the
// site at.
func (p *Provider) state(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !p.limits.allow(limitState, visitorIP(r), p.now()) {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	token := r.URL.Query().Get("token")
	if !urlauth.ValidToken(token) || !p.boundTo(r, token) {
		// Another browser's request, or a page whose cookie is gone: it
		// can only start again.
		writeState(w, stateReply{Status: "expired"})
		return
	}
	e, err := p.Requests.Get(ctx, token)
	switch {
	case errors.Is(err, urlauth.ErrExpired):
		writeState(w, stateReply{Status: "expired"})
		return
	case errors.Is(err, urlauth.ErrInvalid):
		logx.WithContext(ctx).Errorf("oauth: a login request: %v", err)
		writeState(w, stateReply{Status: "expired"})
		return
	case err != nil:
		logx.WithContext(ctx).Errorf("oauth: read a login request: %v", err)
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	reply, err := p.outcome(ctx, e)
	if errors.Is(err, urlauth.ErrExpired) {
		reply = stateReply{Status: "expired"}
	} else if err != nil {
		logx.WithContext(ctx).Errorf("oauth: the outcome of a login request: %v", err)
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	writeState(w, reply)
}

// outcome is what the page of e does now.
func (p *Provider) outcome(ctx context.Context, e *urlauth.Entry) (stateReply, error) {
	switch e.Status {
	case urlauth.StatusPending:
		return stateReply{Status: "pending"}, nil
	case urlauth.StatusDeclined:
		if e.Kind == urlauth.KindOIDC {
			// The user declined: access_denied (RFC 6749 4.1.2.1).
			return stateReply{Status: "done", Redirect: withQuery(e.RedirectURI, withState(url.Values{
				"error":             {"access_denied"},
				"error_description": {"the user declined the login"},
			}, e.State))}, nil
		}
		return stateReply{Status: "declined"}, nil
	}
	// Accepted.
	if e.Kind == urlauth.KindOIDC {
		code, err := p.issueCode(ctx, e.Token)
		if err != nil {
			return stateReply{}, err
		}
		return stateReply{Status: "done", Redirect: withQuery(e.RedirectURI, withState(url.Values{"code": {code}}, e.State))}, nil
	}
	link, err := p.legacyLogin(ctx, e)
	if err != nil {
		return stateReply{}, err
	}
	return stateReply{Status: "done", Redirect: link}, nil
}

// withState adds the site's state to v, when it gave one (RFC 6749
// 4.1.2: "REQUIRED if the "state" parameter was present in the client
// authorization request").
func withState(v url.Values, state string) url.Values {
	if state != "" {
		v.Set("state", state)
	}
	return v
}

// legacyLogin is return_to of an accepted request of the widget format
// with the fields of the login (core.telegram.org/widgets/login-legacy),
// signed with the bot's login key, as a login button gives them to the
// bot's site (pkg/urlauth.LoginLink). photo_url is the public photo of a
// user with a username (ADR 0036 p. 4), as bff gives it.
func (p *Provider) legacyLogin(ctx context.Context, e *urlauth.Entry) (string, error) {
	key, ok, err := p.loginKey(ctx, e.BotId)
	if err != nil {
		return "", err
	}
	if !ok {
		// The bot lost its keys meanwhile (a new token): the login cannot
		// be signed, and the site starts again.
		return "", urlauth.ErrExpired
	}
	u, err := p.Accounts.Account(ctx, e.UserId)
	if err != nil {
		return "", err
	}
	if u == nil {
		return "", urlauth.ErrExpired
	}
	login := &webappdata.Login{
		Id:        u.Id,
		FirstName: u.FirstName,
		LastName:  u.LastName,
		Username:  u.Username,
		AuthDate:  e.AcceptedAt,
	}
	if u.Username != "" {
		login.PhotoUrl = p.Site + "/_l/avatar/" + url.PathEscape(u.Username)
	}
	fields, err := webappdata.SignLogin(login, key)
	if err != nil {
		return "", err
	}
	return urlauth.LoginLink(e.ReturnTo, fields)
}

func writeState(w http.ResponseWriter, reply stateReply) {
	b, err := json.Marshal(reply)
	if err != nil {
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(b)
}
