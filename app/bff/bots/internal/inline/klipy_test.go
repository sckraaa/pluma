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

package inline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/pkg/inlinebots"
	"github.com/teamgram/teamgram-server/pkg/inlinemedia"
)

// testKlipyKey is the app key of the tests.
const testKlipyKey = "test-app-key-0123456789"

// klipyRequest is a request the fake KLIPY got: its endpoint and query.
type klipyRequest struct {
	endpoint string
	query    url.Values
}

// fakeKlipy answers api/v1/{key}/gifs/search and /trending with answer and
// records the requests; another key is refused with 401.
type fakeKlipy struct {
	mu     sync.Mutex
	asked  []klipyRequest
	answer func(r klipyRequest) (status int, header http.Header, body string)
}

func (f *fakeKlipy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rest, ok := strings.CutPrefix(r.URL.Path, "/api/v1/")
	key, endpoint, ok2 := strings.Cut(rest, "/gifs/")
	if !ok || !ok2 || r.Method != http.MethodGet || (endpoint != "search" && endpoint != "trending") {
		http.NotFound(w, r)
		return
	}
	if key != testKlipyKey {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"result":false,"errors":{"message":"invalid key"}}`))
		return
	}
	req := klipyRequest{endpoint: endpoint, query: r.URL.Query()}
	f.mu.Lock()
	f.asked = append(f.asked, req)
	f.mu.Unlock()
	status, header, body := f.answer(req)
	for k, v := range header {
		w.Header()[k] = v
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func (f *fakeKlipy) requests() []klipyRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.asked)
}

// klipyItem is a GIF of an answer, of the form of the example of
// gifs-search-api.md.
type klipyItem struct {
	Id   any                             `json:"id,omitempty"`
	Slug string                          `json:"slug,omitempty"`
	Type string                          `json:"type,omitempty"`
	File map[string]map[string]klipyFile `json:"file,omitempty"`
}

// gifItem is a GIF with an mp4 of each of md (498 px) and sm (320 px).
func gifItem(slug string) klipyItem {
	return klipyItem{
		Id: 8041071659142944, Slug: slug, Type: "gif",
		File: map[string]map[string]klipyFile{
			"hd": {"mp4": {URL: "https://static.klipy.com/ii/" + slug + "/hd.mp4", Width: 498, Height: 498, Size: 119294}},
			"md": {"mp4": {URL: "https://static.klipy.com/ii/" + slug + "/md.mp4", Width: 498, Height: 374, Size: 119294}},
			"sm": {"mp4": {URL: "https://static.klipy.com/ii/" + slug + "/sm.mp4", Width: 320, Height: 240, Size: 49565}},
			"xs": {"mp4": {URL: "https://static.klipy.com/ii/" + slug + "/xs.mp4", Width: 150, Height: 112, Size: 20257}},
		},
	}
}

func gifItems(prefix string, n int) []klipyItem {
	out := make([]klipyItem, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, gifItem(fmt.Sprintf("%s-%d", prefix, i)))
	}
	return out
}

// klipyBody is KLIPY's answer with items and has_next.
func klipyBody(t *testing.T, items []klipyItem, hasNext bool) string {
	t.Helper()
	if items == nil {
		items = []klipyItem{}
	}
	b, err := json.Marshal(map[string]any{
		"result": true,
		"data":   map[string]any{"data": items, "current_page": 1, "per_page": klipyPageSize, "has_next": hasNext},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func newFakeKlipy(t *testing.T, answer func(r klipyRequest) (int, http.Header, string)) (*fakeKlipy, *httptest.Server) {
	t.Helper()
	f := &fakeKlipy{answer: answer}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func newTestKlipy(t *testing.T, base string) *Klipy {
	t.Helper()
	store, _ := testCache(t)
	k, err := NewKlipy(base, testKlipyKey, store)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func slugs(p Page) []string {
	out := make([]string, 0, len(p.Items))
	for _, it := range p.Items {
		out = append(out, it.Source.Ref)
	}
	return out
}

// A text is a search, the empty query the trending GIFs; both ask for a
// page of 20 GIFs in mp4 only, with the medium content filter, the key in
// the path, the region of the app's language as the locale and no
// customer_id.
func TestKlipyRequest(t *testing.T) {
	f, srv := newFakeKlipy(t, func(klipyRequest) (int, http.Header, string) { return 200, nil, klipyBody(t, nil, false) })
	k := newTestKlipy(t, srv.URL+"/")
	if _, err := k.Search(context.Background(), Query{Text: "  happy   cat ", Lang: "pt-br"}); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Search(context.Background(), Query{Lang: "ru"}); err != nil {
		t.Fatal(err)
	}
	got := f.requests()
	if len(got) != 2 {
		t.Fatalf("%d requests", len(got))
	}
	search, trending := got[0], got[1]
	if search.endpoint != "search" || trending.endpoint != "trending" {
		t.Fatalf("endpoints %s, %s", search.endpoint, trending.endpoint)
	}
	want := url.Values{
		"q": {"happy cat"}, "page": {"1"}, "per_page": {"20"}, "content_filter": {"medium"},
		"format_filter": {"mp4"}, "locale": {"br"},
	}
	if search.query.Encode() != want.Encode() {
		t.Errorf("search %s, want %s", search.query.Encode(), want.Encode())
	}
	want = url.Values{"page": {"1"}, "per_page": {"20"}, "content_filter": {"medium"}, "format_filter": {"mp4"}}
	if trending.query.Encode() != want.Encode() {
		t.Errorf("trending %s, want %s", trending.query.Encode(), want.Encode())
	}
}

// The GIFs keep KLIPY's order; each is the md mp4, or the sm one when md
// does not fit, keyed by its slug; a GIF without an mp4 that fits, an ad,
// a repeat and a GIF whose files are not of their form are left out.
func TestKlipyResults(t *testing.T) {
	bigMd := gifItem("big-md")
	bigMd.File["md"]["mp4"] = klipyFile{URL: bigMd.File["md"]["mp4"].URL, Width: 498, Height: 374, Size: inlinemedia.MaxGifBytes + 1}
	wideMd := gifItem("wide-md")
	wideMd.File["md"]["mp4"] = klipyFile{URL: wideMd.File["md"]["mp4"].URL, Width: 1920, Height: 1080, Size: 1000}
	noMp4 := gifItem("no-mp4")
	noMp4.File = map[string]map[string]klipyFile{"md": {"gif": {URL: "https://static.klipy.com/x.gif", Width: 498, Height: 498}}}
	onlyHd := gifItem("only-hd")
	delete(onlyHd.File, "md")
	delete(onlyHd.File, "sm")
	badURL := gifItem("bad-url")
	badURL.File = map[string]map[string]klipyFile{"md": {"mp4": {URL: "ftp://static.klipy.com/x.mp4", Width: 498, Height: 498}}}
	byId := gifItem("")
	byId.Id = 42
	ad := klipyItem{Type: "ad"}
	items := []klipyItem{gifItem("first"), bigMd, wideMd, noMp4, onlyHd, badURL, ad, byId, gifItem("first"), gifItem("last")}

	raw := klipyBody(t, items, false)
	// A GIF whose file is not an object of sizes.
	raw = strings.Replace(raw, `"data":[`, `"data":[{"slug":"odd","type":"gif","file":"https://static.klipy.com/odd.mp4"},`, 1)
	_, srv := newFakeKlipy(t, func(klipyRequest) (int, http.Header, string) { return 200, nil, raw })
	page, err := newTestKlipy(t, srv.URL).Search(context.Background(), Query{Text: "cat"})
	if err != nil {
		t.Fatal(err)
	}
	if s := slugs(page); !slices.Equal(s, []string{"first", "big-md", "wide-md", "42", "last"}) {
		t.Fatalf("slugs %q", s)
	}
	first := page.Items[0].Source
	if *first != (inlinemedia.Source{
		Provider: "klipy", Ref: "first", ProviderRef: "first", Kind: inlinemedia.KindGif,
		URL: "https://static.klipy.com/ii/first/md.mp4", Width: 498, Height: 374,
	}) {
		t.Errorf("source %+v", *first)
	}
	for _, i := range []int{1, 2} {
		if s := page.Items[i].Source; !strings.HasSuffix(s.URL, "/sm.mp4") || s.Width != 320 || s.Height != 240 {
			t.Errorf("%s: %s %dx%d, want the sm mp4", s.Ref, s.URL, s.Width, s.Height)
		}
	}
	if page.CacheTime != klipySearchCacheTime || page.Next != "" {
		t.Errorf("cache time %d, next %q", page.CacheTime, page.Next)
	}
}

// Pages follow KLIPY's pages while it has a next one: every GIF names the
// offset after it, the page the next page of KLIPY, and the last page none;
// an offset into a page kept in Redis goes on without a request.
func TestKlipyPages(t *testing.T) {
	f, srv := newFakeKlipy(t, func(r klipyRequest) (int, http.Header, string) {
		p := r.query.Get("page")
		return 200, nil, klipyBody(t, gifItems("p"+p, klipyPageSize), p != "2")
	})
	k := newTestKlipy(t, srv.URL)
	ctx := context.Background()
	page, err := k.Search(ctx, Query{Text: "cat"})
	if err != nil || len(page.Items) != klipyPageSize || page.Next != "k2.0" {
		t.Fatalf("page 1: %d items, next %q, %v", len(page.Items), page.Next, err)
	}
	if n := page.Items[4].Next; n != "k1.5" {
		t.Errorf("the fifth GIF's next %q", n)
	}
	// Cut short after five GIFs: the next query starts from the sixth.
	rest, err := k.Search(ctx, Query{Text: "cat", Offset: "k1.5"})
	if err != nil || len(rest.Items) != klipyPageSize-5 || rest.Items[0].Source.Ref != "p1-5" || rest.Next != "k2.0" {
		t.Fatalf("from k1.5: %q, next %q, %v", slugs(rest), rest.Next, err)
	}
	last, err := k.Search(ctx, Query{Text: "cat", Offset: "k2.0"})
	if err != nil || len(last.Items) != klipyPageSize || last.Next != "" || last.Items[klipyPageSize-1].Next != "" {
		t.Fatalf("page 2: %d items, next %q, %v", len(last.Items), last.Next, err)
	}
	if got := f.requests(); len(got) != 2 || got[0].query.Get("page") != "1" || got[1].query.Get("page") != "2" {
		t.Fatalf("requests %+v", got)
	}
}

// KLIPY's has_next does not go past the last page asked for.
func TestKlipyLastPage(t *testing.T) {
	_, srv := newFakeKlipy(t, func(r klipyRequest) (int, http.Header, string) {
		return 200, nil, klipyBody(t, gifItems("p"+r.query.Get("page"), 8), true)
	})
	page, err := newTestKlipy(t, srv.URL).Search(context.Background(), Query{Text: "cat", Offset: fmt.Sprintf("k%d.0", klipyMaxPage)})
	if err != nil || len(page.Items) != 8 || page.Next != "" {
		t.Fatalf("page %d: %d items, next %q, %v", klipyMaxPage, len(page.Items), page.Next, err)
	}
}

// A page of KLIPY is asked once for its cache time, which is shorter for
// the trending GIFs; the key in Redis carries neither the text nor the app
// key.
func TestKlipyCache(t *testing.T) {
	f, srv := newFakeKlipy(t, func(r klipyRequest) (int, http.Header, string) {
		return 200, nil, klipyBody(t, gifItems(r.endpoint, 3), false)
	})
	store, mr := testCache(t)
	k, err := NewKlipy(srv.URL, testKlipyKey, store)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if p, err := k.Search(ctx, Query{Text: "secret words"}); err != nil || len(p.Items) != 3 || p.CacheTime != klipySearchCacheTime {
			t.Fatalf("search %d: %q, %d, %v", i, slugs(p), p.CacheTime, err)
		}
		if p, err := k.Search(ctx, Query{}); err != nil || len(p.Items) != 3 || p.CacheTime != klipyTrendingCacheTime {
			t.Fatalf("trending %d: %q, %d, %v", i, slugs(p), p.CacheTime, err)
		}
	}
	if n := len(f.requests()); n != 2 {
		t.Fatalf("%d requests, want one of each", n)
	}
	keys := mr.Keys()
	if len(keys) != 2 {
		t.Fatalf("keys %q", keys)
	}
	ttls := []time.Duration{}
	for _, key := range keys {
		if !strings.HasPrefix(key, klipyListKey) || strings.Contains(key, "secret") || strings.Contains(key, testKlipyKey) {
			t.Errorf("key %q", key)
		}
		v, _ := mr.Get(key)
		if strings.Contains(v, testKlipyKey) {
			t.Errorf("the value of %s has the key", key)
		}
		ttls = append(ttls, mr.TTL(key))
	}
	slices.Sort(ttls)
	if ttls[0] != klipyTrendingCacheTime*time.Second || ttls[1] != klipySearchCacheTime*time.Second {
		t.Errorf("ttls %v", ttls)
	}
}

// A Redis that does not answer leaves the search working.
func TestKlipyCacheDown(t *testing.T) {
	_, srv := newFakeKlipy(t, func(klipyRequest) (int, http.Header, string) { return 200, nil, klipyBody(t, gifItems("g", 2), false) })
	store, mr := testCache(t)
	mr.Close()
	k, err := NewKlipy(srv.URL, testKlipyKey, store)
	if err != nil {
		t.Fatal(err)
	}
	if page, err := k.Search(context.Background(), Query{Text: "cat"}); err != nil || len(page.Items) != 2 {
		t.Fatalf("page %q, %v", slugs(page), err)
	}
}

// An offset this provider did not give is an empty page that asks nothing.
func TestKlipyOffsets(t *testing.T) {
	f, srv := newFakeKlipy(t, func(klipyRequest) (int, http.Header, string) { return 200, nil, klipyBody(t, gifItems("g", 3), false) })
	k := newTestKlipy(t, srv.URL)
	for _, off := range []string{
		"12", "k0.0", fmt.Sprintf("k%d.0", klipyMaxPage+1), "k1.-1", "k01.2", "k1.02", "k1",
		fmt.Sprintf("k1.%d", klipyPageSize), "p1.2", "k1.2.3",
	} {
		page, err := k.Search(context.Background(), Query{Text: "cat", Offset: off})
		if err != nil || len(page.Items) != 0 || page.Next != "" {
			t.Errorf("%q: page %+v, %v", off, page, err)
		}
	}
	if n := len(f.requests()); n != 0 {
		t.Fatalf("%d requests", n)
	}
}

// KLIPY that cannot answer is an error of the provider, which the bots
// service answers with an empty list it does not cache; no error names the
// text or the key.
func TestKlipyErrors(t *testing.T) {
	big := `{"result":true,"data":{"data":[` + strings.Repeat(`{"slug":"x","type":"gif"},`, klipyMaxBody/24) + `{}]}}`
	cases := map[string]func(klipyRequest) (int, http.Header, string){
		"500":          func(klipyRequest) (int, http.Header, string) { return 500, nil, `{"result":false}` },
		"404":          func(klipyRequest) (int, http.Header, string) { return 404, nil, `not found` },
		"not JSON":     func(klipyRequest) (int, http.Header, string) { return 200, nil, "<html>results</html>" },
		"result false": func(klipyRequest) (int, http.Header, string) { return 200, nil, `{"result":false,"data":{"data":[]}}` },
		"truncated": func(klipyRequest) (int, http.Header, string) {
			return 200, nil, `{"result":true,"data":{"data":[{"slug":"a"`
		},
		"over the size": func(klipyRequest) (int, http.Header, string) {
			return 200, nil, big
		},
	}
	for name, answer := range cases {
		t.Run(name, func(t *testing.T) {
			_, srv := newFakeKlipy(t, answer)
			_, err := newTestKlipy(t, srv.URL).Search(context.Background(), Query{Text: "private words"})
			if err == nil {
				t.Fatal("no error")
			}
			if strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), testKlipyKey) {
				t.Fatalf("the error has the text or the key: %v", err)
			}
		})
	}
}

// A key KLIPY refuses is an error that does not name the key.
func TestKlipyKeyRefused(t *testing.T) {
	_, srv := newFakeKlipy(t, func(klipyRequest) (int, http.Header, string) { return 200, nil, klipyBody(t, nil, false) })
	store, _ := testCache(t)
	k, err := NewKlipy(srv.URL, "another-key", store)
	if err != nil {
		t.Fatal(err)
	}
	_, err = k.Search(context.Background(), Query{Text: "cat"})
	if err == nil || !strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "another-key") {
		t.Fatalf("error %v", err)
	}
}

// KLIPY that does not answer: the error names neither the address, which
// has the key and the text, nor the text.
func TestKlipyUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close()
	_, err := newTestKlipy(t, base).Search(context.Background(), Query{Text: "private words"})
	if err == nil || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), testKlipyKey) {
		t.Fatalf("error %v", err)
	}
}

// After a 429 no request goes to KLIPY for the Retry-After, or a minute
// without one; then it is asked again.
func TestKlipyRateLimited(t *testing.T) {
	for name, c := range map[string]struct {
		header http.Header
		wait   time.Duration
	}{
		"Retry-After of seconds": {http.Header{"Retry-After": {"30"}}, 30 * time.Second},
		"no Retry-After":         {nil, klipyRetryAfter},
		"Retry-After too long":   {http.Header{"Retry-After": {"86400"}}, klipyRetryAfterMax},
	} {
		t.Run(name, func(t *testing.T) {
			var limited atomic.Bool
			limited.Store(true)
			f, srv := newFakeKlipy(t, func(klipyRequest) (int, http.Header, string) {
				if limited.Load() {
					return http.StatusTooManyRequests, c.header, `{"result":false}`
				}
				return 200, nil, klipyBody(t, gifItems("g", 2), false)
			})
			k := newTestKlipy(t, srv.URL)
			now := time.Unix(1_800_000_000, 0)
			k.now = func() time.Time { return now }
			ctx := context.Background()
			if _, err := k.Search(ctx, Query{Text: "one"}); err == nil || !strings.Contains(err.Error(), "429") {
				t.Fatalf("429: %v", err)
			}
			limited.Store(false)
			now = now.Add(c.wait - time.Second)
			if _, err := k.Search(ctx, Query{Text: "two"}); !errors.Is(err, errKlipyLimited) {
				t.Fatalf("while waiting: %v", err)
			}
			if n := len(f.requests()); n != 1 {
				t.Fatalf("%d requests while waiting", n)
			}
			now = now.Add(time.Second)
			if page, err := k.Search(ctx, Query{Text: "two"}); err != nil || len(page.Items) != 2 {
				t.Fatalf("after the wait: %q, %v", slugs(page), err)
			}
		})
	}
}

// The emoji categories of the GIF tab are searched as words, with or
// without U+FE0F; every category the app config offers has its words.
func TestKlipyEmojiCategories(t *testing.T) {
	for _, e := range inlinebots.GifSearchEmojies {
		if klipyWords[e] == "" {
			t.Errorf("no words for %s", e)
		}
	}
	f, srv := newFakeKlipy(t, func(klipyRequest) (int, http.Header, string) { return 200, nil, klipyBody(t, nil, false) })
	k := newTestKlipy(t, srv.URL)
	for _, q := range []string{"👍", "👍️", " 😂 ", "🐱"} {
		if _, err := k.Search(context.Background(), Query{Text: q}); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	for _, r := range f.requests() {
		got = append(got, r.query.Get("q"))
	}
	if !slices.Equal(got, []string{"thumbs up", "lol", "🐱"}) {
		// 👍 and 👍️ are one query of the cache.
		t.Fatalf("queries %q", got)
	}
}

func TestKlipyLocale(t *testing.T) {
	for lang, want := range map[string]string{
		"": "", "ru": "", "en": "", "uk": "", "pt-br": "br", "pt_BR": "br", "zh-hans": "", "en-US": "us", "es-419": "",
	} {
		if got := klipyLocale(lang); got != want {
			t.Errorf("%q: %q, want %q", lang, got, want)
		}
	}
}

// The address must be http(s) and the key a key; the error never carries
// the key.
func TestNewKlipy(t *testing.T) {
	store, _ := testCache(t)
	for _, base := range []string{"", "api.klipy.com", "ftp://api.klipy.com", "https://", "https://api.klipy.com?x=1"} {
		if _, err := NewKlipy(base, testKlipyKey, store); err == nil {
			t.Errorf("%q: no error", base)
		}
	}
	for _, key := range []string{"", "a b", "a/b", "ключ", "a\nb", strings.Repeat("k", 257)} {
		_, err := NewKlipy(KlipyURL, key, store)
		if err == nil {
			t.Errorf("%q: no error", key)
		} else if key != "" && strings.Contains(err.Error(), key) {
			t.Errorf("the error has the key: %v", err)
		}
	}
	if _, err := NewKlipy(KlipyURL, testKlipyKey, store); err != nil {
		t.Error(err)
	}
}

// A value in Redis of another form is asked again.
func TestKlipyPageValue(t *testing.T) {
	p := klipyPage{Gifs: []klipyGif{{Slug: "a", URL: "https://static.klipy.com/a.mp4", Width: 2, Height: 2}}, More: true}
	v, err := encodeKlipyPage(p)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := decodeKlipyPage(v); !ok || !got.More || len(got.Gifs) != 1 || got.Gifs[0] != p.Gifs[0] {
		t.Fatalf("%+v, %v", got, ok)
	}
	for _, bad := range []string{
		"", "v0:{}", "v1:", "v1:[]",
		`v1:{"g":[{"s":"a","u":"javascript:x","w":2,"h":2}]}`,
		`v1:{"g":[{"s":"","u":"https://a.example/a.mp4","w":2,"h":2}]}`,
		`v1:{"g":[{"s":"a","u":"https://a.example/a.mp4","w":0,"h":2}]}`,
	} {
		if _, ok := decodeKlipyPage(bad); ok {
			t.Errorf("%q decoded", bad)
		}
	}
	if got, ok := decodeKlipyPage(`v1:{"m":false}`); !ok || got.Gifs == nil {
		t.Errorf("an empty page: %+v, %v", got, ok)
	}
}
