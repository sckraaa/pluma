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
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/dao"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/inline"
	"github.com/teamgram/teamgram-server/app/bff/bots/internal/svc"
	user_client "github.com/teamgram/teamgram-server/app/service/biz/user/client"
	userpb "github.com/teamgram/teamgram-server/app/service/biz/user/user"
	"github.com/teamgram/teamgram-server/pkg/inlinebots"
	"github.com/teamgram/teamgram-server/pkg/inlinemedia"
	"github.com/teamgram/teamgram-server/pkg/logincode"

	"github.com/zeromicro/go-zero/core/jsonx"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// healthy serves the bots it has.
type healthy map[int64]bool

func (h healthy) Usable(id int64) bool { return h[id] }

// pages is a provider with one answer, which counts its queries.
type pages struct {
	mu    sync.Mutex
	page  inline.Page
	err   error
	calls int
	got   inline.Query
}

func (p *pages) Search(_ context.Context, q inline.Query) (inline.Page, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.got = q
	return p.page, p.err
}

// imports has the media of the sources it knows; polled < 0 tries every
// source, otherwise the first polled.
type imports struct {
	media  map[inlinemedia.Key]*inlinemedia.Media
	polled int
	err    error
	owner  int64
}

func (im *imports) Ensure(_ context.Context, owner int64, sources []inlinemedia.Source) (inlinemedia.Imported, error) {
	im.owner = owner
	if im.err != nil {
		return inlinemedia.Imported{}, im.err
	}
	n := len(sources)
	if im.polled >= 0 && im.polled < n {
		n = im.polled
	}
	out := inlinemedia.Imported{Media: make([]*inlinemedia.Media, n), Polled: n}
	for i := 0; i < n; i++ {
		out.Media[i] = im.media[sources[i].Key()]
	}
	return out, nil
}

// counter lets limit queries through, then FLOOD_WAIT_X.
type counter struct {
	n, limit int
}

func (l *counter) Hit(_ context.Context, rule logincode.Rule, _ string) error {
	if rule.Name != builtinRate.Name {
		return errors.New("another rule")
	}
	l.n++
	if l.n > l.limit {
		return mtproto.NewErrFloodWaitX(30)
	}
	return nil
}

// usersOf answers with the users asked for: a bot with a placeholder for the
// ids of the registry, and people for the rest.
type usersOf struct {
	user_client.UserClient
}

func immutableUser(id int64) *mtproto.ImmutableUser {
	data := &mtproto.UserData{Id: id, AccessHash: 1}
	if inlinebots.IsBuiltin(id) {
		data.Bot = mtproto.MakeTLBotData(&mtproto.BotData{BotInlinePlaceholder: wrapperspb.String(inlinebots.Placeholder())}).To_BotData()
	}
	return mtproto.MakeTLImmutableUser(&mtproto.ImmutableUser{User: mtproto.MakeTLUserData(data).To_UserData()}).To_ImmutableUser()
}

func (usersOf) UserGetImmutableUser(_ context.Context, in *userpb.TLUserGetImmutableUser) (*mtproto.ImmutableUser, error) {
	return immutableUser(in.Id), nil
}

func (usersOf) UserGetMutableUsers(_ context.Context, in *userpb.TLUserGetMutableUsers) (*userpb.Vector_ImmutableUser, error) {
	out := &userpb.Vector_ImmutableUser{}
	for _, id := range in.Id {
		out.Datas = append(out.Datas, immutableUser(id))
	}
	return out, nil
}

func testKV(t *testing.T) kv.Store {
	t.Helper()
	mr := miniredis.RunT(t)
	return kv.NewStore(kv.KvConf{cache.NodeConf{
		RedisConf: redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType, DisableIdentity: true},
		Weight:    100,
	}})
}

type builtinFixture struct {
	svc      *svc.ServiceContext
	gif, pic *pages
	imports  *imports
	limiter  *counter
}

func newBuiltinFixture(t *testing.T) *builtinFixture {
	t.Helper()
	f := &builtinFixture{
		gif:     &pages{},
		pic:     &pages{},
		imports: &imports{media: map[inlinemedia.Key]*inlinemedia.Media{}, polled: -1},
		limiter: &counter{limit: 1000},
	}
	f.svc = &svc.ServiceContext{
		Dao:        &dao.Dao{KV: testKV(t), UserClient: usersOf{}},
		InlineBots: healthy{inlinebots.Gif.Id: true, inlinebots.Pic.Id: true},
		InlineProviders: map[inlinebots.Kind]inline.Provider{
			inlinebots.KindGif:   f.gif,
			inlinebots.KindPhoto: f.pic,
		},
		InlineMedia: f.imports,
		Limiter:     f.limiter,
	}
	return f
}

func (f *builtinFixture) core(userId int64) *BotsCore {
	return &BotsCore{
		ctx:    context.Background(),
		svcCtx: f.svc,
		Logger: logx.WithContext(context.Background()),
		MD:     &metadata.RpcMetadata{UserId: userId, LangCode: "ru"},
	}
}

// query asks the bot through messages.getInlineBotResults, as the GIF tab
// of Android does: no chat (EmojiView.java:9200).
func (f *builtinFixture) query(userId int64, bot inlinebots.Bot, text, offset string) (*mtproto.Messages_BotResults, error) {
	return f.core(userId).MessagesGetInlineBotResults(&mtproto.TLMessagesGetInlineBotResults{
		Bot:    mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: bot.Id, AccessHash: 1}).To_InputUser(),
		Peer:   mtproto.MakeTLInputPeerEmpty(nil).To_InputPeer(),
		Query:  text,
		Offset: offset,
	})
}

func gifDocument(id int64) *mtproto.Document {
	return mtproto.MakeTLDocument(&mtproto.Document{Id: id, AccessHash: 2, MimeType: "video/mp4"}).To_Document()
}

func picSource(i int) *inlinemedia.Source {
	u := "https://pics.example/" + strconv.Itoa(i) + ".jpg"
	return &inlinemedia.Source{Provider: "searxng", Ref: u, Kind: inlinemedia.KindPhoto, URL: u}
}

// @gif answers with documents sent as GIFs, in a gallery, and the answer is
// kept for messages.sendInlineBotResult; a medium that could not be had is
// left out, and nothing is asked of a bot process.
func TestBuiltinGif(t *testing.T) {
	f := newBuiltinFixture(t)
	src := &inlinemedia.Source{Provider: "klipy", Ref: "k1", Kind: inlinemedia.KindGif, URL: "https://gifs.example/k1.mp4", Width: 320, Height: 240}
	lost := &inlinemedia.Source{Provider: "klipy", Ref: "k2", Kind: inlinemedia.KindGif, URL: "https://gifs.example/k2.mp4", Width: 320, Height: 240}
	f.imports.media[src.Key()] = &inlinemedia.Media{Document: gifDocument(51)}
	f.gif.page = inline.Page{Items: []inline.Item{
		{Document: gifDocument(50), Next: "1"},
		{Source: src, Next: "2"},
		{Source: lost, Next: "3"},
	}, Next: "3", CacheTime: 600}

	r, err := f.query(10, inlinebots.Gif, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if f.gif.got.Lang != "ru" || f.imports.owner != inlinebots.Gif.Id {
		t.Errorf("query %+v, media of %d", f.gif.got, f.imports.owner)
	}
	if !r.Gallery || r.CacheTime != 600 || r.GetNextOffset().GetValue() != "3" || len(r.Results) != 2 {
		t.Fatalf("results %v", r)
	}
	for i, want := range []int64{50, 51} {
		res := r.Results[i]
		if res.GetPredicateName() != mtproto.Predicate_botInlineMediaResult || res.Type != "gif" || res.Document.GetId() != want ||
			res.SendMessage.GetPredicateName() != mtproto.Predicate_botInlineMessageMediaAuto || len(res.Id) != 32 {
			t.Errorf("result %d: %v", i, res)
		}
	}
	if r.Results[1].Id != src.Key().String() {
		t.Errorf("id %q, want the key of the source", r.Results[1].Id)
	}
	a, err := f.svc.InlineAnswer(context.Background(), r.QueryId)
	if err != nil || a == nil || a.Bot != inlinebots.Gif.Id || len(a.Results) != 2 {
		t.Fatalf("stored answer %v, %v", a, err)
	}
	// The users of the answer are the bot, as the client sees it.
	if len(r.Users) != 1 || r.Users[0].GetId() != inlinebots.Gif.Id {
		t.Errorf("users %v", r.Users)
	}
}

// @pic answers with photos; a medium of another kind is left out.
func TestBuiltinPic(t *testing.T) {
	f := newBuiltinFixture(t)
	src := picSource(1)
	f.imports.media[src.Key()] = &inlinemedia.Media{Photo: mtproto.MakeTLPhoto(&mtproto.Photo{Id: 60}).To_Photo()}
	f.pic.page = inline.Page{Items: []inline.Item{{Source: src, Next: "p1.1"}, {Document: gifDocument(5), Next: "p1.2"}}, CacheTime: 3600}
	r, err := f.query(10, inlinebots.Pic, "cat", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Results) != 1 || r.Results[0].Type != "photo" || r.Results[0].Photo.GetId() != 60 || r.Results[0].Document != nil {
		t.Fatalf("results %v", r.Results)
	}
	if r.NextOffset != nil || r.CacheTime != 3600 {
		t.Errorf("next %v, cache %d", r.NextOffset, r.CacheTime)
	}
}

// The answer is cached for everyone, in any chat: the same query of another
// user does not reach the provider; another page does.
func TestBuiltinSharedCache(t *testing.T) {
	f := newBuiltinFixture(t)
	f.gif.page = inline.Page{Items: []inline.Item{{Document: gifDocument(50), Next: "1"}}, Next: "1", CacheTime: 600}
	first, err := f.query(10, inlinebots.Gif, "", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := f.core(11).MessagesGetInlineBotResults(&mtproto.TLMessagesGetInlineBotResults{
		Bot:  mtproto.MakeTLInputUser(&mtproto.InputUser{UserId: inlinebots.Gif.Id, AccessHash: 1}).To_InputUser(),
		Peer: mtproto.MakeTLInputPeerSelf(nil).To_InputPeer(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if f.gif.calls != 1 || second.QueryId != first.QueryId || len(second.Results) != 1 {
		t.Fatalf("calls %d, queries %d and %d", f.gif.calls, first.QueryId, second.QueryId)
	}
	if f.limiter.n != 1 {
		t.Errorf("a cached answer counted against the limit: %d", f.limiter.n)
	}
	if _, err = f.query(10, inlinebots.Gif, "", "1"); err != nil || f.gif.calls != 2 {
		t.Fatalf("next page: calls %d, %v", f.gif.calls, err)
	}
}

// A provider that fails is an empty answer that is not cached.
func TestBuiltinProviderFails(t *testing.T) {
	f := newBuiltinFixture(t)
	f.pic.err = errors.New("search engine down")
	f.pic.page = inline.Page{CacheTime: 3600}
	for i := 1; i <= 2; i++ {
		r, err := f.query(10, inlinebots.Pic, "cat", "")
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Results) != 0 || r.CacheTime != 0 || r.NextOffset != nil {
			t.Fatalf("answer %v", r)
		}
		if f.pic.calls != i {
			t.Fatalf("query %d: %d calls; the failure was cached", i, f.pic.calls)
		}
	}
	// So is a store of imported media that cannot be read.
	f.pic.err = nil
	f.pic.page = inline.Page{Items: []inline.Item{{Source: picSource(1), Next: "x"}}, CacheTime: 3600}
	f.imports.err = errors.New("mysql down")
	if r, err := f.query(10, inlinebots.Pic, "cat", ""); err != nil || len(r.Results) != 0 || r.CacheTime != 0 {
		t.Fatalf("store down: %v, %v", r, err)
	}
}

// Media found of which none could be imported (the budget is spent, the
// hosts fail) are an empty answer that is not cached.
func TestBuiltinNothingImported(t *testing.T) {
	f := newBuiltinFixture(t)
	f.pic.page = inline.Page{Items: []inline.Item{{Source: picSource(1), Next: "p1.1"}, {Source: picSource(2), Next: "p1.2"}}, CacheTime: 3600}
	for i := 1; i <= 2; i++ {
		r, err := f.query(10, inlinebots.Pic, "cat", "")
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Results) != 0 || r.CacheTime != 0 {
			t.Fatalf("answer %v", r)
		}
		if f.pic.calls != i {
			t.Fatalf("query %d: %d calls; the empty page was cached", i, f.pic.calls)
		}
	}
}

// A bot with no provider answers with empty lists, not cached.
func TestBuiltinNoProvider(t *testing.T) {
	f := newBuiltinFixture(t)
	delete(f.svc.InlineProviders, inlinebots.KindPhoto)
	r, err := f.query(10, inlinebots.Pic, "cat", "")
	if err != nil || len(r.Results) != 0 || r.CacheTime != 0 {
		t.Fatalf("%v, %v", r, err)
	}
}

// A page the deadline cut short goes on after the last item tried and is
// not cached; with nothing ready at all it is an empty answer.
func TestBuiltinDeadline(t *testing.T) {
	f := newBuiltinFixture(t)
	var items []inline.Item
	for i := 0; i < 4; i++ {
		src := picSource(i)
		f.imports.media[src.Key()] = &inlinemedia.Media{Photo: mtproto.MakeTLPhoto(&mtproto.Photo{Id: int64(70 + i)}).To_Photo()}
		items = append(items, inline.Item{Source: src, Next: "p1." + strconv.Itoa(i+1)})
	}
	f.pic.page = inline.Page{Items: items, Next: "p2.0", CacheTime: 3600}
	f.imports.polled = 2
	r, err := f.query(10, inlinebots.Pic, "cat", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Results) != 2 || r.GetNextOffset().GetValue() != "p1.2" || r.CacheTime != 0 {
		t.Fatalf("answer %v", r)
	}
	f.imports.polled = 0
	r, err = f.query(10, inlinebots.Pic, "dog", "")
	if err != nil || len(r.Results) != 0 || r.NextOffset != nil || r.CacheTime != 0 {
		t.Fatalf("nothing in time: %v, %v", r, err)
	}
}

// A provider's next offset over 64 bytes is dropped: clients could not send
// it back (NEXT_OFFSET_INVALID).
func TestBuiltinLongNextOffset(t *testing.T) {
	f := newBuiltinFixture(t)
	f.gif.page = inline.Page{Items: []inline.Item{{Document: gifDocument(50)}}, Next: strings.Repeat("9", maxNextOffset+1), CacheTime: 600}
	r, err := f.query(10, inlinebots.Gif, "", "")
	if err != nil || r.NextOffset != nil || len(r.Results) != 1 {
		t.Fatalf("%v, %v", r, err)
	}
}

// Past the limit a user gets FLOOD_WAIT_X; the provider is not asked.
func TestBuiltinLimit(t *testing.T) {
	f := newBuiltinFixture(t)
	f.limiter.limit = 1
	f.pic.page = inline.Page{CacheTime: 0}
	if _, err := f.query(10, inlinebots.Pic, "a", ""); err != nil {
		t.Fatal(err)
	}
	_, err := f.query(10, inlinebots.Pic, "ab", "")
	if err == nil || !strings.Contains(err.Error(), "FLOOD_WAIT_30") {
		t.Fatalf("over the limit: %v", err)
	}
	if f.pic.calls != 1 {
		t.Fatalf("%d calls", f.pic.calls)
	}
}

// A bot whose account does not match the registry is BOT_INLINE_DISABLED;
// the other is served.
func TestBuiltinMisconfigured(t *testing.T) {
	f := newBuiltinFixture(t)
	f.svc.InlineBots = healthy{inlinebots.Gif.Id: true}
	if _, err := f.query(10, inlinebots.Pic, "cat", ""); !errors.Is(err, mtproto.ErrBotInlineDisabled) {
		t.Fatalf("@pic: %v", err)
	}
	if f.pic.calls != 0 {
		t.Fatal("the provider of a bot not served was asked")
	}
	if _, err := f.query(10, inlinebots.Gif, "", ""); err != nil {
		t.Fatalf("@gif: %v", err)
	}
}

// gifCounter records the GIF sends counted.
type gifCounter struct{ counted []int64 }

func (g *gifCounter) CountGifSend(_ context.Context, documentId int64) error {
	g.counted = append(g.counted, documentId)
	return nil
}

// hide_via drops via_bot_id only for a built-in bot; a GIF of @gif sent is
// counted whether hidden or not, a result of another bot is not.
func TestSendBuiltinHideVia(t *testing.T) {
	const user, other = int64(10), int64(20)
	answer := func(bot int64, result *mtproto.BotInlineResult) string {
		data, err := jsonx.Marshal(&dao.InlineAnswer{Bot: bot, Results: []*mtproto.BotInlineResult{result}})
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	gif := builtinResult(inlinebots.Gif, "", gifDocument(50), nil)
	pic := builtinResult(inlinebots.Pic, "", nil, mtproto.MakeTLPhoto(&mtproto.Photo{Id: 60}).To_Photo())
	sent := &sentMessages{}
	counted := &gifCounter{}
	d := &dao.Dao{MsgClient: sent, SendAs: sendAsChannels{}, KV: &memoryKV{values: map[string]string{
		"bot_inline_answer:1": answer(inlinebots.Gif.Id, gif),
		"bot_inline_answer:2": answer(inlinebots.Pic.Id, pic),
		"bot_inline_answer:3": answer(other, gif),
	}}}
	c := testCore(d, user)
	c.svcCtx.GifSends = counted
	send := func(query int64, id string, hide bool) *mtproto.Message {
		t.Helper()
		sent.got = nil
		if _, err := c.MessagesSendInlineBotResult(&mtproto.TLMessagesSendInlineBotResult{
			Peer:     mtproto.MakeTLInputPeerUser(&mtproto.InputPeer{UserId: 30}).To_InputPeer(),
			RandomId: time.Now().UnixNano(), QueryId: query, Id: id, HideVia: hide,
		}); err != nil {
			t.Fatal(err)
		}
		return sent.got.GetMessage()[0].GetMessage()
	}

	m := send(1, gif.Id, true)
	if m.ViaBotId != nil || m.Media.GetPredicateName() != mtproto.Predicate_messageMediaDocument || m.Media.Document.GetId() != 50 {
		t.Fatalf("@gif, hidden: via %v, media %v", m.ViaBotId, m.Media)
	}
	if m = send(1, gif.Id, false); m.ViaBotId.GetValue() != inlinebots.Gif.Id {
		t.Fatalf("@gif typed: via %v", m.ViaBotId)
	}
	if m = send(2, pic.Id, true); m.ViaBotId != nil || m.Media.GetPredicateName() != mtproto.Predicate_messageMediaPhoto || m.Media.GetPhoto_FLAGPHOTO().GetId() != 60 {
		t.Fatalf("@pic, hidden: via %v, media %v", m.ViaBotId, m.Media)
	}
	if m = send(3, gif.Id, true); m.ViaBotId.GetValue() != other {
		t.Fatalf("another bot hid itself: via %v", m.ViaBotId)
	}
	if len(counted.counted) != 2 || counted.counted[0] != 50 || counted.counted[1] != 50 {
		t.Fatalf("GIF sends counted %v, want two of 50", counted.counted)
	}
}
