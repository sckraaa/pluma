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

// Package mail sends the server's emails: the codes of the recovery email of
// the cloud password (task 091, ADR 0058). A Sender hands one message to a
// provider; the first provider is Resend (resend.go). Another provider is
// another Sender and a value of Config.Provider.
//
// Nothing of a message is logged here: no address, subject or body. The
// caller logs the provider's id of a message and the code of an error.
package mail

import (
	"context"
	"errors"
	"fmt"
)

// Sender hands messages to a mail provider. It is safe for concurrent use.
type Sender interface {
	// Send hands m to the provider and returns the provider's id of it. An
	// error is an *Error, or the context's error.
	Send(ctx context.Context, m Message) (id string, err error)
}

// Message is one email.
type Message struct {
	To      string // one bare address
	Subject string
	Text    string // the plain text part
	HTML    string // the HTML part; may be empty
	// IdempotencyKey makes repeated sends of one message one email at the
	// provider (Resend keeps it 24 hours).
	IdempotencyKey string
	// Tag names the kind of the message at the provider, for its logs.
	Tag string
}

// Error is a send the provider did not accept.
type Error struct {
	// Code is the provider's name of the error ("rate_limit_exceeded"),
	// "http_<status>" when it gave none, or "network".
	Code string
	// Status is the HTTP status of the answer; 0 if there was none.
	Status int
	// Retryable reports whether the same message may be sent again soon
	// with a chance of success (a rate limit, a failure of the provider, the
	// network). Other errors repeat until the operator acts: a revoked key,
	// an unverified domain, a quota used up.
	Retryable bool
}

func (e *Error) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("mail: provider error %s (HTTP %d, retryable %v)", e.Code, e.Status, e.Retryable)
	}
	return fmt.Sprintf("mail: provider error %s (retryable %v)", e.Code, e.Retryable)
}

// Retryable reports whether err is an *Error worth repeating.
func Retryable(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Retryable
}
