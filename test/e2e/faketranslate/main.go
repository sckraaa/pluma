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

// Command faketranslate plays the translation engine of messages
// (llama-server with HY-MT1.5, docker-compose.translate.yaml) for the
// end-to-end tests (task 101). bff sends it the prompts of the hy-mt1.5
// profile as to the real one: POST /v1/chat/completions of the OpenAI API
// with the bearer token, and GET /health while it waits for an engine that
// was unavailable. It checks what a real engine relies on (the model name,
// one message of role "user", no streaming, a bound on the answer) and reads
// the prompt as the model would: the target language, the text, and whether
// the text carries the <sN> tags of the markup.
//
// Its "translation" is deterministic: "[xx] " (the code of the target
// language) and the text in upper case, with every tag where it was. The
// upper case reaches into the verbatim tags as well (code, mentions,
// links), which bff must give back as they were in the source. The tests
// plan and look on a loopback port:
//
//	POST /e2e/plan   {"delay_ms":0,"fail":0,"status":503,"drop_tags":false,"extra_tag":false}
//	                 the answers from now on: fail answers with status first
//	                 (default 503; -1: always, and /health answers it too),
//	                 then the translation after delay_ms; drop_tags answers a
//	                 tagged prompt without its tags, extra_tag with a tag its
//	                 source does not have. It zeroes the counters.
//	GET  /e2e/calls  {"calls":N,"tagged":T,"lang":"xx","max_tokens":M}: the
//	                 completions asked since the plan, how many of them with
//	                 tags, and the target and the bound of the last one
package main

import (
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// maxBody bounds a request: the prompt of the longest item (4096
// characters, some of them tags) is some tens of KB.
const maxBody = 1 << 20

// The prompts of the hy-mt1.5 profile (pkg/translate/profile_hymt.go, from
// the model card of HY-MT1.5): what comes before the name of the target
// language and after it.
const (
	xxHead     = "Translate the following segment into "
	xxTail     = ", without additional explanation.\n\n"
	zhHead     = "将以下文本翻译为"
	taggedHead = "将以下<source></source>之间的文本翻译为"
	// The name ends at the comma of the Chinese prompts.
	zhNameEnd    = "，"
	textSep      = "\n\n"
	sourceOpen   = "<source>"
	sourceClose  = "</source>"
	targetOpen   = "<target>"
	targetClose  = "</target>"
	defaultFails = http.StatusServiceUnavailable
)

// langs are the codes of the languages of the model card by the names its
// prompts give them: English in the prompt for XX<=>XX, Chinese in the
// prompts for ZH<=>XX and for formatted text.
var langs = map[string]string{}

func init() {
	for _, l := range []struct{ code, english, chinese string }{
		{"zh", "Chinese", "中文"},
		{"en", "English", "英语"},
		{"fr", "French", "法语"},
		{"pt", "Portuguese", "葡萄牙语"},
		{"es", "Spanish", "西班牙语"},
		{"ja", "Japanese", "日语"},
		{"tr", "Turkish", "土耳其语"},
		{"ru", "Russian", "俄语"},
		{"ar", "Arabic", "阿拉伯语"},
		{"ko", "Korean", "韩语"},
		{"th", "Thai", "泰语"},
		{"it", "Italian", "意大利语"},
		{"de", "German", "德语"},
		{"vi", "Vietnamese", "越南语"},
		{"ms", "Malay", "马来语"},
		{"id", "Indonesian", "印尼语"},
		{"tl", "Filipino", "菲律宾语"},
		{"hi", "Hindi", "印地语"},
		{"zh-Hant", "Traditional Chinese", "繁体中文"},
		{"pl", "Polish", "波兰语"},
		{"cs", "Czech", "捷克语"},
		{"nl", "Dutch", "荷兰语"},
		{"km", "Khmer", "高棉语"},
		{"my", "Burmese", "缅甸语"},
		{"fa", "Persian", "波斯语"},
		{"gu", "Gujarati", "古吉拉特语"},
		{"ur", "Urdu", "乌尔都语"},
		{"te", "Telugu", "泰卢固语"},
		{"mr", "Marathi", "马拉地语"},
		{"he", "Hebrew", "希伯来语"},
		{"bn", "Bengali", "孟加拉语"},
		{"ta", "Tamil", "泰米尔语"},
		{"uk", "Ukrainian", "乌克兰语"},
		{"bo", "Tibetan", "藏语"},
		{"kk", "Kazakh", "哈萨克语"},
		{"mn", "Mongolian", "蒙古语"},
		{"ug", "Uyghur", "维吾尔语"},
		{"yue", "Cantonese", "粤语"},
	} {
		langs[l.english], langs[l.chinese] = l.code, l.code
	}
}

// tagRe finds the tags of the markup of bff (pkg/translate/markup.go).
var tagRe = regexp.MustCompile(`</?s[0-9]+>`)

type plan struct {
	DelayMs int `json:"delay_ms"`
	// Fail is how many requests fail first; -1: all of them, and the
	// health check fails as well.
	Fail     int  `json:"fail"`
	Status   int  `json:"status"`
	DropTags bool `json:"drop_tags"`
	ExtraTag bool `json:"extra_tag"`
}

func (p plan) failStatus() int {
	if p.Status == 0 {
		return defaultFails
	}
	return p.Status
}

type seen struct {
	Calls     int    `json:"calls"`
	Tagged    int    `json:"tagged"`
	Lang      string `json:"lang"`
	MaxTokens int    `json:"max_tokens"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatRequest is the body bff sends (pkg/translate/http_engine.go).
type chatRequest struct {
	Model         string        `json:"model"`
	Messages      []chatMessage `json:"messages"`
	Stream        bool          `json:"stream"`
	Temperature   float64       `json:"temperature"`
	TopP          float64       `json:"top_p"`
	TopK          int           `json:"top_k"`
	RepeatPenalty float64       `json:"repeat_penalty"`
	MaxTokens     int           `json:"max_tokens"`
	Stop          []string      `json:"stop"`
}

type stub struct {
	token, model string
	mu           sync.Mutex
	plan         plan
	seen         seen
}

// prompt is what the model reads in a prompt of the profile.
type prompt struct {
	lang, text string
	tagged     bool
}

// readPrompt reads the target language and the text of a prompt of the
// hy-mt1.5 profile; ok is false for anything else.
func readPrompt(content string) (p prompt, ok bool) {
	var name string
	switch {
	case strings.HasPrefix(content, taggedHead):
		rest := strings.TrimPrefix(content, taggedHead)
		name, _, ok = strings.Cut(rest, zhNameEnd)
		if !ok {
			return prompt{}, false
		}
		// The instruction names <source></source> itself; the text is
		// the last pair, after the blank line.
		i := strings.Index(rest, textSep+sourceOpen)
		j := strings.LastIndex(rest, sourceClose)
		if i < 0 || j < i+len(textSep+sourceOpen) || j+len(sourceClose) != len(rest) {
			return prompt{}, false
		}
		p = prompt{text: rest[i+len(textSep+sourceOpen) : j], tagged: true}
	case strings.HasPrefix(content, zhHead):
		rest := strings.TrimPrefix(content, zhHead)
		name, _, ok = strings.Cut(rest, zhNameEnd)
		if !ok {
			return prompt{}, false
		}
		_, p.text, ok = strings.Cut(rest, textSep)
	case strings.HasPrefix(content, xxHead):
		name, p.text, ok = strings.Cut(strings.TrimPrefix(content, xxHead), xxTail)
	}
	if !ok {
		return prompt{}, false
	}
	if p.lang, ok = langs[name]; !ok || p.text == "" {
		return prompt{}, false
	}
	return p, true
}

// render is the deterministic translation of p: "[xx] " and the text in
// upper case, the tags kept.
func render(p prompt) string {
	var b strings.Builder
	b.WriteString("[" + p.lang + "] ")
	at := 0
	for _, m := range tagRe.FindAllStringIndex(p.text, -1) {
		b.WriteString(strings.ToUpper(p.text[at:m[0]]))
		b.WriteString(p.text[m[0]:m[1]])
		at = m[1]
	}
	b.WriteString(strings.ToUpper(p.text[at:]))
	return b.String()
}

// answer is the completion of p as the model writes it under pl: a tagged
// prompt is answered in <target>, which llama-server ends before the stop
// word when bff asks it to.
func answer(p prompt, pl plan, stop []string) string {
	out := render(p)
	if !p.tagged {
		return out
	}
	if pl.DropTags {
		out = tagRe.ReplaceAllString(out, "")
	}
	if pl.ExtraTag {
		out += " <s999>EXTRA</s999>"
	}
	out = targetOpen + out
	if !slices.Contains(stop, targetClose) {
		out += targetClose
	}
	return out
}

func (s *stub) authorized(r *http.Request) bool {
	if s.token == "" {
		return true
	}
	got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return ok && subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) == 1
}

func (s *stub) complete(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req chatRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&req); err != nil {
		http.Error(w, "not a chat completion", http.StatusBadRequest)
		return
	}
	switch {
	case req.Model != s.model:
		http.Error(w, "unknown model", http.StatusBadRequest)
		return
	case req.Stream:
		http.Error(w, "stream", http.StatusBadRequest)
		return
	case len(req.Messages) != 1 || req.Messages[0].Role != "user":
		http.Error(w, "the model takes one message of role user", http.StatusBadRequest)
		return
	case req.MaxTokens <= 0 || req.Temperature <= 0 || req.TopP <= 0 || req.TopK <= 0 || req.RepeatPenalty <= 0:
		http.Error(w, "sampling", http.StatusBadRequest)
		return
	}
	p, ok := readPrompt(req.Messages[0].Content)
	if !ok {
		http.Error(w, "not a prompt of the model", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	s.seen.Calls++
	if p.tagged {
		s.seen.Tagged++
	}
	s.seen.Lang, s.seen.MaxTokens = p.lang, req.MaxTokens
	pl := s.plan
	fail := pl.Fail != 0
	if pl.Fail > 0 {
		s.plan.Fail--
	}
	s.mu.Unlock()

	if fail {
		http.Error(w, "planned failure", pl.failStatus())
		return
	}
	select {
	case <-time.After(time.Duration(pl.DelayMs) * time.Millisecond):
	case <-r.Context().Done():
		return
	}
	writeJSON(w, map[string]any{
		"object": "chat.completion",
		"model":  s.model,
		"choices": []map[string]any{{
			"index":         0,
			"message":       chatMessage{Role: "assistant", Content: answer(p, pl, req.Stop)},
			"finish_reason": "stop",
		}},
	})
}

// health answers as llama-server does: 200 once it takes requests, the
// planned status while every request fails.
func (s *stub) health(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	pl := s.plan
	s.mu.Unlock()
	if pl.Fail < 0 {
		http.Error(w, "planned failure", pl.failStatus())
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *stub) setPlan(w http.ResponseWriter, r *http.Request) {
	var p plan
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		http.Error(w, "bad plan", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.plan, s.seen = p, seen{}
	s.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

func (s *stub) calls(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	v := s.seen
	s.mu.Unlock()
	writeJSON(w, v)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("faketranslate: write: %v", err)
	}
}

func (s *stub) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", s.complete)
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("POST /e2e/plan", s.setPlan)
	mux.HandleFunc("GET /e2e/calls", s.calls)
	return mux
}

func main() {
	model := os.Getenv("TRANSLATE_MODEL")
	if model == "" {
		log.Fatal("faketranslate: TRANSLATE_MODEL is not set")
	}
	s := &stub{token: os.Getenv("TRANSLATE_API_TOKEN"), model: model}
	srv := &http.Server{Addr: ":8080", Handler: s.handler(), ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
