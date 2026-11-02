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

// Package demand fetches a sticker or emoji set from Telegram when a user
// asks for one the server lacks (task 030): by its short name, as a link
// t.me/addstickers/<name> or t.me/addemoji/<name> asks, through the bot of
// pkg/tgbot, imported as the catalog is (pkg/catalog/importer.ImportSet).
// One fetch serves everyone who asks for a name meanwhile; a name Telegram
// has no set for is not asked again for an hour.
package demand

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/catalog"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

// Limits of a set fetched on demand.
const (
	MaxDocuments = 200      // stickers or emoji in a set
	MaxFile      = 10 << 20 // one file
	MaxSet       = 64 << 20 // all files of a set
	parallel     = 8
	invalidFor   = time.Hour
	fetchFor     = 90 * time.Second
)

// A short name of a set: letters, digits and underscores (Telegram's
// stickers.checkShortName rule).
var validName = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)

// Source is Telegram (tgbot.Bot).
type Source interface {
	StickerSet(ctx context.Context, in *mtproto.InputStickerSet) (*mtproto.Messages_StickerSet, error)
	File(ctx context.Context, doc *mtproto.Document, thumb string, max int) ([]byte, error)
}

// Importer stores a set (importer.Importer).
type Importer interface {
	ImportSet(ctx context.Context, set *mtproto.Messages_StickerSet, fetch func(context.Context, *mtproto.Document, string) ([]byte, error)) (*catalog.Set, error)
}

// Fetcher fetches sets on demand.
type Fetcher struct {
	src   Source
	im    Importer
	now   func() time.Time
	group singleflight.Group

	mu      sync.Mutex
	invalid map[string]time.Time // lower-cased name → when Telegram said it has none
}

// New returns a fetcher of src's sets into im.
func New(src Source, im Importer) *Fetcher {
	return &Fetcher{src: src, im: im, now: time.Now, invalid: map[string]time.Time{}}
}

// ErrNoSet is a name Telegram has no set for, or one that is no name.
var ErrNoSet = errors.New("demand: no such set")

// ByShortName fetches and stores the set named name, and returns it. It
// waits as long as ctx allows; a fetch that outlives ctx goes on, and the
// set is stored for the next ask.
func (f *Fetcher) ByShortName(ctx context.Context, name string) (*catalog.Set, error) {
	if !validName.MatchString(name) {
		return nil, ErrNoSet
	}
	key := strings.ToLower(name)
	f.mu.Lock()
	at, ok := f.invalid[key]
	f.mu.Unlock()
	if ok && f.now().Sub(at) < invalidFor {
		return nil, ErrNoSet
	}
	ch := f.group.DoChan(key, func() (any, error) {
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), fetchFor)
		defer cancel()
		return f.fetch(fctx, name)
	})
	select {
	case r := <-ch:
		if errors.Is(r.Err, ErrNoSet) {
			f.mu.Lock()
			f.invalid[key] = f.now()
			f.mu.Unlock()
		}
		if r.Err != nil {
			return nil, r.Err
		}
		return r.Val.(*catalog.Set), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (f *Fetcher) fetch(ctx context.Context, name string) (*catalog.Set, error) {
	set, err := f.src.StickerSet(ctx, mtproto.MakeTLInputStickerSetShortName(&mtproto.InputStickerSet{ShortName: name}).To_InputStickerSet())
	if errors.Is(err, mtproto.ErrStickersetInvalid) {
		return nil, ErrNoSet
	}
	if err != nil {
		return nil, err
	}
	docs := set.GetDocuments()
	if len(docs) == 0 || len(docs) > MaxDocuments {
		return nil, fmt.Errorf("demand: a set of %d documents", len(docs))
	}
	// Every file first, at once: then the import takes them from here.
	files, err := f.files(ctx, docs)
	if err != nil {
		return nil, err
	}
	return f.im.ImportSet(ctx, set, func(_ context.Context, doc *mtproto.Document, thumb string) ([]byte, error) {
		b, ok := files[fileKey{doc.GetId(), thumb}]
		if !ok {
			return nil, fmt.Errorf("demand: no file %d/%q", doc.GetId(), thumb)
		}
		return b, nil
	})
}

type fileKey struct {
	id    int64
	thumb string
}

// files downloads the file of every document and of its downloadable
// thumbnails, parallel, at most MaxSet bytes in all.
func (f *Fetcher) files(ctx context.Context, docs []*mtproto.Document) (map[fileKey][]byte, error) {
	var (
		mu    sync.Mutex
		out   = map[fileKey][]byte{}
		total int
	)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(parallel)
	for _, d := range docs {
		for _, thumb := range append([]string{""}, thumbTypes(d)...) {
			d, thumb := d, thumb
			g.Go(func() error {
				b, err := f.src.File(gctx, d, thumb, MaxFile)
				if err != nil {
					return err
				}
				mu.Lock()
				defer mu.Unlock()
				total += len(b)
				if total > MaxSet {
					return fmt.Errorf("demand: a set over %d bytes", MaxSet)
				}
				out[fileKey{d.GetId(), thumb}] = b
				return nil
			})
		}
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}

// thumbTypes are a document's thumbnails that are files.
func thumbTypes(doc *mtproto.Document) []string {
	var out []string
	for _, t := range doc.GetThumbs() {
		switch t.GetPredicateName() {
		case mtproto.Predicate_photoSize, mtproto.Predicate_photoSizeProgressive:
			out = append(out, t.GetType())
		}
	}
	for _, v := range doc.GetVideoThumbs() {
		if v.GetPredicateName() == mtproto.Predicate_videoSize {
			out = append(out, v.GetType())
		}
	}
	return out
}
