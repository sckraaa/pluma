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

	"github.com/teamgram/teamgram-server/pkg/inlinemedia"
)

// searx is a SearXNG: it answers /search with the results of answer for
// the page asked, and records the queries.
type searx struct {
	mu     sync.Mutex
	asked  []url.Values
	answer func(q url.Values) (int, string)
	// inFlight and maxInFlight count the requests at once; hold, when set,
	// keeps each request until it is closed.
	inFlight, maxInFlight atomic.Int32
	hold                  chan struct{}
}

func (s *searx) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := s.inFlight.Add(1)
	defer s.inFlight.Add(-1)
	for {
		m := s.maxInFlight.Load()
		if n <= m || s.maxInFlight.CompareAndSwap(m, n) {
			break
		}
	}
	if s.hold != nil {
		<-s.hold
	}
	if !strings.HasSuffix(r.URL.Path, "/search") || r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	s.mu.Lock()
	s.asked = append(s.asked, q)
	s.mu.Unlock()
	status, body := s.answer(q)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func (s *searx) queries() []url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.asked)
}

// result is a result of SearXNG's images template.
type result struct {
	ImgSrc       string `json:"img_src,omitempty"`
	ThumbnailSrc string `json:"thumbnail_src,omitempty"`
	URL          string `json:"url"`
	Template     string `json:"template"`
}

// answerOf is SearXNG's JSON answer with results and unresponsive engines.
func answerOf(t *testing.T, results []result, unresponsive [][]string) string {
	t.Helper()
	if results == nil {
		results = []result{}
	}
	if unresponsive == nil {
		unresponsive = [][]string{}
	}
	b, err := json.Marshal(map[string]any{
		"query": "x", "results": results, "answers": []any{}, "corrections": []any{},
		"infoboxes": []any{}, "suggestions": []any{}, "unresponsive_engines": unresponsive,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// pictures is n results of a page, each with its image and thumbnail.
func pictures(page, n int) []result {
	out := make([]result, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, result{
			ImgSrc:       fmt.Sprintf("https://img.example/%d/%d.jpg", page, i),
			ThumbnailSrc: fmt.Sprintf("https://thumb.example/%d/%d.jpg", page, i),
			URL:          "https://site.example/",
			Template:     "images.html",
		})
	}
	return out
}

func newSearx(t *testing.T, answer func(q url.Values) (int, string)) (*searx, *httptest.Server) {
	t.Helper()
	s := &searx{answer: answer}
	srv := httptest.NewServer(s)
	t.Cleanup(srv.Close)
	return s, srv
}

func newTestSearxng(t *testing.T, base string) *Searxng {
	t.Helper()
	store, _ := testCache(t)
	p, err := NewSearxng(base, store)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func srcs(p Page) []string {
	out := make([]string, 0, len(p.Items))
	for _, it := range p.Items {
		out = append(out, it.Source.URL)
	}
	return out
}

func nexts(p Page) []string {
	out := make([]string, 0, len(p.Items))
	for _, it := range p.Items {
		out = append(out, it.Next)
	}
	return out
}

// The request names the text, the images category, the JSON format, the
// page, the moderate safe search and the primary language of the app, on
// the path of the configured address.
func TestSearxngRequest(t *testing.T) {
	s, srv := newSearx(t, func(url.Values) (int, string) { return 200, answerOf(t, nil, nil) })
	p := newTestSearxng(t, srv.URL+"/searx/")
	if _, err := p.Search(context.Background(), Query{Text: "  red   cat ", Lang: "pt-br"}); err != nil {
		t.Fatal(err)
	}
	q := s.queries()
	if len(q) != 1 {
		t.Fatalf("%d requests", len(q))
	}
	want := url.Values{
		"q": {"red cat"}, "categories": {"images"}, "format": {"json"},
		"pageno": {"1"}, "safesearch": {"1"}, "language": {"pt"},
	}
	for k, v := range want {
		if !slices.Equal(q[0][k], v) {
			t.Errorf("%s = %q, want %q", k, q[0][k], v)
		}
	}
	if len(q[0]) != len(want) {
		t.Errorf("query %v", q[0])
	}
}

// Only a result with an http(s) image is a picture, each image once; its
// thumbnail, when it is an http(s) address too, is the fallback; the
// picture is imported as a photo keyed by its image.
func TestSearxngResults(t *testing.T) {
	results := []result{
		{ImgSrc: "https://a.example/1.jpg", ThumbnailSrc: "https://t.example/1.jpg"},
		{ThumbnailSrc: "https://t.example/no-image.jpg"},
		{ImgSrc: "data:image/png;base64,AAAA", ThumbnailSrc: "https://t.example/2.jpg"},
		{ImgSrc: "//protocol-relative.example/3.jpg"},
		{ImgSrc: "ftp://a.example/4.jpg"},
		{ImgSrc: "http://a.example/5.png", ThumbnailSrc: "data:image/jpeg;base64,AAAA"},
		{ImgSrc: "https://a.example/1.jpg", ThumbnailSrc: "https://t.example/dup.jpg"},
		{ImgSrc: "https://a.example/" + strings.Repeat("x", searxngMaxURL)},
		{ImgSrc: "https:///no-host.jpg"},
	}
	_, srv := newSearx(t, func(url.Values) (int, string) { return 200, answerOf(t, results, nil) })
	p, err := newTestSearxng(t, srv.URL).Search(context.Background(), Query{Text: "cat"})
	if err != nil {
		t.Fatal(err)
	}
	want := []inlinemedia.Source{
		{Provider: "searxng", Ref: "https://a.example/1.jpg", Kind: inlinemedia.KindPhoto, URL: "https://a.example/1.jpg", FallbackURL: "https://t.example/1.jpg"},
		{Provider: "searxng", Ref: "http://a.example/5.png", Kind: inlinemedia.KindPhoto, URL: "http://a.example/5.png"},
	}
	if len(p.Items) != len(want) {
		t.Fatalf("items %v", srcs(p))
	}
	for i, it := range p.Items {
		if it.Source == nil || *it.Source != want[i] || it.Document != nil || it.Photo != nil {
			t.Errorf("item %d = %+v, want %+v", i, it.Source, want[i])
		}
	}
	if p.CacheTime != searxngCacheTime {
		t.Errorf("cache time %d", p.CacheTime)
	}
}

// Pages of 12 walk the pictures of a page of SearXNG, then go on with its
// next page; each item's offset is the picture after it; the last page of
// SearXNG asked for ends the list.
func TestSearxngPages(t *testing.T) {
	s, srv := newSearx(t, func(q url.Values) (int, string) {
		var page int
		_, _ = fmt.Sscan(q.Get("pageno"), &page)
		return 200, answerOf(t, pictures(page, 15), nil)
	})
	p := newTestSearxng(t, srv.URL)
	ctx := context.Background()

	first, err := p.Search(ctx, Query{Text: "cat"})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != searxngPageSize || first.Next != "p1.12" {
		t.Fatalf("first page: %d items, next %q", len(first.Items), first.Next)
	}
	if first.Items[0].Source.URL != "https://img.example/1/0.jpg" || first.Items[0].Next != "p1.1" {
		t.Fatalf("first item %+v", first.Items[0])
	}

	second, err := p.Search(ctx, Query{Text: "cat", Offset: first.Next})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(nexts(second), []string{"p1.13", "p1.14", "p2.0"}) || second.Next != "p2.0" {
		t.Fatalf("second page: %v, next %q", nexts(second), second.Next)
	}

	third, err := p.Search(ctx, Query{Text: "cat", Offset: second.Next})
	if err != nil {
		t.Fatal(err)
	}
	if third.Items[0].Source.URL != "https://img.example/2/0.jpg" || third.Next != "p2.12" {
		t.Fatalf("third page: %v, next %q", srcs(third), third.Next)
	}

	last, err := p.Search(ctx, Query{Text: "cat", Offset: fmt.Sprintf("p%d.12", searxngMaxPage)})
	if err != nil {
		t.Fatal(err)
	}
	if len(last.Items) != 3 || last.Next != "" || last.Items[2].Next != "" {
		t.Fatalf("last page: %v, next %q", nexts(last), last.Next)
	}

	var pages []string
	for _, q := range s.queries() {
		pages = append(pages, q.Get("pageno"))
	}
	// The second page of @pic came from the page of SearXNG kept in Redis.
	if !slices.Equal(pages, []string{"1", "2", fmt.Sprint(searxngMaxPage)}) {
		t.Fatalf("pages asked %v", pages)
	}
}

// A page of SearXNG with no picture ends the list; an empty page of
// SearXNG is not an error and may be cached.
func TestSearxngNothingFound(t *testing.T) {
	_, srv := newSearx(t, func(url.Values) (int, string) { return 200, answerOf(t, nil, nil) })
	p, err := newTestSearxng(t, srv.URL).Search(context.Background(), Query{Text: "cat"})
	if err != nil || len(p.Items) != 0 || p.Next != "" || p.CacheTime != searxngCacheTime {
		t.Fatalf("page %+v, %v", p, err)
	}
}

// The pictures of a page of SearXNG are kept in Redis for the cache time,
// under a key without the text; a page kept with fewer pictures than an
// offset given before goes on with the next page.
func TestSearxngCache(t *testing.T) {
	var calls atomic.Int32
	_, srv := newSearx(t, func(q url.Values) (int, string) {
		calls.Add(1)
		var page int
		_, _ = fmt.Sscan(q.Get("pageno"), &page)
		return 200, answerOf(t, pictures(page, 5), nil)
	})
	store, mr := testCache(t)
	p, err := NewSearxng(srv.URL, store)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := p.Search(ctx, Query{Text: "secret words", Lang: "en"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Search(ctx, Query{Text: "secret words", Lang: "en"}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("%d requests for the same page", calls.Load())
	}
	keys := mr.Keys()
	if len(keys) != 1 || !strings.HasPrefix(keys[0], searxngListKey) || strings.Contains(keys[0], "secret") {
		t.Fatalf("keys %v", keys)
	}
	if ttl := mr.TTL(keys[0]); ttl != searxngCacheTime*time.Second {
		t.Fatalf("ttl %v", ttl)
	}
	// Another language is another search.
	if _, err := p.Search(ctx, Query{Text: "secret words", Lang: "ru"}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("%d requests", calls.Load())
	}
	// p1.7 was given when the first page had more than 5 pictures.
	page, err := p.Search(ctx, Query{Text: "secret words", Lang: "en", Offset: "p1.7"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 5 || page.Items[0].Source.URL != "https://img.example/2/0.jpg" {
		t.Fatalf("page %v", srcs(page))
	}
}

// Redis down: SearXNG is asked for each query and the answer stands.
func TestSearxngCacheDown(t *testing.T) {
	_, srv := newSearx(t, func(url.Values) (int, string) { return 200, answerOf(t, pictures(1, 2), nil) })
	store, mr := testCache(t)
	mr.Close()
	p, err := NewSearxng(srv.URL, store)
	if err != nil {
		t.Fatal(err)
	}
	page, err := p.Search(context.Background(), Query{Text: "cat"})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("page %v, %v", srcs(page), err)
	}
}

// An empty text, and an offset this provider did not give, are empty pages
// that ask nothing.
func TestSearxngNoQuery(t *testing.T) {
	s, srv := newSearx(t, func(url.Values) (int, string) { return 200, answerOf(t, pictures(1, 3), nil) })
	p := newTestSearxng(t, srv.URL)
	for _, q := range []Query{
		{},
		{Text: "   "},
		{Text: "!! :: <"},
		{Text: "cat", Offset: "12"},
		{Text: "cat", Offset: "p0.0"},
		{Text: "cat", Offset: fmt.Sprintf("p%d.0", searxngMaxPage+1)},
		{Text: "cat", Offset: "p1.-1"},
		{Text: "cat", Offset: "p01.2"},
		{Text: "cat", Offset: "p1.02"},
		{Text: "cat", Offset: "p1"},
		{Text: "cat", Offset: fmt.Sprintf("p1.%d", searxngListMax)},
		{Text: "cat", Offset: "x1.2"},
	} {
		page, err := p.Search(context.Background(), q)
		if err != nil || len(page.Items) != 0 || page.Next != "" {
			t.Errorf("%+v: page %+v, %v", q, page, err)
		}
	}
	if n := len(s.queries()); n != 0 {
		t.Fatalf("%d requests", n)
	}
}

// SearXNG that cannot answer is an error of the provider, which the bots
// service answers with an empty list it does not cache; no error names the
// address asked, which has the text.
func TestSearxngErrors(t *testing.T) {
	big := `{"results":[` + strings.Repeat(`{"img_src":"https://a.example/x.jpg"},`, searxngMaxBody/36) + `{}]}`
	cases := map[string]func(url.Values) (int, string){
		"403 without json":  func(url.Values) (int, string) { return 403, "<html>Forbidden</html>" },
		"400 bad parameter": func(url.Values) (int, string) { return 400, `{"error":"invalid language"}` },
		"500":               func(url.Values) (int, string) { return 500, `{"error":"search error"}` },
		"not JSON":          func(url.Values) (int, string) { return 200, "<html>results</html>" },
		"truncated JSON":    func(url.Values) (int, string) { return 200, `{"results":[{"img_src":"https://a.example/1.jpg"` },
		"over the size":     func(url.Values) (int, string) { return 200, big },
		"engines all failed": func(url.Values) (int, string) {
			return 200, answerOf(t, nil, [][]string{{"bing images", "Suspended: CAPTCHA"}, {"brave.images", "timeout"}})
		},
	}
	for name, answer := range cases {
		t.Run(name, func(t *testing.T) {
			_, srv := newSearx(t, answer)
			_, err := newTestSearxng(t, srv.URL).Search(context.Background(), Query{Text: "private words"})
			if err == nil {
				t.Fatal("no error")
			}
			if strings.Contains(err.Error(), "private") {
				t.Fatalf("the error has the text: %v", err)
			}
		})
	}
}

// Engines that failed while others gave pictures leave a page of what
// they gave.
func TestSearxngSomeEnginesFailed(t *testing.T) {
	_, srv := newSearx(t, func(url.Values) (int, string) {
		return 200, answerOf(t, pictures(1, 2), [][]string{{"bing images", "timeout"}})
	})
	page, err := newTestSearxng(t, srv.URL).Search(context.Background(), Query{Text: "cat"})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("page %v, %v", srcs(page), err)
	}
}

// A redirect (an external bang, a wrong address) is not followed.
func TestSearxngRedirect(t *testing.T) {
	var followed atomic.Bool
	mux := http.NewServeMux()
	mux.HandleFunc("/search", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	})
	mux.HandleFunc("/elsewhere", func(w http.ResponseWriter, r *http.Request) {
		followed.Store(true)
		_, _ = w.Write([]byte(answerOf(t, pictures(1, 1), nil)))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	if _, err := newTestSearxng(t, srv.URL).Search(context.Background(), Query{Text: "cat"}); err == nil || followed.Load() {
		t.Fatalf("redirect: %v, followed %v", err, followed.Load())
	}
}

// SearXNG that does not answer: the error names neither the address nor
// the text.
func TestSearxngUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close()
	_, err := newTestSearxng(t, base).Search(context.Background(), Query{Text: "private words"})
	if err == nil || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "search?") {
		t.Fatalf("error %v", err)
	}
}

// At most four requests go to SearXNG at once; the others wait for a turn
// and give up with the query's time.
func TestSearxngConcurrency(t *testing.T) {
	s, srv := newSearx(t, func(url.Values) (int, string) { return 200, answerOf(t, pictures(1, 1), nil) })
	s.hold = make(chan struct{})
	p := newTestSearxng(t, srv.URL)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := p.Search(context.Background(), Query{Text: fmt.Sprintf("query %d", i)})
			errs <- err
		}()
	}
	deadline := time.Now().Add(5 * time.Second)
	for s.inFlight.Load() < searxngConcurrent && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if n := s.inFlight.Load(); n != searxngConcurrent {
		t.Fatalf("%d requests at once", n)
	}
	close(s.hold)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if m := s.maxInFlight.Load(); m > searxngConcurrent {
		t.Fatalf("%d requests at once", m)
	}
}

// Queries that find the cache empty at once share one request; one that
// goes away before it ends gets its context's error and leaves the
// request to the others.
func TestSearxngSharedRequest(t *testing.T) {
	s, srv := newSearx(t, func(url.Values) (int, string) { return 200, answerOf(t, pictures(1, 3), nil) })
	s.hold = make(chan struct{})
	p := newTestSearxng(t, srv.URL)

	gone, cancel := context.WithCancel(context.Background())
	goneErr := make(chan error, 1)
	go func() {
		_, err := p.Search(gone, Query{Text: "cat"})
		goneErr <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for s.inFlight.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	got := make(chan Page, 1)
	go func() {
		page, err := p.Search(context.Background(), Query{Text: "cat"})
		if err != nil {
			t.Error(err)
		}
		got <- page
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := <-goneErr; err == nil {
		t.Fatal("no error for a query that went away")
	}
	close(s.hold)
	if page := <-got; len(page.Items) != 3 {
		t.Fatalf("page %v", srcs(page))
	}
	if n := len(s.queries()); n != 1 {
		t.Fatalf("%d requests", n)
	}
}

// SearXNG's query syntax is text for the user.
func TestSearxngText(t *testing.T) {
	for in, want := range map[string]string{
		"cat":           "cat",
		"  red\tcat \n": "red cat",
		"!bing cat":     "bing cat",
		"!!g cat":       "g cat",
		"cat :ru <3":    "cat ru 3",
		"!! cat":        "cat",
		"c!at wh:o":     "c!at wh:o",
		"кот !картинки": "кот картинки",
		"<<<:!!":        "",
		"what is this?": "what is this?",
		"   ":           "",
		" cat dog":      "cat dog",
	} {
		if got := searxngText(in); got != want {
			t.Errorf("searxngText(%q) = %q, want %q", in, got, want)
		}
	}
}

// The language is the primary subtag of the app's, in a form SearXNG
// takes, or all.
func TestSearxngLang(t *testing.T) {
	for in, want := range map[string]string{
		"en": "en", "EN": "en", "pt-br": "pt", "zh-hans": "zh", "en_US": "en",
		"fil": "fil", "": "all", "x": "all", "abcd": "all", "12": "all", "é": "all",
	} {
		if got := searxngLang(in); got != want {
			t.Errorf("searxngLang(%q) = %q, want %q", in, got, want)
		}
	}
}

// The address of SearXNG must be an http(s) address of a host.
func TestNewSearxng(t *testing.T) {
	store, _ := testCache(t)
	for _, bad := range []string{"searxng:8080", "ftp://searxng", "http://", "http://searxng/?a=b", "::"} {
		if _, err := NewSearxng(bad, store); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
	for _, good := range []string{"http://searxng:8080", "https://search.example/searx/"} {
		if _, err := NewSearxng(good, store); err != nil {
			t.Errorf("%q: %v", good, err)
		}
	}
}

// A value in Redis of another form is asked again, not trusted.
func TestSearxngListValue(t *testing.T) {
	list := []searxngPicture{{Src: "https://a.example/1.jpg", Thumb: "https://t.example/1.jpg"}, {Src: "http://b.example/2.png"}}
	v, err := encodeSearxngList(list)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := decodeSearxngList(v)
	if !ok || !slices.Equal(got, list) {
		t.Fatalf("decoded %v, %v", got, ok)
	}
	if got, ok := decodeSearxngList(searxngListTag + "[]"); !ok || got == nil || len(got) != 0 {
		t.Fatalf("empty list: %v, %v", got, ok)
	}
	for _, bad := range []string{"", "[]", "v0:[]", searxngListTag + "{", searxngListTag + `[{"s":"file:///etc/passwd"}]`} {
		if _, ok := decodeSearxngList(bad); ok {
			t.Errorf("%q decoded", bad)
		}
	}
}
