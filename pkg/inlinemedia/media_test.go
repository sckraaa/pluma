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

package inlinemedia

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/webfetch"
)

// memRows is inline_media in memory.
type memRows struct {
	mu      sync.Mutex
	rows    map[Key]Row
	putErr  error
	touched int
}

func (m *memRows) Lookup(_ context.Context, provider string, keys []Key) (map[Key]Row, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[Key]Row{}
	for _, k := range keys {
		if r, ok := m.rows[k]; ok && r.Provider == provider {
			out[k] = r
		}
	}
	return out, nil
}

func (m *memRows) Put(_ context.Context, r Row) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.putErr != nil {
		return m.putErr
	}
	var k Key
	copy(k[:], r.SourceKey)
	if _, ok := m.rows[k]; !ok {
		m.rows[k] = r
	}
	return nil
}

func (m *memRows) Touch(_ context.Context, _ string, keys []Key, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.touched += len(keys)
	return nil
}

// memQuota is a budget of limit bytes.
type memQuota struct {
	mu           sync.Mutex
	spent, limit int64
}

func (q *memQuota) Spent(context.Context) (bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.spent >= q.limit, nil
}

func (q *memQuota) Take(_ context.Context, n int64) (bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.spent+n > q.limit {
		return false, nil
	}
	q.spent += n
	return true, nil
}

// sites serves files by address; an address it does not have fails.
// While hold is open, every fetch waits for it.
type sites struct {
	mu    sync.Mutex
	files map[string][]byte
	calls map[string]int
	hold  chan struct{}
}

func (s *sites) get(ctx context.Context, u *url.URL) ([]byte, error) {
	s.mu.Lock()
	s.calls[u.String()]++
	hold := s.hold
	s.mu.Unlock()
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.files[u.String()]
	if !ok {
		return nil, webfetch.ErrStatus
	}
	return b, nil
}

func (s *sites) Image(ctx context.Context, u *url.URL, _ string, limit int64) ([]byte, error) {
	b, err := s.get(ctx, u)
	if err == nil && int64(len(b)) > limit {
		return nil, webfetch.ErrTooLarge
	}
	return b, err
}

func (s *sites) Fetch(ctx context.Context, u *url.URL, _ string, limit int64) (webfetch.Body, error) {
	b, err := s.get(ctx, u)
	if err == nil && int64(len(b)) > limit {
		return webfetch.Body{}, webfetch.ErrTooLarge
	}
	return webfetch.Body{Bytes: b, MimeType: "video/mp4"}, err
}

func (s *sites) count(u string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[u]
}

// dfsFiles is dfs and the media service: what is uploaded can be read back.
type dfsFiles struct {
	mu        sync.Mutex
	next      int64
	photos    map[int64]*mtproto.Photo
	documents map[int64]*mtproto.Document
	owners    []int64
	gifs      []struct{ w, h int32 }
}

func newDfsFiles() *dfsFiles {
	return &dfsFiles{next: 100, photos: map[int64]*mtproto.Photo{}, documents: map[int64]*mtproto.Document{}}
}

func (d *dfsFiles) UploadPhotoOf(_ context.Context, owner int64, jpeg []byte, _ string) (*mtproto.Photo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(jpeg) < 2 || jpeg[0] != 0xff || jpeg[1] != 0xd8 {
		return nil, errors.New("not a JPEG")
	}
	d.next++
	p := mtproto.MakeTLPhoto(&mtproto.Photo{Id: d.next, AccessHash: 1}).To_Photo()
	d.photos[d.next] = p
	d.owners = append(d.owners, owner)
	return p, nil
}

func (d *dfsFiles) UploadAnimation(_ context.Context, owner int64, _ []byte, w, h int32, _ float64) (*mtproto.Document, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.next++
	doc := mtproto.MakeTLDocument(&mtproto.Document{Id: d.next, AccessHash: 1, MimeType: "video/mp4"}).To_Document()
	d.documents[d.next] = doc
	d.owners = append(d.owners, owner)
	d.gifs = append(d.gifs, struct{ w, h int32 }{w, h})
	return doc, nil
}

func (d *dfsFiles) MediaGetPhoto(_ context.Context, in *media.TLMediaGetPhoto) (*mtproto.Photo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if p, ok := d.photos[in.PhotoId]; ok {
		return p, nil
	}
	return mtproto.MakeTLPhotoEmpty(&mtproto.Photo{Id: in.PhotoId}).To_Photo(), nil
}

func (d *dfsFiles) MediaGetDocumentList(_ context.Context, in *media.TLMediaGetDocumentList) (*media.Vector_Document, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := &media.Vector_Document{}
	for _, id := range in.IdList {
		if doc, ok := d.documents[id]; ok {
			out.Datas = append(out.Datas, doc)
		}
	}
	return out, nil
}

func (d *dfsFiles) uploads() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.owners)
}

// picture is a PNG of 64 x 48.
func picture(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for x := 0; x < 64; x++ {
		img.Set(x, x%48, color.RGBA{R: 200, A: 255})
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

type fixture struct {
	rows  *memRows
	quota *memQuota
	sites *sites
	files *dfsFiles
	im    *Importer
}

func newFixture(limit int64) *fixture {
	f := &fixture{
		rows:  &memRows{rows: map[Key]Row{}},
		quota: &memQuota{limit: limit},
		sites: &sites{files: map[string][]byte{}, calls: map[string]int{}},
		files: newDfsFiles(),
	}
	f.im = NewImporter(f.rows, f.quota, f.sites, f.files, f.files)
	return f
}

func photoSource(i int) Source {
	u := "https://pics.example/" + strconv.Itoa(i) + ".png"
	return Source{Provider: "searxng", Ref: u, Kind: KindPhoto, URL: u}
}

const bot = int64(1000102)

// A medium found again is served from inline_media, not fetched again; the
// files are the bot's.
func TestEnsureImportsOnce(t *testing.T) {
	f := newFixture(1 << 30)
	src := photoSource(1)
	f.sites.files[src.URL] = picture(t)

	got, err := f.im.Ensure(context.Background(), bot, []Source{src})
	if err != nil || got.Polled != 1 || got.Media[0] == nil || got.Media[0].Photo == nil {
		t.Fatalf("first: %+v, %v", got, err)
	}
	id := got.Media[0].Photo.GetId()
	again, err := f.im.Ensure(context.Background(), bot, []Source{src})
	if err != nil || again.Polled != 1 || again.Media[0] == nil || again.Media[0].Photo.GetId() != id {
		t.Fatalf("again: %+v, %v", again, err)
	}
	if n := f.sites.count(src.URL); n != 1 {
		t.Errorf("fetched %d times, want 1", n)
	}
	if f.files.uploads() != 1 || f.files.owners[0] != bot {
		t.Errorf("uploads %v, want one of the bot", f.files.owners)
	}
	if f.rows.touched != 1 {
		t.Errorf("touched %d rows, want 1", f.rows.touched)
	}
	row := f.rows.rows[src.Key()]
	if row.MediaId != id || row.Kind != KindPhoto || row.Bytes <= 0 || row.Provider != "searxng" {
		t.Errorf("row %+v", row)
	}
}

// Past the day's budget nothing is fetched; a file that does not fit is
// not stored.
func TestEnsureBudget(t *testing.T) {
	pic := picture(t)
	f := newFixture(10) // smaller than any JPEG
	src := photoSource(1)
	f.sites.files[src.URL] = pic
	got, err := f.im.Ensure(context.Background(), bot, []Source{src})
	if err != nil || got.Polled != 1 || got.Media[0] != nil {
		t.Fatalf("over the budget: %+v, %v", got, err)
	}
	if f.files.uploads() != 0 || len(f.rows.rows) != 0 {
		t.Fatalf("stored past the budget")
	}
	f.quota.spent = 10 // spent: no fetch at all
	other := photoSource(2)
	f.sites.files[other.URL] = pic
	if _, err := f.im.Ensure(context.Background(), bot, []Source{other}); err != nil {
		t.Fatal(err)
	}
	if n := f.sites.count(other.URL); n != 0 {
		t.Errorf("fetched %d times with the budget spent", n)
	}
}

// The deadline of the query stops new imports: the sources tried are the
// first ones, the page goes on from the next; the imports cut short finish
// in the background and the next query has them.
func TestEnsureDeadline(t *testing.T) {
	f := newFixture(1 << 30)
	f.im.fileTimeout = 5 * time.Second
	pic := picture(t)
	sources := make([]Source, PerRequest+2)
	for i := range sources {
		sources[i] = photoSource(i)
		f.sites.files[sources[i].URL] = pic
	}
	f.sites.hold = make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	got, err := f.im.Ensure(ctx, bot, sources)
	if err != nil {
		t.Fatal(err)
	}
	if got.Polled != PerRequest || len(got.Media) != PerRequest {
		t.Fatalf("polled %d (%d media), want %d", got.Polled, len(got.Media), PerRequest)
	}
	for i, m := range got.Media {
		if m != nil {
			t.Errorf("medium %d ready before its fetch", i)
		}
	}
	close(f.sites.hold)
	for deadline := time.Now().Add(5 * time.Second); f.files.uploads() < PerRequest; {
		if time.Now().After(deadline) {
			t.Fatalf("%d background imports finished, want %d", f.files.uploads(), PerRequest)
		}
		time.Sleep(10 * time.Millisecond)
	}
	got, err = f.im.Ensure(context.Background(), bot, sources)
	if err != nil || got.Polled != len(sources) {
		t.Fatalf("next query: %+v, %v", got, err)
	}
	for i, m := range got.Media {
		if m == nil || m.Photo == nil {
			t.Errorf("medium %d missing", i)
		}
	}
	if f.files.uploads() != len(sources) {
		t.Errorf("%d uploads, want %d", f.files.uploads(), len(sources))
	}
}

// Two queries that find the same medium at once import it once.
func TestEnsureOneImportPerKey(t *testing.T) {
	f := newFixture(1 << 30)
	src := photoSource(1)
	f.sites.files[src.URL] = picture(t)
	f.sites.hold = make(chan struct{})
	var wg sync.WaitGroup
	results := make([]Imported, 2)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := f.im.Ensure(context.Background(), bot, []Source{src})
			if err != nil {
				t.Error(err)
			}
			results[i] = got
		}()
	}
	time.Sleep(100 * time.Millisecond)
	close(f.sites.hold)
	wg.Wait()
	for i, r := range results {
		if r.Polled != 1 || r.Media[0] == nil {
			t.Errorf("query %d: %+v", i, r)
		}
	}
	if n := f.sites.count(src.URL); n != 1 {
		t.Errorf("fetched %d times, want 1", n)
	}
	if f.files.uploads() != 1 {
		t.Errorf("%d uploads, want 1", f.files.uploads())
	}
}

// The fallback address is fetched when the file fails; a medium with
// neither is left out and the rest are served.
func TestEnsureFallback(t *testing.T) {
	f := newFixture(1 << 30)
	src := photoSource(1)
	src.FallbackURL = "https://thumbs.example/1.png"
	f.sites.files[src.FallbackURL] = picture(t)
	broken := photoSource(2)
	got, err := f.im.Ensure(context.Background(), bot, []Source{src, broken})
	if err != nil || got.Polled != 2 {
		t.Fatalf("%+v, %v", got, err)
	}
	if got.Media[0] == nil || got.Media[1] != nil {
		t.Fatalf("media %v", got.Media)
	}
}

// A GIF is fetched as MP4 and stored as an animation of its size; one
// without a size, or over 1280 px, is not imported.
func TestEnsureGif(t *testing.T) {
	f := newFixture(1 << 30)
	gif := func(i int, w, h int32) Source {
		u := "https://gifs.example/" + strconv.Itoa(i) + ".mp4"
		f.sites.files[u] = []byte("mp4 of " + u)
		return Source{Provider: "klipy", Ref: strconv.Itoa(i), ProviderRef: strconv.Itoa(i), Kind: KindGif, URL: u, Width: w, Height: h, Duration: 2.5}
	}
	got, err := f.im.Ensure(context.Background(), 1000101, []Source{gif(1, 320, 240), gif(2, 0, 0), gif(3, 2000, 100)})
	if err != nil || got.Polled != 3 {
		t.Fatalf("%+v, %v", got, err)
	}
	if got.Media[0] == nil || got.Media[0].Document == nil || got.Media[1] != nil || got.Media[2] != nil {
		t.Fatalf("media %v", got.Media)
	}
	if len(f.files.gifs) != 1 || f.files.gifs[0].w != 320 || f.files.gifs[0].h != 240 {
		t.Fatalf("animations %v", f.files.gifs)
	}
	again, err := f.im.Ensure(context.Background(), 1000101, []Source{gif(1, 320, 240)})
	if err != nil || again.Media[0] == nil || again.Media[0].Document.GetId() != got.Media[0].Document.GetId() {
		t.Fatalf("again: %+v, %v", again, err)
	}
	if f.rows.rows[KeyOf("klipy", "1")].ProviderRef != "1" {
		t.Errorf("provider_ref not kept")
	}
}

// A row that cannot be written leaves the file served now, imported again
// next time.
func TestEnsureOrphan(t *testing.T) {
	f := newFixture(1 << 30)
	f.rows.putErr = errors.New("down")
	src := photoSource(1)
	f.sites.files[src.URL] = picture(t)
	for i := 1; i <= 2; i++ {
		got, err := f.im.Ensure(context.Background(), bot, []Source{src})
		if err != nil || got.Media[0] == nil {
			t.Fatalf("query %d: %+v, %v", i, got, err)
		}
		if f.files.uploads() != i {
			t.Fatalf("query %d: %d uploads", i, f.files.uploads())
		}
	}
}

// One list is of one provider and kind.
func TestEnsureMixedList(t *testing.T) {
	f := newFixture(1 << 30)
	other := photoSource(2)
	other.Provider = "klipy"
	if _, err := f.im.Ensure(context.Background(), bot, []Source{photoSource(1), other}); err == nil {
		t.Fatal("a list of two providers taken")
	}
}

func TestKeyOf(t *testing.T) {
	a, b := KeyOf("searxng", "https://x.example/1.jpg"), KeyOf("klipy", "https://x.example/1.jpg")
	if a == b {
		t.Error("the provider is not in the key")
	}
	if a != KeyOf("searxng", "https://x.example/1.jpg") {
		t.Error("the key is not stable")
	}
	// No ambiguity between the provider and the reference.
	if KeyOf("ab", "c") == KeyOf("a", "bc") {
		t.Error("the parts run together")
	}
	if s := a.String(); len(s) != 32 {
		t.Errorf("key %q", s)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("абв", 3); got != "а" {
		t.Errorf("%q", got)
	}
	if got := truncate("abc", 3); got != "abc" {
		t.Errorf("%q", got)
	}
}
