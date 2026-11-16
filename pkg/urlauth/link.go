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

// Package urlauth is the login of a user on a site through oauth.pluma.chat
// (task 106, ADR 0068 p. 6; core.telegram.org/api/url-authorization): the
// links that carry a login request to the apps, the request itself while
// the site waits for the user (Redis, oauth_req), the match codes that tie
// the app to the browser, the browser and platform of the site's visitor,
// and the address a site gets the data of a login at.
//
// The provider in the links service (oauth.pluma.chat) makes a request and
// reads its outcome; bff answers messages.requestUrlAuth, acceptUrlAuth,
// declineUrlAuth and checkUrlAuthMatchCode for it.
package urlauth

import (
	"net/url"
	"strings"

	"github.com/teamgram/teamgram-server/pkg/env2"
)

// OAuthPath is the first path segment, and the username, of the links of a
// login request: https://pluma.chat/oauth?startapp=<token>.
const OAuthPath = "oauth"

// Token lengths: NewToken makes 32 characters; a token from a link is
// checked against these bounds before it names a Redis key.
const (
	tokenBytes    = 24
	minTokenChars = 16
	maxTokenChars = 64
)

// linkSchemes are the schemes of the app links of a login request: tg, the
// scheme of the official clients, and plm, the scheme of Pluma's clients
// (ADR 0036).
var linkSchemes = map[string]bool{"tg": true, "plm": true}

// OAuthToken returns the token of a link to a login request, in any of the
// forms the clients pass it to messages.requestUrlAuth (task 106, contract
// of the clients):
//
//   - https://pluma.chat/oauth?startapp=<token> (or t.me) as it came: Android
//     sends the address it was opened with (ui/LinkManager.java:158,
//     1340-1351);
//   - tg://resolve?domain=oauth&startapp=<token>: tdesktop rewrites the link
//     to it (core/local_url_handlers.cpp:707-717), iOS takes it
//     (TelegramUI/Sources/OpenUrl.swift:45-67);
//   - tg://oauth?token=<token>: tdesktop (core/local_url_handlers.cpp:1870-1882)
//     and iOS.
//
// The scheme may be tg or plm, the host of an https (or http) link any host
// of the server's links (env2.IsTDotMe). The token goes in startapp or in
// token for the oauth forms.
func OAuthToken(link string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || u.User != nil {
		return "", false
	}
	q := u.Query()
	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	var token string
	switch {
	case scheme == "https" || scheme == "http":
		if !env2.IsTDotMe(host) || u.Port() != "" || !strings.EqualFold(strings.Trim(u.Path, "/"), OAuthPath) {
			return "", false
		}
		token = firstOf(q, "startapp", "token")
	case linkSchemes[scheme]:
		if u.Path != "" && u.Path != "/" {
			return "", false
		}
		switch host {
		case OAuthPath:
			token = firstOf(q, "token", "startapp")
		case "resolve":
			if !strings.EqualFold(q.Get("domain"), OAuthPath) {
				return "", false
			}
			token = q.Get("startapp")
		}
	}
	if !ValidToken(token) {
		return "", false
	}
	return token, true
}

func firstOf(q url.Values, names ...string) string {
	for _, n := range names {
		if v := q.Get(n); v != "" {
			return v
		}
	}
	return ""
}

// ValidToken tells whether s may be the token of a login request: the
// characters of base64url (those of startapp, core.telegram.org/api/links)
// and a bounded length.
func ValidToken(s string) bool {
	if len(s) < minTokenChars || len(s) > maxTokenChars {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

// AppLink is the link the page of a login request shows as a QR code and
// behind its button, https://<host>/oauth?startapp=<token> (ADR 0068 p. 6):
// a phone camera opens it in the app, and OAuthToken reads it back.
func AppLink(host, token string) string {
	return (&url.URL{Scheme: "https", Host: host, Path: "/" + OAuthPath, RawQuery: url.Values{"startapp": {token}}.Encode()}).String()
}
