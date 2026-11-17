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

// Package translate translates the texts of messages with a model of our
// own (ADR 0064): the profile of the model (profile.go), the markup that
// carries the entities through it (markup.go) and the client of the engine
// that runs it (http_engine.go).
package translate

import (
	"context"
	"errors"
	"fmt"
)

// Request is one completion asked of the engine: a prompt the profile made
// and the sampling of the profile.
type Request struct {
	// Prompt is the one message of role "user": the model has no system
	// prompt (HY-MT1.5 model card).
	Prompt        string
	Temperature   float64
	TopP          float64
	TopK          int
	RepeatPenalty float64
	MaxTokens     int
	// Stop ends the completion at any of these; may be empty.
	Stop []string
	// Chars is the length of the text to translate in characters; the
	// deadline of the request grows with it (ADR 0064, section 6).
	Chars int
}

// Engine completes prompts. It is the seam for the tests and for an engine
// with another protocol; the one implementation is NewHTTPEngine.
type Engine interface {
	// Translate returns the completion of req as the model wrote it. An
	// error wraps ErrUnavailable (try again later), ErrRejected (this
	// request will never be taken) or ErrBadAnswer (the answer is not a
	// translation; the item may be tried again).
	Translate(ctx context.Context, req Request) (string, error)
}

// HealthChecker is an engine that tells whether it can take requests now,
// without one. After ErrUnavailable new items wait until it says so (ADR
// 0064, section 6).
type HealthChecker interface {
	Health(ctx context.Context) error
}

var (
	// ErrUnavailable: the engine did not answer, or answered that it
	// cannot now. The same request may be sent again.
	ErrUnavailable = errors.New("translate: the engine is unavailable")
	// ErrBusy is ErrUnavailable of an engine that is serving others or
	// still loading its model (503, 429); it says nothing about this
	// request.
	ErrBusy = fmt.Errorf("%w: busy", ErrUnavailable)
	// ErrDown is ErrUnavailable of an engine that cannot be used at all
	// now: nothing answers at its address, or it refuses the token or the
	// path of the configuration (401, 403, 404). It says nothing about this
	// request either; it lasts until the engine is up or the configuration
	// is fixed.
	ErrDown = fmt.Errorf("%w: down", ErrUnavailable)
	// ErrRejected: the engine refused this request (4xx other than the
	// above), e.g. a prompt over the context of a slot.
	ErrRejected = errors.New("translate: the engine rejected the request")
)
