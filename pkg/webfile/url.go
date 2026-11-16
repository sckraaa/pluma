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

package webfile

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/webfetch"
	"github.com/zeromicro/go-zero/core/logx"
)

// Limits of a file at an address: the fetch, redirects included, takes at
// most urlTimeout and the file is at most urlMaxBytes.
const (
	urlTimeout      = 10 * time.Second
	urlMaxRedirects = 5
	urlMaxBytes     = 20 << 20
	// urlAccept are the types served: pictures, and the video and audio
	// inline bots send as content.
	urlAccept = "image/*, video/mp4, audio/mpeg, audio/ogg"
	userAgent = "Mozilla/5.0 (compatible; PlumaBot/1.0; +https://pluma.chat)"
)

// NewFetcher returns the client a URL source fetches with: public
// addresses only, 10 s, five redirects.
func NewFetcher() *webfetch.Client {
	return webfetch.New(webfetch.Options{UserAgent: userAgent, Timeout: urlTimeout, MaxRedirects: urlMaxRedirects})
}

// urlSource serves inputWebFileLocation: the thumbnails and content of
// inline bots' results, at the addresses the server signed (Sign) when it
// gave the results out as webDocument.
type urlSource struct {
	key   []byte
	fetch *webfetch.Client
	now   func() time.Time
}

// NewURLSource returns the source of inputWebFileLocation, which takes the
// addresses signed under key and fetches them with fetch (NewFetcher).
func NewURLSource(key []byte, fetch *webfetch.Client) Source {
	return &urlSource{key: key, fetch: fetch, now: time.Now}
}

type urlLocation struct{ u *url.URL }

func (l urlLocation) Key() string { return "url\x00" + l.u.String() }

func (s *urlSource) Cached() bool { return false }

func (s *urlSource) Resolve(_ context.Context, _ Request, loc *mtproto.InputWebFileLocation) (Location, error) {
	raw := loc.GetUrl()
	if raw == "" || len(s.key) == 0 || !verify(s.key, raw, loc.GetAccessHash()) {
		return nil, mtproto.ErrLocationInvalid
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, mtproto.ErrLocationInvalid
	}
	return urlLocation{u: u}, nil
}

func (s *urlSource) Open(ctx context.Context, _ Request, l Location) (File, error) {
	u := l.(urlLocation).u
	body, err := s.fetch.Fetch(ctx, u, urlAccept, urlMaxBytes)
	if err != nil {
		// The address is the bot's, and may say whom it is for: the log
		// gets the kind of the failure only.
		logx.WithContext(ctx).Infof("webfile: a file at an address not fetched: %s", errKind(err))
		return File{}, ErrNotAvailable
	}
	mtime := s.now()
	if !body.Modified.IsZero() {
		mtime = body.Modified
	}
	return File{Bytes: body.Bytes, MimeType: body.MimeType, Mtime: int32(mtime.Unix())}, nil
}

// errKind is what failed, without the address: the errors of net/http
// quote the link, and those of DNS and TLS the host name.
func errKind(err error) string {
	var dnsErr *net.DNSError
	var opErr *net.OpError
	var netErr net.Error
	switch {
	case errors.Is(err, webfetch.ErrNotPublic):
		return "not a public address"
	case errors.Is(err, webfetch.ErrStatus), errors.Is(err, webfetch.ErrType), errors.Is(err, webfetch.ErrTooLarge):
		return err.Error() // returned by webfetch itself, without the link
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return "timeout"
	case errors.As(err, &dnsErr):
		return "host not resolved"
	case errors.As(err, &opErr):
		return "connection failed: " + opErr.Op
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return fmt.Sprintf("%T", err)
}
