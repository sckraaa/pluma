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

package webpage

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/zeromicro/go-zero/core/logx"
)

// memCache is the cache of a test.
type memCache struct {
	mu   sync.Mutex
	rows map[string]*cached
}

func (m *memCache) get(_ context.Context, u string) (*cached, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.rows[u]; ok {
		cp := *c
		return &cp, nil
	}
	return nil, nil
}

func (m *memCache) byID(_ context.Context, id int64) (*mtproto.WebPage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.rows {
		if c.id == id {
			return c.page, nil
		}
	}
	return nil, nil
}

func (m *memCache) put(_ context.Context, u string, page *mtproto.WebPage, _, expires int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := page.GetId()
	if c, ok := m.rows[u]; ok {
		id = c.id // as ON DUPLICATE KEY UPDATE keeps webpage_id
	}
	m.rows[u] = &cached{id: id, page: page, expires: expires}
	return nil
}

type fakePhotos struct {
	n atomic.Int64
}

func (f *fakePhotos) UploadPhoto(_ context.Context, jpg []byte) (*mtproto.Photo, error) {
	if len(jpg) == 0 {
		return nil, errors.New("no picture")
	}
	return mtproto.MakeTLPhoto(&mtproto.Photo{Id: 1000 + f.n.Add(1)}).To_Photo(), nil
}

type testEnv struct {
	s      *Service
	cache  *memCache
	photos *fakePhotos
	hits   atomic.Int64
	clock  time.Time
}

// newTestEnv serves every site of the test from h: whatever host a link
// names, the fetcher connects to the test server.
func newTestEnv(t *testing.T, h http.Handler) *testEnv {
	t.Helper()
	env := &testEnv{cache: &memCache{rows: map[string]*cached{}}, photos: &fakePhotos{}, clock: time.Unix(1_800_000_000, 0)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env.hits.Add(1)
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	dial := func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, srv.Listener.Addr().String())
	}
	env.s = &Service{
		store:  env.cache,
		fetch:  newFetcher(dial),
		photos: env.photos,
		now:    func() time.Time { return env.clock },
		logger: logx.WithContext(context.Background()),
	}
	return env
}

func serveHTML(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

func TestPreviewCard(t *testing.T) {
	var png []byte
	env := newTestEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/post":
			if r.Header.Get("User-Agent") != userAgent {
				t.Errorf("User-Agent %q", r.Header.Get("User-Agent"))
			}
			serveHTML(w, `<head><meta property="og:title" content="Post"><meta property="og:description" content="About">
<meta property="og:site_name" content="Site"><meta property="og:image" content="/img/p.png"></head>`)
		case "/img/p.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
		default:
			http.NotFound(w, r)
		}
	}))
	png = testPNG(t, 600, 315)
	ctx := context.Background()
	p, err := env.s.Preview(ctx, "http://www.Site.example/post#top")
	if err != nil {
		t.Fatal(err)
	}
	if p.GetPredicateName() != mtproto.Predicate_webPage || p.GetUrl_STRING() != "http://site.example/post" ||
		p.GetDisplayUrl() != "site.example/post" || p.GetTitle().GetValue() != "Post" ||
		p.GetDescription().GetValue() != "About" || p.GetSiteName().GetValue() != "Site" ||
		p.GetType().GetValue() != "article" || p.GetPhoto().GetId() != 1001 {
		t.Fatalf("card %v", p)
	}
	if env.hits.Load() != 2 {
		t.Fatalf("%d requests, want the page and its picture", env.hits.Load())
	}
	// Fresh: from the cache, by link and by id.
	again, err := env.s.Preview(ctx, "site.example/post")
	if err != nil || again.GetId() != p.GetId() || env.hits.Load() != 2 {
		t.Fatalf("again: %v, %v, %d requests", again, err, env.hits.Load())
	}
	if byID, err := env.s.ByID(ctx, p.GetId()); err != nil || byID.GetTitle().GetValue() != "Post" {
		t.Fatalf("by id: %v, %v", byID, err)
	}
	// Stale after a day: fetched again, under the same id.
	env.clock = env.clock.Add(pageTTL + time.Second)
	if c, _ := env.cache.get(ctx, "http://site.example/post"); c == nil {
		t.Fatal("a stale card is not kept")
	}
	refreshed, err := env.s.Preview(ctx, "http://site.example/post")
	if err != nil || refreshed.GetId() != p.GetId() || env.hits.Load() != 4 {
		t.Fatalf("refreshed: %v, %v, %d requests", refreshed, err, env.hits.Load())
	}
}

// Pages that get no card: Telegram answers them with webPageEmpty.
func TestPreviewEmpty(t *testing.T) {
	env := newTestEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/title":
			serveHTML(w, `<head><title>Only a title</title></head>`)
		case "/file":
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write([]byte("PK"))
		case "/inside":
			http.Redirect(w, r, "http://127.0.0.1:6379/secret", http.StatusFound)
		case "/secret":
			serveHTML(w, `<head><meta property="og:description" content="Internal"></head>`)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	ctx := context.Background()
	for _, tc := range []struct {
		path string
		ttl  time.Duration
	}{
		{"/title", emptyTTL},
		{"/file", failedTTL},
		{"/missing", failedTTL},
		{"/inside", failedTTL},
		{"/loop", failedTTL},
	} {
		link := "http://site.example" + tc.path
		p, err := env.s.Preview(ctx, link)
		if err != nil {
			t.Fatalf("%s: %v", tc.path, err)
		}
		if p.GetPredicateName() != mtproto.Predicate_webPageEmpty || p.GetUrl_FLAGSTRING().GetValue() != link || p.GetId() == 0 {
			t.Fatalf("%s: %v", tc.path, p)
		}
		c, _ := env.cache.get(ctx, link)
		if c == nil || c.expires != env.clock.Add(tc.ttl).Unix() {
			t.Fatalf("%s: stored %+v, want for %v", tc.path, c, tc.ttl)
		}
	}
}

func TestPreviewNoLink(t *testing.T) {
	env := newTestEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("fetched %s", r.URL)
	}))
	for _, link := range []string{"http://192.168.0.1/", "http://localhost:8080/", "ftp://site.example/", "http://[::1]/"} {
		if _, err := env.s.Preview(context.Background(), link); !errors.Is(err, ErrNoLink) {
			t.Errorf("%s: %v, want ErrNoLink", link, err)
		}
	}
}

// A picture that cannot be used leaves the card without one; a card with a
// picture and nothing else is empty then.
func TestPreviewBadPicture(t *testing.T) {
	env := newTestEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/with-text":
			serveHTML(w, `<head><meta property="og:description" content="Text"><meta property="og:image" content="/broken.png"></head>`)
		case "/only-picture":
			serveHTML(w, `<head><meta property="og:image" content="/broken.png"></head>`)
		case "/broken.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("not a png"))
		}
	}))
	ctx := context.Background()
	p, err := env.s.Preview(ctx, "http://site.example/with-text")
	if err != nil || p.GetPredicateName() != mtproto.Predicate_webPage || p.GetPhoto() != nil {
		t.Fatalf("with text: %v, %v", p, err)
	}
	p, err = env.s.Preview(ctx, "http://site.example/only-picture")
	if err != nil || p.GetPredicateName() != mtproto.Predicate_webPageEmpty {
		t.Fatalf("only a picture: %v, %v", p, err)
	}
}

// All who ask for a link meanwhile wait for one fetch; one who stops
// waiting does not stop it, and the card is stored for the next ask.
func TestPreviewFetchedOnce(t *testing.T) {
	release := make(chan struct{})
	env := newTestEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		serveHTML(w, `<head><meta property="og:description" content="Slow"></head>`)
	}))
	short, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := env.s.Preview(short, "http://slow.example/"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("an impatient caller: %v", err)
	}
	var wg sync.WaitGroup
	ids := make([]int64, 5)
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := env.s.Preview(context.Background(), "http://slow.example/")
			if err != nil {
				t.Error(err)
				return
			}
			ids[i] = p.GetId()
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	for _, id := range ids {
		if id == 0 || id != ids[0] {
			t.Fatalf("ids %v", ids)
		}
	}
	if env.hits.Load() != 1 {
		t.Fatalf("%d fetches, want 1", env.hits.Load())
	}
}

// A page much larger than its head, or a picture over the limit, is not
// read to its end.
func TestPreviewLimits(t *testing.T) {
	big := make([]byte, maxImage+1)
	env := newTestEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			serveHTML(w, `<head><meta property="og:description" content="D"><meta property="og:image" content="/big.png"></head>`)
		case "/big.png":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(big)
		}
	}))
	p, err := env.s.Preview(context.Background(), "http://site.example/")
	if err != nil || p.GetPredicateName() != mtproto.Predicate_webPage || p.GetPhoto() != nil {
		t.Fatalf("%v, %v", p, err)
	}
	if env.photos.n.Load() != 0 {
		t.Fatal("a picture over the limit was stored")
	}
}
