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

package demand

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/catalog"
	"github.com/teamgram/teamgram-server/pkg/catalog/importer"
)

var _ Importer = (*importer.Importer)(nil)

type fakeTelegram struct {
	asks, files atomic.Int32
	gate        chan struct{}
}

func (f *fakeTelegram) StickerSet(_ context.Context, in *mtproto.InputStickerSet) (*mtproto.Messages_StickerSet, error) {
	f.asks.Add(1)
	if f.gate != nil {
		<-f.gate
	}
	switch in.GetShortName() {
	case "Free420":
	case "StaticEmoji", "HugeEmoji":
		// A set of every emoji, as Telegram's: 1857 small files; HugeEmoji
		// states more bytes than a set may have.
		size := int64(4 << 10)
		if in.GetShortName() == "HugeEmoji" {
			size = MaxSet/1857 + 1
		}
		docs := make([]*mtproto.Document, 1857)
		for i := range docs {
			docs[i] = mtproto.MakeTLDocument(&mtproto.Document{Id: int64(i + 1), Size2_INT64: size}).To_Document()
		}
		return mtproto.MakeTLMessagesStickerSet(&mtproto.Messages_StickerSet{
			Set:       mtproto.MakeTLStickerSet(&mtproto.StickerSet{Id: 6, ShortName: in.GetShortName(), Emojis: true}).To_StickerSet(),
			Documents: docs,
		}).To_Messages_StickerSet(), nil
	default:
		return nil, mtproto.ErrStickersetInvalid
	}
	doc := func(id int64) *mtproto.Document {
		return mtproto.MakeTLDocument(&mtproto.Document{Id: id, Thumbs: []*mtproto.PhotoSize{
			mtproto.MakeTLPhotoPathSize(&mtproto.PhotoSize{Type: "j"}).To_PhotoSize(),
			mtproto.MakeTLPhotoSize(&mtproto.PhotoSize{Type: "m"}).To_PhotoSize(),
		}}).To_Document()
	}
	return mtproto.MakeTLMessagesStickerSet(&mtproto.Messages_StickerSet{
		Set:       mtproto.MakeTLStickerSet(&mtproto.StickerSet{Id: 5, ShortName: "Free420"}).To_StickerSet(),
		Documents: []*mtproto.Document{doc(1), doc(2)},
	}).To_Messages_StickerSet(), nil
}

func (f *fakeTelegram) File(_ context.Context, doc *mtproto.Document, thumb string, _ int) ([]byte, error) {
	f.files.Add(1)
	return []byte(thumb + "x"), nil
}

type fakeImporter struct {
	got map[[2]any][]byte
}

func (f *fakeImporter) ImportSet(ctx context.Context, set *mtproto.Messages_StickerSet, fetch func(context.Context, *mtproto.Document, string) ([]byte, error)) (*catalog.Set, error) {
	f.got = map[[2]any][]byte{}
	for _, d := range set.GetDocuments() {
		for _, t := range []string{"", "m"} {
			b, err := fetch(ctx, d, t)
			if err != nil {
				return nil, err
			}
			f.got[[2]any{d.GetId(), t}] = b
		}
	}
	return &catalog.Set{ID: 100, ShortName: set.GetSet().GetShortName()}, nil
}

func TestByShortName(t *testing.T) {
	tg := &fakeTelegram{gate: make(chan struct{})}
	im := &fakeImporter{}
	f := New(tg, im)
	var wg sync.WaitGroup
	sets := make([]*catalog.Set, 4)
	for i := range sets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := f.ByShortName(context.Background(), "Free420")
			if err != nil {
				t.Error(err)
			}
			sets[i] = s
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(tg.gate)
	wg.Wait()
	if tg.asks.Load() != 1 || tg.files.Load() != 4 || len(im.got) != 4 || string(im.got[[2]any{int64(1), "m"}]) != "mx" {
		t.Fatalf("asks %d, files %d, imported %v", tg.asks.Load(), tg.files.Load(), im.got)
	}
	for _, s := range sets {
		if s == nil || s.ID != 100 {
			t.Fatalf("sets %v", sets)
		}
	}
}

// A name Telegram has no set for is not asked again for an hour; a name
// that is no name is never asked.
func TestNoSet(t *testing.T) {
	tg := &fakeTelegram{}
	f := New(tg, &fakeImporter{})
	now := time.Unix(1000, 0)
	f.now = func() time.Time { return now }
	for range 2 {
		if _, err := f.ByShortName(context.Background(), "nothing"); !errors.Is(err, ErrNoSet) {
			t.Fatal(err)
		}
	}
	if tg.asks.Load() != 1 {
		t.Fatalf("asked %d times", tg.asks.Load())
	}
	now = now.Add(invalidFor + time.Second)
	f.ByShortName(context.Background(), "NOTHING")
	if tg.asks.Load() != 2 {
		t.Fatalf("asked %d times after an hour", tg.asks.Load())
	}
	for _, bad := range []string{"", "a b", "../x", "имя", "x;drop"} {
		if _, err := f.ByShortName(context.Background(), bad); !errors.Is(err, ErrNoSet) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if tg.asks.Load() != 2 {
		t.Fatal("a bad name was asked")
	}
}

// A set of every emoji, as the clients ask for by name (StaticEmoji), is
// fetched whole; one whose files are over the bytes of a set is not
// served: STICKERSET_INVALID, and not asked again for an hour.
func TestLargeSets(t *testing.T) {
	tg := &fakeTelegram{}
	f := New(tg, &countingImporter{})
	s, err := f.ByShortName(context.Background(), "StaticEmoji")
	if err != nil || s == nil {
		t.Fatalf("StaticEmoji: %v, %v", s, err)
	}
	if n := tg.files.Load(); n != 1857 {
		t.Fatalf("%d files fetched, want 1857", n)
	}
	before := tg.files.Load()
	for range 2 {
		if _, err := f.ByShortName(context.Background(), "HugeEmoji"); !errors.Is(err, ErrNoSet) {
			t.Fatalf("HugeEmoji: %v, want ErrNoSet", err)
		}
	}
	if tg.files.Load() != before {
		t.Fatal("files of a set over the limit were fetched")
	}
	if tg.asks.Load() != 2 {
		t.Fatalf("asked %d times, want 2 (HugeEmoji once)", tg.asks.Load())
	}
}

// countingImporter imports every document with its file.
type countingImporter struct{}

func (countingImporter) ImportSet(ctx context.Context, set *mtproto.Messages_StickerSet, fetch func(context.Context, *mtproto.Document, string) ([]byte, error)) (*catalog.Set, error) {
	for _, d := range set.GetDocuments() {
		if _, err := fetch(ctx, d, ""); err != nil {
			return nil, err
		}
	}
	return &catalog.Set{ID: 101, ShortName: set.GetSet().GetShortName()}, nil
}

// A file whose download stalls is tried again after fileFor, alone: the
// fetch does not wait for fetchFor.
func TestStalledFileTriedAgain(t *testing.T) {
	defer func(d time.Duration) { fileFor = d }(fileFor)
	fileFor = 50 * time.Millisecond
	src := &stallOnce{fakeTelegram: &fakeTelegram{}, stalled: map[string]bool{}}
	f := New(src, &fakeImporter{})
	if _, err := f.file(context.Background(), mtproto.MakeTLDocument(&mtproto.Document{Id: 9}).To_Document(), ""); err != nil {
		t.Fatalf("a stalled file: %v", err)
	}
	if src.tries != 2 {
		t.Fatalf("%d tries, want 2", src.tries)
	}
}

// stallOnce stalls the first download of each file until its try ends.
type stallOnce struct {
	*fakeTelegram
	mu      sync.Mutex
	stalled map[string]bool
	tries   int
}

func (s *stallOnce) File(ctx context.Context, doc *mtproto.Document, thumb string, max int) ([]byte, error) {
	s.mu.Lock()
	s.tries++
	key := fmt.Sprint(doc.GetId(), thumb)
	first := !s.stalled[key]
	s.stalled[key] = true
	s.mu.Unlock()
	if first {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return s.fakeTelegram.File(ctx, doc, thumb, max)
}
