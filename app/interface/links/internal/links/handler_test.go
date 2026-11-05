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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeData struct {
	peers map[string]*Peer
	sets  map[string]string
	posts []Post
}

func (f *fakeData) Peer(_ context.Context, username string) (*Peer, error) {
	return f.peers[strings.ToLower(username)], nil
}

func (f *fakeData) StickerSet(_ context.Context, name string) (string, bool, bool, error) {
	title, ok := f.sets[name]
	return title, strings.HasPrefix(name, "Emoji"), ok, nil
}

func (f *fakeData) Avatar(_ context.Context, username string) ([]byte, error) {
	if p := f.peers[strings.ToLower(username)]; p != nil && p.HasPhoto {
		return []byte("\xff\xd8jpeg"), nil
	}
	return nil, nil
}

func (f *fakeData) Channel(ctx context.Context, username string) (*Peer, int64, error) {
	p := f.peers[strings.ToLower(username)]
	if p == nil || p.Kind != PeerChannel {
		return nil, 0, nil
	}
	return p, 1000000005, nil
}

func (f *fakeData) Posts(_ context.Context, _ int64, before, limit int) ([]Post, error) {
	var out []Post
	for i := len(f.posts) - 1; i >= 0 && len(out) < limit; i-- {
		if before == 0 || f.posts[i].ID < before {
			out = append(out, f.posts[i])
		}
	}
	return out, nil
}

func (f *fakeData) PostPhoto(_ context.Context, username string, id int) ([]byte, error) {
	for _, p := range f.posts {
		if p.ID == id && p.HasPhoto {
			return []byte("\xff\xd8photo"), nil
		}
	}
	return nil, nil
}

func newTestHandler(t *testing.T) (*Handler, *fakeData) {
	t.Helper()
	data := &fakeData{
		peers: map[string]*Peer{
			"plumanews": {Kind: PeerChannel, Username: "plumanews", Name: "Pluma News", About: "Новости <b>проекта</b>", Members: 1234, HasPhoto: true, Public: true},
			"chatters":  {Kind: PeerGroup, Username: "chatters", Name: "Chatters", Members: 2},
			"alice_dev": {Kind: PeerUser, Username: "alice_dev", Name: "Alice Smith", FirstName: "Alice"},
			"helperbot": {Kind: PeerBot, Username: "helperbot", Name: "Helper", About: "I help."},
		},
		sets: map[string]string{"Animals": "Just zoo it!", "EmojiSet": "Animated Emoji"},
	}
	for i := 1; i <= 25; i++ {
		data.posts = append(data.posts, Post{ID: i, Date: time.Unix(1700000000+int64(i)*60, 0), Text: "post " + itoa(i), Views: i * 1000})
	}
	data.posts[24].HasPhoto, data.posts[24].Media = true, "photo"
	b := &Builder{Site: "https://pluma.chat", Web: "https://web.pluma.chat", Scheme: "plm", Data: data}
	h, err := NewHandler(b, data, &Previews{Builder: b, Data: data})
	if err != nil {
		t.Fatal(err)
	}
	return h, data
}

func get(t *testing.T, h http.Handler, path string, header map[string]string) (*http.Response, string) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range header {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	res := w.Result()
	b, _ := io.ReadAll(res.Body)
	return res, string(b)
}

func mustContain(t *testing.T, what, body string, parts ...string) {
	t.Helper()
	for _, p := range parts {
		if !strings.Contains(body, p) {
			t.Errorf("%s: no %q in the page", what, p)
		}
	}
}

// TestPages: each kind of link shows its object as t.me does, hands off to
// the app by its plm:// link, in the visitor's language.
func TestPages(t *testing.T) {
	h, _ := newTestHandler(t)
	en := map[string]string{"Accept-Language": "en-US,en;q=0.9", "User-Agent": "Mozilla/5.0 (Macintosh)"}
	ru := map[string]string{"Accept-Language": "ru-RU,ru;q=0.9", "User-Agent": "Mozilla/5.0 (iPhone)"}

	res, body := get(t, h, "/plumanews", en)
	if res.StatusCode != http.StatusOK || res.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("channel: %d %v", res.StatusCode, res.Header)
	}
	mustContain(t, "channel", body,
		"<title>Pluma: View @plumanews</title>", `content="Pluma News"`, "1 234 subscribers", "View in Pluma",
		`href="plm://resolve?domain=plumanews"`, `href="/s/plumanews"`, "Preview channel",
		"Новости &lt;b&gt;проекта&lt;/b&gt;", `src="https://pluma.chat/_l/avatar/plumanews"`,
		`href="https://web.pluma.chat/#?tgaddr=plm%3A%2F%2Fresolve%3Fdomain%3Dplumanews"`, `href="https://pluma.chat/mac"`)
	if strings.Contains(body, "<b>проекта</b>") {
		t.Error("the about text not escaped")
	}

	_, body = get(t, h, "/plumanews", ru)
	mustContain(t, "channel, Russian", body, `lang="ru"`, "1 234 подписчика", "Открыть в Pluma", "Посмотреть канал", `href="https://pluma.chat/iphone"`)

	_, body = get(t, h, "/plumanews/7", en)
	mustContain(t, "post", body, "View Post", `href="plm://resolve?domain=plumanews&amp;post=7"`, `href="/s/plumanews/7"`, "View context")

	_, body = get(t, h, "/chatters", en)
	mustContain(t, "group", body, "2 members", "View in Pluma", `class="initials"`, ">C<")
	if strings.Contains(body, "/s/chatters") {
		t.Error("a group has a preview link")
	}

	_, body = get(t, h, "/alice_dev", en)
	mustContain(t, "user", body, "Pluma: Contact @alice_dev", "Alice Smith", "@alice_dev", "Send Message", "you can contact<br>Alice right away")

	_, body = get(t, h, "/helperbot?startapp=x", en)
	mustContain(t, "bot", body, "Pluma: Launch @helperbot", "Open App", `plm://resolve?domain=helperbot&amp;startapp=x`)

	res, body = get(t, h, "/nobody_here", en)
	if res.StatusCode != http.StatusOK {
		t.Errorf("an unknown username: %d, want 200 as t.me", res.StatusCode)
	}
	mustContain(t, "unknown", body, `name="robots" content="noindex, nofollow"`, "Pluma: Contact @nobody_here", "Send Message")

	_, body = get(t, h, "/+Abcdefghij0123456789", en)
	mustContain(t, "invite", body, "Pluma: Join Group Chat", "Join Group", `plm://join?invite=Abcdefghij0123456789`)

	_, body = get(t, h, "/addstickers/Animals", en)
	mustContain(t, "stickers", body, "Add sticker set Just zoo it! on Pluma", "Add Stickers", "plm://addstickers?set=Animals")
	_, body = get(t, h, "/addstickers/EmojiSet", en)
	mustContain(t, "emoji by addstickers", body, "Add Emoji", "plm://addemoji?set=EmojiSet")
	_, body = get(t, h, "/addstickers/Missing", en)
	mustContain(t, "no set", body, "Add sticker set on Pluma", "Sticker Set")

	_, body = get(t, h, "/c/1234/5", en)
	mustContain(t, "private post", body, "Open Message", "plm://privatepost?channel=1234&amp;post=5", "noindex")

	_, body = get(t, h, "/plumanews", map[string]string{"User-Agent": "Mozilla/5.0 (Linux; Android 14)"})
	mustContain(t, "Android", body, `href="plm:resolve?domain=plumanews"`, `href="https://pluma.chat/android"`)
}

// TestNoLink: what is no link goes to the site's home, as t.me's go to
// telegram.org; other methods are refused.
func TestNoLink(t *testing.T) {
	h, _ := newTestHandler(t)
	for _, p := range []string{"/abc", "/c/1234", "/giftcode/aaaa", "/proxy?server=1.2.3.4&port=443&secret=00"} {
		res, _ := get(t, h, p, nil)
		if res.StatusCode != http.StatusFound || res.Header.Get("Location") != "/" {
			t.Errorf("%s: %d to %q", p, res.StatusCode, res.Header.Get("Location"))
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/plumanews", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: %d", w.Code)
	}
}

// TestAssetsAndPhotos: the page's files, the avatar anybody may see, and
// nothing for one without a photo or a name that is no username.
func TestAssetsAndPhotos(t *testing.T) {
	h, _ := newTestHandler(t)
	for path, typ := range map[string]string{"/_l/page.css": "text/css", "/_l/open.js": "text/javascript", "/_l/geologica-latin.woff2": "font/woff2"} {
		res, body := get(t, h, path, nil)
		if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), typ) || body == "" {
			t.Errorf("%s: %d %s", path, res.StatusCode, res.Header.Get("Content-Type"))
		}
	}
	if res, _ := get(t, h, "/_l/page.html", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("the template served: %d", res.StatusCode)
	}
	if res, body := get(t, h, "/_l/avatar/plumanews", nil); res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "image/jpeg" || body == "" {
		t.Errorf("avatar: %d", res.StatusCode)
	}
	for _, p := range []string{"/_l/avatar/alice_dev", "/_l/avatar/../etc", "/_l/avatar/nobody_here"} {
		if res, _ := get(t, h, p, nil); res.StatusCode != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", p, res.StatusCode)
		}
	}
	if res, _ := get(t, h, "/_l/post/plumanews/25/photo", nil); res.StatusCode != http.StatusOK {
		t.Errorf("a post's photo: %d", res.StatusCode)
	}
	if res, _ := get(t, h, "/_l/post/plumanews/3/photo", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("a post without a photo: %d", res.StatusCode)
	}
}

// TestPreview: the posts of a public channel, the newest last, a page of
// PreviewPage with the link to earlier ones; anything else goes to its
// page.
func TestPreview(t *testing.T) {
	h, _ := newTestHandler(t)
	en := map[string]string{"Accept-Language": "en"}
	res, body := get(t, h, "/s/plumanews", en)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("%d", res.StatusCode)
	}
	mustContain(t, "preview", body, "Pluma News – Pluma", `id="post-25"`, `id="post-6"`, `href="/s/plumanews?before=6"`,
		`src="/_l/post/plumanews/25/photo"`, "25K", `href="/plumanews/25"`, "Show earlier posts")
	if strings.Contains(body, `id="post-5"`) {
		t.Error("more than a page")
	}
	if strings.Index(body, `id="post-6"`) > strings.Index(body, `id="post-25"`) {
		t.Error("not the newest last")
	}
	_, body = get(t, h, "/s/plumanews?before=6", en)
	mustContain(t, "earlier", body, `id="post-5"`, `id="post-1"`)
	if strings.Contains(body, "Show earlier posts") {
		t.Error("a link past the first post")
	}
	for _, p := range []string{"/s/chatters", "/s/alice_dev", "/s/nobody_here"} {
		res, _ := get(t, h, p, nil)
		if res.StatusCode != http.StatusFound || res.Header.Get("Location") != "/"+strings.TrimPrefix(p, "/s/") {
			t.Errorf("%s: %d to %q", p, res.StatusCode, res.Header.Get("Location"))
		}
	}
}

// TestRenderText: entities in UTF-16 units over text with emoji and
// Cyrillic, nested, escaped, unsafe links neutralised.
func TestRenderText(t *testing.T) {
	text := "Привет 🙂 <world> visit example.com and docs"
	// "Привет" 0-6, "🙂" 7-9 (two units), "<world>" 10-17, "example.com" 24-35, "docs" 40-44.
	got := string(RenderText(text, []Entity{
		{Kind: "bold", Offset: 0, Length: 6},
		{Kind: "italic", Offset: 10, Length: 7},
		{Kind: "url", Offset: 24, Length: 11},
		{Kind: "text_url", Offset: 40, Length: 4, URL: "javascript:alert(1)"},
	}))
	for _, want := range []string{"<b>Привет</b>", "🙂", "<i>&lt;world&gt;</i>", `<a href="https://example.com" rel="noopener nofollow" target="_blank">example.com</a>`, `<a href="#"`} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in %s", want, got)
		}
	}
	if got := string(RenderText("a\nb", nil)); got != "a<br>b" {
		t.Errorf("line breaks: %s", got)
	}
	if got := string(RenderText("short", []Entity{{Kind: "bold", Offset: 3, Length: 10}})); got != "short" {
		t.Errorf("an entity past the text: %s", got)
	}
}
