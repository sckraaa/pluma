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

// Package enginetest checks that a translation engine can stand behind
// TRANSLATE_URL (ADR 0064, section 1), whatever model or software it is,
// and measures it (bench.go). It talks to a running engine through the
// server's own client, profile and markup (pkg/translate), so what passes
// here is what bff will do.
//
// The suite runs against an engine named in the environment and is skipped
// without one (docs/ops/translation.md, "Проверка движка"):
//
//	TRANSLATE_ENGINE_URL=http://127.0.0.1:8080 TRANSLATE_ENGINE_MODEL=hy-mt1.5-1.8b-q8_0 \
//	TRANSLATE_ENGINE_TOKEN=... go test -count=1 -v ./pkg/translate/enginetest
package enginetest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/translate"
)

// DefaultMinTagsKept is the least share of the tagged samples whose
// formatting must come through the tagged prompt, without the fallback by
// pieces. HY-MT1.5-1.8B Q8_0 with profile hy-mt1.5 version 1 kept the tags
// in 67% of the corpus (75% without the Kazakh targets) over three runs
// (docs/ops/translation.md, "Замер"); the bound leaves room for the
// randomness of sampling and fails a model or a prompt that loses most.
const DefaultMinTagsKept = 0.5

// Target is an engine to check.
type Target struct {
	// Config is the Translate block the server would have: URL, Model,
	// Profile, Slots and Context as the engine runs, and Token if it needs
	// one.
	Config translate.Config
	// Strict also checks what our own service promises and another engine
	// may do differently: 401 for a wrong token, and every request served
	// when more come at once than it has slots.
	Strict bool
	// MinTagsKept, if not zero, replaces DefaultMinTagsKept.
	MinTagsKept float64
}

// engine is what the suite needs of a target with its defaults in.
type engine struct {
	cfg     translate.Config
	profile translate.Profile
	http    *translate.HTTPEngine
}

func newEngine(target Target) (*engine, error) {
	cfg := target.Config.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if !cfg.Enabled() {
		return nil, errors.New("enginetest: no URL of the engine")
	}
	p, err := translate.NewProfile(cfg.Profile, cfg.SlotContext())
	if err != nil {
		return nil, err
	}
	return &engine{cfg: cfg, profile: p, http: translate.NewHTTPEngine(cfg.HTTPConfig())}, nil
}

// translateSample translates s into lang through p and e as bff does, and
// returns how long it took.
func translateSample(ctx context.Context, p translate.Profile, e translate.Engine, s Sample, lang string) (*translate.Result, time.Duration, error) {
	to, err := p.Lang(lang)
	if err != nil {
		return nil, 0, err
	}
	start := time.Now()
	res, err := translate.Translate(ctx, p, to, s.TextWithEntities(), e.Translate)
	return res, time.Since(start), err
}

// Run checks the engine of target.
func Run(t *testing.T, target Target) {
	e, err := newEngine(target)
	if err != nil {
		t.Fatal(err)
	}
	minKept := target.MinTagsKept
	if minKept == 0 {
		minKept = DefaultMinTagsKept
	}
	ctx := context.Background()

	t.Run("health", func(t *testing.T) {
		if err := e.http.Health(ctx); err != nil {
			t.Fatalf("the engine is not ready: %v", err)
		}
	})

	t.Run("plain", func(t *testing.T) {
		for _, pair := range Pairs {
			for _, s := range PlainSamples[pair.From] {
				res, took, err := translateSample(ctx, e.profile, e.http, s, pair.To)
				if err != nil {
					t.Errorf("%s %s: %v", pair, s.Name, err)
					continue
				}
				if err := checkPlain(s, pair.To, res); err != nil {
					t.Errorf("%s %s: %v", pair, s.Name, err)
				}
				t.Logf("%s %s: %d characters in %.2f s", pair, s.Name, s.Length(), took.Seconds())
			}
		}
	})

	t.Run("tagged", func(t *testing.T) {
		kept, all := 0, 0
		for _, s := range TaggedSamples {
			for _, to := range TaggedTargets[s.Lang] {
				res, _, err := translateSample(ctx, e.profile, e.http, s, to)
				if err != nil {
					t.Errorf("%s→%s %q: %v", s.Lang, to, s.Name, err)
					continue
				}
				if err := checkVerbatim(s, res); err != nil {
					t.Errorf("%s→%s %q: %v", s.Lang, to, s.Name, err)
				}
				all++
				if !res.Fallback {
					kept++
				} else {
					t.Logf("%s→%s %q: the tags were not kept, the item went by pieces", s.Lang, to, s.Name)
				}
			}
		}
		if err := checkKept(kept, all, minKept); err != nil {
			t.Error(err)
		}
		t.Logf("tags kept in %d of %d", kept, all)
	})

	t.Run("the longest message", func(t *testing.T) {
		s, _ := Long("ru", e.cfg.MaxItemLength)
		// The deadline is the server's own: the client gives one request
		// Timeout + TimeoutPerChar × its characters.
		res, took, err := translateSample(ctx, e.profile, e.http, s, "en")
		if err != nil {
			t.Fatalf("%d characters: %v", s.Length(), err)
		}
		if err := checkPlain(s, "en", res); err != nil {
			t.Error(err)
		}
		limit := e.cfg.Timeout + time.Duration(s.Length())*e.cfg.TimeoutPerChar
		t.Logf("%d characters in %.1f s (the deadline is %.0f s), %d characters of translation", s.Length(), took.Seconds(), limit.Seconds(), len([]rune(res.Text)))
	})

	if e.cfg.Token != "" {
		t.Run("a wrong token", func(t *testing.T) {
			other := e.cfg
			other.Token = strings.Repeat("x", translate.MinTokenLength)
			_, _, err := translateSample(ctx, e.profile, translate.NewHTTPEngine(other.HTTPConfig()), PlainSamples["ru"][0], "en")
			switch {
			case err == nil:
				t.Fatal("the engine took a wrong token")
			case errors.Is(err, translate.ErrRejected), errors.Is(err, translate.ErrBadAnswer):
				// A wrong TRANSLATE_TOKEN is a mistake of the
				// configuration: it must never read as an item that
				// cannot be translated.
				t.Fatalf("a wrong token reads as a bad item: %v", err)
			case target.Strict && !errors.Is(err, translate.ErrDown):
				t.Errorf("got %v, want the engine unusable until the configuration is fixed (401)", err)
			}
		})
	}

	t.Run("a model it does not have", func(t *testing.T) {
		other := e.cfg
		other.Model = "no-such-model-" + e.cfg.Model
		_, _, err := translateSample(ctx, e.profile, translate.NewHTTPEngine(other.HTTPConfig()), PlainSamples["ru"][0], "en")
		switch {
		case err == nil:
			// llama-server with one model answers whatever the name.
			t.Log("the engine takes any model name")
		case errors.Is(err, translate.ErrRejected), errors.Is(err, translate.ErrBadAnswer):
			t.Fatalf("an unknown model reads as a bad item: %v", err)
		default:
			t.Logf("an unknown model: %v", err)
		}
	})

	t.Run("more requests than slots", func(t *testing.T) {
		if err := checkOverSlots(ctx, e, target.Strict); err != nil {
			t.Error(err)
		}
	})
}

// checkPlain checks a translation of s into lang: a text that is not the
// source, with the words it must keep, and nothing of the prompt in it.
func checkPlain(s Sample, lang string, res *translate.Result) error {
	text := strings.TrimSpace(res.Text)
	switch {
	case text == "":
		return errors.New("an empty translation")
	case res.Calls < 1:
		return errors.New("translated without a request")
	case s.Length() > 40 && text == strings.TrimSpace(s.Text):
		return errors.New("the translation is the source")
	case strings.Contains(text, "<target>"), strings.Contains(text, "</target>"),
		strings.Contains(text, "<source>"), strings.Contains(text, "<s1>"):
		// logaudit:ok a translation of the test corpus, printed in a test report
		return fmt.Errorf("the translation carries the markup of the prompt: %q", text)
	}
	for _, w := range s.Expect[lang] {
		if !strings.Contains(strings.ToLower(text), strings.ToLower(w)) {
			// logaudit:ok a translation of the test corpus, printed in a test report
			return fmt.Errorf("the translation has no %q: %q", w, text)
		}
	}
	return nil
}

// checkVerbatim checks that every verbatim entity of s (code, mentions,
// links… ADR 0064, section 3) is in the translation with the same text and
// the same attributes; the markup restores them whatever the model wrote,
// so a miss is a fault of the server's own code or of the corpus.
func checkVerbatim(s Sample, res *translate.Result) error {
	if strings.TrimSpace(res.Text) == "" {
		return errors.New("an empty translation")
	}
	for _, want := range s.Entities {
		if !verbatim(want) {
			continue
		}
		wantText := covered(s.Text, want)
		found := false
		for _, got := range res.Entities {
			if got.GetPredicateName() == want.GetPredicateName() && covered(res.Text, got) == wantText &&
				got.GetUrl() == want.GetUrl() && got.GetLanguage() == want.GetLanguage() && got.GetDocumentId() == want.GetDocumentId() {
				found = true
				break
			}
		}
		if !found {
			// logaudit:ok a translation of the test corpus, printed in a test report
			return fmt.Errorf("%s %q is not in the translation %q", want.GetPredicateName(), wantText, res.Text)
		}
	}
	return nil
}

// verbatim tells the entities whose content is never translated (ADR 0064,
// section 3) among those of the corpus.
func verbatim(e *mtproto.MessageEntity) bool {
	switch e.GetPredicateName() {
	case mtproto.Predicate_messageEntityCode, mtproto.Predicate_messageEntityPre,
		mtproto.Predicate_messageEntityUrl, mtproto.Predicate_messageEntityEmail,
		mtproto.Predicate_messageEntityMention, mtproto.Predicate_messageEntityHashtag,
		mtproto.Predicate_messageEntityPhone, mtproto.Predicate_messageEntityCustomEmoji:
		return true
	}
	return false
}

// checkKept checks the share of tagged translations that kept their tags.
func checkKept(kept, all int, least float64) error {
	if all == 0 {
		return errors.New("no tagged sample was translated")
	}
	if share := float64(kept) / float64(all); share < least {
		return fmt.Errorf("the tags were kept in %d of %d (%.0f%%), want at least %.0f%%: the formatting would be lost too often", kept, all, 100*share, 100*least)
	}
	return nil
}

// checkOverSlots sends twice as many requests at once as the engine has
// slots, as bff never does but another bff (a restart, a second instance)
// may: the engine must queue them, never refuse one as a bad request.
func checkOverSlots(ctx context.Context, e *engine, strict bool) error {
	n := 2 * e.cfg.SlotCount()
	s := PlainSamples["ru"][1]
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, errs[i] = translateSample(ctx, e.profile, e.http, s, "en")
		}()
	}
	wg.Wait()
	done := 0
	var failed []string
	for _, err := range errs {
		switch {
		case err == nil:
			done++
		case errors.Is(err, translate.ErrRejected), errors.Is(err, translate.ErrBadAnswer):
			return fmt.Errorf("a busy engine reads as a bad item: %w", err)
		default:
			failed = append(failed, err.Error())
		}
	}
	if done == 0 || strict && done < n {
		return fmt.Errorf("%d of %d requests at once were served: %s", done, n, strings.Join(failed, "; "))
	}
	return nil
}
