//go:build integration

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

package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/translation/internal/config"
	"github.com/teamgram/teamgram-server/app/bff/translation/internal/svc"
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/testinfra"
	"github.com/teamgram/teamgram-server/pkg/translate"
	"github.com/teamgram/teamgram-server/pkg/userbox"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"google.golang.org/grpc/status"
)

// The flow of the translation on the storage of production: the cache and
// the hidden bars in MySQL (migrate-20261119-translation.sql), the limits of
// the users in Redis, the engine behind the HTTP client of bff, the queue of
// the scheduler. The messages are read through the fakes of core_test.go,
// as the boxes and channels of the message services give them.

var (
	itMySQL *testinfra.MySQL
	itDB    *sql.DB
	itRedis *testinfra.Redis
)

func TestMain(m *testing.M) {
	ctx := context.Background()
	fail := func(err error) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var err error
	if itMySQL, err = testinfra.StartMySQL(ctx); err != nil {
		fail(err)
	}
	if itDB, err = userbox.Open(itMySQL.DSN()); err != nil {
		fail(err)
	}
	if itRedis, err = testinfra.StartRedis(ctx); err != nil {
		fail(err)
	}
	code := m.Run()
	_ = itDB.Close()
	_ = itRedis.Terminate(ctx)
	_ = itMySQL.Terminate(ctx)
	os.Exit(code)
}

var itTagRe = regexp.MustCompile(`</?s[0-9]+>`)

// itEngine is an engine with the chat endpoint of the OpenAI API: it
// answers the prompts of the hy-mt1.5 profile with the upper case of their
// text, the tags of a formatted one kept, and remembers every prompt.
type itEngine struct {
	mu      sync.Mutex
	prompts []string
}

func (e *itEngine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model    string `json:"model"`
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	if r.URL.Path != "/v1/chat/completions" || json.NewDecoder(r.Body).Decode(&req) != nil || req.Model != "it-model" || len(req.Messages) != 1 {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	prompt := req.Messages[0].Content
	e.mu.Lock()
	e.prompts = append(e.prompts, prompt)
	e.mu.Unlock()

	var answer string
	if i := strings.LastIndex(prompt, "<source>"); i >= 0 && strings.HasSuffix(prompt, "</source>") {
		src := strings.TrimSuffix(prompt[i+len("<source>"):], "</source>")
		var b strings.Builder
		at := 0
		for _, m := range itTagRe.FindAllStringIndex(src, -1) {
			b.WriteString(strings.ToUpper(src[at:m[0]]) + src[m[0]:m[1]])
			at = m[1]
		}
		b.WriteString(strings.ToUpper(src[at:]))
		answer = "<target>" + b.String() + "</target>"
	} else {
		answer = strings.ToUpper(prompt[strings.Index(prompt, "\n\n")+2:])
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"choices": []map[string]any{{"message": map[string]string{"role": "assistant", "content": answer}, "finish_reason": "stop"}},
	})
}

func (e *itEngine) calls() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.prompts)
}

// flow is the translation of bff over the real storage.
type flow struct {
	t        *testing.T
	cfg      translate.Config
	engine   *itEngine
	url      string
	store    *translate.Store
	svc      *svc.ServiceContext
	messages *fakeMessages
	channels *fakeChannels
	stop     func()
}

// newFlow starts the translation over empty tables; edit changes the
// configuration of the stand before it starts.
func newFlow(t *testing.T, edit func(*translate.Config)) *flow {
	t.Helper()
	for _, table := range []string{"translation_cache", "peer_translation_settings"} {
		if _, err := itDB.Exec("DELETE FROM " + table); err != nil {
			t.Fatal(err)
		}
	}
	f := &flow{t: t, engine: &itEngine{}, store: translate.NewStore(itDB)}
	srv := httptest.NewServer(f.engine)
	t.Cleanup(srv.Close)
	f.cfg = translate.Config{URL: srv.URL, Model: "it-model"}
	if edit != nil {
		edit(&f.cfg)
	}
	f.cfg = f.cfg.WithDefaults()
	f.messages = &fakeMessages{boxes: map[int32]*mtproto.MessageBox{}}
	f.channels = &fakeChannels{}
	f.svc = &svc.ServiceContext{
		Config:   config.Config{Translate: f.cfg},
		Settings: f.store,
		Messages: f.messages,
		Channels: f.channels,
		Dialogs: &fakeDialogs{have: map[translate.TranslationPeer]bool{
			{Type: mtproto.PEER_USER, Id: friend}:  true,
			{Type: mtproto.PEER_CHAT, Id: groupId}: true,
		}},
		Users: &fakeUsers{exist: []int64{me, friend}},
	}
	f.start()
	t.Cleanup(func() { f.stop() })
	return f
}

// start starts the scheduler, as bff does at its start.
func (f *flow) start() {
	f.t.Helper()
	profile, err := translate.NewProfile(f.cfg.Profile, f.cfg.SlotContext())
	if err != nil {
		f.t.Fatal(err)
	}
	limits := translate.NewRedisLimits(kv.NewStore(itRedis.KvConf()), f.cfg)
	s, err := translate.StartScheduler(f.cfg, profile, translate.NewHTTPEngine(f.cfg.HTTPConfig()), f.store, limits)
	if err != nil {
		f.t.Fatal(err)
	}
	f.svc.Profile, f.svc.Translator = profile, s
	once := sync.OnceFunc(s.Stop)
	f.stop = once
}

// restart stops the scheduler and starts another one: bff restarted.
func (f *flow) restart() {
	f.stop()
	f.start()
}

func (f *flow) as(user int64) *TranslationCore {
	ctx := context.Background()
	return &TranslationCore{ctx: ctx, svcCtx: f.svc, Logger: logx.WithContext(ctx), MD: &metadata.RpcMetadata{UserId: user}}
}

func (f *flow) cacheRows() int {
	f.t.Helper()
	var n int
	if err := itDB.QueryRow("SELECT COUNT(*) FROM translation_cache").Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func translateAs(c *TranslationCore, in *mtproto.TLMessagesTranslateText) ([]*mtproto.TextWithEntities, error) {
	res, err := c.MessagesTranslateText(in)
	if err != nil {
		return nil, err
	}
	return res.GetResult(), nil
}

// A post of a channel is translated once for its readers and kept in
// MySQL: after a restart of bff the translation comes from there, the
// entities with it. Somebody who cannot read the channel is no peer, and
// its text never reaches the engine.
func TestFlowChannelCache(t *testing.T) {
	f := newFlow(t, nil)
	f.channels.mc = &channelpb.MutableChannel{
		Channel: &channelpb.ImmutableChannel{Id: channelId, AccessHash: channelHash, Broadcast: true},
		Participants: []*channelpb.ImmutableChannelParticipant{
			{ChannelId: channelId, UserId: me, State: channelpb.StateIn},
			{ChannelId: channelId, UserId: friend, State: channelpb.StateIn},
		},
	}
	bold := mtproto.MakeTLMessageEntityBold(&mtproto.MessageEntity{Offset: 0, Length: 4}).To_MessageEntity()
	code := mtproto.MakeTLMessageEntityCode(&mtproto.MessageEntity{Offset: 16, Length: 6}).To_MessageEntity()
	f.channels.messages = map[int32]*mtproto.Message{10: textMessage(10, "News today: run ls -la now", bold, code)}
	ask := &mtproto.TLMessagesTranslateText{Peer: inputChannel(channelHash), Id: []int32{10}, ToLang: "de"}

	check := func(who string, out []*mtproto.TextWithEntities, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", who, err)
		}
		if len(out) != 1 || out[0].GetText() != "NEWS TODAY: RUN ls -la NOW" || len(out[0].GetEntities()) != 2 {
			t.Fatalf("%s: %v", who, out)
		}
		e := out[0].GetEntities()
		if e[0].GetPredicateName() != mtproto.Predicate_messageEntityBold || e[0].GetOffset() != 0 || e[0].GetLength() != 4 ||
			e[1].GetPredicateName() != mtproto.Predicate_messageEntityCode || e[1].GetOffset() != 16 || e[1].GetLength() != 6 {
			t.Fatalf("%s: entities %v", who, e)
		}
	}
	out, err := translateAs(f.as(me), ask)
	check("the first reader", out, err)
	out, err = translateAs(f.as(friend), ask)
	check("the second reader", out, err)
	if n := len(f.engine.calls()); n != 1 {
		t.Fatalf("%d requests to the engine for one post", n)
	}
	if n := f.cacheRows(); n != 1 {
		t.Fatalf("%d rows in the cache", n)
	}

	f.restart()
	out, err = translateAs(f.as(me), ask)
	check("after a restart", out, err)
	if n := len(f.engine.calls()); n != 1 {
		t.Fatalf("the restarted bff asked the engine again: %d requests", n)
	}

	_, err = translateAs(f.as(stranger), ask)
	wantError(t, "a private channel the caller is not in", err, mtproto.ErrPeerIdInvalid)
	if n := len(f.engine.calls()); n != 1 {
		t.Fatalf("a refused request reached the engine: %d requests", n)
	}
}

// An id of the caller's box that belongs to another of its dialogs is an
// empty item: the text of another chat is never translated in place of the
// one asked.
func TestFlowForeignCopy(t *testing.T) {
	f := newFlow(t, nil)
	f.messages.boxes[3] = box(3, mtproto.PEER_USER, me, stranger, textMessage(3, "secret of another chat"))
	f.messages.boxes[5] = box(5, mtproto.PEER_USER, me, friend, textMessage(5, "hello from friend"))

	out, err := translateAs(f.as(me), &mtproto.TLMessagesTranslateText{Peer: inputUser(friend), Id: []int32{3, 5}, ToLang: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if got := texts(out); !slices.Equal(got, []string{"", "HELLO FROM FRIEND"}) {
		t.Fatalf("result %q", got)
	}
	// The same ids asked in a group of the caller: neither is its message.
	out, err = translateAs(f.as(me), &mtproto.TLMessagesTranslateText{Peer: mtproto.MakeTLInputPeerChat(&mtproto.InputPeer{ChatId: groupId}).To_InputPeer(), Id: []int32{3, 5}, ToLang: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if got := texts(out); !slices.Equal(got, []string{"", ""}) {
		t.Fatalf("the ids in a group: %q", got)
	}
	prompts := f.engine.calls()
	if len(prompts) != 1 || strings.Contains(strings.Join(prompts, "\n"), "secret") {
		t.Fatalf("the engine got %q", prompts)
	}
}

// The limits of a user count the items that go to the engine only: what
// the cache answers is free, also for the user who is over the limit.
func TestFlowLimitsCountTheEngineOnly(t *testing.T) {
	f := newFlow(t, func(c *translate.Config) {
		c.MaxItems, c.ItemsPerMinute, c.ItemsPerHour = 2, 2, 2
	})
	const reader, other = int64(2001), int64(2002)
	ask := func(items ...string) *mtproto.TLMessagesTranslateText {
		in := &mtproto.TLMessagesTranslateText{ToLang: "fr"}
		for _, s := range items {
			in.Text_FLAGVECTORTEXTWITHENTITIES = append(in.Text_FLAGVECTORTEXTWITHENTITIES, text(s))
		}
		return in
	}
	if _, err := translateAs(f.as(reader), ask("one", "two")); err != nil {
		t.Fatal(err)
	}
	if _, err := translateAs(f.as(reader), ask("one", "two")); err != nil {
		t.Fatalf("from the cache, over the limit: %v", err)
	}
	_, err := translateAs(f.as(reader), ask("three"))
	if !strings.HasPrefix(status.Convert(err).Message(), "FLOOD_WAIT_") {
		t.Fatalf("over the limit: %v, want FLOOD_WAIT_X", err)
	}
	if out, err := translateAs(f.as(other), ask("three")); err != nil || texts(out)[0] != "THREE" {
		t.Fatalf("another user: %v, %v", out, err)
	}
	if out, err := translateAs(f.as(reader), ask("three")); err != nil || texts(out)[0] != "THREE" {
		t.Fatalf("translated meanwhile by another: %v, %v", out, err)
	}
	if n := len(f.engine.calls()); n != 3 {
		t.Fatalf("%d requests to the engine for three texts", n)
	}
}

// The hidden translation bars are kept in MySQL for the reader alone, and
// shown again; a toggle refused is not stored.
func TestFlowTogglePeerTranslations(t *testing.T) {
	f := newFlow(t, nil)
	ctx := context.Background()
	user := translate.TranslationPeer{Type: mtproto.PEER_USER, Id: friend}
	group := translate.TranslationPeer{Type: mtproto.PEER_CHAT, Id: groupId}
	read := func(who int64) map[translate.TranslationPeer]bool {
		t.Helper()
		got, err := f.store.TranslationsDisabled(ctx, who, user, group)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	for _, disabled := range []bool{true, false} {
		for _, peer := range []*mtproto.InputPeer{inputUser(friend), mtproto.MakeTLInputPeerChat(&mtproto.InputPeer{ChatId: groupId}).To_InputPeer()} {
			if _, err := f.as(me).MessagesTogglePeerTranslations(&mtproto.TLMessagesTogglePeerTranslations{Disabled: disabled, Peer: peer}); err != nil {
				t.Fatal(err)
			}
		}
		if got := read(me); got[user] != disabled || got[group] != disabled {
			t.Fatalf("disabled %v: stored %v", disabled, got)
		}
	}
	if _, err := f.as(me).MessagesTogglePeerTranslations(&mtproto.TLMessagesTogglePeerTranslations{Disabled: true, Peer: inputUser(friend)}); err != nil {
		t.Fatal(err)
	}
	if got := read(friend); got[user] || got[group] {
		t.Fatalf("another reader's bars: %v", got)
	}
	_, err := f.as(me).MessagesTogglePeerTranslations(&mtproto.TLMessagesTogglePeerTranslations{Disabled: true, Peer: inputUser(stranger)})
	wantError(t, "no dialog", err, mtproto.ErrPeerIdInvalid)
	got, err := f.store.TranslationsDisabled(ctx, me, translate.TranslationPeer{Type: mtproto.PEER_USER, Id: stranger})
	if err != nil || len(got) != 0 {
		t.Fatalf("a refused toggle was stored: %v, %v", got, err)
	}
}
