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

// Package oauth is the provider of the login of users on sites,
// oauth.pluma.chat (task 106, ADR 0068 p. 6): a site sends its visitor to
// /auth (OpenID Connect, authorization code flow with PKCE) or to
// /auth/legacy (the format of the login widget,
// core.telegram.org/widgets/login-legacy); the page shows the request as a
// QR code and a button that open it in the app, and the match code the
// user picks there; the page polls the request (/auth/state) and sends the
// browser back to the site once the user has answered in the app. A site
// of OpenID Connect exchanges the code for an ID token at /token and checks
// it with the keys of /.well-known/jwks.json.
//
// The request itself is pkg/urlauth's (oauth_req in Redis): bff answers the
// apps from it (messages.requestUrlAuth, acceptUrlAuth, declineUrlAuth,
// checkUrlAuthMatchCode) and records the site among the user's logged-in
// sites when the user accepts.
package oauth

import (
	"context"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/teamgram/teamgram-server/pkg/botkeys"
	"github.com/teamgram/teamgram-server/pkg/botstore"
	"github.com/teamgram/teamgram-server/pkg/urlauth"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/kv"
)

// Account is what the provider reads of a user: the bot of a site, or the
// user who logs in.
type Account struct {
	Id        int64
	Bot       bool
	FirstName string
	LastName  string
	Username  string
	Phone     string // digits, without "+"
	// PhotoId is the photo anybody may see, 0 for none (the avatars of
	// links, /_l/avatar/<username>).
	PhotoId int64
}

// Accounts finds users; a missing or deleted one is (nil, nil).
type Accounts interface {
	Account(ctx context.Context, id int64) (*Account, error)
}

// Bots is what the provider reads of the bots' settings (*botstore.Store).
type Bots interface {
	LoginClient(ctx context.Context, botId int64) (*botstore.LoginClient, error)
	CheckLoginClientSecret(ctx context.Context, botId int64, secret string) (bool, error)
	LoginDomain(ctx context.Context, botId int64) (string, error)
}

// BotKeys finds the keys of a bot's current token (*botkeys.Store).
type BotKeys interface {
	Get(ctx context.Context, botId int64) (keys botkeys.Keys, ok bool, err error)
}

// Regions names the region of an address (authsession.getIpRegion).
type Regions interface {
	Region(ctx context.Context, ip string) (string, error)
}

// Texts gives the texts of the pages in the language of a request (the
// texts of the links pages, ADR 0036).
type Texts interface {
	Lang(r *http.Request) string
	Text(lang, key string, args ...string) string
}

// Provider serves oauth.pluma.chat.
type Provider struct {
	// Issuer is https://oauth.pluma.chat; Site is https://pluma.chat, whose
	// links open a request in the app (urlauth.AppLink) and whose avatars
	// are the users' pictures.
	Issuer string
	Site   string

	Keys     *Keys
	KV       kv.Store
	Requests *urlauth.Store
	Bots     Bots
	BotKeys  BotKeys
	Accounts Accounts
	Regions  Regions
	Texts    Texts
	// Wordmark is the logo of the site's header, an SVG.
	Wordmark template.HTML

	// Timeout bounds the work of one request.
	Timeout time.Duration
	now     func() time.Time
	limits  *limits
	tmpl    *template.Template
}

// Init checks p and parses its page; call it once before serving.
func (p *Provider) Init() error {
	t, err := parsePage()
	if err != nil {
		return err
	}
	p.tmpl = t
	p.limits = newLimits()
	if p.now == nil {
		p.now = time.Now
	}
	if p.Timeout == 0 {
		p.Timeout = 10 * time.Second
	}
	if p.Requests == nil {
		p.Requests = urlauth.NewStore(p.KV)
	}
	return nil
}

// Host is the host the provider answers on, that of Issuer.
func (p *Provider) Host() string {
	u, err := url.Parse(p.Issuer)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// ServeHTTP serves the provider's paths; links passes it every request for
// its host but the files and avatars of the site (/_l/).
func (p *Provider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), p.Timeout)
	defer cancel()
	r = r.WithContext(ctx)
	switch r.URL.Path {
	case "/auth":
		if getOnly(w, r) {
			p.authorize(w, r)
		}
	case "/auth/legacy":
		if getOnly(w, r) {
			p.authorizeLegacy(w, r)
		}
	case "/auth/state":
		if getOnly(w, r) {
			p.state(w, r)
		}
	case "/token":
		p.token(w, r)
	case "/.well-known/openid-configuration":
		if allowGet(w, r) {
			p.discovery(w)
		}
	case "/.well-known/jwks.json":
		if allowGet(w, r) {
			p.jwks(w)
		}
	case "/_o/oauth.css", "/_o/poll.js":
		if allowGet(w, r) {
			serveAsset(w, r, strings.TrimPrefix(r.URL.Path, "/_o/"))
		}
	case "/":
		http.Redirect(w, r, p.Site+"/", http.StatusFound)
	default:
		http.NotFound(w, r)
	}
}

// getOnly admits GET alone: a HEAD of /auth would make a login request
// nobody sees.
func getOnly(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet {
		return true
	}
	w.Header().Set("Allow", "GET")
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	return false
}

func allowGet(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		return true
	}
	w.Header().Set("Allow", "GET, HEAD")
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	return false
}

// visitorIP is the address of the browser. Caddy, the only way in, sets
// X-Forwarded-For to the address it was connected from and passes no
// address a client put there (teamgramd/deploy/web/Caddyfile); the last
// entry is the one Caddy added. A request from a public address came past
// Caddy, and only its own address counts.
func visitorIP(r *http.Request) string {
	remote := r.RemoteAddr
	if host, _, err := net.SplitHostPort(remote); err == nil {
		remote = host
	}
	ip := net.ParseIP(remote)
	if ip == nil {
		return ""
	}
	if ip.IsPrivate() || ip.IsLoopback() {
		if xff := r.Header.Values("X-Forwarded-For"); len(xff) > 0 {
			parts := strings.Split(xff[len(xff)-1], ",")
			if fwd := net.ParseIP(strings.TrimSpace(parts[len(parts)-1])); fwd != nil {
				return fwd.String()
			}
		}
	}
	return ip.String()
}

// region is the region of ip, "" when it is not known: the region only
// describes the request to the user, so a failure is logged and the login
// goes on without it.
func (p *Provider) region(ctx context.Context, ip string) string {
	if ip == "" || p.Regions == nil {
		return ""
	}
	region, err := p.Regions.Region(ctx, ip)
	if err != nil {
		logx.WithContext(ctx).Errorf("oauth: the region of a visitor: %v", err)
		return ""
	}
	return region
}

// bot is the bot of id, nil when there is none, it is deleted or it is
// no bot: a user's id is never taken for a site's bot.
func (p *Provider) bot(ctx context.Context, id int64) (*Account, error) {
	if id <= 0 {
		return nil, nil
	}
	a, err := p.Accounts.Account(ctx, id)
	if err != nil || a == nil || !a.Bot {
		return nil, err
	}
	return a, nil
}
