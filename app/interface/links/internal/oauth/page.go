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
	"bytes"
	"context"
	"embed"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/zeromicro/go-zero/core/logx"
	"rsc.io/qr"
)

//go:embed assets
var assetFS embed.FS

// assetTypes are the files of the page under /_o/, with their types.
var assetTypes = map[string]string{
	"oauth.css": "text/css; charset=utf-8",
	"poll.js":   "text/javascript; charset=utf-8",
}

// pageCSP is the site's policy (teamgramd/deploy/web/Caddyfile): the page
// has no inline script or style, polls its own host and leaves for the site
// by a navigation, which the policy does not restrict.
const pageCSP = "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'none'"

// pageTexts are the keys of the texts the page shows (the texts of the
// links pages, app/interface/links/internal/links/texts.go).
var pageTexts = []string{
	"download", "oauth_scan", "oauth_button", "oauth_match", "oauth_waiting", "oauth_done",
	"oauth_expired", "oauth_retry", "oauth_declined", "oauth_back", "oauth_error_heading",
}

// page is what the template shows: a login request waiting for the user,
// or an error.
type page struct {
	Lang     string
	Title    string
	Site     string
	Wordmark template.HTML
	T        map[string]string

	// The request.
	Heading  string
	BotLine  string
	PhotoURL string // the bot's avatar on this host, "" for initials
	Initials string
	QR       template.HTML
	AppLink  string // the link that opens the request in the app
	Code     string // the right match code
	Token    string
	Retry    string // this page again: a new request
	Back     string // the site, for a declined login of the widget format

	// Error is the text of an error page.
	Error string
}

func parsePage() (*template.Template, error) {
	b, err := assetFS.ReadFile("assets/oauth.html")
	if err != nil {
		return nil, err
	}
	return template.New("oauth").Parse(string(b))
}

func serveAsset(w http.ResponseWriter, r *http.Request, name string) {
	typ, ok := assetTypes[name]
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
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(b)
}

// newPage is a page in the language of r.
func (p *Provider) newPage(r *http.Request, titleKey string, args ...string) *page {
	lang := p.Texts.Lang(r)
	t := make(map[string]string, len(pageTexts))
	for _, k := range pageTexts {
		t[k] = p.Texts.Text(lang, k)
	}
	return &page{
		Lang:     lang,
		Title:    p.Texts.Text(lang, titleKey, args...),
		Site:     p.Site,
		Wordmark: p.Wordmark,
		T:        t,
	}
}

// errorPage shows the error of key with status.
func (p *Provider) errorPage(w http.ResponseWriter, r *http.Request, status int, key string) {
	pg := p.newPage(r, "oauth_error_title")
	pg.Error = p.Texts.Text(pg.Lang, key)
	p.render(r.Context(), w, status, pg)
}

func (p *Provider) render(ctx context.Context, w http.ResponseWriter, status int, pg *page) {
	var b bytes.Buffer
	if err := p.tmpl.Execute(&b, pg); err != nil {
		logx.WithContext(ctx).Errorf("oauth: render the page: %v", err)
		http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", pageCSP)
	// A request is the browser's own: never cached, never shown in a
	// frame of another site (frame-ancestors).
	h.Set("Cache-Control", "no-store")
	h.Set("Vary", "Accept-Language")
	w.WriteHeader(status)
	_, _ = w.Write(b.Bytes())
}

// botFace fills the bot's photo or initials and its line on pg.
func (p *Provider) botFace(pg *page, bot *Account) {
	name := strings.TrimSpace(bot.FirstName + " " + bot.LastName)
	if bot.Username != "" && bot.PhotoId != 0 {
		pg.PhotoURL = "/_l/avatar/" + url.PathEscape(bot.Username) + "?v=" + strconv.FormatInt(bot.PhotoId, 10)
	} else {
		pg.Initials = initials(name)
	}
	line := name
	if bot.Username != "" {
		line += " (@" + bot.Username + ")"
	}
	pg.BotLine = p.Texts.Text(pg.Lang, "oauth_bot", line)
}

// initials are the letters of an avatar without a photo, as the links
// pages draw them.
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

// qrQuiet is the blank border of the QR code, in modules (ISO/IEC 18004
// asks for 4; the page's white frame adds to it).
const qrQuiet = 2

// qrSVG draws text as a QR code: one path of the dark modules on a white
// field, an inline SVG without styles, which the page's policy allows.
func qrSVG(text string) (template.HTML, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	size := code.Size + 2*qrQuiet
	var d strings.Builder
	for y := 0; y < code.Size; y++ {
		for x := 0; x < code.Size; x++ {
			if code.Black(x, y) {
				fmt.Fprintf(&d, "M%d %dh1v1h-1z", x+qrQuiet, y+qrQuiet)
			}
		}
	}
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges" role="img"><rect width="%d" height="%d" fill="#ffffff"/><path fill="#111111" d="%s"/></svg>`,
		size, size, size, size, d.String())
	return template.HTML(svg), nil //nolint:gosec // built of numbers only
}
