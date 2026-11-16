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
	"errors"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/media/media"

	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// counts is a popular list, which counts its loads; a load waits for
// release when it is set.
type counts struct {
	mu      sync.Mutex
	ids     []int64
	err     error
	calls   int
	release chan struct{}
}

func (c *counts) PopularGifs(_ context.Context, _ time.Time, limit int) ([]int64, error) {
	if c.release != nil {
		<-c.release
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if limit != localPopularMax {
		return nil, errors.New("limit " + strconv.Itoa(limit))
	}
	return c.ids, c.err
}

func (c *counts) loads() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// documents has the documents of docs, answered in reverse order as the
// media service gives no order; an id it has not is gone. Queries may ask
// it at once.
type documents struct {
	mu    sync.Mutex
	docs  map[int64]*mtproto.Document
	err   error
	asked [][]int64
}

func (d *documents) MediaGetDocumentList(_ context.Context, in *media.TLMediaGetDocumentList) (*media.Vector_Document, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.asked = append(d.asked, slices.Clone(in.IdList))
	if d.err != nil {
		return nil, d.err
	}
	out := &media.Vector_Document{}
	for i := len(in.IdList) - 1; i >= 0; i-- {
		if doc, ok := d.docs[in.IdList[i]]; ok {
			out.Datas = append(out.Datas, doc)
		}
	}
	return out, nil
}

func animation(id int64) *mtproto.Document {
	return mtproto.MakeTLDocument(&mtproto.Document{
		Id: id, AccessHash: id * 7, MimeType: "video/mp4",
		Attributes: []*mtproto.DocumentAttribute{
			mtproto.MakeTLDocumentAttributeAnimated(nil).To_DocumentAttribute(),
			mtproto.MakeTLDocumentAttributeVideo(&mtproto.DocumentAttribute{W: 320, H: 240}).To_DocumentAttribute(),
		},
	}).To_Document()
}

func testCache(t *testing.T) (kv.Store, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	return kv.NewStore(kv.KvConf{cache.NodeConf{
		RedisConf: redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType, DisableIdentity: true},
		Weight:    100,
	}}), mr
}

func ids(p Page) []int64 {
	out := make([]int64, 0, len(p.Items))
	for _, it := range p.Items {
		out = append(out, it.Document.GetId())
	}
	return out
}

func seq(from, to int64) []int64 {
	var out []int64
	for i := from; i <= to; i++ {
		out = append(out, i)
	}
	return out
}

// The empty query pages through the popular list in its order, 20 GIFs a
// page; a document that is gone or not a GIF is skipped and the page takes
// the next ones; the offset of each item and of the page go on from where
// it ends, and the last page has no next offset.
func TestLocalPopularPages(t *testing.T) {
	list := seq(1, 45)
	docs := &documents{docs: map[int64]*mtproto.Document{}}
	for _, id := range list {
		docs.docs[id] = animation(id)
	}
	delete(docs.docs, 3)                                                                                  // deleted
	docs.docs[5] = mtproto.MakeTLDocumentEmpty(&mtproto.Document{Id: 5}).To_Document()                    // gone
	docs.docs[7] = mtproto.MakeTLDocument(&mtproto.Document{Id: 7, MimeType: "video/mp4"}).To_Document()  // a video
	docs.docs[8] = mtproto.MakeTLDocument(&mtproto.Document{Id: 8, MimeType: "image/gif"}).To_Document()  // a GIF file
	docs.docs[9] = mtproto.MakeTLDocument(&mtproto.Document{Id: 9, MimeType: "image/webp"}).To_Document() // a sticker
	store, _ := testCache(t)
	l := NewLocal(&counts{ids: list}, docs, store)
	ctx := context.Background()

	first, err := l.Search(ctx, Query{})
	if err != nil {
		t.Fatal(err)
	}
	want := []int64{1, 2, 4, 6, 8}
	want = append(want, seq(10, 24)...)
	if got := ids(first); !slices.Equal(got, want) {
		t.Fatalf("first page %v, want %v", got, want)
	}
	if first.Next != "24" || first.CacheTime != localCacheTime {
		t.Fatalf("first page next %q, cache %d", first.Next, first.CacheTime)
	}
	if first.Items[0].Next != "1" || first.Items[2].Next != "4" || first.Items[19].Next != "24" {
		t.Fatalf("item offsets %q %q %q", first.Items[0].Next, first.Items[2].Next, first.Items[19].Next)
	}
	// The skipped documents were made up for by a second ask.
	if len(docs.asked) != 2 || len(docs.asked[0]) != 20 || !slices.Equal(docs.asked[1], seq(21, 24)) {
		t.Fatalf("asked %v", docs.asked)
	}

	second, err := l.Search(ctx, Query{Offset: first.Next})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(second); !slices.Equal(got, seq(25, 44)) || second.Next != "44" {
		t.Fatalf("second page %v next %q", got, second.Next)
	}
	last, err := l.Search(ctx, Query{Offset: second.Next})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(last); !slices.Equal(got, []int64{45}) || last.Next != "" || last.Items[0].Next != "" {
		t.Fatalf("last page %v next %q", got, last.Next)
	}
}

// A text finds nothing yet: an empty page kept as long as the popular
// ones, without counting anything.
func TestLocalText(t *testing.T) {
	c := &counts{ids: []int64{1}}
	docs := &documents{docs: map[int64]*mtproto.Document{1: animation(1)}}
	store, _ := testCache(t)
	p, err := NewLocal(c, docs, store).Search(context.Background(), Query{Text: "cat"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 0 || p.Next != "" || p.CacheTime != localCacheTime || c.loads() != 0 || len(docs.asked) != 0 {
		t.Fatalf("page %+v, loads %d, asked %v", p, c.loads(), docs.asked)
	}
}

// An offset the provider did not give, or one past the list, is an empty
// page.
func TestLocalOffsets(t *testing.T) {
	docs := &documents{docs: map[int64]*mtproto.Document{1: animation(1), 2: animation(2)}}
	store, _ := testCache(t)
	l := NewLocal(&counts{ids: []int64{1, 2}}, docs, store)
	for _, off := range []string{"0", "-1", "01", "+1", "x", "1.5", "201", "2", "99999999999999999999"} {
		p, err := l.Search(context.Background(), Query{Offset: off})
		if err != nil {
			t.Fatalf("offset %q: %v", off, err)
		}
		if len(p.Items) != 0 || p.Next != "" {
			t.Fatalf("offset %q: %+v", off, p)
		}
	}
	p, err := l.Search(context.Background(), Query{Offset: "1"})
	if err != nil || !slices.Equal(ids(p), []int64{2}) || p.Next != "" {
		t.Fatalf("offset 1: %+v, %v", p, err)
	}
}

// Nobody has saved or sent a GIF yet: an empty page, and the empty list is
// kept like any other.
func TestLocalNothingPopular(t *testing.T) {
	c := &counts{}
	store, _ := testCache(t)
	l := NewLocal(c, &documents{}, store)
	for i := 0; i < 2; i++ {
		p, err := l.Search(context.Background(), Query{})
		if err != nil || len(p.Items) != 0 || p.Next != "" || p.CacheTime != localCacheTime {
			t.Fatalf("page %+v, %v", p, err)
		}
	}
	if c.loads() != 1 {
		t.Fatalf("loads %d, want 1", c.loads())
	}
}

// The list is counted once and kept in Redis for the cache time: the next
// queries, from this replica or another, read it there; after it expires,
// or when the value is of another form, it is counted again.
func TestLocalCache(t *testing.T) {
	c := &counts{ids: []int64{3, 1, 2}}
	docs := &documents{docs: map[int64]*mtproto.Document{1: animation(1), 2: animation(2), 3: animation(3)}}
	store, mr := testCache(t)
	ctx := context.Background()
	if _, err := NewLocal(c, docs, store).Search(ctx, Query{}); err != nil {
		t.Fatal(err)
	}
	if v, _ := mr.Get(localPopularKey); v != "v1:3,1,2" {
		t.Fatalf("kept %q", v)
	}
	if ttl := mr.TTL(localPopularKey); ttl != localCacheTime*time.Second {
		t.Fatalf("kept for %v", ttl)
	}
	other := NewLocal(c, docs, store)
	p, err := other.Search(ctx, Query{})
	if err != nil || !slices.Equal(ids(p), []int64{3, 1, 2}) || c.loads() != 1 {
		t.Fatalf("page %v, %v, loads %d", ids(p), err, c.loads())
	}

	mr.FastForward(localCacheTime * time.Second)
	c.ids = []int64{2}
	if p, err = other.Search(ctx, Query{}); err != nil || !slices.Equal(ids(p), []int64{2}) || c.loads() != 2 {
		t.Fatalf("after expiry %v, %v, loads %d", ids(p), err, c.loads())
	}

	if err = mr.Set(localPopularKey, "3,1,2"); err != nil {
		t.Fatal(err)
	}
	if p, err = other.Search(ctx, Query{}); err != nil || !slices.Equal(ids(p), []int64{2}) || c.loads() != 3 {
		t.Fatalf("another form %v, %v, loads %d", ids(p), err, c.loads())
	}
}

// Redis down: the list is counted for each query and the answer stands.
func TestLocalCacheDown(t *testing.T) {
	c := &counts{ids: []int64{1}}
	store, mr := testCache(t)
	mr.Close()
	p, err := NewLocal(c, &documents{docs: map[int64]*mtproto.Document{1: animation(1)}}, store).Search(context.Background(), Query{})
	if err != nil || !slices.Equal(ids(p), []int64{1}) {
		t.Fatalf("page %v, %v", ids(p), err)
	}
}

// A failed count or documents service is an error of the provider: the
// bots service answers with an empty list it does not cache.
func TestLocalErrors(t *testing.T) {
	store, _ := testCache(t)
	if _, err := NewLocal(&counts{err: errors.New("mysql down")}, &documents{}, store).Search(context.Background(), Query{}); err == nil {
		t.Fatal("no error for a failed count")
	}
	docs := &documents{err: errors.New("media down")}
	if _, err := NewLocal(&counts{ids: []int64{1}}, docs, store).Search(context.Background(), Query{}); err == nil {
		t.Fatal("no error for failed documents")
	}
}

// Queries that find the cache empty at once share one count; one that
// goes away before it ends gets its context's error and leaves the count
// to the others.
func TestLocalSharedLoad(t *testing.T) {
	c := &counts{ids: []int64{1}, release: make(chan struct{})}
	docs := &documents{docs: map[int64]*mtproto.Document{1: animation(1)}}
	store, _ := testCache(t)
	l := NewLocal(c, docs, store)

	gone, cancel := context.WithCancel(context.Background())
	goneErr := make(chan error, 1)
	go func() {
		_, err := l.Search(gone, Query{})
		goneErr <- err
	}()
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		pages [][]int64
	)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := l.Search(context.Background(), Query{})
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			pages = append(pages, ids(p))
			mu.Unlock()
		}()
	}
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := <-goneErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("the query that went away: %v", err)
	}
	close(c.release)
	wg.Wait()
	if c.loads() != 1 {
		t.Fatalf("loads %d, want 1", c.loads())
	}
	for _, p := range pages {
		if !slices.Equal(p, []int64{1}) {
			t.Fatalf("pages %v", pages)
		}
	}
}
