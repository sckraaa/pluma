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

package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	// DefaultEngineTimeout and DefaultEngineTimeoutPerChar make the
	// deadline of one request to the engine: 10 s + 25 ms a character of
	// the text (ADR 0064, section 6).
	DefaultEngineTimeout        = 10 * time.Second
	DefaultEngineTimeoutPerChar = 25 * time.Millisecond

	// healthTimeout bounds a health check whose context has no deadline.
	healthTimeout = 5 * time.Second
	// maxAnswer bounds the answer of the engine. A translation of the
	// longest item (4096 characters) is some tens of KB of JSON.
	maxAnswer = 1 << 20
	// maxDrain is how much of the body of an error answer is read so that
	// the connection can be used again.
	maxDrain = 64 << 10
)

// HTTPConfig is what the client of the engine needs of the configuration.
type HTTPConfig struct {
	// URL is the root of the engine, e.g. http://translate:8080.
	URL string
	// Token, if set, is sent as "Authorization: Bearer".
	Token string
	// Model is the name the engine serves the model by.
	Model string
	// Timeout + TimeoutPerChar × Request.Chars is the deadline of one
	// request; zero takes the default.
	Timeout        time.Duration
	TimeoutPerChar time.Duration
}

// HTTPEngine is the client of POST /v1/chat/completions, the chat endpoint
// of the OpenAI API as llama-server serves it (ADR 0064, section 1).
type HTTPEngine struct {
	completions, health string
	token, model        string
	timeout, perChar    time.Duration
	client              *http.Client
}

var (
	_ Engine        = (*HTTPEngine)(nil)
	_ HealthChecker = (*HTTPEngine)(nil)
)

// NewHTTPEngine returns the engine at c.URL.
func NewHTTPEngine(c HTTPConfig) *HTTPEngine {
	root := strings.TrimRight(c.URL, "/")
	e := &HTTPEngine{
		completions: root + "/v1/chat/completions",
		health:      root + "/health",
		token:       c.Token,
		model:       c.Model,
		timeout:     c.Timeout,
		perChar:     c.TimeoutPerChar,
		// The deadline of a request is its context's.
		client: &http.Client{Transport: http.DefaultTransport.(*http.Transport).Clone()},
	}
	if e.timeout <= 0 {
		e.timeout = DefaultEngineTimeout
	}
	if e.perChar <= 0 {
		e.perChar = DefaultEngineTimeoutPerChar
	}
	return e
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest is the body of a completion. top_k and repeat_penalty are
// not in the OpenAI API; llama-server takes them on this endpoint as well.
type chatRequest struct {
	Model         string        `json:"model"`
	Messages      []chatMessage `json:"messages"`
	Stream        bool          `json:"stream"`
	Temperature   float64       `json:"temperature"`
	TopP          float64       `json:"top_p"`
	TopK          int           `json:"top_k"`
	RepeatPenalty float64       `json:"repeat_penalty"`
	MaxTokens     int           `json:"max_tokens"`
	Stop          []string      `json:"stop,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content *string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

// Translate sends req and returns the content of the first choice as the
// model wrote it; the profile reads it.
func (e *HTTPEngine) Translate(ctx context.Context, req Request) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout+time.Duration(max(req.Chars, 0))*e.perChar)
	defer cancel()

	body, err := json.Marshal(chatRequest{
		Model:         e.model,
		Messages:      []chatMessage{{Role: "user", Content: req.Prompt}},
		Stream:        false,
		Temperature:   req.Temperature,
		TopP:          req.TopP,
		TopK:          req.TopK,
		RepeatPenalty: req.RepeatPenalty,
		MaxTokens:     req.MaxTokens,
		Stop:          req.Stop,
	})
	if err != nil {
		return "", err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.completions, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	hreq.Header.Set("Content-Type", "application/json")
	hreq.Header.Set("Accept", "application/json")
	resp, err := e.do(ctx, hreq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if err = statusError(resp); err != nil {
		return "", err
	}

	var out chatResponse
	if err = json.NewDecoder(io.LimitReader(resp.Body, maxAnswer)).Decode(&out); err != nil {
		// Cut short (the engine went away mid-answer) or not JSON (a
		// proxy's page): the moment, not the request.
		return "", fmt.Errorf("%w: answer: %w", ErrUnavailable, err)
	}
	if len(out.Choices) == 0 || out.Choices[0].Message.Content == nil {
		return "", fmt.Errorf("%w: answer without a content", ErrUnavailable)
	}
	if out.Choices[0].FinishReason == "length" {
		// Cut by max_tokens: a part of a translation, or a model that
		// repeats itself; never given as the translation.
		return "", fmt.Errorf("%w: cut by max_tokens", ErrBadAnswer)
	}
	return *out.Choices[0].Message.Content, nil
}

// Health asks GET /health of the engine: nil once it can take requests.
// llama-server answers 503 there while it loads the model.
func (e *HTTPEngine) Health(ctx context.Context) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, healthTimeout)
		defer cancel()
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodGet, e.health, nil)
	if err != nil {
		return err
	}
	resp, err := e.do(ctx, hreq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return statusError(resp)
}

func (e *HTTPEngine) do(ctx context.Context, hreq *http.Request) (*http.Response, error) {
	if e.token != "" {
		hreq.Header.Set("Authorization", "Bearer "+e.token)
	}
	resp, err := e.client.Do(hreq)
	if err == nil {
		return resp, nil
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" && ctx.Err() == nil {
		// Nothing listens there (stopped, starting, not deployed): the
		// request never reached an engine.
		return nil, fmt.Errorf("%w: %w", ErrDown, err)
	}
	return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
}

// statusError maps the status of an answer to the errors of Engine; nil for
// 200. The body of an error answer is drained, never read into an error: it
// may quote the prompt.
func statusError(resp *http.Response) error {
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDrain))
	switch st := resp.StatusCode; {
	case st == http.StatusServiceUnavailable || st == http.StatusTooManyRequests:
		return fmt.Errorf("%w (status %d)", ErrBusy, st)
	case st == http.StatusUnauthorized || st == http.StatusForbidden || st == http.StatusNotFound:
		// The token or the path of the configuration, until it is fixed;
		// never this request's fault.
		return fmt.Errorf("%w (status %d)", ErrDown, st)
	case st >= 400 && st < 500:
		return fmt.Errorf("%w: status %d", ErrRejected, st)
	default:
		return fmt.Errorf("%w: status %d", ErrUnavailable, st)
	}
}
