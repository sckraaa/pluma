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

package inlinebots

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/metric"
)

// peerUser is mtproto.PEER_USER, the peer_type of a user's username row.
const peerUser = 2

// metricMisconfigured is 1 for a bot of the registry the database does not
// match: it is not served until it does (ADR 0062, p. 1).
var metricMisconfigured = metric.NewGaugeVec(&metric.GaugeVecOpts{
	Namespace: "inline",
	Name:      "bot_misconfigured",
	Help:      "1 when the account of a built-in inline bot does not match the registry; the bot answers BOT_INLINE_DISABLED.",
	Labels:    []string{"bot"},
})

// Querier is the database the check reads (*sqlx.DB).
type Querier interface {
	QueryRowsPartial(ctx context.Context, v interface{}, query string, args ...interface{}) error
}

// account is what the database has for a bot of the registry.
type account struct {
	user     *userRow
	bot      *botRow
	username *usernameRow
}

type userRow struct {
	Id      int64 `db:"id"`
	IsBot   bool  `db:"is_bot"`
	Deleted bool  `db:"deleted"`
}

type botRow struct {
	BotId       int64  `db:"bot_id"`
	Placeholder string `db:"bot_inline_placeholder"`
	Geo         bool   `db:"bot_inline_geo"`
}

type usernameRow struct {
	Username string `db:"username"`
	PeerType int32  `db:"peer_type"`
	PeerId   int64  `db:"peer_id"`
	Deleted  bool   `db:"deleted"`
}

// check tells how the account differs from the registry's bot, nil when it
// does not: the username is the bot's, the account is a bot that is not
// deleted, and its bots row has a placeholder and the bot's bot_inline_geo.
func (b Bot) check(a account) error {
	switch u := a.username; {
	case u == nil:
		return fmt.Errorf("username %s is not taken", b.Username)
	case u.PeerType != peerUser || u.PeerId != b.Id:
		return fmt.Errorf("username %s belongs to peer %d of type %d", b.Username, u.PeerId, u.PeerType)
	case u.Deleted:
		return fmt.Errorf("username %s is deleted", b.Username)
	}
	switch u := a.user; {
	case u == nil:
		return fmt.Errorf("no account %d", b.Id)
	case !u.IsBot:
		return fmt.Errorf("account %d is not a bot", b.Id)
	case u.Deleted:
		return fmt.Errorf("account %d is deleted", b.Id)
	}
	switch r := a.bot; {
	case r == nil:
		return fmt.Errorf("no bots row of %d", b.Id)
	case r.Placeholder == "":
		return fmt.Errorf("bot %d has no inline placeholder", b.Id)
	case r.Geo != b.Geo:
		return fmt.Errorf("bot %d has bot_inline_geo %v, want %v", b.Id, r.Geo, b.Geo)
	}
	return nil
}

// load reads the accounts of the registry's bots.
func load(ctx context.Context, db Querier) (map[int64]account, error) {
	bots := All()
	in := "(?" + strings.Repeat(", ?", len(bots)-1) + ")"
	ids := make([]interface{}, 0, len(bots))
	names := make([]interface{}, 0, len(bots))
	for _, b := range bots {
		ids = append(ids, b.Id)
		names = append(names, b.Username)
	}
	var users []userRow
	if err := db.QueryRowsPartial(ctx, &users, "SELECT id, is_bot, deleted FROM users WHERE id IN "+in, ids...); err != nil {
		return nil, fmt.Errorf("inlinebots: users: %w", err)
	}
	var botRows []botRow
	if err := db.QueryRowsPartial(ctx, &botRows,
		"SELECT bot_id, bot_inline_placeholder, bot_inline_geo FROM bots WHERE bot_id IN "+in, ids...); err != nil {
		return nil, fmt.Errorf("inlinebots: bots: %w", err)
	}
	var usernames []usernameRow
	if err := db.QueryRowsPartial(ctx, &usernames,
		"SELECT username, peer_type, peer_id, deleted FROM username WHERE username IN "+in, names...); err != nil {
		return nil, fmt.Errorf("inlinebots: username: %w", err)
	}
	out := make(map[int64]account, len(bots))
	for _, b := range bots {
		var a account
		for i := range users {
			if users[i].Id == b.Id {
				a.user = &users[i]
			}
		}
		for i := range botRows {
			if botRows[i].BotId == b.Id {
				a.bot = &botRows[i]
			}
		}
		for i := range usernames {
			// The column compares without case; so does this.
			if strings.EqualFold(usernames[i].Username, b.Username) {
				a.username = &usernames[i]
			}
		}
		out[b.Id] = a
	}
	return out, nil
}

// Health is what the last check of the registry against the database found.
// A bot is served only after a check found its account in order: before the
// first check that read the database, none is.
type Health struct {
	mu       sync.RWMutex
	ok       map[int64]bool
	verified bool
}

// NewHealth returns a Health no check has run on yet.
func NewHealth() *Health {
	return &Health{ok: map[int64]bool{}}
}

// Usable reports whether the bot of the registry may be served.
func (h *Health) Usable(id int64) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.ok[id]
}

// Verified reports whether a check has read the database and every bot is
// in order.
func (h *Health) Verified() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if !h.verified {
		return false
	}
	for _, b := range All() {
		if !h.ok[b.Id] {
			return false
		}
	}
	return true
}

// ErrMisconfigured is Check when an account does not match the registry; the
// other bots are served.
var ErrMisconfigured = errors.New("inlinebots: built-in bot accounts do not match the registry")

// Check compares the registry with the database and keeps what it found. A
// bot that differs is logged, counted in inline_bot_misconfigured and not
// served. A database that cannot be read changes nothing and is the error.
func (h *Health) Check(ctx context.Context, db Querier) error {
	accounts, err := load(ctx, db)
	if err != nil {
		return err
	}
	h.mu.RLock()
	before, wasVerified := h.ok, h.verified
	h.mu.RUnlock()
	ok := make(map[int64]bool, len(accounts))
	bad := false
	for _, b := range All() {
		if err := b.check(accounts[b.Id]); err != nil {
			// Logged when it breaks, not at every check while it stays so.
			if !wasVerified || before[b.Id] {
				logx.WithContext(ctx).Errorf("inlinebots: @%s is not served: %v", b.Username, err)
			}
			metricMisconfigured.Set(1, b.Username)
			bad = true
			continue
		}
		if wasVerified && !before[b.Id] {
			logx.WithContext(ctx).Infof("inlinebots: @%s is served again", b.Username)
		}
		metricMisconfigured.Set(0, b.Username)
		ok[b.Id] = true
	}
	h.mu.Lock()
	h.ok, h.verified = ok, true
	h.mu.Unlock()
	if bad {
		return ErrMisconfigured
	}
	return nil
}

// Intervals of Watch: soon while a bot is not served, so that an account
// repaired in the database (or a database back after an outage) serves again
// without a restart, and rarely once all are in order.
const (
	checkTimeout   = 10 * time.Second
	retryInterval  = time.Minute
	steadyInterval = 10 * time.Minute
)

// Watch checks the registry now and then again in the background until ctx
// ends: every minute while some bot is not served, every ten minutes after.
// It returns after the first check, so the bots are known when the service
// starts taking requests.
func (h *Health) Watch(ctx context.Context, db Querier) {
	h.checkOnce(ctx, db)
	go func() {
		for {
			next := steadyInterval
			if !h.Verified() {
				next = retryInterval
			}
			t := time.NewTimer(next)
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
			h.checkOnce(ctx, db)
		}
	}()
}

func (h *Health) checkOnce(ctx context.Context, db Querier) {
	cctx, cancel := context.WithTimeout(ctx, checkTimeout)
	defer cancel()
	if err := h.Check(cctx, db); err != nil && !errors.Is(err, ErrMisconfigured) {
		logx.WithContext(ctx).Errorf("inlinebots: check of the built-in bots: %v", err)
	}
}
