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
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ResendEndpoint is the API of Resend.
const ResendEndpoint = "https://api.resend.com"

// userAgent names the client: Resend refuses requests without a User-Agent
// (403, error 1010; https://resend.com/docs/api-reference/introduction).
const userAgent = "pluma-server"

// maxAnswer is the most of an answer read: {"id"} or {name, message}.
const maxAnswer = 16 << 10

// resendTimeout bounds one request when the caller's context has no
// deadline.
const resendTimeout = 10 * time.Second

// Resend sends through POST /emails of Resend
// (https://resend.com/docs/api-reference/emails/send-email).
type Resend struct {
	key, from, replyTo string
	endpoint           string
	client             *http.Client
}

// NewResend returns the Sender of Resend with key, sending from from (and
// replyTo, if not empty); endpoint replaces ResendEndpoint if not empty.
func NewResend(key, from, replyTo, endpoint string) *Resend {
	if endpoint == "" {
		endpoint = ResendEndpoint
	}
	return &Resend{
		key:      key,
		from:     from,
		replyTo:  replyTo,
		endpoint: strings.TrimRight(endpoint, "/"),
		client:   &http.Client{Timeout: resendTimeout},
	}
}

type resendTag struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type resendRequest struct {
	From    string      `json:"from"`
	To      []string    `json:"to"`
	Subject string      `json:"subject"`
	Text    string      `json:"text"`
	HTML    string      `json:"html,omitempty"`
	ReplyTo string      `json:"reply_to,omitempty"`
	Tags    []resendTag `json:"tags,omitempty"`
}

// Send implements Sender.
func (r *Resend) Send(ctx context.Context, m Message) (string, error) {
	req := resendRequest{
		From:    r.from,
		To:      []string{m.To},
		Subject: m.Subject,
		Text:    m.Text,
		HTML:    m.HTML,
		ReplyTo: r.replyTo,
	}
	if m.Tag != "" {
		req.Tags = []resendTag{{Name: "type", Value: m.Tag}}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", &Error{Code: "encode"}
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, r.endpoint+"/emails", bytes.NewReader(body))
	if err != nil {
		return "", &Error{Code: "request"}
	}
	hr.Header.Set("Authorization", "Bearer "+r.key)
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("User-Agent", userAgent)
	if m.IdempotencyKey != "" {
		hr.Header.Set("Idempotency-Key", m.IdempotencyKey)
	}

	resp, err := r.client.Do(hr)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		// The request may or may not have reached Resend; the idempotency
		// key makes a repeat safe.
		return "", &Error{Code: "network", Retryable: true}
	}
	defer resp.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxAnswer))

	if resp.StatusCode/100 == 2 {
		var ok struct {
			Id string `json:"id"`
		}
		if readErr != nil || json.Unmarshal(data, &ok) != nil || ok.Id == "" {
			// Accepted, but the answer is unreadable: a repeat with the
			// same idempotency key gets the same answer, never a second
			// email.
			return "", &Error{Code: "bad_answer", Status: resp.StatusCode, Retryable: true}
		}
		return ok.Id, nil
	}
	var e struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(data, &e) // an answer without a name keeps the status
	return "", resendError(resp.StatusCode, e.Name)
}

// resendError classifies an error of Resend
// (https://resend.com/docs/api-reference/errors): a rate limit, the
// daily quota, a request the same idempotency key is still running and a
// failure of Resend are worth repeating; the rest (a wrong key, an
// unverified domain, the monthly quota, a bad request) are not.
func resendError(status int, name string) *Error {
	e := &Error{Code: name, Status: status}
	if e.Code == "" {
		e.Code = "http_" + strconv.Itoa(status)
	}
	switch {
	case status == http.StatusTooManyRequests:
		e.Retryable = name != "monthly_quota_exceeded"
	case status == http.StatusConflict:
		e.Retryable = name == "concurrent_idempotent_requests" || name == "resource_locked"
	case status >= 500:
		e.Retryable = true
	}
	return e
}

var _ Sender = (*Resend)(nil)
