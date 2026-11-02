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

// Package webpage builds the cards of links (task 023, ADR 0027): the
// webPage of messages.getWebPagePreview, messages.getWebPage and of a
// message with a link. It fetches the page from the public internet only,
// reads its Open Graph and Twitter Card tags, stores its picture as a photo
// and keeps the card in MySQL by address.
package webpage

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"net/url"
	"time"

	"github.com/teamgram/marmota/pkg/stores/sqlx"
	"github.com/teamgram/proto/mtproto"
	"github.com/zeromicro/go-zero/core/logx"
	"golang.org/x/sync/singleflight"
)

// How long a card is kept before the page is fetched again: a card for a
// day, the absence of one for six hours, a page that could not be fetched
// (a timeout, a 404) for ten minutes.
const (
	pageTTL   = 24 * time.Hour
	emptyTTL  = 6 * time.Hour
	failedTTL = 10 * time.Minute
)

// ErrNoLink is a link that gets no card at all, not even an empty one: not
// http(s), or an address inside a network. Telegram answers such a preview
// with messageMediaEmpty.
var ErrNoLink = errors.New("webpage: no card for this link")

// Photos stores the picture of a card as a photo.
type Photos interface {
	UploadPhoto(ctx context.Context, jpeg []byte) (*mtproto.Photo, error)
}

// cache keeps the cards (Store, a map in tests).
type cache interface {
	get(ctx context.Context, u string) (*cached, error)
	byID(ctx context.Context, id int64) (*mtproto.WebPage, error)
	put(ctx context.Context, u string, page *mtproto.WebPage, fetched, expires int64) error
}

// Service builds and keeps the cards of links.
type Service struct {
	store  cache
	fetch  *fetcher
	photos Photos
	group  singleflight.Group
	now    func() time.Time
	logger logx.Logger
}

// NewService returns the service over db; photos stores the pictures.
func NewService(db *sqlx.DB, photos Photos) *Service {
	return &Service{
		store:  NewStore(db),
		fetch:  newFetcher(publicDial),
		photos: photos,
		now:    time.Now,
		logger: logx.WithContext(context.Background()),
	}
}

// Preview returns the card of link: the stored one while it is fresh,
// otherwise a new one, fetched once for all who ask meanwhile. It waits as
// long as ctx allows; a fetch that outlives ctx goes on and is stored for
// the next ask. A page without a card has webPageEmpty; a link that gets
// none is ErrNoLink.
func (s *Service) Preview(ctx context.Context, link string) (*mtproto.WebPage, error) {
	u, err := parseLink(link)
	if err != nil {
		return nil, ErrNoLink
	}
	u = normalize(u)
	key := u.String()
	c, err := s.store.get(ctx, key)
	if err != nil {
		return nil, err
	}
	if c != nil && c.expires > s.now().Unix() {
		return c.page, nil
	}
	var id int64
	if c != nil {
		id = c.id
	}
	ch := s.group.DoChan(key, func() (any, error) {
		// Not the caller's context: the card is stored whoever waits.
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), fetchTimeout+5*time.Second)
		defer cancel()
		return s.build(fctx, u, id)
	})
	select {
	case r := <-ch:
		if r.Err != nil {
			return nil, r.Err
		}
		return r.Val.(*mtproto.WebPage), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ByID returns the stored card with id, nil if there is none.
func (s *Service) ByID(ctx context.Context, id int64) (*mtproto.WebPage, error) {
	return s.store.byID(ctx, id)
}

// build fetches the page of u, makes its card and stores it under id (a
// new id if 0).
func (s *Service) build(ctx context.Context, u *url.URL, id int64) (*mtproto.WebPage, error) {
	if id == 0 {
		var err error
		if id, err = newID(); err != nil {
			return nil, err
		}
	}
	now := s.now()
	final, meta, err := s.fetch.page(ctx, u)
	if err != nil {
		// No address is logged: the links are what users write.
		s.logger.Infof("webpage: a page not fetched: %v", errKind(err))
		page := emptyPage(id, u)
		return page, s.store.put(ctx, u.String(), page, now.Unix(), now.Add(failedTTL).Unix())
	}
	var photo *mtproto.Photo
	if meta.Image != "" && s.photos != nil {
		photo = s.picture(ctx, final, meta.Image)
	}
	if photo == nil {
		meta.Image = ""
	}
	if !meta.hasCard() {
		page := emptyPage(id, u)
		return page, s.store.put(ctx, u.String(), page, now.Unix(), now.Add(emptyTTL).Unix())
	}
	page := buildPage(id, u, meta, photo)
	return page, s.store.put(ctx, u.String(), page, now.Unix(), now.Add(pageTTL).Unix())
}

// picture fetches and stores a page's picture; nil if it cannot.
func (s *Service) picture(ctx context.Context, base *url.URL, ref string) *mtproto.Photo {
	iu, err := base.Parse(ref)
	if err != nil {
		return nil
	}
	b, err := s.fetch.image(ctx, iu)
	if err != nil {
		s.logger.Infof("webpage: a picture not fetched: %v", errKind(err))
		return nil
	}
	jpg, _, _, err := pictureJPEG(b)
	if err != nil {
		s.logger.Infof("webpage: a picture not decoded: %v", err)
		return nil
	}
	photo, err := s.photos.UploadPhoto(ctx, jpg)
	if err != nil {
		s.logger.Errorf("webpage: a picture not stored: %v", err)
		return nil
	}
	return photo
}

// errKind is an error without the address it may quote.
func errKind(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}

// newID is a positive random id of a card.
func newID() (int64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	return int64(binary.LittleEndian.Uint64(b[:])>>1) | 1, nil
}
