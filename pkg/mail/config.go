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

package mail

import (
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"strings"
)

// ProviderResend is the value of Config.Provider for Resend.
const ProviderResend = "resend"

// DefaultDailyLimit is the default Config.DailyLimit: below the 100 emails
// a day of Resend's free plan, so that one person cannot use up the day of
// everyone (ADR 0058, p. 3).
const DefaultDailyLimit = 80

// ErrMailMisconfigured is returned by New for a key without a usable sender
// address or for an unknown provider: the server must not start half
// configured.
var ErrMailMisconfigured = errors.New("mail: MAIL_API_KEY is set, but MAIL_FROM is not a valid address or Mail.Provider is unknown (docs/ops/email.md)")

// Config is the mail of the server (the Mail section of bff.yaml,
// docs/ops/email.md). Mail is off when ApiKey is empty.
type Config struct {
	// Provider is the provider of the key: "resend" (the default).
	Provider string `json:",default=resend"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// ApiKey is the provider's key, ${MAIL_API_KEY}: a Resend key with
	// sending access to the domain of From only. Empty: no mail.
	ApiKey string
	// From is the sender, ${MAIL_FROM}: "Pluma <noreply@pluma.chat>".
	From string
	// ReplyTo is the address answers go to; empty: none.
	ReplyTo string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Endpoint replaces the provider's API address (the test stand's fake
	// provider); empty: the provider's own.
	Endpoint string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// DailyLimit is how many emails the whole server sends in a UTC day.
	DailyLimit int64 `json:",default=80"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// Enabled reports whether c turns mail on.
func (c Config) Enabled() bool { return c.ApiKey != "" }

// New returns the Sender of c, or nil if mail is off (no key). A key with
// an unusable From, ReplyTo, Endpoint or Provider is ErrMailMisconfigured.
func New(c Config) (Sender, error) {
	if !c.Enabled() {
		return nil, nil
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	switch c.Provider {
	case "", ProviderResend:
		return NewResend(c.ApiKey, c.From, c.ReplyTo, c.Endpoint), nil
	default:
		return nil, fmt.Errorf("%w: provider %q", ErrMailMisconfigured, c.Provider)
	}
}

func (c Config) validate() error {
	if a, err := mail.ParseAddress(c.From); err != nil || a.Address == "" || strings.ContainsAny(c.From, "\r\n") {
		return fmt.Errorf("%w: MAIL_FROM", ErrMailMisconfigured)
	}
	if c.ReplyTo != "" {
		if a, err := mail.ParseAddress(c.ReplyTo); err != nil || a.Address == "" || strings.ContainsAny(c.ReplyTo, "\r\n") {
			return fmt.Errorf("%w: Mail.ReplyTo", ErrMailMisconfigured)
		}
	}
	if c.Endpoint != "" {
		u, err := url.Parse(c.Endpoint)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return fmt.Errorf("%w: Mail.Endpoint", ErrMailMisconfigured)
		}
	}
	if c.DailyLimit < 0 {
		return fmt.Errorf("%w: Mail.DailyLimit", ErrMailMisconfigured)
	}
	return nil
}
