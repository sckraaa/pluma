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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/translate"
)

// TestEngine checks the engine named in the environment:
//
//	TRANSLATE_ENGINE_URL        its base URL; without it the test is skipped
//	TRANSLATE_ENGINE_MODEL      the model name it answers to
//	TRANSLATE_ENGINE_TOKEN      its bearer token, if it has one
//	TRANSLATE_ENGINE_SLOTS      its slots, if not 4
//	TRANSLATE_ENGINE_CTX        its context, if not 16384
//	TRANSLATE_ENGINE_STRICT     1 for our own service (teamgramd/deploy/translate)
//	TRANSLATE_ENGINE_MIN_KEPT   the least share of kept tags, if not DefaultMinTagsKept
func TestEngine(t *testing.T) {
	target, ok := envTarget(t)
	if !ok {
		t.Skip("TRANSLATE_ENGINE_URL is not set: no engine to check")
	}
	Run(t, target)
}

// TestBench measures the engine named in the environment as TestEngine
// takes it, when TRANSLATE_ENGINE_BENCH is 1:
//
//	TRANSLATE_ENGINE_VARIANTS  comma separated: profile, english, prefill,
//	                           temperature=0.3; a name twice measures it twice
//	TRANSLATE_ENGINE_REPEAT    times each plain message is translated (3)
//	TRANSLATE_ENGINE_ONLY      plain, tagged or batch
//	TRANSLATE_ENGINE_OUT       a file for every translation, to read them
func TestBench(t *testing.T) {
	target, ok := envTarget(t)
	if !ok || os.Getenv("TRANSLATE_ENGINE_BENCH") != "1" {
		t.Skip("TRANSLATE_ENGINE_URL and TRANSLATE_ENGINE_BENCH=1 are not set: nothing to measure")
	}
	opt := BenchOptions{Batch: true, Only: os.Getenv("TRANSLATE_ENGINE_ONLY")}
	for _, v := range strings.Split(os.Getenv("TRANSLATE_ENGINE_VARIANTS"), ",") {
		switch v = strings.TrimSpace(v); {
		case v == "" || v == "profile":
			opt.Variants = append(opt.Variants, AsIs)
		case v == "english":
			opt.Variants = append(opt.Variants, EnglishTagged)
		case v == "prefill":
			opt.Variants = append(opt.Variants, Prefill)
		case strings.HasPrefix(v, "temperature="):
			x, err := strconv.ParseFloat(strings.TrimPrefix(v, "temperature="), 64)
			if err != nil {
				t.Fatalf("TRANSLATE_ENGINE_VARIANTS: %v", err)
			}
			opt.Variants = append(opt.Variants, Temperature(x))
		default:
			t.Fatalf("TRANSLATE_ENGINE_VARIANTS: unknown variant %q", v)
		}
	}
	if v := os.Getenv("TRANSLATE_ENGINE_REPEAT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			t.Fatalf("TRANSLATE_ENGINE_REPEAT: %q", v)
		}
		opt.Repeat = n
	}
	if path := os.Getenv("TRANSLATE_ENGINE_OUT"); path != "" {
		f, err := os.Create(path) //nolint:gosec // G304: the file the operator named
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		opt.Out = f
	}
	Bench(t, target, opt)
}

func envTarget(t *testing.T) (Target, bool) {
	t.Helper()
	url := os.Getenv("TRANSLATE_ENGINE_URL")
	if url == "" {
		return Target{}, false
	}
	target := Target{
		Config: translate.Config{
			URL:     url,
			Model:   os.Getenv("TRANSLATE_ENGINE_MODEL"),
			Token:   os.Getenv("TRANSLATE_ENGINE_TOKEN"),
			Slots:   os.Getenv("TRANSLATE_ENGINE_SLOTS"),
			Context: os.Getenv("TRANSLATE_ENGINE_CTX"),
		},
		Strict: os.Getenv("TRANSLATE_ENGINE_STRICT") == "1",
	}
	if v := os.Getenv("TRANSLATE_ENGINE_MIN_KEPT"); v != "" {
		x, err := strconv.ParseFloat(v, 64)
		if err != nil || x <= 0 || x > 1 {
			t.Fatalf("TRANSLATE_ENGINE_MIN_KEPT: %q", v)
		}
		target.MinTagsKept = x
	}
	return target, true
}

// fakeEngine answers as llama-server does on the endpoints bff uses: a
// "translation" that is the text with a mark before it and the names the
// corpus expects spelled as in the target, the tags kept unless loseTags.
type fakeEngine struct {
	token    string
	loseTags bool
	status   int // if not zero, every completion answers it
	calls    atomic.Int32
}

func (f *fakeEngine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/health" {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.URL.Path != "/v1/chat/completions" || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	if f.token != "" && r.Header.Get("Authorization") != "Bearer "+f.token {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	f.calls.Add(1)
	if f.status != 0 {
		w.WriteHeader(f.status)
		return
	}
	var req struct {
		Model    string `json:"model"`
		Stream   bool   `json:"stream"`
		Messages []struct {
			Role, Content string
		} `json:"messages"`
		MaxTokens int `json:"max_tokens"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Stream || len(req.Messages) != 1 || req.Messages[0].Role != "user" || req.MaxTokens < 1 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	prompt := req.Messages[0].Content
	var content string
	if i := strings.LastIndex(prompt, "<source>"); i >= 0 {
		src := strings.TrimSuffix(prompt[i+len("<source>"):], "</source>")
		if f.loseTags {
			src = tagsRe.ReplaceAllString(src, "")
		}
		content = "<target>[t] " + src
	} else {
		content = "[t] " + names.Replace(prompt[strings.Index(prompt, "\n\n")+2:])
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": content}}},
	})
}

var (
	tagsRe = regexp.MustCompile(`</?s[0-9]+>`)
	// names are the names the corpus expects, as a translation spells them.
	names = strings.NewReplacer("Казань", "Kazan", "Anna", "Анна")
)

func fakeTarget(t *testing.T, f *fakeEngine) Target {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return Target{Config: translate.Config{URL: srv.URL, Model: "fake", Token: f.token}, Strict: true}
}

// TestRunAgainstAnEngineOfTheProtocol runs the whole suite against an
// engine that does everything right: the suite must pass it, or it fails
// every real engine as well.
func TestRunAgainstAnEngineOfTheProtocol(t *testing.T) {
	f := &fakeEngine{token: strings.Repeat("k", translate.MinTokenLength)}
	Run(t, fakeTarget(t, f))
	if f.calls.Load() == 0 {
		t.Fatal("the suite sent nothing to the engine")
	}
}

// TestChecksCatchFaults: each check fails the fault it is for.
func TestChecksCatchFaults(t *testing.T) {
	ctx := context.Background()

	t.Run("lost tags", func(t *testing.T) {
		e, err := newEngine(fakeTarget(t, &fakeEngine{loseTags: true}))
		if err != nil {
			t.Fatal(err)
		}
		kept, all := 0, 0
		for _, s := range TaggedSamples {
			res, _, err := translateSample(ctx, e.profile, e.http, s, TaggedTargets[s.Lang][0])
			if err != nil {
				t.Fatal(err)
			}
			// The fallback by pieces still keeps the verbatim entities.
			if err := checkVerbatim(s, res); err != nil {
				t.Errorf("%q: %v", s.Name, err)
			}
			all++
			if !res.Fallback {
				kept++
			}
		}
		if checkKept(kept, all, DefaultMinTagsKept) == nil {
			t.Fatalf("tags kept in %d of %d passed", kept, all)
		}
	})

	t.Run("a verbatim entity changed", func(t *testing.T) {
		s := TaggedSamples[2] // code
		res := &translate.Result{Text: "Run make all again", Entities: []*mtproto.MessageEntity{{PredicateName: mtproto.Predicate_messageEntityCode, Offset: 4, Length: 8}}}
		if checkVerbatim(s, res) == nil {
			t.Fatal("a changed code entity passed")
		}
		res = &translate.Result{Text: "Run make build again", Entities: []*mtproto.MessageEntity{{PredicateName: mtproto.Predicate_messageEntityCode, Offset: 4, Length: 10}}}
		if err := checkVerbatim(s, res); err != nil {
			t.Fatalf("the same code entity failed: %v", err)
		}
	})

	t.Run("the markup of the prompt in a translation", func(t *testing.T) {
		s := PlainSamples["en"][0]
		if checkPlain(s, "ru", &translate.Result{Text: "<target>Опаздываю</target>", Calls: 1}) == nil {
			t.Fatal("a translation with <target> passed")
		}
		if checkPlain(PlainSamples["ru"][2], "en", &translate.Result{Text: "Tomorrow we go to Moscow at 7:40", Calls: 1}) == nil {
			t.Fatal("a translation without an expected word passed")
		}
	})

	t.Run("an engine that refuses requests", func(t *testing.T) {
		e, err := newEngine(fakeTarget(t, &fakeEngine{status: http.StatusBadRequest}))
		if err != nil {
			t.Fatal(err)
		}
		if checkOverSlots(ctx, e, false) == nil {
			t.Fatal("an engine answering 400 passed")
		}
	})

	t.Run("an engine that is busy", func(t *testing.T) {
		e, err := newEngine(fakeTarget(t, &fakeEngine{status: http.StatusServiceUnavailable}))
		if err != nil {
			t.Fatal(err)
		}
		if checkOverSlots(ctx, e, true) == nil {
			t.Fatal("an engine that served nothing passed")
		}
	})
}

// TestCorpus keeps the corpus sound: entities within their texts and over
// what they are meant to cover, languages of the profile, lengths of the
// measurement.
func TestCorpus(t *testing.T) {
	p, err := translate.NewProfile(translate.DefaultProfile, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range Pairs {
		for _, l := range []string{pair.From, pair.To} {
			if _, err := p.Lang(l); err != nil {
				t.Errorf("%s: %s is not a language of the profile", pair, l)
			}
		}
		if len(PlainSamples[pair.From]) != 3 {
			t.Errorf("%s: %d plain messages, want one of each length", pair, len(PlainSamples[pair.From]))
		}
	}
	for lang, samples := range PlainSamples {
		for i, want := range []int{20, 60, 300} {
			if n := samples[i].Length(); n < want*3/4 || n > want*3/2 {
				t.Errorf("%s %s: %d characters, want about %d", lang, samples[i].Name, n, want)
			}
		}
	}
	for _, s := range TaggedSamples {
		if len(TaggedTargets[s.Lang]) == 0 {
			t.Errorf("%q: no target for %s", s.Name, s.Lang)
		}
		n := int32(len(utf16.Encode([]rune(s.Text))))
		for _, e := range s.Entities {
			if e.Offset < 0 || e.Length <= 0 || e.Offset+e.Length > n {
				t.Errorf("%q: %s at %d+%d out of %d", s.Name, e.PredicateName, e.Offset, e.Length, n)
			}
		}
	}
	// The custom emoji is a surrogate pair: two units, one character.
	if e := TaggedSamples[3].Entities[0]; e.Length != 2 || covered(TaggedSamples[3].Text, e) != "🔥" {
		t.Errorf("custom emoji: %d units over %q", e.Length, covered(TaggedSamples[3].Text, e))
	}
	for _, lang := range []string{"ru", "en"} {
		s, ok := Long(lang, translate.DefaultMaxItemLength)
		if !ok || s.Length() > translate.DefaultMaxItemLength || s.Length() < translate.DefaultMaxItemLength-600 {
			t.Errorf("long %s: %d characters", lang, s.Length())
		}
		if n := len(utf16.Encode([]rune(s.Text))); n > translate.DefaultMaxItemLength {
			t.Errorf("long %s: %d UTF-16 units, over MaxItemLength", lang, n)
		}
	}
	if _, ok := Long("kk", 4096); ok {
		t.Error("a long Kazakh message without paragraphs")
	}
}

func TestEnglishTagged(t *testing.T) {
	p, err := translate.NewProfile(translate.DefaultProfile, 0)
	if err != nil {
		t.Fatal(err)
	}
	v := EnglishTagged.Wrap(p)
	r, err := v.Request("ru", "<s1>Hi</s1> there", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(r.Prompt, "Translate the text between <source></source> into Russian,") || !strings.HasSuffix(r.Prompt, "<source><s1>Hi</s1> there</source>") {
		t.Errorf("prompt %q", r.Prompt)
	}
	base, _ := p.Request("ru", "<s1>Hi</s1> there", true)
	if r.Temperature != base.Temperature || r.MaxTokens != base.MaxTokens || len(r.Stop) != len(base.Stop) {
		t.Errorf("the variant changed the sampling: %+v, want %+v", r, base)
	}
	if plain, _ := v.Request("ru", "Hi", false); plain.Prompt != mustRequest(t, p, "ru", "Hi").Prompt {
		t.Errorf("the variant changed the plain prompt: %q", plain.Prompt)
	}
	if _, err := v.Request("zh", "<s1>Hi</s1>", true); err == nil {
		t.Error("an English prompt into Chinese, which the profile prompts in Chinese")
	}
	if r, _ := Temperature(0.3).Wrap(p).Request("en", "Привет", false); r.Temperature != 0.3 {
		t.Errorf("temperature %v", r.Temperature)
	}
}

func mustRequest(t *testing.T, p translate.Profile, lang, text string) translate.Request {
	t.Helper()
	r, err := p.Request(lang, text, false)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestTimingP(t *testing.T) {
	tm := Timing{}
	for i := 10; i >= 1; i-- {
		tm.Times = append(tm.Times, time.Duration(i)*time.Second)
	}
	if got := tm.P(0.5); got != 5*time.Second {
		t.Errorf("p50 %v", got)
	}
	if got := tm.P(0.95); got != 10*time.Second {
		t.Errorf("p95 %v", got)
	}
	if got := (Timing{}).P(0.5); got != 0 {
		t.Errorf("p50 of nothing %v", got)
	}
}

func TestMarkUp(t *testing.T) {
	s := TaggedSamples[0] // bold inside a link
	got := markUp(s.Text, s.Entities)
	want := "Подробности в [TextUrl]нашей [Bold]новой[/Bold] статье[/TextUrl] на сайте, почитай до вечера."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
