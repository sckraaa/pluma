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
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// MinTokenLength is the shortest accepted Config.Token, in bytes.
const MinTokenLength = 32

// The defaults of the Translate block (ADR 0064, sections 6, 7 and 12).
const (
	DefaultProfile = hymtName
	// DefaultSlots is TRANSLATE_SLOTS: llama-server --parallel 4.
	DefaultSlots = 4
	// DefaultContext is TRANSLATE_CTX: the context of the whole engine,
	// shared by its slots (4096 tokens a slot).
	DefaultContext = 16384
	// DefaultSyncWait is how long a batch of a chat is waited for: under
	// the 60 s up to which Web K repeats a FLOOD_WAIT_X by itself, and
	// long enough for 20 items on 4 slots (brief 101, "Замер").
	DefaultSyncWait = 25 * time.Second
	// DefaultSyncWaitSingle is how long one text a person waits for is
	// waited for: the translation screen of iOS shows an error on
	// FLOOD_WAIT and does not repeat it (TelegramCore/.../Messages/
	// Translate.swift:48-64, 919-926), so it gets the longer wait.
	DefaultSyncWaitSingle = 45 * time.Second
	// DefaultFailedTTL is how long an item that failed is answered as
	// failed before it is tried again.
	DefaultFailedTTL = 60 * time.Second
	// DefaultCacheTTL keeps a translation 30 days after it was last read.
	DefaultCacheTTL = 30 * 24 * time.Hour
	// DefaultMaxItemLength is message_length_max and the caption of
	// Premium, in UTF-16 code units.
	DefaultMaxItemLength = 4096
	// DefaultMaxItems and DefaultMaxRequestLength bound one request; the
	// clients send up to 20 items and about 25 000 characters (Android
	// TranslateController.java:66-67, tdesktop
	// history_view_translate_tracker.cpp:35-36, Web K
	// appTranslationsManager.ts:16).
	DefaultMaxItems         = 100
	DefaultMaxRequestLength = 32768
	// DefaultItemsPerMinute and DefaultItemsPerHour bound the items one
	// user sends to the engine (ADR 0064, section 7); what the cache
	// answers is free.
	DefaultItemsPerMinute = 300
	DefaultItemsPerHour   = 3000
)

// The bounds Validate keeps the settings in.
const (
	maxSlots = 16
	// minSlotContext is the least context of a slot: the prompt of an
	// item of a few words and its answer.
	minSlotContext = 512
	// maxSyncWait stays under the 150 s the session gives a call to bff
	// (teamgramd/etc2/session.yaml, BFF Timeout).
	maxSyncWait = 120 * time.Second
	// maxItemLengthLimit is the longest item a client can send: a
	// message or a caption of Premium.
	maxItemLengthLimit = 4096
	maxItemsLimit      = 100
	maxRequestLimit    = maxItemsLimit * maxItemLengthLimit
)

// Config is the Translate block of bff.yaml (TRANSLATE_* of .env,
// docs/ops/translation.md). Everything is optional: without URL the
// translation is off, messages.translateText answers
// ERR_ENTERPRISE_IS_BLOCKED and the clients are told the keys of the app
// config as "disabled" (ADR 0064, section 9); a zero of the rest is its
// default (WithDefaults).
type Config struct {
	// URL is the root of the engine, a service with POST
	// /v1/chat/completions of the OpenAI API: http://translate:8080.
	URL string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Token is the bearer token of the engine; none if empty.
	Token string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Model is the name the engine serves the model by. It is a part of
	// the key of the cache: another model translates every text again.
	Model string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Profile names the prompts and the sampling of the model
	// (profile.go): "hy-mt1.5".
	Profile string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's

	// Slots is how many requests go to the engine at once
	// (TRANSLATE_SLOTS, "4"): its llama-server --parallel, which the
	// engine of docker-compose.translate.yaml takes from the same
	// variable. One item is one request. A string, so that an empty
	// variable takes the default (SlotCount).
	Slots string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Context is the context of the engine in tokens (TRANSLATE_CTX,
	// "16384": llama-server --ctx-size), shared evenly by its Slots; the
	// profile bounds the answer of an item by the context of one slot.
	Context string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's

	// SyncWait is how long a batch of a chat is waited for before the
	// answer is FLOOD_WAIT_X; the work goes on into the cache, and the
	// repeat of the client finds it there.
	SyncWait time.Duration `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// SyncWaitSingle is the same for one text a person waits for.
	SyncWaitSingle time.Duration `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// Timeout + TimeoutPerChar × the characters of the text is the
	// deadline of one request to the engine.
	Timeout        time.Duration `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	TimeoutPerChar time.Duration `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// FailedTTL is how long an item that failed every attempt is answered
	// with FLOOD_WAIT_X before it is tried again.
	FailedTTL time.Duration `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	// CacheTTL is how long a translation is kept after it was last read.
	CacheTTL time.Duration `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's

	// MaxItemLength, MaxItems and MaxRequestLength bound a request: an
	// item, the number of items and their sum, in UTF-16 code units.
	// Over any of them the answer is INPUT_TEXT_TOO_LONG.
	MaxItemLength    int `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	MaxItems         int `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	MaxRequestLength int `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's

	// ItemsPerMinute and ItemsPerHour bound the items of one user that
	// go to the engine; over them the answer is FLOOD_WAIT_X.
	ItemsPerMinute int `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
	ItemsPerHour   int `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// Enabled reports whether an engine is configured.
func (c Config) Enabled() bool { return c.URL != "" }

// WithDefaults returns c with every zero setting replaced by its default.
func (c Config) WithDefaults() Config {
	if c.Profile == "" {
		c.Profile = DefaultProfile
	}
	if strings.TrimSpace(c.Slots) == "" {
		c.Slots = strconv.Itoa(DefaultSlots)
	}
	if strings.TrimSpace(c.Context) == "" {
		c.Context = strconv.Itoa(DefaultContext)
	}
	if c.SyncWait == 0 {
		c.SyncWait = DefaultSyncWait
	}
	if c.SyncWaitSingle == 0 {
		c.SyncWaitSingle = DefaultSyncWaitSingle
	}
	if c.Timeout == 0 {
		c.Timeout = DefaultEngineTimeout
	}
	if c.TimeoutPerChar == 0 {
		c.TimeoutPerChar = DefaultEngineTimeoutPerChar
	}
	if c.FailedTTL == 0 {
		c.FailedTTL = DefaultFailedTTL
	}
	if c.CacheTTL == 0 {
		c.CacheTTL = DefaultCacheTTL
	}
	if c.MaxItemLength == 0 {
		c.MaxItemLength = DefaultMaxItemLength
	}
	if c.MaxItems == 0 {
		c.MaxItems = DefaultMaxItems
	}
	if c.MaxRequestLength == 0 {
		c.MaxRequestLength = DefaultMaxRequestLength
	}
	if c.ItemsPerMinute == 0 {
		c.ItemsPerMinute = DefaultItemsPerMinute
	}
	if c.ItemsPerHour == 0 {
		c.ItemsPerHour = DefaultItemsPerHour
	}
	return c
}

// Validate checks a configuration with its defaults in. A configuration
// without URL is valid whatever the rest: the translation is off.
func (c Config) Validate() error {
	if !c.Enabled() {
		return nil
	}
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("translate: TRANSLATE_URL must be an http(s) URL (docs/ops/translation.md)")
	}
	if c.Model == "" || len(c.Model) > cacheMaxModel {
		return fmt.Errorf("translate: TRANSLATE_MODEL is required with TRANSLATE_URL, up to %d characters (docs/ops/translation.md)", cacheMaxModel)
	}
	if c.Token != "" && len(c.Token) < MinTokenLength {
		return fmt.Errorf("translate: TRANSLATE_TOKEN must be at least %d characters (openssl rand -hex 32)", MinTokenLength)
	}
	if _, err := NewProfile(c.Profile, 1); err != nil {
		return fmt.Errorf("translate: TRANSLATE_PROFILE: %w", err)
	}
	if slots := c.SlotCount(); slots < 1 || slots > maxSlots {
		return fmt.Errorf("translate: TRANSLATE_SLOTS must be a number from 1 to %d", maxSlots)
	}
	if c.SlotContext() < minSlotContext {
		return fmt.Errorf("translate: TRANSLATE_CTX must give each of the TRANSLATE_SLOTS at least %d tokens", minSlotContext)
	}
	switch {
	case c.SyncWait <= 0, c.SyncWait > maxSyncWait,
		c.SyncWaitSingle <= 0, c.SyncWaitSingle > maxSyncWait,
		c.Timeout < time.Second, c.TimeoutPerChar < 0,
		c.FailedTTL < time.Second, c.CacheTTL < time.Hour:
		return errors.New("translate: a time of the Translate block is out of range (docs/ops/translation.md)")
	case c.MaxItemLength < 1, c.MaxItemLength > maxItemLengthLimit,
		c.MaxItems < 1, c.MaxItems > maxItemsLimit,
		c.MaxRequestLength < c.MaxItemLength, c.MaxRequestLength > maxRequestLimit:
		return errors.New("translate: a bound of a request of the Translate block is out of range (docs/ops/translation.md)")
	case c.ItemsPerMinute < c.MaxItems, c.ItemsPerHour < c.ItemsPerMinute:
		// A limit under one whole request would refuse that request
		// forever.
		return errors.New("translate: ItemsPerMinute must hold MaxItems, and ItemsPerHour ItemsPerMinute (docs/ops/translation.md)")
	}
	return nil
}

// SlotCount is Slots as a number, 0 if it is none.
func (c Config) SlotCount() int { return number(c.Slots) }

// SlotContext is the context of one slot of the engine in tokens, as
// NewProfile takes it; 0 without a valid Slots or Context.
func (c Config) SlotContext() int {
	slots, size := c.SlotCount(), number(c.Context)
	if slots < 1 || size < 0 {
		return 0
	}
	return size / slots
}

// number is s as a decimal number, 0 if it is none.
func number(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

// HTTPConfig is what NewHTTPEngine takes of c.
func (c Config) HTTPConfig() HTTPConfig {
	return HTTPConfig{
		URL:            c.URL,
		Token:          c.Token,
		Model:          c.Model,
		Timeout:        c.Timeout,
		TimeoutPerChar: c.TimeoutPerChar,
	}
}
