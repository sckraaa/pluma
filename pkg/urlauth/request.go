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

package urlauth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/zeromicro/go-zero/core/stores/kv"
)

// Limits of a login request (task 106, «Пределы и ошибки»).
const (
	// RequestTTL is how long a request waits for the user, from the moment
	// the site made it; then the apps get URL_EXPIRED and the site starts
	// again.
	RequestTTL = 300 * time.Second
	// MaxWrongCodes wrong match codes, checked or sent with the acceptance,
	// end the request (task 106, «Пределы и ошибки»). The apps take a wrong
	// code for a failed login, so a user who opened a link the page did not
	// show guesses one code in four a time, and the link stops working after
	// at most three such openings.
	MaxWrongCodes = 3
	// maxMatchCode bounds a match code from a client before it goes to
	// Redis; the emoji of the set are a few bytes each.
	maxMatchCode = 64
)

// Scopes of a request of OpenID Connect a user consents to in the app
// (core.telegram.org/bots/telegram-login).
const (
	ScopeOpenID    = "openid"
	ScopeProfile   = "profile"
	ScopePhone     = "phone"
	ScopeBotAccess = "telegram:bot_access"
)

// Kind is the protocol of the site that made a request.
type Kind uint8

const (
	// KindOIDC is OpenID Connect: the site gets an authorization code at
	// RedirectURI and exchanges it for an ID token.
	KindOIDC Kind = 1
	// KindLegacy is the format of the login widget
	// (core.telegram.org/widgets/login-legacy): the browser goes to ReturnTo
	// with the signed fields of the login.
	KindLegacy Kind = 2
)

// Request is what a site asks for and what the page knows of its visitor.
// The provider in links fills it in; the apps see Bot, Domain and the
// visitor in urlAuthResultRequest.
type Request struct {
	Kind  Kind  `json:"kind"`
	BotId int64 `json:"bot_id"` // the client_id, or the bot_id of the widget
	// Domain is the host of the site as the app shows it: the host of
	// RedirectURI, or the login domain of the bot for the widget format.
	Domain string `json:"domain"`

	// OpenID Connect.
	RedirectURI   string   `json:"redirect_uri,omitempty"`
	State         string   `json:"state,omitempty"`
	CodeChallenge string   `json:"code_challenge,omitempty"` // PKCE S256
	Scopes        []string `json:"scopes,omitempty"`
	Nonce         string   `json:"nonce,omitempty"`

	// The widget format.
	Origin       string `json:"origin,omitempty"`
	ReturnTo     string `json:"return_to,omitempty"`
	RequestWrite bool   `json:"request_write,omitempty"` // request_access=write

	// The visitor: the browser and the platform from its User-Agent
	// (UserAgent), its address and the region of the address.
	Browser  string `json:"browser"`
	Platform string `json:"platform"`
	IP       string `json:"ip"`
	Region   string `json:"region"`

	// MatchCodes the apps offer, in their order; the right one is kept
	// apart (Entry.Code).
	MatchCodes []string `json:"match_codes"`
	CreatedAt  int64    `json:"created_at"` // unix seconds
}

// WantsWrite tells whether the site asks to let its bot write to the user:
// the scope telegram:bot_access, or request_access=write of the widget.
func (r *Request) WantsWrite() bool {
	return r.RequestWrite || (r.Kind == KindOIDC && slices.Contains(r.Scopes, ScopeBotAccess))
}

// WantsPhone tells whether the site asks for the user's phone number: the
// scope phone of OpenID Connect only (the widget has no such field).
func (r *Request) WantsPhone() bool {
	return r.Kind == KindOIDC && slices.Contains(r.Scopes, ScopePhone)
}

// Status is where a request is.
type Status string

const (
	StatusPending  Status = "pending"
	StatusAccepted Status = "accepted"
	StatusDeclined Status = "declined"
)

// Entry is a stored request with its state.
type Entry struct {
	Request
	Token  string
	Code   string // the right match code, shown on the page
	Status Status
	// Accepted requests: the user and what the user consented to.
	UserId       int64
	WriteAllowed bool // telegram:bot_access, or request_access=write
	SharePhone   bool // phone
	AcceptedAt   int64
	Attempts     int64 // wrong match codes so far
}

var (
	// ErrExpired: no such request, or no longer one a user may answer
	// (URL_EXPIRED).
	ErrExpired = errors.New("urlauth: login request expired")
	// ErrInvalid: a stored request that cannot be read (OAUTH_REQUEST_INVALID).
	ErrInvalid = errors.New("urlauth: login request invalid")
)

// Redis layout, one key a request; every script touches that key only, so
// the store works on a go-zero kv cluster as on a single node.
//
//	oauth_req:<token>  hash {req: Request JSON, code, state, attempts,
//	                         user_id, write, phone, accepted_at}
//
// The key lives RequestTTL from its creation; answering it does not extend
// it. An answered request is taken (Take) by the page that waits for it.
const keyPrefix = "oauth_req:"

func key(token string) string { return keyPrefix + token }

const (
	// KEYS[1]; ARGV req, code, ttl seconds. 0 when the token is taken.
	createScript = `
if redis.call('EXISTS', KEYS[1]) == 1 then return 0 end
redis.call('HSET', KEYS[1], 'req', ARGV[1], 'code', ARGV[2], 'state', 'pending', 'attempts', 0, 'user_id', 0, 'write', 0, 'phone', 0, 'accepted_at', 0)
redis.call('EXPIRE', KEYS[1], ARGV[3])
return 1`

	// wrongCode counts a wrong code and ends the request at the limit
	// (ARGV[2]): -2 when it ended, 0 otherwise.
	wrongCode = `
local n = redis.call('HINCRBY', KEYS[1], 'attempts', 1)
if n >= tonumber(ARGV[2]) then
  redis.call('DEL', KEYS[1])
  return -2
end
return 0`

	// KEYS[1]; ARGV code, limit. 1 right, 0 wrong, -2 wrong and ended,
	// -1 no pending request.
	checkScript = `
local v = redis.call('HMGET', KEYS[1], 'state', 'code')
if not v[1] or v[1] ~= 'pending' then return -1 end
if v[2] == ARGV[1] then return 1 end` + wrongCode

	// KEYS[1]; ARGV code, limit, user_id, write, phone, now. 1 accepted,
	// 2 accepted before by this user with this code (a repeated call),
	// 0 wrong, -2 wrong and ended, -1 no pending request.
	acceptScript = `
local v = redis.call('HMGET', KEYS[1], 'state', 'code', 'user_id')
if not v[1] then return -1 end
if v[1] == 'accepted' and v[2] == ARGV[1] and v[3] == ARGV[3] then return 2 end
if v[1] ~= 'pending' then return -1 end
if v[2] == ARGV[1] then
  redis.call('HSET', KEYS[1], 'state', 'accepted', 'user_id', ARGV[3], 'write', ARGV[4], 'phone', ARGV[5], 'accepted_at', ARGV[6])
  return 1
end` + wrongCode

	// KEYS[1]. 1 declined (now or before), 0 accepted already, -1 none.
	declineScript = `
local s = redis.call('HGET', KEYS[1], 'state')
if not s then return -1 end
if s == 'pending' then
  redis.call('HSET', KEYS[1], 'state', 'declined')
  return 1
end
if s == 'declined' then return 1 end
return 0`

	// KEYS[1]. The request; an answered one is removed with the reading.
	takeScript = `
local all = redis.call('HGETALL', KEYS[1])
if #all == 0 then return all end
if redis.call('HGET', KEYS[1], 'state') ~= 'pending' then redis.call('DEL', KEYS[1]) end
return all`
)

// Store keeps the login requests in Redis (the KV of bff and links).
type Store struct {
	kv  kv.Store
	now func() time.Time
}

// NewStore returns a Store on kv.
func NewStore(kv kv.Store) *Store {
	return &Store{kv: kv, now: time.Now}
}

// NewToken returns a new token of a request: 24 random bytes in base64url.
func NewToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("urlauth: read random: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Create stores a new request with fresh match codes and returns it with
// its token and the right code, which the page shows.
func (s *Store) Create(ctx context.Context, r Request) (*Entry, error) {
	if r.Kind != KindOIDC && r.Kind != KindLegacy || r.BotId == 0 || r.Domain == "" {
		return nil, fmt.Errorf("urlauth: incomplete request (kind %d, bot %d, domain %q)", r.Kind, r.BotId, r.Domain)
	}
	codes, right, err := NewMatchCodes()
	if err != nil {
		return nil, err
	}
	r.MatchCodes = codes
	r.CreatedAt = s.now().Unix()
	data, err := json.Marshal(&r)
	if err != nil {
		return nil, err
	}
	// A clash of 192 random bits does not happen; the loop only keeps a
	// broken random source from overwriting a request.
	for range 3 {
		token, err := NewToken()
		if err != nil {
			return nil, err
		}
		res, err := s.kv.EvalCtx(ctx, createScript, key(token), string(data), right, int64(RequestTTL/time.Second))
		if err != nil {
			return nil, err
		}
		if n, err := toInt64(res); err != nil {
			return nil, err
		} else if n == 1 {
			return &Entry{Request: r, Token: token, Code: right, Status: StatusPending}, nil
		}
	}
	return nil, errors.New("urlauth: no free token")
}

// Get returns the request of token: ErrExpired when there is none.
func (s *Store) Get(ctx context.Context, token string) (*Entry, error) {
	if !ValidToken(token) {
		return nil, ErrExpired
	}
	m, err := s.kv.HgetallCtx(ctx, key(token))
	if err != nil {
		return nil, err
	}
	return parseEntry(token, m)
}

// Take returns the request of token and removes it once it is answered: the
// page of the request reads it so, and an accepted request gives out one
// authorization only.
func (s *Store) Take(ctx context.Context, token string) (*Entry, error) {
	if !ValidToken(token) {
		return nil, ErrExpired
	}
	res, err := s.kv.EvalCtx(ctx, takeScript, key(token))
	if err != nil {
		return nil, err
	}
	list, ok := res.([]any)
	if !ok || len(list)%2 != 0 {
		return nil, fmt.Errorf("urlauth: take: unexpected reply %T", res)
	}
	m := make(map[string]string, len(list)/2)
	for i := 0; i < len(list); i += 2 {
		k, _ := list[i].(string)
		v, _ := list[i+1].(string)
		m[k] = v
	}
	return parseEntry(token, m)
}

func parseEntry(token string, m map[string]string) (*Entry, error) {
	if len(m) == 0 {
		return nil, ErrExpired
	}
	e := &Entry{Token: token, Code: m["code"], Status: Status(m["state"])}
	if err := json.Unmarshal([]byte(m["req"]), &e.Request); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	switch e.Status {
	case StatusPending, StatusAccepted, StatusDeclined:
	default:
		return nil, fmt.Errorf("%w: state %q", ErrInvalid, e.Status)
	}
	if e.Code == "" || len(e.MatchCodes) != MatchCodeCount || !slices.Contains(e.MatchCodes, e.Code) || e.BotId == 0 || e.Domain == "" {
		return nil, fmt.Errorf("%w: incomplete", ErrInvalid)
	}
	var err error
	if e.UserId, err = strconv.ParseInt(m["user_id"], 10, 64); err != nil {
		return nil, fmt.Errorf("%w: user_id: %v", ErrInvalid, err)
	}
	if e.Attempts, err = strconv.ParseInt(m["attempts"], 10, 64); err != nil {
		return nil, fmt.Errorf("%w: attempts: %v", ErrInvalid, err)
	}
	if e.AcceptedAt, err = strconv.ParseInt(m["accepted_at"], 10, 64); err != nil {
		return nil, fmt.Errorf("%w: accepted_at: %v", ErrInvalid, err)
	}
	e.WriteAllowed = m["write"] == "1"
	e.SharePhone = m["phone"] == "1"
	return e, nil
}

// CodeResult is the outcome of a match code.
type CodeResult int

const (
	// CodeWrong: not the right code; the request waits for another.
	CodeWrong CodeResult = iota
	// CodeEnded: not the right code, and the last one the request took.
	CodeEnded
	// CodeRight: the right code.
	CodeRight
	// CodeRepeated: an acceptance this user made before with this code,
	// sent again (a retry after a lost answer).
	CodeRepeated
)

// codeResult maps the reply of checkScript and acceptScript.
func codeResult(res any) (CodeResult, error) {
	n, err := toInt64(res)
	if err != nil {
		return 0, err
	}
	switch n {
	case 1:
		return CodeRight, nil
	case 2:
		return CodeRepeated, nil
	case 0:
		return CodeWrong, nil
	case -2:
		return CodeEnded, nil
	case -1:
		return 0, ErrExpired
	}
	return 0, fmt.Errorf("urlauth: unexpected reply %d", n)
}

// CheckCode tells whether code is the right match code of a pending request
// (messages.checkUrlAuthMatchCode); a wrong one counts towards
// MaxWrongCodes.
func (s *Store) CheckCode(ctx context.Context, token, code string) (CodeResult, error) {
	if !ValidToken(token) {
		return 0, ErrExpired
	}
	if len(code) > maxMatchCode {
		code = ""
	}
	res, err := s.kv.EvalCtx(ctx, checkScript, key(token), code, MaxWrongCodes)
	if err != nil {
		return 0, err
	}
	return codeResult(res)
}

// Acceptance is the user's answer to a request.
type Acceptance struct {
	UserId       int64
	Code         string
	WriteAllowed bool
	SharePhone   bool
}

// Accept accepts a pending request for a user who sent its right match code
// (messages.acceptUrlAuth): CodeRight, or CodeRepeated for the same
// acceptance sent again. A wrong code counts towards MaxWrongCodes and
// leaves the request as it was.
func (s *Store) Accept(ctx context.Context, token string, a Acceptance) (CodeResult, error) {
	if !ValidToken(token) || a.UserId == 0 {
		return 0, ErrExpired
	}
	if len(a.Code) > maxMatchCode {
		a.Code = ""
	}
	res, err := s.kv.EvalCtx(ctx, acceptScript, key(token),
		a.Code, MaxWrongCodes, a.UserId, flag(a.WriteAllowed), flag(a.SharePhone), s.now().Unix())
	if err != nil {
		return 0, err
	}
	return codeResult(res)
}

// Decline declines a request (messages.declineUrlAuth): true when it is
// declined, now or before; false when a user has accepted it already.
func (s *Store) Decline(ctx context.Context, token string) (bool, error) {
	if !ValidToken(token) {
		return false, ErrExpired
	}
	res, err := s.kv.EvalCtx(ctx, declineScript, key(token))
	if err != nil {
		return false, err
	}
	n, err := toInt64(res)
	if err != nil {
		return false, err
	}
	switch n {
	case 1:
		return true, nil
	case 0:
		return false, nil
	case -1:
		return false, ErrExpired
	}
	return false, fmt.Errorf("urlauth: decline: unexpected reply %d", n)
}

func flag(b bool) int {
	if b {
		return 1
	}
	return 0
}

// toInt64 converts an integer of a Redis reply.
func toInt64(v any) (int64, error) {
	switch x := v.(type) {
	case int64:
		return x, nil
	case string:
		return strconv.ParseInt(x, 10, 64)
	case nil:
		return 0, errors.New("urlauth: nil reply")
	}
	return 0, fmt.Errorf("urlauth: unexpected reply %T", v)
}
