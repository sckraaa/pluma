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
	"context"
	"html/template"
	"net/url"
	"strconv"
	"strings"
)

// PeerKind is what a username names.
type PeerKind int

const (
	PeerUser PeerKind = iota + 1
	PeerBot
	PeerChannel
	PeerGroup
)

// Peer is what anybody may see of the owner of a username: its privacy
// for everybody applied (Resolver).
type Peer struct {
	Kind      PeerKind
	Username  string
	Name      string // the full name, or the title
	FirstName string
	About     string
	Members   int
	HasPhoto  bool
	// Public: a channel whose posts anybody may read (the preview).
	Public bool
}

// Resolver gives the data of the pages. A missing object is (nil, nil).
type Resolver interface {
	Peer(ctx context.Context, username string) (*Peer, error)
	// StickerSet is the title of the set of the short name, and whether it
	// is a set of emoji.
	StickerSet(ctx context.Context, name string) (title string, emoji bool, found bool, err error)
}

// Page is what the template shows.
type Page struct {
	Lang     string
	Title    string // <title>
	OgTitle  string
	OgDesc   string
	OgImage  string // absolute URL
	NoIndex  bool
	PhotoURL string // the avatar, on this site; "" for an icon
	Icon     string // user, group, stickers, share, ...
	Initials string // the avatar of a peer without a photo
	Name     string
	Extra    []string
	DescHead string
	Desc     template.HTML
	Button   string
	AppURL   template.URL // plm://… or plm:… (Android)
	WebURL   string       // the web client at the object
	Context  *Anchor      // "Preview channel" / "View context"
	Addition template.HTML
	// Download leads to the site's page of the visitor's device.
	Download string
	Texts    map[string]string
}

// Anchor is a link of the page.
type Anchor struct {
	Text, Href string
}

// Builder builds the pages of links.
type Builder struct {
	// Site is the link host's site, https://pluma.chat.
	Site string
	// Web is the web client, https://web.pluma.chat.
	Web string
	// Scheme is the app's scheme, plm.
	Scheme string
	Data   Resolver
}

// Visitor is what the page depends on of the visitor.
type Visitor struct {
	Lang    string // en or ru
	Android bool   // plm:path, as t.me gives Android tg:path
	Device  string // the site's page of the device: iphone, android, mac, windows, linux
}

func esc(s string) string { return template.HTMLEscapeString(s) }

// html is the text of key in lang with escaped args, as HTML.
func (v Visitor) html(key string, args ...string) template.HTML {
	escaped := make([]string, len(args))
	for i, a := range args {
		escaped[i] = esc(a)
	}
	return template.HTML(text(v.Lang, key, escaped...)) //nolint:gosec // our own texts, the arguments escaped
}

func (v Visitor) text(key string, args ...string) string { return text(v.Lang, key, args...) }

// appURL is the app link of path (resolve?domain=x).
func (b *Builder) appURL(v Visitor, path string) template.URL {
	if v.Android {
		return template.URL(b.Scheme + ":" + path) //nolint:gosec // built of parsed, escaped parts
	}
	return template.URL(b.Scheme + "://" + path) //nolint:gosec // built of parsed, escaped parts
}

// webURL opens the object in the web client (Web K's tgaddr).
func (b *Builder) webURL(path string) string {
	return b.Web + "/#?tgaddr=" + url.QueryEscape(b.Scheme+"://"+path)
}

// Build builds the page of l, or nil for KindNone and the channel preview
// (Preview).
func (b *Builder) Build(ctx context.Context, l Link, v Visitor) (*Page, error) {
	p := &Page{
		Lang:     v.Lang,
		OgImage:  b.Site + "/og.png",
		AppURL:   b.appURL(v, l.App),
		WebURL:   b.webURL(l.App),
		Download: b.Site + "/" + v.Device,
		Texts:    texts[v.Lang],
	}
	if v.Device == "" {
		p.Download = b.Site + "/"
	}
	p.OgDesc = v.text("default_desc")
	switch l.Kind {
	case KindPeer, KindPost, KindBoost:
		return b.peerPage(ctx, l, v, p)
	case KindInvite:
		p.Title, p.OgTitle = v.text("join_title"), v.text("join_og")
		p.Icon, p.Desc, p.Button = "group", v.html("join_desc"), v.text("join_button")
	case KindPhone:
		phone := formatPhone(l.Subject)
		p.Title, p.OgTitle = v.text("phone_title"), v.text("phone_title")
		p.Icon, p.Name, p.Button = "user", v.text("phone_heading", phone), v.text("open_chat")
		p.Addition = v.html("if_contact", phone)
		p.NoIndex = true
	case KindPrivatePost:
		p.Title, p.OgTitle = v.text("private_title"), v.text("private_title")
		p.Icon, p.Desc, p.Button, p.NoIndex = "group", v.html("private_post_desc"), v.text("open_message"), true
	case KindPrivateBoost:
		p.Title, p.OgTitle = v.text("private_title"), v.text("private_title")
		p.Icon, p.Desc, p.Button, p.NoIndex = "group", v.html("private_boost"), v.text("boost_button"), true
	case KindStickers, KindEmoji:
		title, emoji, found, err := b.Data.StickerSet(ctx, l.Subject)
		if err != nil {
			return nil, err
		}
		if found && emoji && l.Kind == KindStickers {
			l.Kind = KindEmoji
		}
		p.Icon = "stickers"
		if l.Kind == KindEmoji {
			p.Title, p.Button = v.text("emoji_title"), v.text("emoji_button")
			p.AppURL, p.WebURL = b.appURL(v, "addemoji?set="+l.Subject), b.webURL("addemoji?set="+l.Subject)
		} else {
			p.Title, p.Button = v.text("stickers_title"), v.text("stickers_button")
		}
		if !found {
			title = v.text("stickers_none")
			p.OgTitle = v.text("stickers_og_none")
		} else if l.Kind == KindEmoji {
			p.OgTitle = v.text("emoji_og", title)
		} else {
			p.OgTitle = v.text("stickers_og", title)
		}
		if l.Kind == KindEmoji {
			p.Desc = v.html("emoji_desc", title)
		} else {
			p.Desc = v.html("stickers_desc", title)
		}
	case KindShare:
		q, _ := url.ParseQuery(strings.SplitN(l.App, "?", 2)[1])
		p.Title, p.OgTitle, p.NoIndex = v.text("share_title"), v.text("share_title"), true
		p.Icon, p.DescHead, p.Button = "share", q.Get("url"), v.text("share_button")
		p.Desc = template.HTML(esc(q.Get("text"))) //nolint:gosec // escaped
	case KindProxy:
		p.Title, p.OgTitle, p.Icon, p.Button, p.NoIndex = v.text("proxy_title"), v.text("proxy_title"), "proxy", v.text("proxy_button"), true
	case KindSocks:
		p.Title, p.OgTitle, p.Icon, p.Button, p.NoIndex = v.text("socks_title"), v.text("socks_title"), "proxy", v.text("socks_button"), true
	case KindLanguage:
		p.Title, p.OgTitle, p.Icon = v.text("language_title"), v.text("language_og"), "language"
		p.Desc, p.Button = v.html("language_desc"), v.text("language_button")
	case KindTheme:
		p.Title, p.OgTitle, p.Icon, p.NoIndex = v.text("theme_title"), v.text("theme_og"), "theme", true
		p.Desc, p.Button = v.html("theme_desc", l.Subject), v.text("theme_button")
	case KindBackground:
		p.Title, p.OgTitle, p.Icon, p.Button, p.NoIndex = v.text("bg_title"), v.text("bg_title"), "theme", v.text("bg_button"), true
	case KindFolder:
		p.Title, p.OgTitle, p.Icon, p.NoIndex = v.text("folder_title"), v.text("folder_og"), "group", true
		p.Desc, p.Button = v.html("folder_desc"), v.text("folder_button")
	case KindLogin:
		p.Title, p.OgTitle, p.Icon, p.NoIndex = v.text("login_title"), v.text("login_title"), "user", true
		p.Desc, p.Button = v.html("login_desc", l.Subject), v.text("login_button")
	case KindContact:
		p.Title, p.OgTitle, p.Icon, p.NoIndex = v.text("private_title"), v.text("private_title"), "user", true
		p.Desc, p.Button = v.html("contact_link"), v.text("open_contact")
	default:
		return nil, nil
	}
	return p, nil
}

// peerPage is the page of a username, a post of it or its boost link: the
// owner as anybody sees it, or, for a username nobody has, the page of one
// that does not tell so (as t.me: never a 404).
func (b *Builder) peerPage(ctx context.Context, l Link, v Visitor, p *Page) (*Page, error) {
	peer, err := b.Data.Peer(ctx, l.Subject)
	if err != nil {
		return nil, err
	}
	at := "@" + l.Subject
	if peer == nil {
		p.Title, p.OgTitle, p.NoIndex = v.text("contact_title", l.Subject), v.text("contact_title", l.Subject), true
		p.OgDesc = ""
		p.Icon, p.Button = "user", v.text("send_message")
		p.Desc = v.html("unknown_desc", at)
		return p, nil
	}
	p.Name, p.OgTitle = peer.Name, peer.Name
	if peer.About != "" {
		p.OgDesc = peer.About
		p.Desc = template.HTML(strings.ReplaceAll(esc(peer.About), "\n", "<br>")) //nolint:gosec // escaped
	}
	if peer.HasPhoto {
		// Relative on the page: it is served on www.pluma.chat as well,
		// whose CSP allows images of its own origin only. Absolute for
		// those who fetch the page (og:image).
		p.PhotoURL = "/_l/avatar/" + url.PathEscape(peer.Username)
		p.OgImage = b.Site + p.PhotoURL
	} else {
		p.Initials = initials(peer.Name)
	}
	switch peer.Kind {
	case PeerChannel, PeerGroup:
		p.Title = v.text("view_title", peer.Username)
		key := "members"
		if peer.Kind == PeerChannel {
			key = "subscribers"
		}
		p.Extra = []string{count(v.Lang, key, peer.Members)}
		p.Button = v.text("view_in_app")
		p.Addition = v.html("if_join", peer.Name)
		if peer.About == "" {
			p.OgDesc = text(v.Lang, "if_join", at)
			p.OgDesc = strings.NewReplacer("<strong>", "", "</strong>", "", "<br>", " ").Replace(p.OgDesc)
		}
		if l.Kind == KindPost {
			p.Button = v.text("view_post")
			p.Addition = v.html("if_post", peer.Name)
		}
		if peer.Kind == PeerChannel && peer.Public {
			if l.Kind == KindPost {
				p.Context = &Anchor{Text: v.text("view_context"), Href: "/s/" + peer.Username + "/" + itoa(l.Post) + "#post-" + itoa(l.Post)}
			} else {
				p.Context = &Anchor{Text: v.text("preview_channel"), Href: "/s/" + peer.Username}
			}
		}
	case PeerBot:
		p.Title = v.text("launch_title", peer.Username)
		p.Extra = []string{at}
		p.Button = v.text("start_bot")
		if strings.Contains(l.App, "startapp") || strings.Contains(l.App, "appname=") {
			p.Button = v.text("open_app")
		}
		p.Addition = v.html("if_launch", peer.Name)
	default:
		p.Title = v.text("contact_title", peer.Username)
		p.Extra = []string{at}
		p.Button = v.text("send_message")
		first := peer.FirstName
		if first == "" {
			first = peer.Name
		}
		p.Addition = v.html("if_contact", first)
		if peer.About == "" {
			p.OgDesc = strings.NewReplacer("<strong>", "", "</strong>", "", "<br>", " ").Replace(text(v.Lang, "if_contact", at))
		}
	}
	return p, nil
}

// initials are the letters of a peer's avatar without a photo.
func initials(name string) string {
	var out []rune
	for _, w := range strings.Fields(name) {
		r := []rune(w)
		if len(r) > 0 {
			out = append(out, r[0])
		}
		if len(out) == 2 {
			break
		}
	}
	return strings.ToUpper(string(out))
}

// formatPhone writes the digits of a phone link with a plus.
func formatPhone(digits string) string {
	return "+" + digits
}

func itoa(n int) string { return strconv.Itoa(n) }
