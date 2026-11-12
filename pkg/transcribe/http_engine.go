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

package transcribe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"strings"
	"time"
)

// maxAnswer bounds the answer of the engine. An hour of speech is about
// 150 KB of text; this is ten times more than a day of it.
const maxAnswer = 32 << 20

// httpEngine is the client of POST /v1/audio/transcriptions, the
// transcription endpoint of the OpenAI API as LocalAI, speaches and
// app/ops/stt serve it (ADR 0056, p. 2).
type httpEngine struct {
	url, token, model, language string
	timeout                     Timeout
	client                      *http.Client
}

// expectContinue is how long a request waits for "100 Continue" before it
// sends the sound anyway, to an engine that does not answer it.
const expectContinue = time.Second

// NewHTTPEngine returns the engine of c, which must be enabled and valid.
func NewHTTPEngine(c Config) Engine {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ExpectContinueTimeout = expectContinue
	return &httpEngine{
		url:      strings.TrimRight(c.URL, "/") + "/v1/audio/transcriptions",
		token:    c.Token,
		model:    c.Model,
		language: c.Language,
		timeout:  c.EngineTimeout(),
		// The deadline of a request is its context's (Config.Timeout).
		client: &http.Client{Transport: transport},
	}
}

func (e *httpEngine) Transcribe(ctx context.Context, wav io.Reader, duration time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout.For(duration))
	defer cancel()

	// The form is written as the engine reads it: the sound is never
	// whole in memory. The fields go first, for an engine that reads the
	// form as a stream.
	pr, pw := io.Pipe()
	form := multipart.NewWriter(pw)
	go func() {
		pw.CloseWithError(e.writeForm(form, wav))
	}()
	defer pr.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.url, pr)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	// The sound goes only once the engine has taken the request: a busy
	// engine (503) or one that refuses the token answers before an hour of
	// it is on the wire, and the answer is that, not a broken write.
	req.Header.Set("Expect", "100-continue")
	if e.token != "" {
		req.Header.Set("Authorization", "Bearer "+e.token)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		var op *net.OpError
		if errors.As(err, &op) && op.Op == "dial" && ctx.Err() == nil {
			// Nothing listens there (stopped, starting, not deployed):
			// the request never reached an engine.
			return "", fmt.Errorf("%w: %w", ErrDown, err)
		}
		return "", fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusBadRequest, http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		// The sound cannot be read. Anything else (the token, the name of
		// the model or the language, a length over another engine's own
		// limit, an overload) is the configuration or the moment, and must
		// not spoil the record for good.
		return "", fmt.Errorf("%w: status %d", ErrRejected, resp.StatusCode)
	case http.StatusServiceUnavailable, http.StatusTooManyRequests:
		return "", fmt.Errorf("%w (status %d)", ErrBusy, resp.StatusCode)
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		// The token, the name of the model or the language (app/ops/stt
		// answers 404 to both): the configuration, until it is fixed.
		return "", fmt.Errorf("%w (status %d)", ErrDown, resp.StatusCode)
	default:
		return "", fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	}
	var out struct {
		Text *string `json:"text"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, maxAnswer)).Decode(&out); err != nil {
		return "", fmt.Errorf("%w: answer: %w", ErrUnavailable, err)
	}
	if out.Text == nil {
		return "", fmt.Errorf("%w: answer without a text", ErrUnavailable)
	}
	return strings.TrimSpace(*out.Text), nil
}

func (e *httpEngine) writeForm(form *multipart.Writer, wav io.Reader) error {
	fields := [][2]string{{"model", e.model}, {"response_format", "json"}}
	if e.language != "" {
		fields = append(fields, [2]string{"language", e.language})
	}
	for _, f := range fields {
		if err := form.WriteField(f[0], f[1]); err != nil {
			return err
		}
	}
	part, err := form.CreateFormFile("file", "audio.wav")
	if err != nil {
		return err
	}
	if _, err = io.Copy(part, wav); err != nil {
		return err
	}
	return form.Close()
}
