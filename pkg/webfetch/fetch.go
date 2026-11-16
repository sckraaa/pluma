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

// Package webfetch fetches from the public internet only: the pages of
// links (pkg/webpage), the files of inline bots (pkg/webfile) and the media
// the built-in inline bots import. Every address is checked before the
// request and again when it is dialled (safe.go).
package webfetch

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

// ErrStatus is an answer other than 200 OK.
var ErrStatus = errors.New("webfetch: not served")

// ErrTooLarge is a body over the limit of the fetch.
var ErrTooLarge = errors.New("webfetch: over the size limit")

// ErrType is a body of a type the fetch does not accept.
var ErrType = errors.New("webfetch: type not accepted")

// Options set up a Client.
type Options struct {
	// Dial connects to an address; nil is PublicDial. Tests serve the
	// sites themselves through their own.
	Dial func(ctx context.Context, network, address string) (net.Conn, error)
	// UserAgent names the fetcher to the sites.
	UserAgent string
	// Timeout bounds a whole fetch, redirects and the body included.
	Timeout time.Duration
	// MaxRedirects is the most redirects a fetch follows.
	MaxRedirects int
	// AllowHosts are hosts fetched from although they are not public
	// (Config.AllowHosts): the test stand's own sites. Production sets none.
	AllowHosts []string
}

// Config is WebFetch of bff.yaml: what the server's fetches of inline
// bots' media and of upload.getWebFile may reach beyond the public
// internet.
type Config struct {
	// AllowHosts are names, or addresses, of hosts on the network of the
	// services that are fetched from as if they were public: only the end
	// to end test stand sets it (test/e2e/run.sh: test/e2e/fakeinline plays
	// the sites of the web there). A production config never has it: it
	// opens the internal network to the addresses users and bots name.
	AllowHosts []string `json:",optional"` //nolint:staticcheck // a go-zero conf option, not encoding/json's
}

// Client fetches from public addresses only, and from the hosts of
// Options.AllowHosts.
type Client struct {
	client    *http.Client
	userAgent string
	allow     map[string]bool
}

// New returns a Client.
func New(o Options) *Client {
	dial := o.Dial
	if dial == nil {
		dial = PublicDial
	}
	allow := make(map[string]bool, len(o.AllowHosts))
	for _, h := range o.AllowHosts {
		if h = hostKey(h); h != "" {
			allow[h] = true
		}
	}
	if len(allow) > 0 {
		// The transport dials the host the link names, before it is
		// resolved: an allowed one is dialled without the check of
		// PublicDial, every other one with it.
		checked, plain := dial, o.Dial
		if plain == nil {
			plain = (&net.Dialer{Timeout: 5 * time.Second}).DialContext
		}
		dial = func(ctx context.Context, network, address string) (net.Conn, error) {
			if host, _, err := net.SplitHostPort(address); err == nil && allow[hostKey(host)] {
				return plain(ctx, network, address)
			}
			return checked(ctx, network, address)
		}
	}
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
	maxRedirects := o.MaxRedirects
	c := &Client{
		userAgent: o.UserAgent,
		allow:     allow,
	}
	c.client = &http.Client{
		Transport: transport,
		Timeout:   o.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("webfetch: too many redirects")
			}
			return c.fetchable(req.URL)
		},
	}
	return c
}

// fetchable is Fetchable, but for a host of AllowHosts, which needs only
// the scheme and no user info.
func (c *Client) fetchable(u *url.URL) error {
	if !c.allow[hostKey(u.Hostname())] {
		return Fetchable(u)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("webfetch: scheme %q", u.Scheme)
	}
	if u.User != nil {
		return errors.New("webfetch: user info in the link")
	}
	return nil
}

// hostKey is a host name or address as AllowHosts compares it: in lower
// case, without the dot of a fully qualified name or the brackets of an
// IPv6 address.
func hostKey(h string) string {
	h = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(h), "["), "]")
	return strings.TrimSuffix(strings.ToLower(h), ".")
}

// Get requests u and returns the answer if it is 200 OK; the caller closes
// its body.
func (c *Client) Get(ctx context.Context, u *url.URL, accept string) (*http.Response, error) {
	if err := c.fetchable(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", accept)
	req.Header.Set("Accept-Language", "en-US,en;q=0.8")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%w: HTTP %d", ErrStatus, resp.StatusCode)
	}
	return resp, nil
}

// Image fetches a picture, at most limit bytes. An answer without a
// Content-Type is taken: the picture is decoded later anyway.
func (c *Client) Image(ctx context.Context, u *url.URL, accept string, limit int64) ([]byte, error) {
	resp, err := c.Get(ctx, u, accept)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "image/") {
		return nil, fmt.Errorf("%w: picture of type %s", ErrType, ct)
	}
	return readLimited(resp, limit)
}

// Body is a fetched file.
type Body struct {
	Bytes []byte
	// MimeType is the media type the site named, without parameters, in
	// lower case.
	MimeType string
	// Modified is the Last-Modified of the answer, zero without one.
	Modified time.Time
}

// Fetch fetches u, at most limit bytes, if the site names its type and
// accept takes it. accept is an Accept header: a list of media types, a
// type may be "image/*", parameters (q=) are ignored.
func (c *Client) Fetch(ctx context.Context, u *url.URL, accept string, limit int64) (Body, error) {
	resp, err := c.Get(ctx, u, accept)
	if err != nil {
		return Body{}, err
	}
	defer resp.Body.Close()
	mt, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil {
		return Body{}, fmt.Errorf("%w: no type", ErrType)
	}
	mt = strings.ToLower(mt)
	if !Accepts(accept, mt) {
		return Body{}, fmt.Errorf("%w: %s", ErrType, mt)
	}
	b, err := readLimited(resp, limit)
	if err != nil {
		return Body{}, err
	}
	body := Body{Bytes: b, MimeType: mt}
	if lm, err := http.ParseTime(resp.Header.Get("Last-Modified")); err == nil {
		body.Modified = lm
	}
	return body, nil
}

// Accepts reports whether the Accept header accept takes the media type mt.
func Accepts(accept, mt string) bool {
	for _, a := range strings.Split(accept, ",") {
		a = strings.ToLower(strings.TrimSpace(a))
		if i := strings.IndexByte(a, ';'); i >= 0 {
			a = strings.TrimSpace(a[:i])
		}
		switch {
		case a == "":
		case a == mt, a == "*/*":
			return true
		case strings.HasSuffix(a, "/*") && strings.HasPrefix(mt, a[:len(a)-1]):
			return true
		}
	}
	return false
}

// readLimited reads the body of resp, at most limit bytes.
func readLimited(resp *http.Response, limit int64) ([]byte, error) {
	if resp.ContentLength > limit {
		return nil, fmt.Errorf("%w: %d bytes", ErrTooLarge, resp.ContentLength)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, ErrTooLarge
	}
	return b, nil
}
