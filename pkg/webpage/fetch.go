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
	"strings"
	"time"
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

// errNoPage is a page that is no HTML document or not there (404, 500).
var errNoPage = errors.New("webpage: no page")

// fetcher fetches pages and pictures from the public internet only.
type fetcher struct {
	client *http.Client
}

// publicDial connects to public addresses only (dialControl).
var publicDial = (&net.Dialer{Timeout: 5 * time.Second, Control: dialControl}).DialContext

// newFetcher returns a fetcher that connects with dial: publicDial but in
// tests, which serve the pages themselves.
func newFetcher(dial func(ctx context.Context, network, address string) (net.Conn, error)) *fetcher {
	transport := &http.Transport{
		Proxy:                  nil, // never through a proxy, which would dial for us
		DialContext:            dial,
		ForceAttemptHTTP2:      true,
		TLSHandshakeTimeout:    5 * time.Second,
		ResponseHeaderTimeout:  8 * time.Second,
		MaxResponseHeaderBytes: 64 << 10,
		MaxIdleConns:           16,
		IdleConnTimeout:        30 * time.Second,
	}
	return &fetcher{client: &http.Client{
		Transport: transport,
		Timeout:   fetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("webpage: too many redirects")
			}
			return fetchable(req.URL)
		},
	}}
}

func (f *fetcher) get(ctx context.Context, u *url.URL, accept string) (*http.Response, error) {
	if err := fetchable(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", accept)
	req.Header.Set("Accept-Language", "en-US,en;q=0.8")
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%w: HTTP %d", errNoPage, resp.StatusCode)
	}
	return resp, nil
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
	resp, err := f.get(ctx, u, "image/avif,image/webp,image/png,image/jpeg,image/*;q=0.8")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "image/") {
		return nil, fmt.Errorf("webpage: picture of type %s", ct)
	}
	if resp.ContentLength > maxImage {
		return nil, fmt.Errorf("webpage: picture of %d bytes", resp.ContentLength)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxImage+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxImage {
		return nil, errors.New("webpage: picture over the limit")
	}
	return b, nil
}
