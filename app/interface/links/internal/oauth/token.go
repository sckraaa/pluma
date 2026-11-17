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
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/teamgram/teamgram-server/pkg/urlauth"

	"github.com/zeromicro/go-zero/core/logx"
)

// IDTokenTTL is the life of an ID token (task 106, «Пределы и ошибки»).
const IDTokenTTL = 10 * time.Minute

// maxTokenBody bounds the form of a token request.
const maxTokenBody = 16 << 10

// idClaims are the claims of an ID token (OpenID Connect Core 1.0, 2 and
// 5.1; task 106, «Провайдер входа»): the user's id is sub, the bot's id
// (the client_id) aud. The profile goes with the scope profile, the phone
// number with the scope phone and the user's consent in the app.
type idClaims struct {
	Iss      string `json:"iss"`
	Aud      string `json:"aud"`
	Sub      string `json:"sub"`
	Iat      int64  `json:"iat"`
	Exp      int64  `json:"exp"`
	AuthTime int64  `json:"auth_time"`
	Nonce    string `json:"nonce,omitempty"`

	Id                int64  `json:"id,omitempty"`
	Name              string `json:"name,omitempty"`
	GivenName         string `json:"given_name,omitempty"`
	FamilyName        string `json:"family_name,omitempty"`
	PreferredUsername string `json:"preferred_username,omitempty"`
	Picture           string `json:"picture,omitempty"`

	PhoneNumber         string `json:"phone_number,omitempty"`
	PhoneNumberVerified *bool  `json:"phone_number_verified,omitempty"`
}

// tokenError is an error of the token endpoint (RFC 6749 5.2).
type tokenError struct {
	status int
	code   string
	desc   string
}

func (e *tokenError) Error() string { return e.code + ": " + e.desc }

func invalidRequest(desc string) *tokenError {
	return &tokenError{http.StatusBadRequest, "invalid_request", desc}
}

func invalidGrant(desc string) *tokenError {
	return &tokenError{http.StatusBadRequest, "invalid_grant", desc}
}

// token is POST /token, the token endpoint (RFC 6749 4.1.3, 5; OpenID
// Connect Core 1.0, 3.1.3): the site's server exchanges an authorization
// code for an ID token. The client authenticates with client_secret_basic
// or client_secret_post; code_verifier is required when the request had a
// code challenge (RFC 7636 4.5). The answer has no access token: the
// provider has nothing for one to reach (no UserInfo, as Telegram's has
// none, core.telegram.org/bots/telegram-login).
func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !p.limits.allow(limitToken, visitorIP(r), p.now()) {
		w.Header().Set("Retry-After", "1")
		writeTokenError(w, &tokenError{http.StatusTooManyRequests, "slow_down", "too many requests"}, false)
		return
	}
	reply, basic, err := p.exchange(r)
	var te *tokenError
	switch {
	case errors.As(err, &te):
		writeTokenError(w, te, basic)
		return
	case err != nil:
		logx.WithContext(r.Context()).Errorf("oauth: exchange a code: %v", err)
		writeTokenError(w, &tokenError{http.StatusServiceUnavailable, "temporarily_unavailable", "try again later"}, false)
		return
	}
	b, err := json.Marshal(reply)
	if err != nil {
		writeTokenError(w, &tokenError{http.StatusServiceUnavailable, "temporarily_unavailable", "try again later"}, false)
		return
	}
	noStore(w)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

// tokenReply is the answer of the token endpoint.
type tokenReply struct {
	IdToken   string `json:"id_token"`
	TokenType string `json:"token_type"`
	ExpiresIn int64  `json:"expires_in"`
}

// exchange checks a token request and makes its ID token; basic tells
// whether the client authenticated with HTTP Basic.
func (p *Provider) exchange(r *http.Request) (*tokenReply, bool, error) {
	ctx := r.Context()
	r.Body = http.MaxBytesReader(nil, r.Body, maxTokenBody)
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(strings.ToLower(ct), "application/x-www-form-urlencoded") {
		return nil, false, invalidRequest("the request must be application/x-www-form-urlencoded")
	}
	if err := r.ParseForm(); err != nil {
		return nil, false, invalidRequest("the form cannot be read")
	}
	form := r.PostForm
	for name, v := range form {
		if len(v) > 1 {
			return nil, false, invalidRequest(name + " is given more than once")
		}
	}
	clientId, secret, basic, err := clientCredentials(r, form)
	if err != nil {
		return nil, basic, err
	}
	if form.Get("grant_type") != "authorization_code" {
		return nil, basic, &tokenError{http.StatusBadRequest, "unsupported_grant_type", "only authorization_code is supported"}
	}
	botId, err := strconv.ParseInt(clientId, 10, 64)
	if err != nil || botId <= 0 {
		return nil, basic, invalidClient()
	}
	ok, err := p.Bots.CheckLoginClientSecret(ctx, botId, secret)
	if err != nil {
		return nil, basic, err
	}
	if !ok {
		return nil, basic, invalidClient()
	}

	code := form.Get("code")
	token, ok := tokenOfCode(code)
	if !ok {
		return nil, basic, invalidGrant("the code is invalid or expired")
	}
	e, err := p.Requests.Get(ctx, token)
	if errors.Is(err, urlauth.ErrExpired) || errors.Is(err, urlauth.ErrInvalid) {
		return nil, basic, invalidGrant("the code is invalid or expired")
	}
	if err != nil {
		return nil, basic, err
	}
	// The code must be the client's, sent with the redirect_uri it was
	// issued at (RFC 6749 4.1.3), and with the verifier of its challenge.
	if e.Kind != urlauth.KindOIDC || e.BotId != botId || e.Status != urlauth.StatusAccepted {
		return nil, basic, invalidGrant("the code is invalid or expired")
	}
	if form.Get("redirect_uri") != e.RedirectURI {
		return nil, basic, invalidGrant("redirect_uri is not the one of the authorization request")
	}
	if err := checkVerifier(e.CodeChallenge, form.Get("code_verifier")); err != nil {
		return nil, basic, err
	}
	redeemed, err := p.redeemCode(ctx, token, code)
	if err != nil {
		return nil, basic, err
	}
	if !redeemed {
		return nil, basic, invalidGrant("the code is invalid or expired")
	}
	idToken, err := p.idToken(ctx, e)
	if err != nil {
		return nil, basic, err
	}
	return &tokenReply{IdToken: idToken, TokenType: "Bearer", ExpiresIn: int64(IDTokenTTL / time.Second)}, basic, nil
}

func invalidClient() *tokenError {
	return &tokenError{http.StatusUnauthorized, "invalid_client", "client authentication failed"}
}

// clientCredentials are the client_id and client_secret of a token request:
// HTTP Basic, the form-urlencoded id and secret (RFC 6749 2.3.1), or the
// form's client_id and client_secret; never both ("The client MUST NOT use
// more than one authentication method in each request").
func clientCredentials(r *http.Request, form url.Values) (id, secret string, basic bool, err error) {
	user, pass, basic := r.BasicAuth()
	if basic {
		if form.Has("client_secret") {
			return "", "", true, invalidRequest("more than one client authentication method")
		}
		if id, err = url.QueryUnescape(user); err != nil {
			return "", "", true, invalidClient()
		}
		if secret, err = url.QueryUnescape(pass); err != nil {
			return "", "", true, invalidClient()
		}
		if fid := form.Get("client_id"); fid != "" && fid != id {
			return "", "", true, invalidRequest("client_id differs from the authenticated client")
		}
		return id, secret, true, nil
	}
	id, secret = form.Get("client_id"), form.Get("client_secret")
	if id == "" || secret == "" {
		return "", "", false, invalidClient()
	}
	return id, secret, false, nil
}

// checkVerifier checks the code_verifier of a code whose request had
// challenge (RFC 7636 4.6): BASE64URL(SHA256(verifier)) == challenge. A
// verifier with no challenge is refused too: a site that sent one expected
// its request to carry a challenge.
func checkVerifier(challenge, verifier string) error {
	if challenge == "" {
		if verifier != "" {
			return invalidGrant("code_verifier without a code challenge")
		}
		return nil
	}
	if !validVerifier(verifier) {
		return invalidGrant("code_verifier is missing or malformed")
	}
	sum := sha256.Sum256([]byte(verifier))
	if subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(challenge)) != 1 {
		return invalidGrant("code_verifier does not match the code challenge")
	}
	return nil
}

// validVerifier: 43 to 128 unreserved characters (RFC 7636 4.1).
func validVerifier(s string) bool {
	if len(s) < 43 || len(s) > 128 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if unreserved := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_' || c == '~'; !unreserved {
			return false
		}
	}
	return true
}

// idToken signs the ID token of the accepted request e.
func (p *Provider) idToken(ctx context.Context, e *urlauth.Entry) (string, error) {
	u, err := p.Accounts.Account(ctx, e.UserId)
	if err != nil {
		return "", err
	}
	if u == nil || u.Bot {
		return "", invalidGrant("the user is no longer there")
	}
	now := p.now()
	c := idClaims{
		Iss:      p.Issuer,
		Aud:      strconv.FormatInt(e.BotId, 10),
		Sub:      strconv.FormatInt(u.Id, 10),
		Iat:      now.Unix(),
		Exp:      now.Add(IDTokenTTL).Unix(),
		AuthTime: e.AcceptedAt,
		Nonce:    e.Nonce,
	}
	if slices.Contains(e.Scopes, urlauth.ScopeProfile) {
		c.Id = u.Id
		c.GivenName, c.FamilyName = u.FirstName, u.LastName
		c.Name = strings.TrimSpace(u.FirstName + " " + u.LastName)
		c.PreferredUsername = u.Username
		if u.Username != "" && u.PhotoId != 0 {
			c.Picture = p.Site + "/_l/avatar/" + url.PathEscape(u.Username) + "?v=" + strconv.FormatInt(u.PhotoId, 10)
		}
	}
	if e.SharePhone && e.WantsPhone() && u.Phone != "" {
		verified := true
		c.PhoneNumber = "+" + strings.TrimPrefix(u.Phone, "+")
		c.PhoneNumberVerified = &verified
	}
	return p.Keys.Sign(&c)
}

func noStore(w http.ResponseWriter) {
	// RFC 6749 5.1: "Cache-Control: no-store", "Pragma: no-cache".
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
}

func writeTokenError(w http.ResponseWriter, e *tokenError, basic bool) {
	noStore(w)
	if e.status == http.StatusUnauthorized && basic {
		// RFC 6749 5.2: with HTTP Basic the server answers 401 with the
		// scheme the client used.
		w.Header().Set("WWW-Authenticate", `Basic realm="oauth"`)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.status)
	// A map of strings always marshals.
	b, _ := json.Marshal(map[string]string{"error": e.code, "error_description": e.desc})
	_, _ = w.Write(b)
}
