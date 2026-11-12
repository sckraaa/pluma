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

// Package transcribe is the server side of voice transcription (task 090,
// ADR 0056): the configuration, the speech engine behind an interface and
// its HTTP client, the decoder of what clients upload, the tables of jobs
// and requests, and the metrics. The handlers and the worker are in
// app/bff/transcription.
//
// The server knows a URL, a token, the name of a model and a language,
// nothing about the model itself: another model or another engine is a
// change of the configuration (docs/ops/transcription.md).
package transcribe

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// MinTokenLength is the shortest accepted Config.Token, in bytes.
const MinTokenLength = 32

// maxModelLength is the width of the model column of the tables.
const maxModelLength = 64

// Config is the Transcribe block of bff.yaml. Everything is optional: no
// URL, no transcription (messages.transcribeAudio answers
// TRANSCRIPTION_FAILED); a zero of the rest is its default.
//
// Nothing here bounds what users ask for (the owner's decision of
// 2026-11-12, ADR 0056): a record of any length is transcribed, as many as
// anyone asks, in the order they were asked. What is bounded is the
// engine's load: Workers records at a time, each with the time its real
// length needs.
type Config struct {
	// URL is the base of the engine, a service with POST
	// /v1/audio/transcriptions of the OpenAI API: http://stt:9379.
	URL string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Token is the bearer token of the engine; none if empty.
	Token string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Model is the name the engine knows its model by. It is part of the
	// key of a stored text: after a change every record is transcribed
	// again when asked, and the old texts are kept, not served.
	Model string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Language is the language of the speech, for the engines that are
	// told it; "" leaves it to the engine.
	Language string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Timeout is how long the engine has for a record, as
	// "<duration>+<factor>x": that much plus factor seconds for every
	// second of sound. "" is DefaultTimeout. A slower engine, or one that
	// queues the requests of others, gets more here (ParseTimeout).
	Timeout string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's

	// SyncWait is how long messages.transcribeAudio waits for the text
	// before it answers "pending" (3s).
	SyncWait time.Duration `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// DeliverDelay is the least time between a "pending" answer and the
	// update with the text (1s): a client must know the id first.
	DeliverDelay time.Duration `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Workers is how many records are given to the engine at once: the
	// number of recognitions it runs side by side (1: app/ops/stt runs
	// one). More workers than that only wait for each other's turn.
	Workers int `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// MaxAttempts is how many attempts that reached the engine and came
	// to nothing a record is given (4). Waiting for an engine that is down
	// or busy is not an attempt: nothing a user asks for is dropped for
	// the engine's sake.
	MaxAttempts int `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// FailedTTL is how long a failure that may pass is answered as a
	// failure before the record is tried again (10m).
	FailedTTL time.Duration `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// DefaultTimeout is the time of an engine that runs several times faster
// than the speech, as GigaAM on two CPUs does (docs/ops/transcription.md has
// the measurements).
const DefaultTimeout = "30s+0.5x"

// Timeout is how long the engine has for a record: Base plus PerSecond
// times its length.
type Timeout struct {
	Base      time.Duration
	PerSecond float64
}

// For is the time the engine has for a record that long.
func (t Timeout) For(duration time.Duration) time.Duration {
	return t.Base + time.Duration(t.PerSecond*float64(duration))
}

// ParseTimeout reads "<duration>+<factor>x", as "30s+0.5x" or "2m+3x".
// The base is from a second to an hour, the factor from 0 to 20.
func ParseTimeout(s string) (Timeout, error) {
	bad := fmt.Errorf("transcribe: STT_TIMEOUT %q is not <duration>+<factor>x, as %s (docs/ops/transcription.md)", s, DefaultTimeout)
	base, factor, ok := strings.Cut(strings.TrimSpace(s), "+")
	factor, isFactor := strings.CutSuffix(strings.TrimSpace(factor), "x")
	if !ok || !isFactor {
		return Timeout{}, bad
	}
	var (
		t   Timeout
		err error
	)
	if t.Base, err = time.ParseDuration(strings.TrimSpace(base)); err != nil || t.Base < time.Second || t.Base > time.Hour {
		return Timeout{}, bad
	}
	if t.PerSecond, err = strconv.ParseFloat(strings.TrimSpace(factor), 64); err != nil || math.IsNaN(t.PerSecond) || t.PerSecond < 0 || t.PerSecond > 20 {
		return Timeout{}, bad
	}
	return t, nil
}

// EngineTimeout is the parsed Timeout of a valid configuration.
func (c Config) EngineTimeout() Timeout {
	spec := c.Timeout
	if spec == "" {
		spec = DefaultTimeout
	}
	t, err := ParseTimeout(spec)
	if err != nil {
		// Validate refuses such a configuration before it is used.
		t, _ = ParseTimeout(DefaultTimeout)
	}
	return t
}

// Enabled reports whether an engine is configured.
func (c Config) Enabled() bool { return c.URL != "" }

// WithDefaults returns c with every zero setting replaced by its default.
func (c Config) WithDefaults() Config {
	if c.SyncWait == 0 {
		c.SyncWait = 3 * time.Second
	}
	if c.DeliverDelay == 0 {
		c.DeliverDelay = time.Second
	}
	if c.Workers == 0 {
		c.Workers = 1
	}
	if c.MaxAttempts == 0 {
		c.MaxAttempts = 4
	}
	if c.FailedTTL == 0 {
		c.FailedTTL = 10 * time.Minute
	}
	return c
}

// Validate checks a configuration with its defaults in.
func (c Config) Validate() error {
	if !c.Enabled() {
		return nil
	}
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("transcribe: STT_URL must be an http(s) URL (docs/ops/transcription.md)")
	}
	if c.Model == "" || len(c.Model) > maxModelLength {
		return fmt.Errorf("transcribe: STT_MODEL is required with STT_URL, up to %d characters (docs/ops/transcription.md)", maxModelLength)
	}
	if c.Token != "" && len(c.Token) < MinTokenLength {
		return fmt.Errorf("transcribe: STT_TOKEN must be at least %d characters (openssl rand -hex 32)", MinTokenLength)
	}
	if c.Timeout != "" {
		if _, err := ParseTimeout(c.Timeout); err != nil {
			return err
		}
	}
	switch {
	case c.SyncWait < 0, c.DeliverDelay < 0, c.Workers < 1, c.Workers > 16,
		c.MaxAttempts < 1, c.MaxAttempts > len(retryPauses)+1, c.FailedTTL < 0:
		return errors.New("transcribe: a setting of the Transcribe block is out of range (docs/ops/transcription.md)")
	}
	return nil
}
