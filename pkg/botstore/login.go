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

package botstore

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
)

// What a bot logs users in with on sites (ADR 0068 p. 6): its login domain
// (bots.login_domain), which keyboardButtonUrlAuth links and the
// url_auth_domains of the app config go to, and its OpenID Connect client
// (bot_login_clients) for oauth.pluma.chat.

// Limits of a login client: the redirect addresses and origins it lists,
// and the length of each, which keeps the columns small.
const (
	MaxLoginDomain        = 253
	maxLoginClientEntries = 16
	maxLoginClientEntry   = 1024
	clientSecretBytes     = 32
)

// Errors of the login settings.
var (
	ErrLoginDomainInvalid = errors.New("botstore: invalid login domain")
	ErrLoginDomainTaken   = errors.New("botstore: the login domain belongs to another bot")
	ErrNoLoginClient      = errors.New("botstore: the bot has no login client")
	ErrLoginClientInvalid = errors.New("botstore: invalid login client")
)

// NormalizeLoginDomain returns the domain in the form it is stored and
// compared in, lower case: a host name of at least two labels, without a
// scheme, port, path or trailing dot, and not an IP address. ok is false
// for anything else. A button's link must have exactly this host
// (core.telegram.org/bots/api#loginurl: "The url's domain must be the same
// as the domain linked with the bot").
func NormalizeLoginDomain(domain string) (string, bool) {
	d := strings.ToLower(domain)
	if d == "" || len(d) > MaxLoginDomain {
		return "", false
	}
	labels := strings.Split(d, ".")
	if len(labels) < 2 {
		return "", false
	}
	for _, l := range labels {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return "", false
		}
		for i := 0; i < len(l); i++ {
			c := l[i]
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return "", false
			}
		}
	}
	// A top level domain is never all digits, which leaves out IPv4
	// addresses; IPv6 ones have no dots or carry ':'.
	if tld := labels[len(labels)-1]; strings.Trim(tld, "0123456789") == "" {
		return "", false
	}
	return d, true
}

// SetLoginDomain links the bot to a site domain, "" to unlink it. A domain
// belongs to one bot at most: url_auth_domains links resolve to the bot of
// their domain. The read of the domain locks its range of the login_domain
// index, so two bots setting the same domain at once do not both get it:
// both hold the gap of the free domain, InnoDB breaks the deadlock of their
// updates, and the one rolled back runs again and finds the domain taken.
func (s *Store) SetLoginDomain(ctx context.Context, botId int64, domain string) error {
	if domain != "" {
		d, ok := NormalizeLoginDomain(domain)
		if !ok {
			return ErrLoginDomainInvalid
		}
		domain = d
	}
	for attempt := 1; ; attempt++ {
		err := s.setLoginDomain(ctx, botId, domain)
		if retryable(err) && attempt < randomIdAttempts {
			continue
		}
		return err
	}
}

func (s *Store) setLoginDomain(ctx context.Context, botId int64, domain string) error {
	return s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		if domain != "" {
			var owners []int64
			if err := tx.QueryRowsPartial(&owners, "SELECT bot_id FROM bots WHERE login_domain = ? AND bot_id <> ? FOR UPDATE", domain, botId); err != nil {
				return err
			}
			if len(owners) > 0 {
				return ErrLoginDomainTaken
			}
		}
		var bots []int64
		if err := tx.QueryRowsPartial(&bots, "SELECT bot_id FROM bots WHERE bot_id = ? FOR UPDATE", botId); err != nil {
			return err
		}
		if len(bots) == 0 {
			return ErrNoBot
		}
		_, err := tx.Exec("UPDATE bots SET login_domain = ? WHERE bot_id = ?", domain, botId)
		return err
	})
}

// LoginDomain returns the bot's login domain, "" for none.
func (s *Store) LoginDomain(ctx context.Context, botId int64) (string, error) {
	var rows []string
	if err := s.db.QueryRowsPartial(ctx, &rows, "SELECT login_domain FROM bots WHERE bot_id = ?", botId); err != nil || len(rows) == 0 {
		return "", err
	}
	return rows[0], nil
}

// BotOfLoginDomain returns the bot linked to the domain, 0 for none; the
// domain is compared without regard to case.
func (s *Store) BotOfLoginDomain(ctx context.Context, domain string) (int64, error) {
	d, ok := NormalizeLoginDomain(domain)
	if !ok {
		return 0, nil
	}
	var rows []int64
	if err := s.db.QueryRowsPartial(ctx, &rows,
		"SELECT b.bot_id FROM bots b JOIN users u ON u.id = b.bot_id WHERE b.login_domain = ? AND u.deleted = 0", d); err != nil || len(rows) == 0 {
		return 0, err
	}
	return rows[0], nil
}

// LoginDomains returns the domains of all the bots that have one, sorted:
// the url_auth_domains of the app config.
func (s *Store) LoginDomains(ctx context.Context) ([]string, error) {
	rows := []string{}
	err := s.db.QueryRowsPartial(ctx, &rows,
		"SELECT b.login_domain FROM bots b JOIN users u ON u.id = b.bot_id WHERE b.login_domain <> '' AND u.deleted = 0 ORDER BY b.login_domain")
	return rows, err
}

// LoginClient is the OpenID Connect client of a bot; its client_id is the
// bot's id. The secret itself is never stored, only its hash.
type LoginClient struct {
	BotId        int64
	RedirectURIs []string
	Origins      []string
	CreatedAt    int32
}

// validRedirectURI is an absolute https address without a fragment
// (RFC 6749 3.1.2: "The endpoint URI MUST NOT include a fragment
// component"), compared byte for byte with the redirect_uri of a request.
func validRedirectURI(s string) bool {
	if len(s) > maxLoginClientEntry {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.Fragment == "" && !strings.Contains(s, "#") && u.User == nil
}

// validOrigin is a web origin, https://host[:port], as browsers send it in
// Origin (RFC 6454): no path, query or trailing slash.
func validOrigin(s string) bool {
	if len(s) > maxLoginClientEntry {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && u.User == nil &&
		s == "https://"+u.Host && strings.ToLower(u.Host) == u.Host
}

func checkLoginClient(redirectURIs, origins []string) error {
	if len(redirectURIs) == 0 || len(redirectURIs) > maxLoginClientEntries {
		return fmt.Errorf("%w: 1 to %d redirect addresses", ErrLoginClientInvalid, maxLoginClientEntries)
	}
	for _, r := range redirectURIs {
		if !validRedirectURI(r) {
			return fmt.Errorf("%w: redirect address %q is not an https:// address without a fragment", ErrLoginClientInvalid, r)
		}
	}
	if len(origins) > maxLoginClientEntries {
		return fmt.Errorf("%w: over %d origins", ErrLoginClientInvalid, maxLoginClientEntries)
	}
	for _, o := range origins {
		if !validOrigin(o) {
			return fmt.Errorf("%w: origin %q is not https://host[:port] in lower case", ErrLoginClientInvalid, o)
		}
	}
	return nil
}

// jsonList is a list as the columns hold it: [] for none.
func jsonList(list []string) (string, error) {
	if list == nil {
		list = []string{}
	}
	b, err := json.Marshal(list)
	return string(b), err
}

// SetLoginClient makes the bot's login client, or replaces its addresses
// and its secret, and returns the new secret: the only time it is seen.
func (s *Store) SetLoginClient(ctx context.Context, botId int64, redirectURIs, origins []string, now int32) (secret string, err error) {
	if err = checkLoginClient(redirectURIs, origins); err != nil {
		return "", err
	}
	redirects, err := jsonList(redirectURIs)
	if err != nil {
		return "", err
	}
	allowed, err := jsonList(origins)
	if err != nil {
		return "", err
	}
	b := make([]byte, clientSecretBytes)
	if _, err = rand.Read(b); err != nil {
		return "", fmt.Errorf("botstore: read random: %w", err)
	}
	secret = base64.RawURLEncoding.EncodeToString(b)
	err = s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		var bots []int64
		if err := tx.QueryRowsPartial(&bots, "SELECT bot_id FROM bots WHERE bot_id = ?", botId); err != nil {
			return err
		}
		if len(bots) == 0 {
			return ErrNoBot
		}
		_, err := tx.Exec(`INSERT INTO bot_login_clients (bot_id, secret_hash, redirect_uris, origins, created_at) VALUES (?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE secret_hash = VALUES(secret_hash), redirect_uris = VALUES(redirect_uris), origins = VALUES(origins), created_at = VALUES(created_at)`,
			botId, hashOf(secret), redirects, allowed, now)
		return err
	})
	if err != nil {
		return "", err
	}
	return secret, nil
}

// EditLoginClient replaces the addresses of the bot's login client and
// keeps its secret.
func (s *Store) EditLoginClient(ctx context.Context, botId int64, redirectURIs, origins []string) error {
	if err := checkLoginClient(redirectURIs, origins); err != nil {
		return err
	}
	redirects, err := jsonList(redirectURIs)
	if err != nil {
		return err
	}
	allowed, err := jsonList(origins)
	if err != nil {
		return err
	}
	return s.db.Transact(ctx, func(tx *sqlx.Tx) error {
		var rows []int64
		if err := tx.QueryRowsPartial(&rows, "SELECT bot_id FROM bot_login_clients WHERE bot_id = ? FOR UPDATE", botId); err != nil {
			return err
		}
		if len(rows) == 0 {
			return ErrNoLoginClient
		}
		_, err := tx.Exec("UPDATE bot_login_clients SET redirect_uris = ?, origins = ? WHERE bot_id = ?", redirects, allowed, botId)
		return err
	})
}

// DeleteLoginClient removes the bot's login client.
func (s *Store) DeleteLoginClient(ctx context.Context, botId int64) error {
	r, err := s.db.Exec(ctx, "DELETE FROM bot_login_clients WHERE bot_id = ?", botId)
	if err != nil {
		return err
	}
	if n, err := r.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrNoLoginClient
	}
	return nil
}

type loginClientRow struct {
	BotId        int64  `db:"bot_id"`
	SecretHash   string `db:"secret_hash"`
	RedirectURIs string `db:"redirect_uris"`
	Origins      string `db:"origins"`
	CreatedAt    int32  `db:"created_at"`
}

func (s *Store) loginClient(ctx context.Context, botId int64) (*loginClientRow, error) {
	var rows []loginClientRow
	if err := s.db.QueryRowsPartial(ctx, &rows,
		"SELECT bot_id, secret_hash, redirect_uris, origins, created_at FROM bot_login_clients WHERE bot_id = ?", botId); err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

// LoginClient returns the bot's login client, nil for none.
func (s *Store) LoginClient(ctx context.Context, botId int64) (*LoginClient, error) {
	r, err := s.loginClient(ctx, botId)
	if err != nil || r == nil {
		return nil, err
	}
	c := &LoginClient{BotId: r.BotId, CreatedAt: r.CreatedAt}
	if err = json.Unmarshal([]byte(r.RedirectURIs), &c.RedirectURIs); err != nil {
		return nil, fmt.Errorf("botstore: redirect addresses of the login client of %d: %w", botId, err)
	}
	if err = json.Unmarshal([]byte(r.Origins), &c.Origins); err != nil {
		return nil, fmt.Errorf("botstore: origins of the login client of %d: %w", botId, err)
	}
	return c, nil
}

// CheckLoginClientSecret tells whether secret is the client_secret of the
// bot's login client; false for a bot without one.
func (s *Store) CheckLoginClientSecret(ctx context.Context, botId int64, secret string) (bool, error) {
	r, err := s.loginClient(ctx, botId)
	if err != nil || r == nil || secret == "" {
		return false, err
	}
	return subtle.ConstantTimeCompare([]byte(hashOf(secret)), []byte(r.SecretHash)) == 1, nil
}
