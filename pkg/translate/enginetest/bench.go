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
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/translate"
)

// The measurement (brief 101, part 1 and "Замер"): how long one message
// takes by direction and length, how long a batch of a chat takes on the
// slots of the engine, how often the tags survive, for the prompts of the
// profile and for candidates to replace them. The memory of the engine is
// read from outside (docker stats, the cgroup), not here.

// Variant is a way to prompt the model the measurement compares with the
// profile as it is.
type Variant struct {
	Name string
	// Wrap returns the profile the variant prompts with; nil is the
	// profile itself.
	Wrap func(translate.Profile) translate.Profile
	// Engine returns the engine the variant sends its requests to; nil is
	// the server's own client of cfg.
	Engine func(cfg translate.Config) translate.Engine
}

// Prefill starts the answer of the model to a tagged prompt with <target>
// (an assistant message after the prompt, which llama-server continues:
// --prefill-assistant, on by default in release b11913), so that the
// model writes the translation into the form the prompt asks for.
var Prefill = Variant{
	Name: "prefilled <target>",
	Wrap: func(p translate.Profile) translate.Profile {
		return wrapped{Profile: p, answer: func(answer string, tagged bool) (string, error) {
			if tagged && !strings.HasPrefix(strings.TrimSpace(answer), "<target>") {
				answer = "<target>" + answer
			}
			return p.Answer(answer, tagged)
		}}
	},
	Engine: func(cfg translate.Config) translate.Engine { return newPrefillEngine(cfg) },
}

// AsIs is the profile as bff runs it.
var AsIs = Variant{Name: "profile"}

// Temperature is the profile with another temperature of sampling.
func Temperature(v float64) Variant {
	return Variant{
		Name: fmt.Sprintf("temperature %.2g", v),
		Wrap: func(p translate.Profile) translate.Profile {
			return wrapped{Profile: p, request: func(r translate.Request, _, _ string, _ bool) (translate.Request, error) {
				r.Temperature = v
				return r, nil
			}}
		},
	}
}

// englishTagged is a candidate for the prompt of formatted translation:
// the instruction of the model card's "formatted translation" (given there
// in Chinese only) said in English, as its XX⇔XX prompt is.
const englishTagged = "Translate the text between <source></source> into {target_language}, without additional explanation. " +
	"The <sn></sn> tags in the source mark text that carries formatting; keep each tag around the corresponding part of the translation. " +
	"Output format: <target>str</target>\n\n<source>{source_text}</source>"

// EnglishTagged prompts formatted translation in English, with the English
// name of the target, which the profile's own XX⇔XX prompt gives. It does
// not apply to a target the profile prompts in Chinese.
var EnglishTagged = Variant{
	Name: "english tagged prompt",
	Wrap: func(p translate.Profile) translate.Profile {
		return wrapped{Profile: p, request: func(r translate.Request, lang, text string, tagged bool) (translate.Request, error) {
			if !tagged {
				return r, nil
			}
			plain, err := p.Request(lang, "", false)
			if err != nil {
				return r, err
			}
			const head, tail = "Translate the following segment into ", ", without additional explanation."
			name, ok := strings.CutPrefix(plain.Prompt, head)
			if ok {
				name, _, ok = strings.Cut(name, tail)
			}
			if !ok {
				return r, fmt.Errorf("enginetest: the profile has no English prompt into %s", lang)
			}
			r.Prompt = strings.NewReplacer("{target_language}", name, "{source_text}", text).Replace(englishTagged)
			return r, nil
		}}
	},
}

// recorder is an engine that keeps the answers of the one it wraps.
type recorder struct {
	engine  translate.Engine
	mu      sync.Mutex
	answers []string
}

func (r *recorder) Translate(ctx context.Context, req translate.Request) (string, error) {
	a, err := r.engine.Translate(ctx, req)
	if err == nil {
		r.mu.Lock()
		r.answers = append(r.answers, a)
		r.mu.Unlock()
	}
	return a, err
}

// wrapped is a profile whose requests or reading of answers a variant
// changes; a nil func keeps the profile's own.
type wrapped struct {
	translate.Profile
	request func(r translate.Request, lang, text string, tagged bool) (translate.Request, error)
	answer  func(answer string, tagged bool) (string, error)
}

func (w wrapped) Request(lang, text string, tagged bool) (translate.Request, error) {
	r, err := w.Profile.Request(lang, text, tagged)
	if err != nil || w.request == nil {
		return r, err
	}
	return w.request(r, lang, text, tagged)
}

func (w wrapped) Answer(answer string, tagged bool) (string, error) {
	if w.answer == nil {
		return w.Profile.Answer(answer, tagged)
	}
	return w.answer(answer, tagged)
}

// BenchOptions are what Bench measures.
type BenchOptions struct {
	// Variants are compared; none is AsIs alone.
	Variants []Variant
	// Repeat is how many times each plain message is translated (3 if
	// zero): sampling makes the time of one vary.
	Repeat int
	// Long is the length of the longest message (MaxItemLength if zero).
	Long int
	// Batch: also a batch of a chat, 20 messages on the slots of the
	// engine at once, as the scheduler of bff sends them.
	Batch bool
	// Only, if set, limits the measurement to "plain", "tagged" or
	// "batch".
	Only string
	// Out, if set, receives every translation, for reading their quality.
	Out io.Writer
}

// Timing is the time of one kind of item.
type Timing struct {
	Variant, Pair, Sample string
	Chars                 int
	Times                 []time.Duration
	Err                   error
}

// P returns the q-quantile of the times (0.5 is the median), nearest rank.
func (t Timing) P(q float64) time.Duration {
	if len(t.Times) == 0 {
		return 0
	}
	s := slices.Clone(t.Times)
	slices.Sort(s)
	i := int(q*float64(len(s))+0.999999) - 1
	return s[min(max(i, 0), len(s)-1)]
}

// Report is what Bench measured.
type Report struct {
	Timings []Timing
	// Kept and Tagged count, by variant, the tagged translations that
	// kept their tags and all of them.
	Kept, Tagged map[string]int
	// Batch is the time of a batch of 20 by variant.
	Batch map[string]time.Duration
}

// Bench measures the engine of target as opt says and logs a table.
func Bench(t *testing.T, target Target, opt BenchOptions) Report {
	e, err := newEngine(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(opt.Variants) == 0 {
		opt.Variants = []Variant{AsIs}
	}
	if opt.Repeat == 0 {
		opt.Repeat = 3
	}
	if opt.Long == 0 {
		opt.Long = e.cfg.MaxItemLength
	}
	out := opt.Out
	if out == nil {
		out = io.Discard
	}
	ctx := context.Background()
	rep := Report{Kept: map[string]int{}, Tagged: map[string]int{}, Batch: map[string]time.Duration{}}
	do := func(part string) bool { return opt.Only == "" || opt.Only == part }

	for _, v := range opt.Variants {
		p := e.profile
		if v.Wrap != nil {
			p = v.Wrap(p)
		}
		var eng translate.Engine = e.http
		if v.Engine != nil {
			eng = v.Engine(e.cfg)
		}
		fmt.Fprintf(out, "\n## %s\n", v.Name)

		if do("plain") {
			for _, pair := range Pairs {
				samples := slices.Clone(PlainSamples[pair.From])
				if long, ok := Long(pair.From, opt.Long); ok && (pair.To == "en" || pair.To == "ru") {
					samples = append(samples, long)
				}
				for _, s := range samples {
					tm := Timing{Variant: v.Name, Pair: pair.String(), Sample: s.Name, Chars: s.Length()}
					for i := 0; i < opt.Repeat; i++ {
						res, took, err := translateSample(ctx, p, eng, s, pair.To)
						if err == nil {
							err = checkPlain(s, pair.To, res)
						}
						if err != nil {
							tm.Err = err
							break
						}
						tm.Times = append(tm.Times, took)
						if i == 0 {
							fmt.Fprintf(out, "\n### %s %s (%d characters, %.2f s)\n\n%s\n\n%s\n", pair, s.Name, s.Length(), took.Seconds(), s.Text, res.Text)
						}
					}
					rep.Timings = append(rep.Timings, tm)
				}
			}
		}

		if do("tagged") {
			for _, s := range TaggedSamples {
				for _, to := range TaggedTargets[s.Lang] {
					// The first answer is that of the tagged prompt: what
					// the model made of the tags, kept or not.
					rec := &recorder{engine: eng}
					res, took, err := translateSample(ctx, p, rec, s, to)
					if err == nil {
						err = checkVerbatim(s, res)
					}
					if err != nil {
						t.Errorf("%s %s→%s %q: %v", v.Name, s.Lang, to, s.Name, err)
						continue
					}
					rep.Tagged[v.Name]++
					state := "tags kept"
					if res.Fallback {
						state = "by pieces"
					} else {
						rep.Kept[v.Name]++
					}
					fmt.Fprintf(out, "\n### %s→%s %q: %s, %.2f s\n\n%s\n\n%s\n", s.Lang, to, s.Name, state, took.Seconds(), markUp(s.Text, s.Entities), markUp(res.Text, res.Entities))
					if res.Fallback && len(rec.answers) > 0 {
						fmt.Fprintf(out, "\nThe answer to the tagged prompt:\n\n%s\n", rec.answers[0])
					}
				}
			}
		}

		if opt.Batch && do("batch") {
			took, err := batch(ctx, p, eng, e.cfg.SlotCount(), 20)
			if err != nil {
				t.Errorf("%s: a batch of 20: %v", v.Name, err)
			} else {
				rep.Batch[v.Name] = took
			}
		}
	}

	logReport(t, e.cfg, rep)
	return rep
}

// batch translates n messages of a chat at once on the slots of the
// engine, the plain Russian ones in turn, into English, and returns the time
// of the whole.
func batch(ctx context.Context, p translate.Profile, eng translate.Engine, slots, n int) (time.Duration, error) {
	items := make(chan Sample, n)
	for i := 0; i < n; i++ {
		src := PlainSamples["ru"]
		items <- src[i%len(src)]
	}
	close(items)
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	start := time.Now()
	for w := 0; w < slots; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range items {
				if _, _, err := translateSample(ctx, p, eng, s, "en"); err != nil {
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	return time.Since(start), errors.Join(errs...)
}

func logReport(t *testing.T, cfg translate.Config, rep Report) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "engine %s, model %s, %d slots of %d tokens\n", cfg.URL, cfg.Model, cfg.SlotCount(), cfg.SlotContext())
	if len(rep.Timings) > 0 {
		b.WriteString("variant | pair | message | characters | p50 s | p95 s | characters/s\n")
		for _, tm := range rep.Timings {
			if tm.Err != nil {
				fmt.Fprintf(&b, "%s | %s | %s | %d | failed: %v\n", tm.Variant, tm.Pair, tm.Sample, tm.Chars, tm.Err)
				continue
			}
			p50 := tm.P(0.5)
			fmt.Fprintf(&b, "%s | %s | %s | %d | %.2f | %.2f | %.0f\n", tm.Variant, tm.Pair, tm.Sample, tm.Chars, p50.Seconds(), tm.P(0.95).Seconds(), float64(tm.Chars)/p50.Seconds())
		}
	}
	for v, n := range rep.Tagged {
		fmt.Fprintf(&b, "%s: tags kept in %d of %d\n", v, rep.Kept[v], n)
	}
	for v, d := range rep.Batch {
		fmt.Fprintf(&b, "%s: a batch of 20 in %.1f s\n", v, d.Seconds())
	}
	t.Log("\n" + b.String())
}

// markUp shows the entities of a text as the markup sends them to the
// model, for reading a translation: [name]…[/name].
func markUp(text string, ents []*mtproto.MessageEntity) string {
	type mark struct {
		at    int
		open  bool
		label string
	}
	var marks []mark
	for _, e := range ents {
		name := strings.TrimPrefix(e.GetPredicateName(), "messageEntity")
		marks = append(marks, mark{int(e.GetOffset()), true, "[" + name + "]"}, mark{int(e.GetOffset() + e.GetLength()), false, "[/" + name + "]"})
	}
	slices.SortStableFunc(marks, func(a, b mark) int {
		if a.at != b.at {
			return a.at - b.at
		}
		if a.open != b.open {
			if a.open {
				return 1
			}
			return -1
		}
		return 0
	})
	u := utf16.Encode([]rune(text))
	var b strings.Builder
	at := 0
	for _, m := range marks {
		if m.at < at || m.at > len(u) {
			continue
		}
		b.WriteString(string(utf16.Decode(u[at:m.at])))
		b.WriteString(m.label)
		at = m.at
	}
	b.WriteString(string(utf16.Decode(u[at:])))
	return b.String()
}
