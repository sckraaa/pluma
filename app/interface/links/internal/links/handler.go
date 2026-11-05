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
	"bytes"
	"context"
	"embed"
	"html/template"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

//go:embed assets
var assetFS embed.FS

// assets are the files of the pages under /_l/ (a path no username takes:
// a username starts with a letter), with their types.
var assets = map[string]string{
	"page.css":              "text/css; charset=utf-8",
	"preview.css":           "text/css; charset=utf-8",
	"open.js":               "text/javascript; charset=utf-8",
	"geologica-latin.woff2": "font/woff2",
	"geologica-cyrillic.woff2": "font/woff2",
}

// Avatars gives the photo of a username's owner that anybody may see, as
// JPEG; nil when there is none.
type Avatars interface {
	Avatar(ctx context.Context, username string) ([]byte, error)
}

// Handler serves the links of the link host. Caddy sends it every path of
// the site that is not a file of the site (teamgramd/deploy/web/Caddyfile).
type Handler struct {
	Builder *Builder
	Avatars Avatars
	Preview *Previews
	// Timeout bounds the data of one page.
	Timeout time.Duration
	tmpl    *template.Template
}

// NewHandler returns the handler of b's pages.
func NewHandler(b *Builder, avatars Avatars, preview *Previews) (*Handler, error) {
	wordmark, err := assetFS.ReadFile("assets/wordmark.svg")
	if err != nil {
		return nil, err
	}
	page, err := assetFS.ReadFile("assets/page.html")
	if err != nil {
		return nil, err
	}
	t := template.New("page")
	if _, err = t.New("wordmark").Parse(string(wordmark)); err != nil {
		return nil, err
	}
	if _, err = t.Parse(string(page)); err != nil {
		return nil, err
	}
	if preview != nil {
		if err = preview.parse(t); err != nil {
			return nil, err
		}
	}
	return &Handler{Builder: b, Avatars: avatars, Preview: preview, Timeout: 10 * time.Second, tmpl: t}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), h.Timeout)
	defer cancel()
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/_l/post/"):
		h.postPhoto(ctx, w, strings.TrimPrefix(p, "/_l/post/"))
		return
	case strings.HasPrefix(p, "/_l/avatar/"):
		h.avatar(ctx, w, strings.TrimPrefix(p, "/_l/avatar/"))
		return
	case strings.HasPrefix(p, "/_l/"):
		h.asset(w, r, strings.TrimPrefix(p, "/_l/"))
		return
	}
	v := visitorOf(r)
	l := Parse(p, r.URL.Query())
	if l.Kind == KindChannelPreview {
		if h.Preview == nil {
			http.Redirect(w, r, "/"+l.Subject, http.StatusFound)
			return
		}
		h.preview(ctx, w, r, l, v)
		return
	}
	page, err := h.Builder.Build(ctx, l, v)
	if err != nil {
		// Never the link itself in the log: names and hashes are private.
		logx.WithContext(ctx).Errorf("links: a page of kind %d: %v", l.Kind, err)
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	if page == nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	h.render(ctx, w, "page", page)
}

func (h *Handler) render(ctx context.Context, w http.ResponseWriter, name string, data any) {
	var b bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&b, name, data); err != nil {
		logx.WithContext(ctx).Errorf("links: render %s: %v", name, err)
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// As t.me: the page is of the object now, never cached.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "Accept-Language, User-Agent")
	_, _ = w.Write(b.Bytes())
}

func (h *Handler) asset(w http.ResponseWriter, r *http.Request, name string) {
	typ, ok := assets[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	b, err := assetFS.ReadFile(path.Join("assets", name))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(b)
}

func (h *Handler) avatar(ctx context.Context, w http.ResponseWriter, username string) {
	if !usernameRe.MatchString(username) || h.Avatars == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	b, err := h.Avatars.Avatar(ctx, username)
	if err != nil {
		logx.WithContext(ctx).Errorf("links: an avatar: %v", err)
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

// visitorOf is the language and the device of the visitor: the language
// of the site the browser prefers (the site's own choice, app.js: Russian
// for ru, else English), the device by its user agent.
func visitorOf(r *http.Request) Visitor {
	v := Visitor{Lang: "en"}
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		tag := strings.ToLower(strings.TrimSpace(strings.SplitN(part, ";", 2)[0]))
		if tag == "" {
			continue
		}
		if tag == "ru" || strings.HasPrefix(tag, "ru-") {
			v.Lang = "ru"
		}
		break
	}
	ua := r.UserAgent()
	switch {
	case strings.Contains(ua, "Android"):
		v.Android, v.Device = true, "android"
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPad"), strings.Contains(ua, "iPod"):
		v.Device = "iphone"
	case strings.Contains(ua, "Macintosh"):
		v.Device = "mac"
	case strings.Contains(ua, "Windows"):
		v.Device = "windows"
	case strings.Contains(ua, "Linux"), strings.Contains(ua, "X11"):
		v.Device = "linux"
	}
	return v
}
