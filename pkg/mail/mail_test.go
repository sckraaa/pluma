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
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testKey = "re_test_0123456789abcdef"

type captured struct {
	method, path string
	header       http.Header
	body         resendRequest
}

// resendStub answers every request with status and body and keeps the
// last request.
func resendStub(t *testing.T, status int, body string) (*httptest.Server, *captured) {
	t.Helper()
	got := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.method, got.path, got.header = r.Method, r.URL.Path, r.Header.Clone()
		data, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(data, &got.body); err != nil {
			t.Errorf("request body: %v", err)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func testMessage() Message {
	return Message{
		To:             "ab@example.com",
		Subject:        "Subject",
		Text:           "text 123456",
		HTML:           "<p>123456</p>",
		IdempotencyKey: "pluma/recovery/42/c0de",
		Tag:            "recovery_code",
	}
}

func TestResendRequest(t *testing.T) {
	srv, got := resendStub(t, http.StatusOK, `{"id":"49a3999c-0ce1-4ea6-ab68-afcd6dc2e794"}`)
	r := NewResend(testKey, "Pluma <noreply@pluma.chat>", "", srv.URL+"/")
	id, err := r.Send(context.Background(), testMessage())
	if err != nil || id != "49a3999c-0ce1-4ea6-ab68-afcd6dc2e794" {
		t.Fatalf("Send: %q %v", id, err)
	}
	if got.method != http.MethodPost || got.path != "/emails" {
		t.Fatalf("request %s %s", got.method, got.path)
	}
	for h, want := range map[string]string{
		"Authorization":   "Bearer " + testKey,
		"User-Agent":      "pluma-server",
		"Content-Type":    "application/json",
		"Idempotency-Key": "pluma/recovery/42/c0de",
	} {
		if v := got.header.Get(h); v != want {
			t.Errorf("%s: %q, want %q", h, v, want)
		}
	}
	b := got.body
	if b.From != "Pluma <noreply@pluma.chat>" || len(b.To) != 1 || b.To[0] != "ab@example.com" ||
		b.Subject != "Subject" || b.Text != "text 123456" || b.HTML != "<p>123456</p>" || b.ReplyTo != "" ||
		len(b.Tags) != 1 || b.Tags[0] != (resendTag{Name: "type", Value: "recovery_code"}) {
		t.Fatalf("body %+v", b)
	}

	srv2, got2 := resendStub(t, http.StatusOK, `{"id":"x"}`)
	m := testMessage()
	m.IdempotencyKey, m.Tag = "", ""
	if _, err = NewResend(testKey, "a@b.c", "help@pluma.chat", srv2.URL).Send(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if got2.header.Get("Idempotency-Key") != "" || got2.body.ReplyTo != "help@pluma.chat" || got2.body.Tags != nil {
		t.Fatalf("optional fields: %v %+v", got2.header, got2.body)
	}
}

// The errors of https://resend.com/docs/api-reference/errors.
func TestResendErrors(t *testing.T) {
	for _, tc := range []struct {
		status    int
		body      string
		code      string
		retryable bool
	}{
		{400, `{"statusCode":400,"name":"validation_error","message":"x"}`, "validation_error", false},
		{401, `{"statusCode":401,"name":"missing_api_key","message":"x"}`, "missing_api_key", false},
		{403, `{"statusCode":403,"name":"invalid_api_key","message":"x"}`, "invalid_api_key", false},
		{403, `{"statusCode":403,"name":"validation_error","message":"domain is not verified"}`, "validation_error", false},
		{404, `{"statusCode":404,"name":"not_found","message":"x"}`, "not_found", false},
		{405, `{"statusCode":405,"name":"method_not_allowed","message":"x"}`, "method_not_allowed", false},
		{409, `{"statusCode":409,"name":"invalid_idempotent_request","message":"x"}`, "invalid_idempotent_request", false},
		{409, `{"statusCode":409,"name":"concurrent_idempotent_requests","message":"x"}`, "concurrent_idempotent_requests", true},
		{422, `{"statusCode":422,"name":"invalid_from_address","message":"x"}`, "invalid_from_address", false},
		{429, `{"statusCode":429,"name":"rate_limit_exceeded","message":"x"}`, "rate_limit_exceeded", true},
		{429, `{"statusCode":429,"name":"daily_quota_exceeded","message":"x"}`, "daily_quota_exceeded", true},
		{429, `{"statusCode":429,"name":"monthly_quota_exceeded","message":"x"}`, "monthly_quota_exceeded", false},
		{500, `{"statusCode":500,"name":"internal_server_error","message":"x"}`, "internal_server_error", true},
		{503, `{"statusCode":503,"name":"service_unavailable","message":"x"}`, "service_unavailable", true},
		// Not JSON, or cut short: the status decides.
		{502, `<html>Bad Gateway</html>`, "http_502", true},
		{401, `{"statusCode":401,"na`, "http_401", false},
		// Accepted, but no id: the same key makes a repeat safe.
		{200, `not json`, "bad_answer", true},
		{200, `{}`, "bad_answer", true},
	} {
		srv, _ := resendStub(t, tc.status, tc.body)
		id, err := NewResend(testKey, "a@b.c", "", srv.URL).Send(context.Background(), testMessage())
		var e *Error
		if !errors.As(err, &e) || e.Code != tc.code || e.Status != tc.status || e.Retryable != tc.retryable || id != "" {
			t.Errorf("%d %s: %q %#v", tc.status, tc.body, id, err)
			continue
		}
		if Retryable(err) != tc.retryable {
			t.Errorf("%d %s: Retryable %v", tc.status, tc.body, Retryable(err))
		}
		if strings.Contains(err.Error(), testKey) {
			t.Errorf("the error shows the key: %v", err)
		}
	}
}

func TestResendNetworkAndContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	_, err := NewResend(testKey, "a@b.c", "", url).Send(context.Background(), testMessage())
	var e *Error
	if !errors.As(err, &e) || e.Code != "network" || !e.Retryable {
		t.Fatalf("a closed server: %#v", err)
	}

	block := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-block }))
	t.Cleanup(slow.Close)
	t.Cleanup(func() { close(block) })
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err = NewResend(testKey, "a@b.c", "", slow.URL).Send(ctx, testMessage()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("past the deadline: %v", err)
	}
}

func TestResendLimitsTheAnswer(t *testing.T) {
	srv, _ := resendStub(t, http.StatusOK, `{"id":"`+strings.Repeat("a", 2*maxAnswer)+`"}`)
	_, err := NewResend(testKey, "a@b.c", "", srv.URL).Send(context.Background(), testMessage())
	var e *Error
	if !errors.As(err, &e) || e.Code != "bad_answer" {
		t.Fatalf("a huge answer: %v", err)
	}
}

func TestNew(t *testing.T) {
	if s, err := New(Config{From: "Pluma <noreply@pluma.chat>"}); s != nil || err != nil {
		t.Fatalf("no key: %v %v", s, err)
	}
	ok := Config{Provider: "resend", ApiKey: testKey, From: "Pluma <noreply@pluma.chat>", DailyLimit: 80}
	if s, err := New(ok); err != nil || s == nil {
		t.Fatalf("resend: %v %v", s, err)
	}
	for name, c := range map[string]Config{
		"no from":         {ApiKey: testKey},
		"bad from":        {ApiKey: testKey, From: "Pluma noreply"},
		"from with crlf":  {ApiKey: testKey, From: "a@b.c\r\nBcc: x@y.z"},
		"bad reply-to":    {ApiKey: testKey, From: "a@b.c", ReplyTo: "nobody"},
		"bad endpoint":    {ApiKey: testKey, From: "a@b.c", Endpoint: "ftp://x"},
		"unknown":         {ApiKey: testKey, From: "a@b.c", Provider: "smtp"},
		"negative limit":  {ApiKey: testKey, From: "a@b.c", DailyLimit: -1},
		"endpoint nohost": {ApiKey: testKey, From: "a@b.c", Endpoint: "http://"},
	} {
		if _, err := New(c); !errors.Is(err, ErrMailMisconfigured) {
			t.Errorf("%s: %v", name, err)
		}
		if _, err := New(c); err != nil && strings.Contains(err.Error(), testKey) {
			t.Errorf("%s: the error shows the key", name)
		}
	}
}
