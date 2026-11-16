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
	"net/url"
	"testing"
)

const testToken = "AbCdEfGhIjKlMnOpQrStUvWxYz012-_9"

// Every form the clients send a login request in gives its token.
func TestOAuthTokenForms(t *testing.T) {
	for _, link := range []string{
		"https://pluma.chat/oauth?startapp=" + testToken,
		"https://pluma.chat/oauth/?startapp=" + testToken,
		"https://PLUMA.chat/OAuth?startapp=" + testToken,
		"http://pluma.chat/oauth?startapp=" + testToken,
		"https://t.me/oauth?startapp=" + testToken,
		"https://pluma.chat/oauth?token=" + testToken,
		"tg://resolve?domain=oauth&startapp=" + testToken,
		"tg://resolve?startapp=" + testToken + "&domain=OAUTH",
		"tg://oauth?token=" + testToken,
		"tg://oauth?startapp=" + testToken,
		"plm://oauth?token=" + testToken,
		"plm://resolve?domain=oauth&startapp=" + testToken,
		"  https://pluma.chat/oauth?startapp=" + testToken + "  ",
	} {
		got, ok := OAuthToken(link)
		if !ok || got != testToken {
			t.Errorf("%q: %q, %v", link, got, ok)
		}
	}
}

// Anything else is no login request: the bff answers URL_INVALID or takes
// it for a link of url_auth_domains.
func TestOAuthTokenRejects(t *testing.T) {
	for _, link := range []string{
		"",
		"oauth",
		"https://site.example/oauth?startapp=" + testToken,
		"https://pluma.chat.evil.example/oauth?startapp=" + testToken,
		"https://pluma.chat:8443/oauth?startapp=" + testToken,
		"https://user@pluma.chat/oauth?startapp=" + testToken,
		"https://pluma.chat/oauthx?startapp=" + testToken,
		"https://pluma.chat/somebot/oauth?startapp=" + testToken,
		"https://pluma.chat/oauth",
		"https://pluma.chat/oauth?startapp=short",
		"https://pluma.chat/oauth?startapp=" + testToken + "%2F",
		"https://pluma.chat/resolve?domain=oauth&startapp=" + testToken,
		"ftp://pluma.chat/oauth?startapp=" + testToken,
		"tg://resolve?domain=durov&startapp=" + testToken,
		"tg://resolve?domain=oauth&token=" + testToken,
		"tg://oauth/x?token=" + testToken,
		"tg://passport?token=" + testToken,
		"evil://oauth?token=" + testToken,
		"https://pluma.chat/oauth?startapp=" + testToken + testToken + testToken,
	} {
		if got, ok := OAuthToken(link); ok {
			t.Errorf("%q: token %q", link, got)
		}
	}
}

// The link of the page reads back to its token.
func TestAppLinkRoundTrip(t *testing.T) {
	token, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 32 || !ValidToken(token) {
		t.Fatalf("token %q", token)
	}
	link := AppLink("pluma.chat", token)
	if link != "https://pluma.chat/oauth?startapp="+token {
		t.Fatalf("link %q", link)
	}
	if got, ok := OAuthToken(link); !ok || got != token {
		t.Fatalf("%q: %q, %v", link, got, ok)
	}
}

// The fields of a login go after the site's own query, replace the fields of
// the same names there, and keep the fragment.
func TestLoginLink(t *testing.T) {
	fields := url.Values{"id": {"10"}, "auth_date": {"1700000000"}, "hash": {"ab"}}
	for _, c := range []struct{ link, want string }{
		{"https://site.example/login", "https://site.example/login?auth_date=1700000000&hash=ab&id=10"},
		{"https://site.example/login?next=%2Fhome&x=1", "https://site.example/login?next=%2Fhome&x=1&auth_date=1700000000&hash=ab&id=10"},
		{"https://site.example/login?id=7&hash=zz&keep=1#top", "https://site.example/login?keep=1&auth_date=1700000000&hash=ab&id=10#top"},
		{"https://site.example/login?", "https://site.example/login?auth_date=1700000000&hash=ab&id=10"},
	} {
		got, err := LoginLink(c.link, fields)
		if err != nil || got != c.want {
			t.Errorf("%s: %q, %v, want %q", c.link, got, err, c.want)
		}
	}
}
