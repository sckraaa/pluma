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

package links

import (
	"net/url"
	"testing"
)

// TestParse: the paths of core.telegram.org/api/links and their app
// links, as t.me hands them off (docs/tasks/026-research/tme.md, the table).
func TestParse(t *testing.T) {
	for _, c := range []struct {
		link string
		kind Kind
		app  string
	}{
		{"/durov", KindPeer, "resolve?domain=durov"},
		{"/durov/", KindPeer, "resolve?domain=durov"},
		{"/telegram/1", KindPost, "resolve?domain=telegram&post=1"},
		{"/tdlibchat/1/2", KindPost, "resolve?domain=tdlibchat&post=2&thread=1"},
		{"/telegram/1?comment=5", KindPost, "resolve?domain=telegram&post=1&comment=5"},
		{"/telegram/1?single", KindPost, "resolve?domain=telegram&post=1&single"},
		{"/BotFather?start=abc", KindPeer, "resolve?domain=BotFather&start=abc"},
		{"/BotFather/someapp?startapp=x", KindPeer, "resolve?domain=BotFather&appname=someapp&startapp=x"},
		{"/levlam?text=hello%20you", KindPeer, "resolve?domain=levlam&text=hello+you"},
		{"/telegram/s/1", KindPeer, "resolve?domain=telegram&story=1"},
		{"/+AAAAAAAAAAAAAAAA", KindInvite, "join?invite=AAAAAAAAAAAAAAAA"},
		{"/joinchat/AAAAAAAAAAAAAAAA", KindInvite, "join?invite=AAAAAAAAAAAAAAAA"},
		{"/+79991234567", KindPhone, "resolve?phone=79991234567"},
		{"/c/1234/5", KindPrivatePost, "privatepost?channel=1234&post=5"},
		{"/c/1234/3/5", KindPrivatePost, "privatepost?channel=1234&post=5&thread=3"},
		{"/c/1234?boost", KindPrivateBoost, "boost?channel=1234"},
		{"/boost?c=1234", KindPrivateBoost, "boost?channel=1234"},
		{"/boost/telegram", KindBoost, "boost?domain=telegram"},
		{"/addstickers/Animals", KindStickers, "addstickers?set=Animals"},
		{"/addemoji/RestrictedEmoji", KindEmoji, "addemoji?set=RestrictedEmoji"},
		{"/share/url?url=https%3A%2F%2Fexample.com&text=hi", KindShare, "msg_url?url=https%3A%2F%2Fexample.com&text=hi"},
		{"/share?url=https%3A%2F%2Fexample.com", KindShare, "msg_url?url=https%3A%2F%2Fexample.com"},
		{"/proxy?server=1.2.3.4&port=443&secret=dd0123456789abcdef0123456789abcdef", KindProxy, "proxy?server=1.2.3.4&port=443&secret=dd0123456789abcdef0123456789abcdef"},
		{"/socks?server=1.2.3.4&port=1080", KindSocks, "socks?server=1.2.3.4&port=1080"},
		{"/setlanguage/ru", KindLanguage, "setlanguage?lang=ru"},
		{"/addtheme/day", KindTheme, "addtheme?slug=day"},
		{"/bg/ff0000-00ff00?rotation=45", KindBackground, "bg?gradient=ff0000-00ff00&rotation=45"},
		{"/bg/aaaaaa", KindBackground, "bg?color=aaaaaa"},
		{"/addlist/abcdef", KindFolder, "addlist?slug=abcdef"},
		{"/login/12345", KindLogin, "login?code=12345"},
		{"/contact/abcdef", KindContact, "contact?token=abcdef"},
		{"/s/telegram", KindChannelPreview, "resolve?domain=telegram"},
	} {
		u, err := url.Parse(c.link)
		if err != nil {
			t.Fatal(err)
		}
		l := Parse(u.Path, u.Query())
		if l.Kind != c.kind || l.App != c.app {
			t.Errorf("%s: kind %d, app %q; want %d, %q", c.link, l.Kind, l.App, c.kind, c.app)
		}
	}
	// What t.me sends to telegram.org: here, the site's home.
	for _, link := range []string{
		"/", "/abc", "/abc!def", "/c/1234", "/addstickers/", "/proxy?server=1.2.3.4&port=443&secret=00",
		"/share/zzz?url=x", "/setlanguage", "/msg/url?url=x", "/giftcode/aaaa", "/$aaaa",
	} {
		u, _ := url.Parse(link)
		if l := Parse(u.Path, u.Query()); l.Kind != KindNone {
			t.Errorf("%s: kind %d, want none", link, l.Kind)
		}
	}
}
