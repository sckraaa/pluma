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
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/teamgram/teamgram-server/pkg/webfetch"
)

// Limits of a fetch: a page is read up to its body, at most maxPage bytes;
// a picture at most maxImage bytes; the whole fetch of a page and its
// picture takes at most fetchTimeout.
const (
	maxPage        = 1 << 20
	maxImage       = 8 << 20
	maxRedirects   = 5
	maxTitle       = 256
	maxDescription = 1024
	fetchTimeout   = 10 * time.Second
	// userAgent names the fetcher as a link preview bot: sites that serve
	// their Open Graph tags to such bots (they look for "bot" or
	// "TwitterBot") answer it as they answer Telegram's.
	userAgent = "Mozilla/5.0 (compatible; PlumaBot/1.0; +https://pluma.chat) like TwitterBot"
)

// errNoPage is a page that is no HTML document.
var errNoPage = errors.New("webpage: no page")

// fetcher fetches pages and pictures from the public internet only
// (pkg/webfetch).
type fetcher struct {
	client *webfetch.Client
}

// newFetcher returns a fetcher that connects with dial: webfetch.PublicDial
// but in tests, which serve the pages themselves.
func newFetcher(dial func(ctx context.Context, network, address string) (net.Conn, error)) *fetcher {
	return &fetcher{client: webfetch.New(webfetch.Options{
		Dial:         dial,
		UserAgent:    userAgent,
		Timeout:      fetchTimeout,
		MaxRedirects: maxRedirects,
	})}
}

func (f *fetcher) get(ctx context.Context, u *url.URL, accept string) (*http.Response, error) {
	return f.client.Get(ctx, u, accept)
}

// page fetches u and reads its head; it returns the address it ended at
// after redirects.
func (f *fetcher) page(ctx context.Context, u *url.URL) (*url.URL, Meta, error) {
	resp, err := f.get(ctx, u, "text/html,application/xhtml+xml;q=0.9,*/*;q=0.1")
	if err != nil {
		return nil, Meta{}, err
	}
	defer resp.Body.Close()
	ct := resp.Header.Get("Content-Type")
	mt, _, _ := mime.ParseMediaType(ct)
	if mt != "text/html" && mt != "application/xhtml+xml" {
		return nil, Meta{}, fmt.Errorf("%w: %s", errNoPage, mt)
	}
	m, err := parseMeta(io.LimitReader(resp.Body, maxPage), ct)
	if err != nil {
		return nil, Meta{}, err
	}
	return resp.Request.URL, m, nil
}

// image fetches a picture, at most maxImage bytes.
func (f *fetcher) image(ctx context.Context, u *url.URL) ([]byte, error) {
	return f.client.Image(ctx, u, "image/avif,image/webp,image/png,image/jpeg,image/*;q=0.8", maxImage)
}
