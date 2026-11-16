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

// Package inlinemedia imports the media the built-in inline bots
// (pkg/inlinebots) find on the open web into dfs when a query finds them,
// so that clients get photos and documents of the server and never reach a
// third-party host (task 099, ADR 0062). An imported medium is kept in
// inline_media by the provider's key of it and served from there the next
// time; the bytes imported in a UTC day are bounded by a Budget.
package inlinemedia

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/media/media"
	"github.com/teamgram/teamgram-server/pkg/webfetch"

	"github.com/zeromicro/go-zero/core/logx"
	"golang.org/x/sync/singleflight"
)

// Kind is what a medium is stored as.
type Kind int8

const (
	// KindPhoto is a picture, stored as a photo.
	KindPhoto Kind = 1
	// KindGif is a GIF as MP4, stored as an animated document.
	KindGif Kind = 2
)

// String is the name of the kind, as metrics label it.
func (k Kind) String() string {
	switch k {
	case KindPhoto:
		return "photo"
	case KindGif:
		return "gif"
	}
	return "unknown"
}

// Limits of an import (ADR 0062).
const (
	// PerRequest is how many media one query imports at once.
	PerRequest = 6
	// PerProcess is how many media the process imports at once.
	PerProcess = 16
	// FileTimeout bounds the import of one medium: the fetch, the
	// conversion and the upload to dfs.
	FileTimeout = 5 * time.Second
	// MaxPhotoBytes is the largest picture fetched, as for the pictures of
	// link previews (pkg/webpage).
	MaxPhotoBytes = 8 << 20
	// MaxGifBytes is the largest MP4 of a GIF fetched.
	MaxGifBytes = 2 << 20
	// maxProviderRef bounds inline_media.provider_ref.
	maxProviderRef = 128
	// maxProvider bounds inline_media.provider.
	maxProvider = 16
)

// Accept headers of the fetches.
const (
	acceptPhoto = "image/jpeg, image/png, image/webp, image/gif;q=0.8, image/*;q=0.5"
	acceptGif   = "video/mp4"
)

// Key is the key of a medium: sha256(provider, 0x00, ref)[:16]. The
// provider's address of a medium is not kept, only its key.
type Key [16]byte

// KeyOf is the key of the medium ref (the provider's id of it, or its URL)
// of provider.
func KeyOf(provider, ref string) Key {
	h := sha256.New()
	h.Write([]byte(provider))
	h.Write([]byte{0})
	h.Write([]byte(ref))
	var k Key
	copy(k[:], h.Sum(nil))
	return k
}

// String is the key in hex, 32 characters: the id of an inline result.
func (k Key) String() string {
	return hex.EncodeToString(k[:])
}

// Source is a medium a provider found.
type Source struct {
	// Provider names where the medium was found: "searxng", "klipy". At
	// most 16 bytes.
	Provider string
	// Ref is what identifies the medium at the provider: its id there, or
	// the address of the file. It makes the Key.
	Ref string
	// ProviderRef is the provider's id of the medium kept for revalidation
	// (KLIPY), empty for none. At most 128 bytes.
	ProviderRef string
	Kind        Kind
	// URL is the file: a picture of any of JPEG, PNG, GIF or WebP for
	// KindPhoto, an MP4 for KindGif.
	URL string
	// FallbackURL is fetched when URL fails (a smaller copy, as the
	// thumbnail_src of a SearXNG result); empty for none.
	FallbackURL string
	// Width, Height and Duration (seconds) describe the MP4 of KindGif;
	// the provider tells them, nothing here reads them from the file.
	Width, Height int32
	Duration      float64
}

// Key is the key of the medium.
func (s Source) Key() Key {
	return KeyOf(s.Provider, s.Ref)
}

// Media is an imported medium: a photo for KindPhoto, an animated document
// for KindGif.
type Media struct {
	Photo    *mtproto.Photo
	Document *mtproto.Document
}

// Fetcher fetches files from public addresses (*webfetch.Client).
type Fetcher interface {
	Image(ctx context.Context, u *url.URL, accept string, limit int64) ([]byte, error)
	Fetch(ctx context.Context, u *url.URL, accept string, limit int64) (webfetch.Body, error)
}

// Uploader stores files in dfs as an account's (webfetch.RPCPhotos).
type Uploader interface {
	UploadPhotoOf(ctx context.Context, owner int64, jpeg []byte, name string) (*mtproto.Photo, error)
	UploadAnimation(ctx context.Context, owner int64, mp4 []byte, w, h int32, duration float64) (*mtproto.Document, error)
}

// Files reads stored media back (the media service).
type Files interface {
	MediaGetPhoto(ctx context.Context, in *media.TLMediaGetPhoto) (*mtproto.Photo, error)
	MediaGetDocumentList(ctx context.Context, in *media.TLMediaGetDocumentList) (*media.Vector_Document, error)
}

// Rows is where imported media are recorded (*Store).
type Rows interface {
	Lookup(ctx context.Context, provider string, keys []Key) (map[Key]Row, error)
	Put(ctx context.Context, r Row) error
	Touch(ctx context.Context, provider string, keys []Key, now time.Time) error
}

// Quota is the daily budget of the imports (*Budget).
type Quota interface {
	Spent(ctx context.Context) (bool, error)
	Take(ctx context.Context, n int64) (bool, error)
}

// userAgent names the importer to the hosts of the media.
const userAgent = "Mozilla/5.0 (compatible; PlumaBot/1.0; +https://pluma.chat)"

// NewFetcher returns the Fetcher of the imports: public addresses only, up
// to five redirects, FileTimeout for a whole fetch.
func NewFetcher() *webfetch.Client {
	return webfetch.New(webfetch.Options{UserAgent: userAgent, Timeout: FileTimeout, MaxRedirects: 5})
}

// Importer imports media into dfs and finds the ones imported before.
type Importer struct {
	rows   Rows
	quota  Quota
	fetch  Fetcher
	upload Uploader
	files  Files
	now    func() time.Time
	// fileTimeout is FileTimeout; tests shorten it.
	fileTimeout time.Duration

	// slots bounds the imports of the process (PerProcess).
	slots chan struct{}
	// group makes one import of a key at a time in the process.
	group singleflight.Group
}

// NewImporter returns an Importer.
func NewImporter(rows Rows, quota Quota, fetch Fetcher, upload Uploader, files Files) *Importer {
	return &Importer{
		rows:   rows,
		quota:  quota,
		fetch:  fetch,
		upload: upload,
		files:  files,
		now:    time.Now,
		slots:  make(chan struct{}, PerProcess),

		fileTimeout: FileTimeout,
	}
}

// Imported is what Ensure found of a list of sources.
type Imported struct {
	// Media are the media of the first Polled sources, in their order;
	// nil for a source that could not be had.
	Media []*Media
	// Polled is how many sources, from the first, were tried before the
	// deadline: the sources after them were not, and the next page goes on
	// from them.
	Polled int
}

// errBudget is an import refused because the day's budget is spent.
var errBudget = errors.New("inlinemedia: the import budget of the day is spent")

// Ensure returns the media of the sources, all of one provider and kind,
// stored under owner (the bot's account) in dfs: the ones imported before
// from inline_media, the others imported now, PerRequest at a time and in
// the order of the list, until ctx ends. An import that ctx cuts short goes
// on in the background up to FileTimeout, so that the next query finds the
// medium. A medium that cannot be had is nil, logged and counted, never an
// error: the query shows the rest. The error is a failure to read
// inline_media, when nothing is known.
func (im *Importer) Ensure(ctx context.Context, owner int64, sources []Source) (Imported, error) {
	out := Imported{Media: make([]*Media, len(sources))}
	if len(sources) == 0 {
		return out, nil
	}
	provider, kind := sources[0].Provider, sources[0].Kind
	keys := make([]Key, len(sources))
	for i, s := range sources {
		if s.Provider != provider || s.Kind != kind {
			return out, fmt.Errorf("inlinemedia: sources of %s/%s and %s/%s in one list", provider, kind, s.Provider, s.Kind)
		}
		keys[i] = s.Key()
	}
	if len(provider) == 0 || len(provider) > maxProvider {
		return out, fmt.Errorf("inlinemedia: provider %q", provider)
	}
	rows, err := im.rows.Lookup(ctx, provider, keys)
	if err != nil {
		return out, err
	}
	stored, err := im.storedMedia(ctx, kind, rows)
	if err != nil {
		// Those media are left out of this page; they are not imported
		// again, which a media service that does not answer would turn
		// into an import of every medium shown.
		logx.WithContext(ctx).Errorf("inlinemedia: stored %s media: %v", kind, err)
	}
	var served []Key
	for _, k := range keys {
		if _, ok := stored[k]; ok {
			served = append(served, k)
		} else if r, ok := rows[k]; ok && err == nil {
			logx.WithContext(ctx).Errorf("inlinemedia: %s %d of an inline_media row is gone", kind, r.MediaId)
		}
	}
	if err := im.rows.Touch(ctx, provider, served, im.now()); err != nil {
		// last_served_at only serves a later collection of unused media.
		logx.WithContext(ctx).Errorf("inlinemedia: %v", err)
	}

	var (
		mu      sync.Mutex
		next    int
		stopped bool
		wg      sync.WaitGroup
	)
	// take hands out the sources in their order while ctx lasts; once it
	// ends no more is started, so the sources started are the first ones.
	take := func() (int, bool) {
		mu.Lock()
		defer mu.Unlock()
		if stopped || next >= len(sources) {
			return 0, false
		}
		if ctx.Err() != nil {
			stopped = true
			return 0, false
		}
		i := next
		next++
		return i, true
	}
	for w := 0; w < min(PerRequest, len(sources)); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				i, ok := take()
				if !ok {
					return
				}
				if _, ok := rows[keys[i]]; ok {
					// Imported before; nil when its medium cannot be read.
					out.Media[i] = stored[keys[i]]
					continue
				}
				out.Media[i] = im.importShared(ctx, owner, sources[i], keys[i])
			}
		}()
	}
	wg.Wait()
	out.Polled = next
	out.Media = out.Media[:next]
	return out, nil
}

// storedMedia reads the media of the rows from the media service, by key.
// A row whose medium is gone (an empty photo or document) is left out and
// logged: dfs deletes nothing, so it is a fault to look into.
func (im *Importer) storedMedia(ctx context.Context, kind Kind, rows map[Key]Row) (map[Key]*Media, error) {
	out := make(map[Key]*Media, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	switch kind {
	case KindGif:
		ids := make([]int64, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, r.MediaId)
		}
		docs, err := im.files.MediaGetDocumentList(ctx, &media.TLMediaGetDocumentList{IdList: ids})
		if err != nil {
			return out, err
		}
		byId := make(map[int64]*mtproto.Document, len(docs.GetDatas()))
		for _, d := range docs.GetDatas() {
			if d.GetPredicateName() == mtproto.Predicate_document {
				byId[d.GetId()] = d
			}
		}
		for k, r := range rows {
			if d, ok := byId[r.MediaId]; ok {
				out[k] = &Media{Document: d}
			}
		}
	case KindPhoto:
		// One photo per call: the media service has no list of photos, and
		// a page has at most a dozen.
		var firstErr error
		for k, r := range rows {
			p, err := im.files.MediaGetPhoto(ctx, &media.TLMediaGetPhoto{PhotoId: r.MediaId})
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			if p.GetPredicateName() == mtproto.Predicate_photo {
				out[k] = &Media{Photo: p}
			}
		}
		return out, firstErr
	}
	return out, nil
}

// importShared imports the source, or waits for the import of the same key
// already under way, until ctx ends: nil when it ends first or the import
// fails.
func (im *Importer) importShared(ctx context.Context, owner int64, src Source, key Key) *Media {
	ch := im.group.DoChan(key.String(), func() (interface{}, error) {
		// Not the request's context: an import cut short by the request's
		// deadline still finishes for the next query.
		ictx, cancel := context.WithTimeout(context.Background(), im.fileTimeout)
		defer cancel()
		return im.importOne(ictx, owner, src, key)
	})
	select {
	case <-ctx.Done():
		return nil
	case r := <-ch:
		if r.Err != nil {
			return nil
		}
		return r.Val.(*Media)
	}
}

// importOne fetches the source, stores it in dfs and records it, and counts
// how it went.
func (im *Importer) importOne(ctx context.Context, owner int64, src Source, key Key) (*Media, error) {
	select {
	case im.slots <- struct{}{}:
		defer func() { <-im.slots }()
	case <-ctx.Done():
		metricImports.Inc(src.Kind.String(), "busy")
		return nil, ctx.Err()
	}
	m, size, err := im.store(ctx, owner, src)
	if err != nil {
		result := "error"
		switch {
		case errors.Is(err, errBudget):
			result = "budget"
		case errors.Is(err, context.DeadlineExceeded):
			result = "timeout"
		}
		metricImports.Inc(src.Kind.String(), result)
		if result != "budget" {
			// The address is not logged: it carries what was searched.
			logx.WithContext(ctx).Infof("inlinemedia: %s import from %s: %v", src.Kind, src.Provider, redact(err))
		}
		return nil, err
	}
	metricImports.Inc(src.Kind.String(), "ok")
	metricImportBytes.Add(float64(size), src.Kind.String())

	now := im.now().Unix()
	row := Row{
		Provider:    src.Provider,
		SourceKey:   key[:],
		Kind:        src.Kind,
		MediaId:     mediaId(m),
		ProviderRef: truncate(src.ProviderRef, maxProviderRef),
		Bytes:       size,
		CreatedAt:   now,
		ServedAt:    now,
	}
	if err := im.rows.Put(ctx, row); err != nil {
		// The file is stored and is served now; without its row the next
		// query imports it again.
		metricOrphans.Inc()
		logx.WithContext(ctx).Errorf("inlinemedia: record of an imported %s: %v", src.Kind, err)
	}
	return m, nil
}

// store fetches the source (its fallback if the file fails), converts it,
// takes its size of the budget and uploads it to dfs.
func (im *Importer) store(ctx context.Context, owner int64, src Source) (*Media, int64, error) {
	spent, err := im.quota.Spent(ctx)
	if err != nil {
		return nil, 0, err
	}
	if spent {
		return nil, 0, errBudget
	}
	var data []byte
	var w, h int32
	switch src.Kind {
	case KindPhoto:
		data, err = im.fetchFirst(ctx, src, func(u *url.URL) ([]byte, error) {
			b, err := im.fetch.Image(ctx, u, acceptPhoto, MaxPhotoBytes)
			if err != nil {
				return nil, err
			}
			jpeg, _, _, err := webfetch.PictureJPEG(b)
			return jpeg, err
		})
	case KindGif:
		w, h = src.Width, src.Height
		if w <= 0 || h <= 0 || w > webfetch.MaxSide || h > webfetch.MaxSide {
			return nil, 0, fmt.Errorf("inlinemedia: animation of %d x %d", w, h)
		}
		data, err = im.fetchFirst(ctx, src, func(u *url.URL) ([]byte, error) {
			body, err := im.fetch.Fetch(ctx, u, acceptGif, MaxGifBytes)
			return body.Bytes, err
		})
	default:
		return nil, 0, fmt.Errorf("inlinemedia: kind %d", src.Kind)
	}
	if err != nil {
		return nil, 0, err
	}
	size := int64(len(data))
	ok, err := im.quota.Take(ctx, size)
	if err != nil {
		return nil, 0, err
	}
	if !ok {
		return nil, 0, errBudget
	}
	switch src.Kind {
	case KindPhoto:
		p, err := im.upload.UploadPhotoOf(ctx, owner, data, "photo.jpg")
		if err != nil {
			return nil, 0, err
		}
		return &Media{Photo: p}, size, nil
	default:
		d, err := im.upload.UploadAnimation(ctx, owner, data, w, h, src.Duration)
		if err != nil {
			return nil, 0, err
		}
		return &Media{Document: d}, size, nil
	}
}

// fetchFirst gets the file of the source by get, from its fallback address
// when the first one fails.
func (im *Importer) fetchFirst(ctx context.Context, src Source, get func(*url.URL) ([]byte, error)) ([]byte, error) {
	var firstErr error
	for _, raw := range []string{src.URL, src.FallbackURL} {
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil {
			err = errors.New("inlinemedia: not an address")
		} else {
			var b []byte
			if b, err = get(u); err == nil {
				return b, nil
			}
		}
		if firstErr == nil {
			firstErr = err
		}
		if ctx.Err() != nil {
			break
		}
	}
	if firstErr == nil {
		firstErr = errors.New("inlinemedia: no address")
	}
	return nil, firstErr
}

// redact drops the address from an error of net/http (*url.Error), which
// names it.
func redact(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}

func mediaId(m *Media) int64 {
	if m.Photo != nil {
		return m.Photo.GetId()
	}
	return m.Document.GetId()
}

// truncate cuts s to at most n bytes, on a rune boundary.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && s[n]&0xc0 == 0x80 {
		n--
	}
	return s[:n]
}
