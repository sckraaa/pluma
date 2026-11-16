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

package enginetest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/teamgram/teamgram-server/pkg/translate"
)

// prefillEngine is the client of the Prefill variant: the request of the
// server's client (translate.HTTPEngine) with an assistant message that
// starts the answer of a tagged prompt. Only the measurement uses it.
type prefillEngine struct {
	url, token, model string
	timeout, perChar  time.Duration
	client            *http.Client
}

func newPrefillEngine(cfg translate.Config) *prefillEngine {
	return &prefillEngine{
		url:     strings.TrimRight(cfg.URL, "/") + "/v1/chat/completions",
		token:   cfg.Token,
		model:   cfg.Model,
		timeout: cfg.Timeout,
		perChar: cfg.TimeoutPerChar,
		client:  &http.Client{},
	}
}

func (e *prefillEngine) Translate(ctx context.Context, req translate.Request) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout+time.Duration(req.Chars)*e.perChar)
	defer cancel()
	messages := []map[string]string{{"role": "user", "content": req.Prompt}}
	if strings.Contains(req.Prompt, "<source>") {
		messages = append(messages, map[string]string{"role": "assistant", "content": "<target>"})
	}
	body, err := json.Marshal(map[string]any{
		"model": e.model, "messages": messages, "stream": false,
		"temperature": req.Temperature, "top_p": req.TopP, "top_k": req.TopK,
		"repeat_penalty": req.RepeatPenalty, "max_tokens": req.MaxTokens, "stop": req.Stop,
	})
	if err != nil {
		return "", err
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	hreq.Header.Set("Content-Type", "application/json")
	if e.token != "" {
		hreq.Header.Set("Authorization", "Bearer "+e.token)
	}
	resp, err := e.client.Do(hreq)
	if err != nil {
		return "", fmt.Errorf("%w: %w", translate.ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		if resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusTooManyRequests {
			return "", fmt.Errorf("%w: status %d", translate.ErrRejected, resp.StatusCode)
		}
		return "", fmt.Errorf("%w: status %d", translate.ErrUnavailable, resp.StatusCode)
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil || len(out.Choices) == 0 {
		return "", fmt.Errorf("%w: answer: %v", translate.ErrUnavailable, err)
	}
	if out.Choices[0].FinishReason == "length" {
		return "", fmt.Errorf("%w: cut by max_tokens", translate.ErrBadAnswer)
	}
	return out.Choices[0].Message.Content, nil
}
