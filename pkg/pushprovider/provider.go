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

// Package pushprovider sends pushes to Apple (APNs) and Google (FCM HTTP v1)
// (task 009, ADR 0017). A send is retried while the provider is temporarily
// unable, within the caller's deadline; a provider that keeps failing is
// skipped for a while instead of holding every push (breaker).
package pushprovider

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Outcome is how a send ended, the label of the metrics.
type Outcome string

const (
	// OutcomeSent: the provider accepted the push.
	OutcomeSent Outcome = "sent"
	// OutcomeTokenInvalid: the token is no longer valid; the device is to
	// be forgotten.
	OutcomeTokenInvalid Outcome = "token_invalid"
	// OutcomeAuthFailed: the provider refused our credentials. Needs the
	// owner: an alert fires.
	OutcomeAuthFailed Outcome = "auth_failed"
	// OutcomeRejected: the provider refused the request itself (a bug).
	OutcomeRejected Outcome = "rejected"
	// OutcomeUnavailable: the provider failed after the retries, or is
	// skipped for a while; the push is dropped.
	OutcomeUnavailable Outcome = "unavailable"
	// OutcomeDisabled: the provider is not configured.
	OutcomeDisabled Outcome = "disabled"
)

// Retries of a temporary failure.
const (
	attempts       = 3
	breakerFails   = 5
	breakerOpenFor = 30 * time.Second
)

// firstPause is the pause before the second attempt, tripled before the
// third; a variable for the tests.
var firstPause = 200 * time.Millisecond

// attempt is one try: the outcome and whether it may be tried again; after
// a positive Retry-After, not sooner.
type attempt struct {
	outcome    Outcome
	retry      bool
	retryAfter time.Duration
	err        error
}

// errBreakerOpen is the error of a send skipped by the breaker.
var errBreakerOpen = errors.New("provider: failing, skipped for a while")

// sendWithRetries runs try until it is final, attempts times at most and
// within the deadline of ctx.
func sendWithRetries(ctx context.Context, b *breaker, try func(ctx context.Context) attempt) (Outcome, error) {
	if !b.allow() {
		return OutcomeUnavailable, errBreakerOpen
	}
	pause := firstPause
	var last attempt
	for i := 0; i < attempts; i++ {
		last = try(ctx)
		if !last.retry {
			break
		}
		wait := pause
		if last.retryAfter > wait {
			wait = last.retryAfter
		}
		if deadline, ok := ctx.Deadline(); i == attempts-1 || ok && time.Until(deadline) < wait {
			break
		}
		select {
		case <-ctx.Done():
			i = attempts
		case <-time.After(wait):
		}
		pause *= 3
	}
	if last.retry {
		last.outcome = OutcomeUnavailable
	}
	b.record(last.outcome != OutcomeUnavailable)
	return last.outcome, last.err
}

// retryAfter reads a Retry-After header in seconds.
func retryAfter(h http.Header) time.Duration {
	s, err := strconv.Atoi(h.Get("Retry-After"))
	if err != nil || s <= 0 {
		return 0
	}
	return time.Duration(s) * time.Second
}

// breaker skips a provider for breakerOpenFor after breakerFails sends in a
// row ended unavailable; then one send tries again.
type breaker struct {
	mu        sync.Mutex
	fails     int
	openUntil time.Time
	now       func() time.Time
}

func newBreaker() *breaker { return &breaker{now: time.Now} }

func (b *breaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.openUntil.IsZero() || b.now().After(b.openUntil) {
		return true
	}
	return false
}

func (b *breaker) record(ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ok {
		b.fails, b.openUntil = 0, time.Time{}
		return
	}
	b.fails++
	if b.fails >= breakerFails {
		b.openUntil = b.now().Add(breakerOpenFor)
	}
}
