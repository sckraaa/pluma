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

// Package links serves the links of the link host (pluma.chat/<...>) in a
// browser as t.me serves Telegram's (task 026): a page of the object with
// a button and an automatic hand-off to the app by its plm:// link, the
// paths and parameters of core.telegram.org/api/links, the pages of t.me
// as observed (docs/tasks/026-research/tme.md).
package links

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"

	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
)

// Kind is the kind of a link.
type Kind int

const (
	// KindNone is no link: t.me sends such paths to telegram.org, this
	// server to the site's home page.
	KindNone Kind = iota
	KindPeer
	KindPost
	KindInvite
	KindPhone
	KindPrivatePost
	KindPrivateBoost
	KindBoost
	KindStickers
	KindEmoji
	KindShare
	KindProxy
	KindSocks
	KindLanguage
	KindTheme
	KindBackground
	KindFolder
	KindLogin
	KindContact
	KindChannelPreview
)

// Link is a parsed link: its kind, what it names (a username, a hash, a set
// name, a slug) and the app link it hands off to, without the scheme
// ("resolve?domain=x&post=1").
type Link struct {
	Kind    Kind
	Subject string
	Post    int
	App     string
	// Before, After and Query: the channel preview's page
	// (/s/<name>?before=, ?after=, ?q=).
	Before, After int
	Query         string
}

var (
	usernameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{4,31}$`) // user.MinUsernameLen to MaxUsernameLen
	hashRe     = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	slugRe     = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	digitsRe   = regexp.MustCompile(`^[0-9]{3,15}$`)
	secretRe   = regexp.MustCompile(`^(?:dd|ee)?[0-9a-fA-F]{32}(?:[0-9a-fA-F]*)$|^[A-Za-z0-9_-]{22,}$`)
)

// peerParams are the parameters of a username link the app link carries
// (core.telegram.org/api/links: bot, chat and message links).
var peerParams = []string{
	"start", "startgroup", "startchannel", "admin", "game", "startapp", "appname",
	"text", "profile", "direct", "voicechat", "videochat", "livestream", "boost", "ref",
	"attach", "choose", "thread", "comment", "t", "single", "story", "album", "collection",
}

// Parse parses the path and query of a link of the link host.
func Parse(path string, q url.Values) Link {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		return Link{}
	}
	head := parts[0]
	switch {
	case strings.HasPrefix(head, "+"):
		rest := head[1:]
		if digitsRe.MatchString(rest) {
			return Link{Kind: KindPhone, Subject: rest, App: "resolve?phone=" + rest}
		}
		return invite(rest)
	case head == "joinchat" && len(parts) > 1:
		return invite(parts[1])
	case head == "c" && len(parts) > 1:
		return privateLink(parts[1:], q)
	case head == "s" && len(parts) > 1 && usernameRe.MatchString(parts[1]):
		l := Link{Kind: KindChannelPreview, Subject: parts[1], App: "resolve?domain=" + parts[1]}
		if len(parts) > 2 {
			l.Post, _ = strconv.Atoi(parts[2])
		}
		l.Before, _ = strconv.Atoi(q.Get("before"))
		l.After, _ = strconv.Atoi(q.Get("after"))
		l.Query = q.Get("q")
		return l
	case head == "boost":
		if len(parts) > 1 && usernameRe.MatchString(parts[1]) {
			return Link{Kind: KindBoost, Subject: parts[1], App: "boost?domain=" + parts[1]}
		}
		if c := q.Get("c"); digitsOnly(c) {
			return Link{Kind: KindPrivateBoost, Subject: c, App: "boost?channel=" + c}
		}
		return Link{}
	case head == "addstickers" || head == "addemoji":
		if len(parts) < 2 || !slugRe.MatchString(parts[1]) {
			return Link{}
		}
		if head == "addemoji" {
			return Link{Kind: KindEmoji, Subject: parts[1], App: "addemoji?set=" + parts[1]}
		}
		return Link{Kind: KindStickers, Subject: parts[1], App: "addstickers?set=" + parts[1]}
	case head == "share":
		u := q.Get("url")
		if u == "" || (len(parts) > 1 && parts[1] != "url") {
			return Link{}
		}
		return Link{Kind: KindShare, Subject: u, App: "msg_url?" + encode(q, "url", "text")}
	case head == "proxy" || head == "socks":
		if q.Get("server") == "" || !digitsOnly(q.Get("port")) {
			return Link{}
		}
		if head == "socks" {
			return Link{Kind: KindSocks, Subject: q.Get("server"), App: "socks?" + encode(q, "server", "port", "user", "pass")}
		}
		if !secretRe.MatchString(q.Get("secret")) {
			return Link{}
		}
		return Link{Kind: KindProxy, Subject: q.Get("server"), App: "proxy?" + encode(q, "server", "port", "secret")}
	case head == "setlanguage" && len(parts) > 1 && slugRe.MatchString(parts[1]):
		return Link{Kind: KindLanguage, Subject: parts[1], App: "setlanguage?lang=" + parts[1]}
	case head == "addtheme" && len(parts) > 1 && slugRe.MatchString(parts[1]):
		return Link{Kind: KindTheme, Subject: parts[1], App: "addtheme?slug=" + parts[1]}
	case head == "addlist" && len(parts) > 1 && slugRe.MatchString(parts[1]):
		return Link{Kind: KindFolder, Subject: parts[1], App: "addlist?slug=" + parts[1]}
	case head == "login" && len(parts) > 1 && digitsOnly(parts[1]):
		return Link{Kind: KindLogin, Subject: parts[1], App: "login?code=" + parts[1]}
	case head == "contact" && len(parts) > 1 && slugRe.MatchString(parts[1]):
		return Link{Kind: KindContact, Subject: parts[1], App: "contact?token=" + parts[1]}
	case head == "bg" && len(parts) > 1:
		return background(parts[1], q)
	case usernameRe.MatchString(head) && !userpb.ReservedUsername(head):
		return peerLink(head, parts[1:], q)
	}
	return Link{}
}

func invite(hash string) Link {
	if !hashRe.MatchString(hash) {
		return Link{}
	}
	return Link{Kind: KindInvite, Subject: hash, App: "join?invite=" + hash}
}

// peerLink: /<username>, /<username>/<post>, /<username>/<thread>/<post>,
// /<username>/s/<story>, /<username>/a/<album>, /<username>/c/<collection>,
// /<bot>/<app>.
func peerLink(name string, rest []string, q url.Values) Link {
	app := url.Values{"domain": {name}}
	l := Link{Kind: KindPeer, Subject: name}
	switch {
	case len(rest) >= 2 && (rest[0] == "s" || rest[0] == "a" || rest[0] == "c"):
		key := map[string]string{"s": "story", "a": "album", "c": "collection"}[rest[0]]
		app.Set(key, rest[1])
	case len(rest) >= 2 && digitsOnly(rest[0]) && digitsOnly(rest[1]):
		l.Kind, l.Post = KindPost, atoi(rest[1])
		app.Set("post", rest[1])
		app.Set("thread", rest[0])
	case len(rest) >= 1 && digitsOnly(rest[0]):
		l.Kind, l.Post = KindPost, atoi(rest[0])
		app.Set("post", rest[0])
	case len(rest) >= 1 && slugRe.MatchString(rest[0]) && rest[0] != "":
		app.Set("appname", rest[0])
	}
	for _, k := range peerParams {
		if v, ok := q[k]; ok && k != "single" {
			app.Set(k, v[0])
		} else if ok {
			app.Set(k, "")
		}
	}
	l.App = "resolve?" + orderedEncode(app, append([]string{"domain", "post", "thread", "story", "album", "collection", "appname"}, peerParams...))
	return l
}

// privateLink: /c/<channel>/<post>, /c/<channel>/<thread>/<post>,
// /c/<channel>?boost.
func privateLink(rest []string, q url.Values) Link {
	if !digitsOnly(rest[0]) {
		return Link{}
	}
	ch := rest[0]
	switch {
	case len(rest) >= 3 && digitsOnly(rest[1]) && digitsOnly(rest[2]):
		return Link{Kind: KindPrivatePost, Subject: ch, Post: atoi(rest[2]), App: "privatepost?channel=" + ch + "&post=" + rest[2] + "&thread=" + rest[1]}
	case len(rest) >= 2 && digitsOnly(rest[1]):
		app := "privatepost?channel=" + ch + "&post=" + rest[1]
		for _, k := range []string{"thread", "comment", "t", "single"} {
			if v, ok := q[k]; ok {
				app += "&" + k + "=" + url.QueryEscape(v[0])
			}
		}
		return Link{Kind: KindPrivatePost, Subject: ch, Post: atoi(rest[1]), App: app}
	case len(rest) == 1 && q.Has("boost"):
		return Link{Kind: KindPrivateBoost, Subject: ch, App: "boost?channel=" + ch}
	}
	return Link{}
}

// background: /bg/<slug>, /bg/<color>, /bg/<color1>-<color2>[?rotation=].
func background(v string, q url.Values) Link {
	colorRe := regexp.MustCompile(`^[0-9a-fA-F]{6}$`)
	gradientRe := regexp.MustCompile(`^[0-9a-fA-F]{6}(?:[-~][0-9a-fA-F]{6}){1,3}$`)
	var app string
	switch {
	case colorRe.MatchString(v):
		app = "bg?color=" + v
	case gradientRe.MatchString(v):
		app = "bg?gradient=" + v
		if r := q.Get("rotation"); digitsOnly(r) {
			app += "&rotation=" + r
		}
	case slugRe.MatchString(v):
		app = "bg?slug=" + v + optional(q, "mode", "bg_color", "intensity")
	default:
		return Link{}
	}
	return Link{Kind: KindBackground, Subject: v, App: app}
}

func optional(q url.Values, keys ...string) string {
	s := ""
	for _, k := range keys {
		if v := q.Get(k); v != "" {
			s += "&" + k + "=" + url.QueryEscape(v)
		}
	}
	return s
}

// encode is the query of the keys of q present, in their order.
func encode(q url.Values, keys ...string) string {
	return orderedEncode(q, keys)
}

func orderedEncode(q url.Values, keys []string) string {
	var b strings.Builder
	seen := map[string]bool{}
	for _, k := range keys {
		v, ok := q[k]
		if !ok || seen[k] {
			continue
		}
		seen[k] = true
		if b.Len() > 0 {
			b.WriteByte('&')
		}
		b.WriteString(url.QueryEscape(k))
		if len(v) > 0 && v[0] != "" {
			b.WriteByte('=')
			b.WriteString(url.QueryEscape(v[0]))
		}
	}
	return b.String()
}

func digitsOnly(s string) bool {
	if s == "" || len(s) > 19 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
