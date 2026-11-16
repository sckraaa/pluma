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

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/translate"
)

// The fake is read by the client of bff (pkg/translate) with the prompts of
// the profile bff uses: a change of either that the other does not follow
// fails here, not in the end-to-end run.

const (
	testModel = "e2e-translate"
	testToken = "0123456789abcdef0123456789abcdef"
)

func newEngine(t *testing.T) (*stub, *translate.HTTPEngine, translate.Profile) {
	t.Helper()
	s := &stub{token: testToken, model: testModel}
	srv := httptest.NewServer(s.handler())
	t.Cleanup(srv.Close)
	p, err := translate.NewProfile(translate.DefaultProfile, 0)
	if err != nil {
		t.Fatal(err)
	}
	return s, translate.NewHTTPEngine(translate.HTTPConfig{URL: srv.URL, Token: testToken, Model: testModel}), p
}

func setPlan(t *testing.T, s *stub, p plan) {
	t.Helper()
	body, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/e2e/plan", bytes.NewReader(body)))
	if w.Code != http.StatusNoContent {
		t.Fatalf("plan: status %d", w.Code)
	}
}

func calls(t *testing.T, s *stub) seen {
	t.Helper()
	w := httptest.NewRecorder()
	s.handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/e2e/calls", nil))
	var v seen
	if err := json.NewDecoder(w.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// TestReadsEveryPromptOfTheProfile: every language of the fake is one of
// the profile, and the fake reads it and the text from each of the three
// prompts the profile makes (XX<=>XX, ZH<=>XX, formatted).
func TestReadsEveryPromptOfTheProfile(t *testing.T) {
	p, err := translate.NewProfile(translate.DefaultProfile, 0)
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	for _, code := range langs {
		codes[code] = true
	}
	if len(codes) != 38 {
		t.Fatalf("%d languages, the model card has 38", len(codes))
	}
	const text = "Hello, <s1>world</s1>!\n\nSecond line."
	for code := range codes {
		if got, err := p.Lang(code); err != nil || got != code {
			t.Errorf("%s is %q (%v) for the profile", code, got, err)
			continue
		}
		for _, tagged := range []bool{false, true} {
			req, err := p.Request(code, text, tagged)
			if err != nil {
				t.Fatal(err)
			}
			got, ok := readPrompt(req.Prompt)
			if !ok || got.lang != code || got.text != text || got.tagged != tagged {
				t.Errorf("%s, tagged %v: read %+v (%v)", code, tagged, got, ok)
			}
		}
	}
	for _, junk := range []string{"", "Hello", xxHead + "Klingon" + xxTail + "text", taggedHead + "俄语，<source>x"} {
		if got, ok := readPrompt(junk); ok {
			t.Errorf("%q read as %+v", junk, got)
		}
	}
}

func item(text string, ents ...*mtproto.MessageEntity) *mtproto.TextWithEntities {
	return mtproto.MakeTLTextWithEntities(&mtproto.TextWithEntities{Text: text, Entities: ents}).To_TextWithEntities()
}

// TestTranslatesThroughTheClient: bff's markup gets back its tags from the
// fake, the verbatim content as it was; a planned loss of the tags or an
// extra one sends bff to its fallback by pieces.
func TestTranslatesThroughTheClient(t *testing.T) {
	s, engine, p := newEngine(t)
	ctx := context.Background()
	src := item("Read the docs and run make test now",
		mtproto.MakeTLMessageEntityBold(&mtproto.MessageEntity{Offset: 0, Length: 4}).To_MessageEntity(),
		mtproto.MakeTLMessageEntityCode(&mtproto.MessageEntity{Offset: 22, Length: 9}).To_MessageEntity(),
	)

	res, err := translate.Translate(ctx, p, "ru", src, engine.Translate)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "[ru] READ THE DOCS AND RUN make test NOW" || res.Fallback || res.Calls != 1 || len(res.Entities) != 2 ||
		res.Entities[0].GetOffset() != 5 || res.Entities[0].GetLength() != 4 ||
		res.Entities[1].GetOffset() != 27 || res.Entities[1].GetLength() != 9 {
		t.Fatalf("translation %q %v, fallback %v, %d calls", res.Text, res.Entities, res.Fallback, res.Calls)
	}
	if v := calls(t, s); v.Calls != 1 || v.Tagged != 1 || v.Lang != "ru" || v.MaxTokens <= 0 {
		t.Fatalf("the fake saw %+v", v)
	}

	for _, pl := range []plan{{DropTags: true}, {ExtraTag: true}} {
		setPlan(t, s, pl)
		res, err = translate.Translate(ctx, p, "de", src, engine.Translate)
		if err != nil {
			t.Fatal(err)
		}
		// Two pieces around the code; the bold is lost.
		if res.Text != "[de] READ THE DOCS AND RUN make test [de] NOW" || !res.Fallback || res.Markup != translate.MarkupLost ||
			len(res.Entities) != 1 || res.Entities[0].GetPredicateName() != mtproto.Predicate_messageEntityCode {
			t.Fatalf("%+v: translation %q %v, fallback %v", pl, res.Text, res.Entities, res.Fallback)
		}
		if v := calls(t, s); v.Calls != 3 || v.Tagged != 1 {
			t.Fatalf("%+v: the fake saw %+v", pl, v)
		}
	}

	setPlan(t, s, plan{})
	res, err = translate.Translate(ctx, p, "zh-Hant", item("plain words"), engine.Translate)
	if err != nil || res.Text != "[zh-Hant] PLAIN WORDS" {
		t.Fatalf("%v, %v", res, err)
	}
}

// TestFailsAsPlanned: a planned failure reaches the client as the engine's
// unavailability, the health check fails while every request does, and a
// wrong token is refused.
func TestFailsAsPlanned(t *testing.T) {
	s, engine, p := newEngine(t)
	ctx := context.Background()
	src := item("hello")

	setPlan(t, s, plan{Fail: -1})
	if _, err := translate.Translate(ctx, p, "ru", src, engine.Translate); !errors.Is(err, translate.ErrBusy) {
		t.Fatalf("got %v, want the engine busy", err)
	}
	if err := engine.Health(ctx); !errors.Is(err, translate.ErrBusy) {
		t.Fatalf("health %v while every request fails", err)
	}

	setPlan(t, s, plan{Fail: 1, Status: http.StatusBadRequest})
	if err := engine.Health(ctx); err != nil {
		t.Fatalf("health %v with one planned failure", err)
	}
	if _, err := translate.Translate(ctx, p, "ru", src, engine.Translate); !errors.Is(err, translate.ErrRejected) {
		t.Fatalf("got %v, want a rejection", err)
	}
	if res, err := translate.Translate(ctx, p, "ru", src, engine.Translate); err != nil || res.Text != "[ru] HELLO" {
		t.Fatalf("after the failure: %v, %v", res, err)
	}
	if v := calls(t, s); v.Calls != 2 {
		t.Fatalf("the fake saw %+v", v)
	}

	wrong := translate.NewHTTPEngine(translate.HTTPConfig{URL: strings.TrimSuffix(engineURL(t, s), "/"), Token: strings.Repeat("x", 32), Model: testModel})
	if _, err := translate.Translate(ctx, p, "ru", src, wrong.Translate); !errors.Is(err, translate.ErrDown) {
		t.Fatalf("a wrong token: %v", err)
	}
	other := translate.NewHTTPEngine(translate.HTTPConfig{URL: engineURL(t, s), Token: testToken, Model: "another"})
	if _, err := translate.Translate(ctx, p, "ru", src, other.Translate); !errors.Is(err, translate.ErrRejected) {
		t.Fatalf("another model: %v", err)
	}
}

func engineURL(t *testing.T, s *stub) string {
	t.Helper()
	srv := httptest.NewServer(s.handler())
	t.Cleanup(srv.Close)
	return srv.URL
}
