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
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/teamgram/proto/mtproto"
)

// fakeModel answers the prompts of the hy-mt1.5 profile. Its "translation"
// is the upper case of the text out of the tags, so that the length stays
// and every offset can be checked; tagged and plain may replace it.
type fakeModel struct {
	tagged func(src string) string // the answer to a tagged prompt
	plain  func(src string) string // the answer to a plain one
	err    error
	calls  []Request
}

func (f *fakeModel) call(_ context.Context, req Request) (string, error) {
	f.calls = append(f.calls, req)
	if f.err != nil {
		return "", f.err
	}
	if strings.HasPrefix(req.Prompt, "将以下<source>") {
		src := req.Prompt[strings.LastIndex(req.Prompt, "<source>")+len("<source>") : strings.LastIndex(req.Prompt, "</source>")]
		if f.tagged != nil {
			return f.tagged(src), nil
		}
		return "<target>" + upperOutOfTags(src) + "</target>", nil
	}
	src := req.Prompt[strings.Index(req.Prompt, "\n\n")+2:]
	if f.plain != nil {
		return f.plain(src), nil
	}
	return strings.ToUpper(src), nil
}

func upperOutOfTags(s string) string {
	var b strings.Builder
	at := 0
	for _, m := range tagRe.FindAllStringIndex(s, -1) {
		b.WriteString(strings.ToUpper(s[at:m[0]]))
		b.WriteString(s[m[0]:m[1]])
		at = m[1]
	}
	b.WriteString(strings.ToUpper(s[at:]))
	return b.String()
}

func testProfile(t *testing.T) Profile {
	t.Helper()
	p, err := NewProfile("hy-mt1.5", 0)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// u16 returns the UTF-16 offset of the byte i of s.
func u16(s string, i int) int32 { return int32(len(utf16.Encode([]rune(s[:i])))) }

// at returns an entity over the first sub of s.
func at(pred, s, sub string, set ...func(*mtproto.MessageEntity)) *mtproto.MessageEntity {
	i := strings.Index(s, sub)
	if i < 0 {
		panic(fmt.Sprintf("%q not in %q", sub, s))
	}
	e := &mtproto.MessageEntity{PredicateName: pred, Offset: u16(s, i), Length: u16(s, i+len(sub)) - u16(s, i)}
	for _, f := range set {
		f(e)
	}
	return e
}

// covered returns the text of e in s.
func covered(s string, e *mtproto.MessageEntity) string {
	u := utf16.Encode([]rune(s))
	if int(e.Offset+e.Length) > len(u) || e.Offset < 0 || e.Length <= 0 {
		return fmt.Sprintf("<out of range %d+%d of %d>", e.Offset, e.Length, len(u))
	}
	return string(utf16.Decode(u[e.Offset : e.Offset+e.Length]))
}

func translate(t *testing.T, f *fakeModel, text string, ents ...*mtproto.MessageEntity) *Result {
	t.Helper()
	res, err := Translate(context.Background(), testProfile(t), "ru", &mtproto.TextWithEntities{Text: text, Entities: ents}, f.call)
	if err != nil {
		t.Fatalf("Translate(%q): %v", text, err)
	}
	return res
}

// want is an entity of a result: its predicate and the text it covers.
type want struct{ pred, text string }

func checkEntities(t *testing.T, res *Result, wants ...want) {
	t.Helper()
	var got []want
	for _, e := range res.Entities {
		got = append(got, want{e.PredicateName, covered(res.Text, e)})
	}
	if fmt.Sprint(got) != fmt.Sprint(wants) {
		t.Errorf("entities of %q:\n got %v\nwant %v", res.Text, got, wants)
	}
}

func TestMarkupEveryEntity(t *testing.T) {
	const text = "say word to all"
	cases := []struct {
		pred     string
		verbatim bool
		set      func(*mtproto.MessageEntity)
		check    func(*mtproto.MessageEntity) bool
	}{
		{pred: mtproto.Predicate_messageEntityBold},
		{pred: mtproto.Predicate_messageEntityItalic},
		{pred: mtproto.Predicate_messageEntityUnderline},
		{pred: mtproto.Predicate_messageEntityStrike},
		{pred: mtproto.Predicate_messageEntitySpoiler},
		{
			pred:  mtproto.Predicate_messageEntityTextUrl,
			set:   func(e *mtproto.MessageEntity) { e.Url = "https://example.org/a" },
			check: func(e *mtproto.MessageEntity) bool { return e.Url == "https://example.org/a" },
		},
		{
			pred:  mtproto.Predicate_messageEntityBlockquote,
			set:   func(e *mtproto.MessageEntity) { e.Collapsed = true },
			check: func(e *mtproto.MessageEntity) bool { return e.Collapsed },
		},
		{pred: mtproto.Predicate_messageEntityCode, verbatim: true},
		{
			pred:     mtproto.Predicate_messageEntityPre,
			verbatim: true,
			set:      func(e *mtproto.MessageEntity) { e.Language = "go" },
			check:    func(e *mtproto.MessageEntity) bool { return e.Language == "go" },
		},
		{pred: mtproto.Predicate_messageEntityUrl, verbatim: true},
		{pred: mtproto.Predicate_messageEntityEmail, verbatim: true},
		{pred: mtproto.Predicate_messageEntityMention, verbatim: true},
		{
			pred:     mtproto.Predicate_messageEntityMentionName,
			verbatim: true,
			set:      func(e *mtproto.MessageEntity) { e.UserId_INT64 = 777 },
			check:    func(e *mtproto.MessageEntity) bool { return e.UserId_INT64 == 777 },
		},
		{pred: mtproto.Predicate_messageEntityHashtag, verbatim: true},
		{pred: mtproto.Predicate_messageEntityCashtag, verbatim: true},
		{pred: mtproto.Predicate_messageEntityBotCommand, verbatim: true},
		{pred: mtproto.Predicate_messageEntityPhone, verbatim: true},
		{pred: mtproto.Predicate_messageEntityBankCard, verbatim: true},
		{
			pred:     mtproto.Predicate_messageEntityCustomEmoji,
			verbatim: true,
			set:      func(e *mtproto.MessageEntity) { e.DocumentId = 5368324170671202286 },
			check:    func(e *mtproto.MessageEntity) bool { return e.DocumentId == 5368324170671202286 },
		},
		{
			pred:     mtproto.Predicate_messageEntityFormattedDate,
			verbatim: true,
			set: func(e *mtproto.MessageEntity) {
				e.Date = 1763200000
				e.ShortDate = true
			},
			check: func(e *mtproto.MessageEntity) bool { return e.Date == 1763200000 && e.ShortDate },
		},
		{pred: mtproto.Predicate_messageEntityTonAddress, verbatim: true},
		{pred: mtproto.Predicate_messageEntityUnknown, verbatim: true},
	}
	for _, c := range cases {
		t.Run(c.pred, func(t *testing.T) {
			var set []func(*mtproto.MessageEntity)
			if c.set != nil {
				set = append(set, c.set)
			}
			f := &fakeModel{}
			res := translate(t, f, text, at(c.pred, text, "word", set...))
			if len(f.calls) != 1 || f.calls[0].Stop == nil {
				t.Fatalf("calls %d, want one tagged", len(f.calls))
			}
			if !strings.Contains(f.calls[0].Prompt, "<source>say <s1>word</s1> to all</source>") {
				t.Errorf("prompt %q", f.calls[0].Prompt)
			}
			word := "WORD"
			if c.verbatim {
				word = "word"
			}
			if res.Text != "SAY "+word+" TO ALL" {
				t.Errorf("text %q", res.Text)
			}
			checkEntities(t, res, want{c.pred, word})
			if c.check != nil && !c.check(res.Entities[0]) {
				t.Errorf("attributes lost: %v", res.Entities[0])
			}
			if res.Markup != MarkupKept || res.Fallback {
				t.Errorf("markup %d fallback %v", res.Markup, res.Fallback)
			}
		})
	}
}

func TestMarkupDropsOtherEntities(t *testing.T) {
	const text = "keep it simple"
	f := &fakeModel{}
	res := translate(t, f, text,
		at(mtproto.Predicate_messageEntityDiffInsert, text, "it"),
		at(mtproto.Predicate_inputMessageEntityMentionName, text, "simple", func(e *mtproto.MessageEntity) {
			e.UserId_INPUTUSER = &mtproto.InputUser{PredicateName: mtproto.Predicate_inputUser, UserId: 5}
		}),
	)
	// Without entities left the text goes by the plain prompt.
	if len(f.calls) != 1 || f.calls[0].Stop != nil || !strings.HasPrefix(f.calls[0].Prompt, "Translate the following segment into Russian") {
		t.Fatalf("calls %+v", f.calls)
	}
	if res.Text != "KEEP IT SIMPLE" || len(res.Entities) != 0 || res.Fallback {
		t.Errorf("result %+v", res)
	}
}

func TestMarkupNestedAndCrossing(t *testing.T) {
	const text = "one two three four five"
	f := &fakeModel{}
	res := translate(t, f, text,
		at(mtproto.Predicate_messageEntityBold, text, "one two three"),
		at(mtproto.Predicate_messageEntityItalic, text, "three four"),
		at(mtproto.Predicate_messageEntityTextUrl, text, "two", func(e *mtproto.MessageEntity) { e.Url = "https://example.org" }),
		at(mtproto.Predicate_messageEntityCode, text, "five"),
	)
	// The italic crosses the end of the bold: cut there, one again after.
	if want := "<source><s1>one <s2>two</s2> <s3>three</s3></s1><s4> four</s4> <s5>five</s5></source>"; !strings.Contains(f.calls[0].Prompt, want) {
		t.Errorf("prompt %q, want %q", f.calls[0].Prompt, want)
	}
	checkEntities(t, res,
		want{mtproto.Predicate_messageEntityBold, "ONE TWO THREE"},
		want{mtproto.Predicate_messageEntityTextUrl, "TWO"},
		want{mtproto.Predicate_messageEntityItalic, "THREE FOUR"},
		want{mtproto.Predicate_messageEntityCode, "five"},
	)
}

func TestMarkupFormattingCrossingVerbatim(t *testing.T) {
	// The bold crosses the start of the code: the code stays whole, the
	// part of the bold in it comes back with its content, and the two
	// parts of the bold are one again.
	const text = "make run fmt now"
	f := &fakeModel{}
	res := translate(t, f, text,
		at(mtproto.Predicate_messageEntityBold, text, "make run"),
		at(mtproto.Predicate_messageEntityCode, text, "run fmt"),
	)
	if want := "<source><s1>make </s1><s2>run fmt</s2> now</source>"; !strings.Contains(f.calls[0].Prompt, want) {
		t.Errorf("prompt %q, want %q", f.calls[0].Prompt, want)
	}
	checkEntities(t, res,
		want{mtproto.Predicate_messageEntityBold, "MAKE run"},
		want{mtproto.Predicate_messageEntityCode, "run fmt"},
	)
}

func TestMarkupVerbatimWithInnerEntities(t *testing.T) {
	const text = "see go fmt ./... here"
	f := &fakeModel{tagged: func(src string) string {
		// The model translates the content of the code: it comes back
		// from the source.
		return "<target>" + upperOutOfTags(strings.Replace(src, "go fmt ./...", "иди", 1)) + "</target>"
	}}
	res := translate(t, f, text,
		at(mtproto.Predicate_messageEntityPre, text, "go fmt ./...", func(e *mtproto.MessageEntity) { e.Language = "sh" }),
		at(mtproto.Predicate_messageEntityBold, text, "fmt"),
	)
	if res.Text != "SEE go fmt ./... HERE" {
		t.Errorf("text %q", res.Text)
	}
	checkEntities(t, res,
		want{mtproto.Predicate_messageEntityPre, "go fmt ./..."},
		want{mtproto.Predicate_messageEntityBold, "fmt"},
	)
}

func TestMarkupSurrogatePairs(t *testing.T) {
	const text = "hi 😀 there 👍🏽 ok"
	f := &fakeModel{}
	res := translate(t, f, text,
		at(mtproto.Predicate_messageEntityCustomEmoji, text, "😀", func(e *mtproto.MessageEntity) { e.DocumentId = 1 }),
		// An entity from the middle of 👍 to the middle of 🏽 is widened
		// to both: a pair is never cut.
		&mtproto.MessageEntity{PredicateName: mtproto.Predicate_messageEntityBold, Offset: u16(text, strings.Index(text, "👍")) + 1, Length: 2},
	)
	if res.Text != "HI 😀 THERE 👍🏽 OK" {
		t.Errorf("text %q", res.Text)
	}
	checkEntities(t, res,
		want{mtproto.Predicate_messageEntityCustomEmoji, "😀"},
		want{mtproto.Predicate_messageEntityBold, "👍🏽"},
	)
	// The translation is longer before the emoji: offsets move in UTF-16.
	f = &fakeModel{tagged: func(src string) string {
		return "<target>" + strings.Replace(src, "hi", "привет 🙂", 1) + "</target>"
	}}
	res = translate(t, f, text, at(mtproto.Predicate_messageEntityCustomEmoji, text, "😀", func(e *mtproto.MessageEntity) { e.DocumentId = 1 }))
	checkEntities(t, res, want{mtproto.Predicate_messageEntityCustomEmoji, "😀"})
	if e := res.Entities[0]; e.Offset != 10 || e.Length != 2 {
		t.Errorf("offset %d length %d, want 10 and 2", e.Offset, e.Length)
	}
}

func TestMarkupSpacesAtTags(t *testing.T) {
	const text = "a  bold\n word b"
	f := &fakeModel{tagged: func(src string) string {
		// The model moves the spaces into the tag.
		return "<target>\n" + strings.Replace(upperOutOfTags(src), "<s1>", " <s1> ", 1) + " \n</target>"
	}}
	res := translate(t, f, text, at(mtproto.Predicate_messageEntityBold, text, " bold\n word "))
	if !strings.Contains(f.calls[0].Prompt, "<source>a  <s1>bold\n word</s1> b</source>") {
		t.Errorf("prompt %q", f.calls[0].Prompt)
	}
	if res.Text != "A    BOLD\n WORD B" {
		t.Errorf("text %q", res.Text)
	}
	checkEntities(t, res, want{mtproto.Predicate_messageEntityBold, "BOLD\n WORD"})
}

func TestMarkupSiblingsReordered(t *testing.T) {
	const text = "red car fast"
	f := &fakeModel{tagged: func(string) string {
		return "<target><s2>машина</s2> <s1>красная</s1> быстрая</target>"
	}}
	res := translate(t, f, text,
		at(mtproto.Predicate_messageEntityItalic, text, "red"),
		at(mtproto.Predicate_messageEntityBold, text, "car"),
	)
	if res.Fallback {
		t.Fatal("reordered siblings went by pieces")
	}
	checkEntities(t, res,
		want{mtproto.Predicate_messageEntityBold, "машина"},
		want{mtproto.Predicate_messageEntityItalic, "красная"},
	)
}

// TestMarkupWithoutTarget: an answer with every tag but without <target>
// is a translation with its formatting (docs/ops/translation.md, "Замер":
// the model leaves the wrapper out more often than not).
func TestMarkupWithoutTarget(t *testing.T) {
	const text = "red car fast"
	f := &fakeModel{tagged: func(string) string { return "<s1>красная</s1> машина быстрая\n" }}
	res := translate(t, f, text, at(mtproto.Predicate_messageEntityBold, text, "red"))
	if res.Fallback || len(f.calls) != 1 {
		t.Fatalf("fallback %v after %d calls", res.Fallback, len(f.calls))
	}
	checkEntities(t, res, want{mtproto.Predicate_messageEntityBold, "красная"})
}

func TestMarkupFallback(t *testing.T) {
	const text = "hello @alice and bold #tag bye"
	ents := []*mtproto.MessageEntity{
		at(mtproto.Predicate_messageEntityMention, text, "@alice"),
		at(mtproto.Predicate_messageEntityBold, text, "bold"),
		at(mtproto.Predicate_messageEntityHashtag, text, "#tag"),
	}
	answers := map[string]string{
		"lost tag":        "<target>HELLO <s1>@alice</s1> AND BOLD <s3>#tag</s3> BYE</target>",
		"added tag":       "<target>HELLO <s1>@alice</s1> AND <s2>BOLD</s2> <s3>#tag</s3> <s4>BYE</s4></target>",
		"repeated tag":    "<target>HELLO <s1>@alice</s1> <s1>@alice</s1> AND <s2>BOLD</s2> <s3>#tag</s3></target>",
		"broken nesting":  "<target>HELLO <s2><s1>@alice</s1> AND BOLD</s2> <s3>#tag</s3> BYE</target>",
		"crossed tags":    "<target>HELLO <s1>@alice <s2>AND</s1> BOLD</s2> <s3>#tag</s3></target>",
		"source repeated": "<source>HELLO <s1>@alice</s1> AND <s2>BOLD</s2> <s3>#tag</s3> BYE</source>",
		"tag-like":        "<target>HELLO <s1>@alice</s1> AND <s2>BOLD</s2> <s3>#tag</s3> <s 4>BYE</target>",
		"unclosed at end": "<target>HELLO <s1>@alice</s1> AND <s2>BOLD <s3>#tag</s3> BYE</target>",
	}
	for name, answer := range answers {
		t.Run(name, func(t *testing.T) {
			f := &fakeModel{
				tagged: func(string) string { return answer },
				plain:  func(src string) string { return "[" + strings.ToUpper(src) + "]" },
			}
			res := translate(t, f, text, ents...)
			// One tagged request, then the pieces between the verbatim
			// entities, each alone.
			var pieces []string
			for _, c := range f.calls[1:] {
				pieces = append(pieces, c.Prompt[strings.Index(c.Prompt, "\n\n")+2:])
			}
			if fmt.Sprint(pieces) != "[hello and bold bye]" {
				t.Errorf("pieces %q", pieces)
			}
			if res.Text != "[HELLO] @alice [AND BOLD] #tag [BYE]" {
				t.Errorf("text %q", res.Text)
			}
			checkEntities(t, res,
				want{mtproto.Predicate_messageEntityMention, "@alice"},
				want{mtproto.Predicate_messageEntityHashtag, "#tag"},
			)
			if !res.Fallback || res.Markup != MarkupLost || res.Calls != 4 {
				t.Errorf("fallback %v markup %d calls %d", res.Fallback, res.Markup, res.Calls)
			}
		})
	}
}

func TestMarkupFallbackKeepsVerbatimOnly(t *testing.T) {
	// Only verbatim entities: by pieces nothing is lost.
	const text = "run `make` now"
	f := &fakeModel{tagged: func(string) string { return "<target>RUN NOW</target>" }}
	res := translate(t, f, text, at(mtproto.Predicate_messageEntityCode, text, "`make`"))
	if !res.Fallback || res.Markup != MarkupKept {
		t.Errorf("fallback %v markup %d", res.Fallback, res.Markup)
	}
	if res.Text != "RUN `make` NOW" {
		t.Errorf("text %q", res.Text)
	}
}

func TestMarkupTagLikeSource(t *testing.T) {
	// A text that looks like the markup is never sent with tags.
	const text = "write <s1> and <target> in bold"
	f := &fakeModel{}
	res := translate(t, f, text, at(mtproto.Predicate_messageEntityBold, text, "bold"))
	if len(f.calls) != 1 || f.calls[0].Stop != nil {
		t.Fatalf("calls %+v", f.calls)
	}
	if res.Text != "WRITE <S1> AND <TARGET> IN BOLD" || len(res.Entities) != 0 || res.Markup != MarkupLost {
		t.Errorf("result %+v", res)
	}
}

func TestMarkupNothingToTranslate(t *testing.T) {
	const text = "😀 https://example.org !!! 42"
	f := &fakeModel{}
	res := translate(t, f, text, at(mtproto.Predicate_messageEntityUrl, text, "https://example.org"))
	if len(f.calls) != 0 || res.Calls != 0 {
		t.Fatalf("calls %d", len(f.calls))
	}
	if res.Text != text {
		t.Errorf("text %q", res.Text)
	}
	checkEntities(t, res, want{mtproto.Predicate_messageEntityUrl, "https://example.org"})

	res = translate(t, f, " \n ")
	if res.Text != "" || len(res.Entities) != 0 || len(f.calls) != 0 {
		t.Errorf("empty item: %+v", res)
	}
}

func TestMarkupBadAnswers(t *testing.T) {
	const text = "hello world"
	ents := []*mtproto.MessageEntity{at(mtproto.Predicate_messageEntityBold, text, "world")}
	cases := map[string]*fakeModel{
		"empty plain":   {plain: func(string) string { return "  \n" }},
		"empty tagged":  {tagged: func(string) string { return "<target> <s1> </s1> </target>" }},
		"too long":      {tagged: func(string) string { return "<target><s1>x</s1>" + strings.Repeat("y", 3*len(text)+65) + "</target>" }},
		"too long text": {plain: func(string) string { return strings.Repeat("z", 3*len(text)+65) }},
	}
	for name, f := range cases {
		t.Run(name, func(t *testing.T) {
			var in []*mtproto.MessageEntity
			if f.tagged != nil {
				in = ents
			}
			_, err := Translate(context.Background(), testProfile(t), "ru", &mtproto.TextWithEntities{Text: text, Entities: in}, f.call)
			if !errors.Is(err, ErrBadAnswer) {
				t.Errorf("err %v, want ErrBadAnswer", err)
			}
		})
	}
	// The engine's error is the item's.
	f := &fakeModel{err: fmt.Errorf("%w: test", ErrBusy)}
	if _, err := Translate(context.Background(), testProfile(t), "ru", &mtproto.TextWithEntities{Text: text, Entities: ents}, f.call); !errors.Is(err, ErrUnavailable) {
		t.Errorf("err %v, want ErrUnavailable", err)
	}
}

func TestMarkupStopWordKept(t *testing.T) {
	// An engine that leaves out the stop word: no closing </target>.
	const text = "hello world"
	f := &fakeModel{tagged: func(src string) string { return "<target>" + upperOutOfTags(src) }}
	res := translate(t, f, text, at(mtproto.Predicate_messageEntityItalic, text, "world"))
	if res.Fallback || res.Text != "HELLO WORLD" {
		t.Errorf("result %+v", res)
	}
}

func TestNormalize(t *testing.T) {
	const text = "ping  Bob and Ann and me now"
	exist := func(_ context.Context, ids []int64) (map[int64]bool, error) {
		if fmt.Sprint(ids) != "[10 11 7]" {
			return nil, fmt.Errorf("asked %v", ids)
		}
		return map[int64]bool{10: true, 7: true}, nil
	}
	input := func(id int64, self bool) func(*mtproto.MessageEntity) {
		return func(e *mtproto.MessageEntity) {
			u := &mtproto.InputUser{PredicateName: mtproto.Predicate_inputUser, UserId: id}
			if self {
				u = &mtproto.InputUser{PredicateName: mtproto.Predicate_inputUserSelf}
			}
			e.UserId_INPUTUSER = u
		}
	}
	in := []*mtproto.MessageEntity{
		at(mtproto.Predicate_messageEntityItalic, text, " now"),
		at(mtproto.Predicate_inputMessageEntityMentionName, text, "Bob", input(10, false)),
		at(mtproto.Predicate_inputMessageEntityMentionName, text, "Ann", input(11, false)),
		at(mtproto.Predicate_inputMessageEntityMentionName, text, "me", input(0, true)),
		at(mtproto.Predicate_messageEntityBold, text, "ping  "),
		at(mtproto.Predicate_messageEntityBold, text, "ping  "), // a duplicate
		{PredicateName: mtproto.Predicate_messageEntityBold, Offset: 3, Length: 0},
		{PredicateName: mtproto.Predicate_messageEntityBold, Offset: -1, Length: 3},
		{PredicateName: mtproto.Predicate_messageEntityBold, Offset: 100, Length: 3},
		{PredicateName: mtproto.Predicate_messageEntityBold, Offset: 25, Length: 100}, // cut at the end
		at(mtproto.Predicate_messageEntityDiffDelete, text, "and"),
	}
	out, err := Normalize(context.Background(), text, in, 7, exist)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range out {
		s := e.PredicateName + ":" + covered(text, e)
		if e.PredicateName == mtproto.Predicate_messageEntityMentionName {
			s += fmt.Sprintf("#%d", e.UserId_INT64)
		}
		got = append(got, s)
	}
	want := []string{
		"messageEntityBold:ping",
		"messageEntityMentionName:Bob#10",
		"messageEntityMentionName:me#7",
		"messageEntityBold:now",
		"messageEntityItalic:now",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("\n got %v\nwant %v", got, want)
	}

	// Without a way to check the users the input mentions are dropped.
	out, err = Normalize(context.Background(), text, in[1:2], 7, nil)
	if err != nil || len(out) != 0 {
		t.Errorf("nil exist: %v %v", out, err)
	}
	// A failed check is the caller's error.
	fail := errors.New("users unavailable")
	if _, err = Normalize(context.Background(), text, in[1:2], 7, func(context.Context, []int64) (map[int64]bool, error) { return nil, fail }); !errors.Is(err, fail) {
		t.Errorf("err %v", err)
	}
}

func TestMarkupInputMentionTranslated(t *testing.T) {
	// The caller normalizes a client's text with its users; the mention
	// comes back as messageEntityMentionName.
	const text = "thanks Bob a lot"
	ents, err := Normalize(context.Background(), text, []*mtproto.MessageEntity{
		at(mtproto.Predicate_inputMessageEntityMentionName, text, "Bob", func(e *mtproto.MessageEntity) {
			e.UserId_INPUTUSER = &mtproto.InputUser{PredicateName: mtproto.Predicate_inputUser, UserId: 42}
		}),
	}, 1, func(context.Context, []int64) (map[int64]bool, error) { return map[int64]bool{42: true}, nil })
	if err != nil {
		t.Fatal(err)
	}
	res := translate(t, &fakeModel{}, text, ents...)
	checkEntities(t, res, want{mtproto.Predicate_messageEntityMentionName, "Bob"})
	if res.Entities[0].UserId_INT64 != 42 {
		t.Errorf("user %d", res.Entities[0].UserId_INT64)
	}
}

func TestMarkupRandomEntities(t *testing.T) {
	// Random texts with crossing, nested and repeated entities of every
	// kind: the translation keeps every verbatim content whole and in
	// place, every formatting entity comes back in one piece without
	// spaces at its edges, and every offset is within the text.
	alphabet := []rune("ab я\n😀👍🏽 ,")
	preds := []string{
		mtproto.Predicate_messageEntityBold,
		mtproto.Predicate_messageEntityItalic,
		mtproto.Predicate_messageEntityTextUrl,
		mtproto.Predicate_messageEntityBlockquote,
		mtproto.Predicate_messageEntityCode,
		mtproto.Predicate_messageEntityUrl,
		mtproto.Predicate_messageEntityCustomEmoji,
		mtproto.Predicate_messageEntityMentionName,
	}
	rnd := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 3000; i++ {
		runes := make([]rune, 1+rnd.IntN(30))
		for j := range runes {
			runes[j] = alphabet[rnd.IntN(len(alphabet))]
		}
		text := string(runes)
		if strings.TrimSpace(text) == "" {
			continue // an empty item, whatever its entities
		}
		units := len(utf16.Encode(runes))
		var in []*mtproto.MessageEntity
		for range rnd.IntN(6) {
			off := rnd.IntN(units)
			in = append(in, &mtproto.MessageEntity{
				PredicateName: preds[rnd.IntN(len(preds))],
				Offset:        int32(off),
				Length:        int32(1 + rnd.IntN(units-off)),
			})
		}
		norm, err := Normalize(context.Background(), text, in, 0, nil)
		if err != nil {
			t.Fatal(err)
		}
		// The expected text: upper case out of the verbatim entities.
		verbatim := make([]bool, units)
		var wantVerbatim, wantFormat []string
		for _, e := range norm {
			if kindOf(e) == kindVerbatim {
				for u := e.Offset; u < e.Offset+e.Length; u++ {
					verbatim[u] = true
				}
				wantVerbatim = append(wantVerbatim, e.PredicateName+":"+covered(text, e))
			} else {
				wantFormat = append(wantFormat, e.PredicateName)
			}
		}
		var expected strings.Builder
		u := 0
		for _, r := range runes {
			if verbatim[u] {
				expected.WriteRune(r)
			} else {
				expected.WriteString(strings.ToUpper(string(r)))
			}
			u += utf16.RuneLen(r)
		}

		res, err := Translate(context.Background(), testProfile(t), "ru", &mtproto.TextWithEntities{Text: text, Entities: in}, (&fakeModel{}).call)
		if err != nil {
			t.Fatalf("%q %v: %v", text, in, err)
		}
		if strings.TrimSpace(res.Text) != strings.TrimSpace(expected.String()) {
			t.Fatalf("%q %v: text %q, want %q", text, in, res.Text, expected.String())
		}
		if res.Fallback {
			t.Fatalf("%q %v: went by pieces", text, in)
		}
		resUnits := int32(len(utf16.Encode([]rune(res.Text))))
		var gotVerbatim, gotFormat []string
		for _, e := range res.Entities {
			if e.Offset < 0 || e.Length <= 0 || e.Offset+e.Length > resUnits {
				t.Fatalf("%q %v: entity %v out of %q", text, in, e, res.Text)
			}
			c := covered(res.Text, e)
			if kindOf(e) == kindVerbatim {
				gotVerbatim = append(gotVerbatim, e.PredicateName+":"+c)
				continue
			}
			if strings.TrimSpace(c) != c {
				t.Fatalf("%q %v: formatting %q with spaces at its edges", text, in, c)
			}
			gotFormat = append(gotFormat, e.PredicateName)
		}
		slices.Sort(gotVerbatim)
		slices.Sort(wantVerbatim)
		slices.Sort(gotFormat)
		slices.Sort(wantFormat)
		if fmt.Sprint(gotVerbatim) != fmt.Sprint(wantVerbatim) || fmt.Sprint(gotFormat) != fmt.Sprint(wantFormat) {
			t.Fatalf("%q %v:\n got %v %v\nwant %v %v", text, in, gotVerbatim, gotFormat, wantVerbatim, wantFormat)
		}
	}
}
