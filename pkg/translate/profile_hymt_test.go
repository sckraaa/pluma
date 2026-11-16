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
	"strings"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
)

func TestNewProfile(t *testing.T) {
	p := testProfile(t)
	if p.Name() != "hy-mt1.5" || p.Version() != 1 || p.ID() != "hy-mt1.5/1" {
		t.Errorf("profile %s %d %s", p.Name(), p.Version(), p.ID())
	}
	if _, err := NewProfile("translategemma", 0); err == nil {
		t.Error("an unknown profile was taken")
	}
	if _, err := NewProfile("hy-mt1.5", -1); err == nil {
		t.Error("a negative slot context was taken")
	}
	if len(hymtLangs) != 38 {
		t.Errorf("%d languages, the model card has 38", len(hymtLangs))
	}
}

func TestHYMTLang(t *testing.T) {
	p := testProfile(t)
	cases := map[string]string{
		"en":         "en",
		"EN":         "en",
		"ru":         "ru",
		"uk":         "uk",
		"kk":         "kk",
		"pt-BR":      "pt",
		"pt_br":      "pt",
		"en-US":      "en",
		"zh":         "zh",
		"zh-CN":      "zh",
		"zh-Hans":    "zh",
		"zh_hans_HK": "zh",
		"zh-SG":      "zh",
		"zh-TW":      "zh-Hant",
		"zh-HK":      "zh-Hant",
		"zh-Hant":    "zh-Hant",
		"ZH_HANT":    "zh-Hant",
		"yue":        "yue",
		"iw":         "he",
		"in":         "id",
		"fil":        "tl",
		"he":         "he",
		"bo":         "bo",
	}
	for in, want := range cases {
		got, err := p.Lang(in)
		if err != nil || got != want {
			t.Errorf("Lang(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "xx", "nb", "no", "be", "zh-", "-ru", "en--US", "ru RU", "ру", "en-US-aaaaaaaaaaaaaa", "Hant"} {
		if got, err := p.Lang(in); !errors.Is(err, mtproto.ErrToLangInvalid) {
			t.Errorf("Lang(%q) = %q, %v; want TO_LANG_INVALID", in, got, err)
		}
	}
}

func TestHYMTRequest(t *testing.T) {
	p := testProfile(t)
	cases := []struct {
		lang   string
		tagged bool
		prompt string
	}{
		{"ru", false, "Translate the following segment into Russian, without additional explanation.\n\nHello, {source_text}!"},
		{"tl", false, "Translate the following segment into Filipino, without additional explanation.\n\nHello, {source_text}!"},
		{"zh", false, "将以下文本翻译为中文，注意只需要输出翻译后的结果，不要额外解释：\n\nHello, {source_text}!"},
		{"zh-Hant", false, "将以下文本翻译为繁体中文，注意只需要输出翻译后的结果，不要额外解释：\n\nHello, {source_text}!"},
		{"ru", true, "将以下<source></source>之间的文本翻译为俄语，注意只需要输出翻译后的结果，不要额外解释，原文中的<sn></sn>标签表示标签内文本包含格式信息，需要在译文中相应的位置尽量保留该标签。输出格式为：<target>str</target>\n\n<source>Hello, {source_text}!</source>"},
	}
	for _, c := range cases {
		// A placeholder in the text stays as it is.
		req, err := p.Request(c.lang, "Hello, {source_text}!", c.tagged)
		if err != nil {
			t.Fatal(err)
		}
		if req.Prompt != c.prompt {
			t.Errorf("%s tagged=%v:\n got %q\nwant %q", c.lang, c.tagged, req.Prompt, c.prompt)
		}
		if req.Temperature != 0.7 || req.TopP != 0.6 || req.TopK != 20 || req.RepeatPenalty != 1.05 {
			t.Errorf("sampling %+v", req)
		}
		if req.Chars != 21 || req.MaxTokens != 3*21/2+64 {
			t.Errorf("chars %d max_tokens %d", req.Chars, req.MaxTokens)
		}
		if (req.Stop != nil) != c.tagged {
			t.Errorf("stop %v for tagged=%v", req.Stop, c.tagged)
		}
	}
	if _, err := p.Request("xx", "a", false); !errors.Is(err, mtproto.ErrToLangInvalid) {
		t.Errorf("unknown language: %v", err)
	}

	// max_tokens: at most half the context of a slot.
	req, _ := p.Request("en", strings.Repeat("я", 4096), false)
	if req.MaxTokens != DefaultSlotContext/2 || req.Chars != 4096 {
		t.Errorf("max_tokens %d chars %d", req.MaxTokens, req.Chars)
	}
	big, err := NewProfile("hy-mt1.5", 32768)
	if err != nil {
		t.Fatal(err)
	}
	req, _ = big.Request("en", strings.Repeat("я", 4096), false)
	if req.MaxTokens != 3*4096/2+64 {
		t.Errorf("max_tokens %d with a large slot", req.MaxTokens)
	}
}

func TestHYMTAnswer(t *testing.T) {
	p := testProfile(t)
	cases := []struct {
		answer string
		tagged bool
		want   string
		err    bool
	}{
		{"  Привет \n", false, "Привет", false},
		{"<target>Привет <s1>мир</s1></target>", true, "Привет <s1>мир</s1>", false},
		{"\n<target> Привет</target>\nlater", true, " Привет", false},
		{"<target>Привет", true, "Привет", false}, // the stop word left out
		{"Привет", true, "", true},
		{"", true, "", true},
	}
	for _, c := range cases {
		got, err := p.Answer(c.answer, c.tagged)
		if got != c.want || (err != nil) != c.err {
			t.Errorf("Answer(%q, %v) = %q, %v", c.answer, c.tagged, got, err)
		}
	}
}

func TestFloodWait(t *testing.T) {
	cases := map[time.Duration]string{
		0:                       "FLOOD_WAIT_3",
		time.Second:             "FLOOD_WAIT_3",
		10 * time.Second:        "FLOOD_WAIT_10",
		10*time.Second + 1:      "FLOOD_WAIT_11",
		time.Minute:             "FLOOD_WAIT_30",
		-5 * time.Second:        "FLOOD_WAIT_3",
		29*time.Second + 100000: "FLOOD_WAIT_30",
	}
	for d, want := range cases {
		if got := FloodWait(d).Error(); !strings.Contains(got, want) {
			t.Errorf("FloodWait(%v) = %s, want %s", d, got, want)
		}
	}
}
