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

// Package sender is the queue of the code sender (task 073, ADR 0042): it
// takes login codes from bff and hands them, one at a time and with human
// pauses, to the Telegram account. What it remembers (the limits, a flood
// wait, contacts still to remove) lives in a file of its volume and survives
// a restart. Codes never touch the disk: a code waiting in the queue is lost
// with the process, and the user asks for a new one.
package sender

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	mrand "math/rand/v2"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Result is what became of a code. The values are the wire format of
// POST /v1/send, which pkg/logincode reads.
type Result string

const (
	// Sent: the account's message went out.
	Sent Result = "sent"
	// Queued: not sent yet when the caller stopped waiting; it stays in the queue.
	Queued Result = "queued"
	// NoTelegram: the phone has no Telegram account.
	NoTelegram Result = "no_telegram"
	// Undeliverable: the account exists but refuses the message (blocked, privacy).
	Undeliverable Result = "undeliverable"
	// RecipientLimited: too many codes to this phone recently.
	RecipientLimited Result = "recipient_limited"
	// AccountLimited: Telegram limits the account, or its own budget is spent.
	AccountLimited Result = "account_limited"
	// Busy: the queue is full.
	Busy Result = "busy"
	// Unavailable: no session, or a failure the sender cannot classify.
	Unavailable Result = "unavailable"
	// Expired: the code waited in the queue too long to be useful.
	Expired Result = "expired"
)

// Session is the state of the account's TDLib session. The values are read
// by the panel from GET /v1/state.
const (
	SessionStarting  = "starting"   // TDLib is opening its database
	SessionReady     = "ready"      // authorized, can send
	SessionNoSession = "no_session" // not authorized: revoked, or never logged in
	SessionBroken    = "broken"     // the database cannot be opened
)

// Delivery is the outcome of handing one code to the account.
type Delivery struct {
	Result Result
	// UserID is the imported contact, to remove; 0 if none was imported.
	UserID int64
	// RetryAfter is how long Telegram told the account to wait.
	RetryAfter time.Duration
	// Banned reports that Telegram limits this account (PEER_FLOOD), which
	// lasts far longer than a flood wait.
	Banned bool
}

// Messenger is the account: the real one is internal/account, on TDLib.
type Messenger interface {
	// Session returns one of the Session* states.
	Session() string
	// Deliver imports the contact and sends it text.
	Deliver(ctx context.Context, phone, text string) Delivery
	// RemoveContacts removes imported contacts.
	RemoveContacts(ctx context.Context, userIDs []int64) error
}

// Limits decide how much the account does. A fresh account that writes to
// strangers is what Telegram restricts, and how much it tolerates is not
// documented: the limits are deliberately low for a server of about 100
// people.
type Limits struct {
	MinGap, MaxJitter  time.Duration // pause between sends: MinGap + [0, MaxJitter)
	PerRecipientHour   int
	PerRecipientDay    int
	PerHour, PerDay    int
	QueueLength        int
	MaxWaitInQueue     time.Duration
	DeliverTimeout     time.Duration
	BannedCooldown     time.Duration // no sends after PEER_FLOOD
	RemovalRetryPeriod time.Duration
}

// DefaultLimits are the production limits. Codes per phone: the app lets a
// user ask again after a minute, three in an hour is more than one login needs.
var DefaultLimits = Limits{
	MinGap:             5 * time.Second,
	MaxJitter:          5 * time.Second,
	PerRecipientHour:   3,
	PerRecipientDay:    8,
	PerHour:            30,
	PerDay:             120,
	QueueLength:        32,
	MaxWaitInQueue:     3 * time.Minute,
	DeliverTimeout:     45 * time.Second,
	BannedCooldown:     24 * time.Hour,
	RemovalRetryPeriod: time.Minute,
}

// persisted is the file of the volume. Phones are not in it, only their
// HMACs under a random salt of the same file.
type persisted struct {
	Salt         string             `json:"salt"`
	NotBefore    int64              `json:"not_before"` // unix seconds: flood wait or cooldown
	Recipients   map[string][]int64 `json:"recipients"`
	Sends        []int64            `json:"sends"`
	ToRemove     []int64            `json:"to_remove"`
	LastDelivery int64              `json:"last_delivery"`
}

type item struct {
	phone, code string
	accepted    time.Time
	done        chan Result
}

// Queue holds the codes waiting to be sent. It is safe for concurrent use.
type Queue struct {
	account Messenger
	limits  Limits
	path    string
	items   chan *item
	now     func() time.Time

	mu sync.Mutex
	st persisted
}

// State is what GET /v1/state shows the panel.
type State struct {
	Session          string `json:"session"`
	Sendable         bool   `json:"sendable"`
	LimitedUntil     int64  `json:"limited_until,omitempty"` // unix seconds
	QueueLength      int    `json:"queue_length"`
	SentLastHour     int    `json:"sent_last_hour"`
	SentLastDay      int    `json:"sent_last_day"`
	ContactsToRemove int    `json:"contacts_to_remove"`
}

// Open loads the state file at path (or starts a new one). An unreadable file
// is an error, not a fresh start: its limits and flood wait protect the account.
func Open(account Messenger, limits Limits, path string) (*Queue, error) {
	q := &Queue{
		account: account,
		limits:  limits,
		path:    path,
		items:   make(chan *item, limits.QueueLength),
		now:     time.Now,
	}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, fmt.Errorf("read the sender state: %w", err)
	default:
		if err := json.Unmarshal(data, &q.st); err != nil {
			return nil, fmt.Errorf("the sender state %s is damaged (remove it to reset the limits): %w", path, err)
		}
	}
	if q.st.Salt == "" {
		salt := make([]byte, 32)
		if _, err := rand.Read(salt); err != nil {
			return nil, err
		}
		q.st.Salt = hex.EncodeToString(salt)
	}
	if q.st.Recipients == nil {
		q.st.Recipients = map[string][]int64{}
	}
	return q, q.save()
}

// save writes the state atomically; called with mu held, or before the queue
// runs.
func (q *Queue) save() error {
	data, err := json.Marshal(q.st)
	if err != nil {
		return err
	}
	tmp := q.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, q.path)
}

func (q *Queue) saveOrLog() {
	if err := q.save(); err != nil {
		metricStateSaveErrors.Inc()
		log.Printf("sender state: %v", err)
	}
}

func (q *Queue) recipientKey(phone string) string {
	m := hmac.New(sha256.New, []byte(q.st.Salt))
	m.Write([]byte(phone))
	return hex.EncodeToString(m.Sum(nil)[:16])
}

// prune keeps the timestamps within the last day.
func prune(times []int64, now int64) []int64 {
	limit := now - 24*3600
	kept := times[:0]
	for _, t := range times {
		if t > limit {
			kept = append(kept, t)
		}
	}
	return kept
}

func countSince(times []int64, since int64) int {
	n := 0
	for _, t := range times {
		if t > since {
			n++
		}
	}
	return n
}

// reserve applies every limit and counts the code under them.
func (q *Queue) reserve(phone string) Result {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.now().Unix()
	if now < q.st.NotBefore {
		return AccountLimited
	}
	q.st.Sends = prune(q.st.Sends, now)
	if countSince(q.st.Sends, now-3600) >= q.limits.PerHour || len(q.st.Sends) >= q.limits.PerDay {
		return AccountLimited
	}
	key := q.recipientKey(phone)
	times := prune(q.st.Recipients[key], now)
	if len(times) == 0 {
		delete(q.st.Recipients, key)
	}
	if countSince(times, now-3600) >= q.limits.PerRecipientHour || len(times) >= q.limits.PerRecipientDay {
		return RecipientLimited
	}
	q.st.Recipients[key] = append(times, now)
	q.st.Sends = append(q.st.Sends, now)
	q.saveOrLog()
	return ""
}

func (q *Queue) refund(phone string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	key := q.recipientKey(phone)
	if t := q.st.Recipients[key]; len(t) > 0 {
		q.st.Recipients[key] = t[:len(t)-1]
	}
	if n := len(q.st.Sends); n > 0 {
		q.st.Sends = q.st.Sends[:n-1]
	}
	q.saveOrLog()
}

// Submit queues a code for phone and waits up to wait for the result. A code
// still queued then answers Queued and goes out when its turn comes; the
// caller's context does not cancel it.
func (q *Queue) Submit(ctx context.Context, phone, code string, wait time.Duration) Result {
	if q.account.Session() != SessionReady {
		metricSent.WithLabelValues(string(Unavailable)).Inc()
		return Unavailable
	}
	if r := q.reserve(phone); r != "" {
		metricSent.WithLabelValues(string(r)).Inc()
		return r
	}
	it := &item{phone: phone, code: code, accepted: q.now(), done: make(chan Result, 1)}
	select {
	case q.items <- it:
	default:
		q.refund(phone)
		metricSent.WithLabelValues(string(Busy)).Inc()
		return Busy
	}
	metricQueue.Set(float64(len(q.items)))
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case r := <-it.done:
		return r
	case <-timer.C:
		return Queued
	case <-ctx.Done():
		return Queued
	}
}

func finish(it *item, r Result) {
	metricSent.WithLabelValues(string(r)).Inc()
	it.done <- r
}

// Run sends the queued codes until ctx is cancelled. The pause is between any
// two sends, with a random part, so that the account does not write like a
// machine; after a restart it starts from the last send recorded.
func (q *Queue) Run(ctx context.Context) {
	retry := time.NewTicker(q.limits.RemovalRetryPeriod)
	defer retry.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-retry.C:
			q.retryRemovals(ctx)
			q.publish()
		case it := <-q.items:
			metricQueue.Set(float64(len(q.items)))
			if !q.pause(ctx) {
				finish(it, Unavailable)
				return
			}
			q.deliver(ctx, it)
			q.publish()
		}
	}
}

// pause waits for the end of the pause after the last send.
func (q *Queue) pause(ctx context.Context) bool {
	q.mu.Lock()
	last := q.st.LastDelivery
	q.mu.Unlock()
	if last == 0 {
		return true
	}
	gap := q.limits.MinGap
	if q.limits.MaxJitter > 0 {
		gap += time.Duration(mrand.Int64N(int64(q.limits.MaxJitter)))
	}
	wait := time.UnixMilli(last).Add(gap).Sub(q.now())
	if wait <= 0 {
		return true
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func (q *Queue) deliver(ctx context.Context, it *item) {
	if q.now().Sub(it.accepted) > q.limits.MaxWaitInQueue {
		finish(it, Expired)
		return
	}
	q.mu.Lock()
	blocked := q.now().Unix() < q.st.NotBefore
	q.mu.Unlock()
	if blocked {
		finish(it, AccountLimited)
		return
	}

	dctx, cancel := context.WithTimeout(ctx, q.limits.DeliverTimeout)
	defer cancel()
	d := q.account.Deliver(dctx, it.phone, text(it.code))

	q.mu.Lock()
	q.st.LastDelivery = q.now().UnixMilli()
	switch {
	case d.Banned:
		q.st.NotBefore = q.now().Add(q.limits.BannedCooldown).Unix()
	case d.RetryAfter > 0:
		q.st.NotBefore = q.now().Add(d.RetryAfter).Unix()
	}
	q.saveOrLog()
	q.mu.Unlock()

	// The message comes first: only then the contact goes.
	finish(it, d.Result)
	if d.UserID != 0 {
		if err := q.account.RemoveContacts(ctx, []int64{d.UserID}); err != nil {
			metricRemovalErrors.Inc()
			log.Printf("sender: an imported contact could not be removed, will retry")
			q.mu.Lock()
			q.st.ToRemove = append(q.st.ToRemove, d.UserID)
			q.saveOrLog()
			q.mu.Unlock()
		}
	}
}

// retryRemovals retries the contacts that could not be removed.
func (q *Queue) retryRemovals(ctx context.Context) {
	q.mu.Lock()
	ids := append([]int64(nil), q.st.ToRemove...)
	q.mu.Unlock()
	if len(ids) == 0 || q.account.Session() != SessionReady {
		return
	}
	rctx, cancel := context.WithTimeout(ctx, q.limits.DeliverTimeout)
	defer cancel()
	if err := q.account.RemoveContacts(rctx, ids); err != nil {
		metricRemovalErrors.Inc()
		return
	}
	q.mu.Lock()
	q.st.ToRemove = q.st.ToRemove[len(ids):]
	q.saveOrLog()
	q.mu.Unlock()
}

// State returns the state for the panel, and sets the gauges.
func (q *Queue) State() State {
	session := q.account.Session()
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.now().Unix()
	st := State{
		Session:          session,
		QueueLength:      len(q.items),
		SentLastHour:     countSince(q.st.Sends, now-3600),
		SentLastDay:      countSince(q.st.Sends, now-24*3600),
		ContactsToRemove: len(q.st.ToRemove),
	}
	if q.st.NotBefore > now {
		st.LimitedUntil = q.st.NotBefore
	}
	st.Sendable = session == SessionReady && st.LimitedUntil == 0
	return st
}

// publish refreshes the gauges from the state.
func (q *Queue) publish() {
	st := q.State()
	sessionReady := 0.0
	if st.Session == SessionReady {
		sessionReady = 1
	}
	limited := 0.0
	if st.LimitedUntil != 0 {
		limited = 1
	}
	metricSessionReady.Set(sessionReady)
	metricAccountLimited.Set(limited)
	metricQueue.Set(float64(st.QueueLength))
	metricContactsToRemove.Set(float64(st.ContactsToRemove))
}

// Publish refreshes the gauges; main calls it periodically so that a lost
// session shows with no codes going through.
func (q *Queue) Publish() { q.publish() }

// StatePath is the file of the sender's state within the data directory.
func StatePath(dataDir string) string {
	return filepath.Join(dataDir, "sender-state.json")
}

// text is the message the account sends: Russian, as all people of the
// server read it, with one digit run so that clients offer to copy it.
func text(code string) string {
	return "Код для входа в Pluma: " + code + "\n\nНикому его не сообщайте. Если вы не запрашивали код, просто проигнорируйте это сообщение."
}
