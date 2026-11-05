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
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/zeromicro/go-zero/core/logx"
)

// The channel preview, /s/<channel> (t.me/s/<channel>): the posts of a
// public channel for anybody, newest last, with the channel beside them;
// ?before=<id> pages back, /s/<channel>/<id> shows the posts around one.
// Other usernames, and channels nobody may read, go to their page.

// PreviewPage is the number of posts a preview page shows (t.me: 20).
const PreviewPage = 20

// Post is a post as the preview shows it.
type Post struct {
	ID       int
	Date     time.Time
	Text     string
	Entities []Entity
	// Media: "photo", "video", "file", "media" (another kind) or "".
	Media    string
	HasPhoto bool
	Views    int
	Service  bool
}

// Entity is a formatting entity of a post's text, in UTF-16 code units as
// the API counts them.
type Entity struct {
	Kind           string // bold, italic, code, pre, strike, underline, url, text_url, mention, spoiler, blockquote
	Offset, Length int
	URL            string
}

// PreviewSource gives the posts of public channels.
type PreviewSource interface {
	// Channel is the public channel of the username (nil for anything
	// else) and its id.
	Channel(ctx context.Context, username string) (*Peer, int64, error)
	// Posts are the posts of the channel with ids below before (0: the
	// newest), at most limit, newest first.
	Posts(ctx context.Context, channelId int64, before, limit int) ([]Post, error)
	// PostPhoto is the photo of a post of a public channel, as JPEG.
	PostPhoto(ctx context.Context, username string, id int) ([]byte, error)
}

// Previews builds the channel previews.
type Previews struct {
	Builder *Builder
	Data    PreviewSource
}

// PreviewView is what the preview template shows.
type PreviewView struct {
	*Page
	Channel  *Peer
	Posts    []PostView
	More     string // the link to earlier posts, "" for none
	Username string
}

// PostView is a post as the template shows it.
type PostView struct {
	ID       int
	Text     template.HTML
	Photo    string
	Media    string
	MediaURL string
	Views    string
	Date     string
	DateISO  string
	Link     string
	Service  bool
}

func (p *Previews) parse(t *template.Template) error {
	b, err := assetFS.ReadFile("assets/preview.html")
	if err != nil {
		return err
	}
	_, err = t.New("preview").Parse(string(b))
	return err
}

func (h *Handler) preview(ctx context.Context, w http.ResponseWriter, r *http.Request, l Link, v Visitor) {
	p := h.Preview
	peer, channelId, err := p.Data.Channel(ctx, l.Subject)
	if err != nil {
		logx.WithContext(ctx).Errorf("links: a channel preview: %v", err)
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	if peer == nil {
		http.Redirect(w, r, "/"+l.Subject, http.StatusFound)
		return
	}
	before := l.Before
	if l.Post > 0 {
		before = l.Post + PreviewPage/2
	}
	posts, err := p.Data.Posts(ctx, channelId, before, PreviewPage)
	if err != nil {
		logx.WithContext(ctx).Errorf("links: a channel preview: %v", err)
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	page, err := h.Builder.Build(ctx, Link{Kind: KindPeer, Subject: peer.Username, App: "resolve?domain=" + peer.Username}, v)
	if err != nil || page == nil {
		logx.WithContext(ctx).Errorf("links: a channel preview: %v", err)
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	page.Title = text(v.Lang, "preview_title", peer.Name)
	view := &PreviewView{Page: page, Channel: peer, Username: peer.Username}
	for i := len(posts) - 1; i >= 0; i-- {
		view.Posts = append(view.Posts, h.Builder.postView(peer.Username, posts[i], v))
	}
	if len(posts) == PreviewPage {
		view.More = "/s/" + peer.Username + "?before=" + strconv.Itoa(posts[len(posts)-1].ID)
	}
	h.render(ctx, w, "preview", view)
}

func (b *Builder) postView(username string, p Post, v Visitor) PostView {
	link := "/" + username + "/" + strconv.Itoa(p.ID)
	pv := PostView{
		ID:      p.ID,
		Text:    RenderText(p.Text, p.Entities),
		Date:    p.Date.UTC().Format("02.01.2006 15:04"),
		DateISO: p.Date.UTC().Format(time.RFC3339),
		Link:    link,
		Service: p.Service,
	}
	if v.Lang == "en" {
		pv.Date = p.Date.UTC().Format("Jan 2, 2006 at 15:04")
	}
	if p.Views > 0 {
		pv.Views = compactCount(p.Views)
	}
	if p.HasPhoto {
		pv.Photo = "/_l/post/" + url.PathEscape(username) + "/" + strconv.Itoa(p.ID) + "/photo"
	}
	if p.Media != "" && !p.HasPhoto {
		pv.Media = text(v.Lang, "preview_"+p.Media)
		pv.MediaURL = string(b.appURL(v, "resolve?domain="+username+"&post="+strconv.Itoa(p.ID)))
	}
	if p.Service && p.Text == "" {
		pv.Text = template.HTML(esc(text(v.Lang, "preview_service"))) //nolint:gosec // escaped
	}
	return pv
}

// compactCount writes a count as t.me does: 1.65M, 12.4K, 950.
func compactCount(n int) string {
	switch {
	case n >= 1_000_000:
		return trimZero(strconv.FormatFloat(float64(n)/1_000_000, 'f', 2, 64)) + "M"
	case n >= 10_000:
		return trimZero(strconv.FormatFloat(float64(n)/1_000, 'f', 1, 64)) + "K"
	}
	return strconv.Itoa(n)
}

func trimZero(s string) string {
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

// RenderText renders a post's text with its entities as HTML, escaped,
// line breaks kept. Entities count UTF-16 code units; overlapping ones
// nest in order of their starts.
func RenderText(s string, entities []Entity) template.HTML {
	units := utf16.Encode([]rune(s))
	type mark struct {
		at    int
		open  bool
		ent   Entity
		order int
	}
	var marks []mark
	for i, e := range entities {
		if e.Offset < 0 || e.Length <= 0 || e.Offset+e.Length > len(units) {
			continue
		}
		marks = append(marks, mark{at: e.Offset, open: true, ent: e, order: i}, mark{at: e.Offset + e.Length, open: false, ent: e, order: i})
	}
	// Closings before openings at one place; openings by start, then
	// the longer first; closings in reverse.
	sortMarks := func(a, b mark) bool {
		if a.at != b.at {
			return a.at < b.at
		}
		if a.open != b.open {
			return !a.open
		}
		if a.open {
			return a.ent.Length > b.ent.Length
		}
		return a.order > b.order
	}
	for i := 1; i < len(marks); i++ {
		for j := i; j > 0 && sortMarks(marks[j], marks[j-1]); j-- {
			marks[j], marks[j-1] = marks[j-1], marks[j]
		}
	}
	var out strings.Builder
	pos := 0
	write := func(to int) {
		if to > pos {
			chunk := string(utf16.Decode(units[pos:to]))
			out.WriteString(strings.ReplaceAll(esc(chunk), "\n", "<br>"))
			pos = to
		}
	}
	for _, m := range marks {
		write(m.at)
		if m.open {
			out.WriteString(openTag(m.ent, string(utf16.Decode(units[m.ent.Offset:m.ent.Offset+m.ent.Length]))))
		} else {
			out.WriteString(closeTag(m.ent))
		}
	}
	write(len(units))
	return template.HTML(out.String()) //nolint:gosec // every chunk escaped, tags our own
}

func openTag(e Entity, inner string) string {
	switch e.Kind {
	case "bold":
		return "<b>"
	case "italic":
		return "<i>"
	case "underline":
		return "<u>"
	case "strike":
		return "<s>"
	case "code":
		return "<code>"
	case "pre":
		return "<pre>"
	case "spoiler":
		return `<span class="spoiler">`
	case "blockquote":
		return "<blockquote>"
	case "url":
		return `<a href="` + esc(safeURL(inner)) + `" rel="noopener nofollow" target="_blank">`
	case "text_url":
		return `<a href="` + esc(safeURL(e.URL)) + `" rel="noopener nofollow" target="_blank">`
	case "mention":
		return `<a href="/` + esc(strings.TrimPrefix(inner, "@")) + `">`
	}
	return "<span>"
}

func closeTag(e Entity) string {
	switch e.Kind {
	case "bold":
		return "</b>"
	case "italic":
		return "</i>"
	case "underline":
		return "</u>"
	case "strike":
		return "</s>"
	case "code":
		return "</code>"
	case "pre":
		return "</pre>"
	case "blockquote":
		return "</blockquote>"
	case "url", "text_url", "mention":
		return "</a>"
	}
	return "</span>"
}

// safeURL is u as a link of the page: http and https only, a bare host
// taken as https.
func safeURL(u string) string {
	p, err := url.Parse(strings.TrimSpace(u))
	if err != nil {
		return "#"
	}
	switch p.Scheme {
	case "http", "https":
		return p.String()
	case "":
		if p.Host == "" && !strings.HasPrefix(u, "/") {
			return "https://" + strings.TrimSpace(u)
		}
	}
	return "#"
}

// postPhoto serves the photo of a post of a public channel.
func (h *Handler) postPhoto(ctx context.Context, w http.ResponseWriter, rest string) {
	parts := strings.Split(rest, "/")
	if h.Preview == nil || len(parts) != 3 || parts[2] != "photo" || !usernameRe.MatchString(parts[0]) || !digitsOnly(parts[1]) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	b, err := h.Preview.Data.PostPhoto(ctx, parts[0], atoi(parts[1]))
	if err != nil {
		logx.WithContext(ctx).Errorf("links: a post's photo: %v", err)
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	if b == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=600")
	_, _ = w.Write(b)
}
