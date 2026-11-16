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
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/webfetch"
)

// urlServer is a Server with the URL source over a test site: whatever
// host an address names, the source connects to the site.
func urlServer(t *testing.T, h http.Handler) (*Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(site.Close)
	fetch := webfetch.New(webfetch.Options{
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, site.Listener.Addr().String())
		},
		UserAgent:    userAgent,
		Timeout:      5 * time.Second,
		MaxRedirects: urlMaxRedirects,
	})
	cache, err := NewDiskCache(t.TempDir(), 1<<30, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(cache)
	s.Register(mtproto.Predicate_inputWebFileLocation, NewURLSource(testKey, fetch))
	return s, &hits
}

func urlLocationOf(u string, hash int64) *mtproto.InputWebFileLocation {
	return mtproto.MakeTLInputWebFileLocation(&mtproto.InputWebFileLocation{Url: u, AccessHash: hash}).To_InputWebFileLocation()
}

func signed(u string) *mtproto.InputWebFileLocation { return urlLocationOf(u, Sign(testKey, u)) }

// A file at a signed address comes whole from its parts, fetched once.
func TestURLSourceParts(t *testing.T) {
	file := make([]byte, 300_000)
	for i := range file {
		file[i] = byte(i * 7)
	}
	modified := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	s, hits := urlServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Last-Modified", modified.Format(http.TimeFormat))
		_, _ = w.Write(file)
	}))
	loc := signed("http://bot.example/thumb.jpg")
	var got []byte
	for offset := int32(0); ; offset += 131072 {
		f, err := s.Get(context.Background(), Request{UserId: 1}, loc, offset, 131072)
		if err != nil {
			t.Fatal(err)
		}
		if f.GetSize2() != int32(len(file)) || f.GetMimeType() != "image/jpeg" || f.GetMtime() != int32(modified.Unix()) ||
			f.GetFileType().GetPredicateName() != mtproto.Predicate_storage_fileJpeg {
			t.Fatalf("part at %d: %d, %s, %d, %v", offset, f.GetSize2(), f.GetMimeType(), f.GetMtime(), f.GetFileType())
		}
		if len(f.GetBytes()) == 0 {
			break
		}
		got = append(got, f.GetBytes()...)
	}
	if !bytes.Equal(got, file) {
		t.Fatalf("%d bytes back, not the file", len(got))
	}
	if hits.Load() != 1 {
		t.Fatalf("%d fetches, want 1", hits.Load())
	}
}

// Only the addresses the server signed are fetched.
func TestURLSourceRefusesForeignHash(t *testing.T) {
	s, hits := urlServer(t, http.NotFoundHandler())
	const u = "http://bot.example/a.jpg"
	for _, loc := range []*mtproto.InputWebFileLocation{
		urlLocationOf(u, 0),
		urlLocationOf(u, Sign(testKey, u)+1),
		urlLocationOf(u, Sign([]byte("another-example-key-of-32-bytes-or-more"), u)),
		urlLocationOf("http://bot.example/b.jpg", Sign(testKey, u)),
		urlLocationOf("", Sign(testKey, "")),
	} {
		if _, err := s.Get(context.Background(), Request{}, loc, 0, 4096); !errors.Is(err, mtproto.ErrLocationInvalid) {
			t.Errorf("%q %d: %v", loc.GetUrl(), loc.GetAccessHash(), err)
		}
	}
	if hits.Load() != 0 {
		t.Fatal("a foreign address fetched")
	}
}

func TestURLSourceNotAvailable(t *testing.T) {
	s, _ := urlServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>"))
		case "/huge.mp4":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(make([]byte, urlMaxBytes+1))
		case "/song.ogg":
			w.Header().Set("Content-Type", "audio/ogg")
			_, _ = w.Write([]byte("OggS"))
		default:
			http.Error(w, "down", http.StatusServiceUnavailable)
		}
	}))
	ctx := context.Background()
	for _, u := range []string{
		"http://bot.example/page",
		"http://bot.example/huge.mp4",
		"http://bot.example/down.jpg",
		"http://127.0.0.1/a.jpg",
		"http://10.0.0.7/a.jpg",
		"http://[::1]/a.jpg",
		"http://localhost/a.jpg",
		"ftp://bot.example/a.jpg",
	} {
		if _, err := s.Get(ctx, Request{}, signed(u), 0, 4096); !errors.Is(err, ErrNotAvailable) {
			t.Errorf("%s: %v, want WEBFILE_NOT_AVAILABLE", u, err)
		}
	}
	f, err := s.Get(ctx, Request{}, signed("http://bot.example/song.ogg"), 0, 4096)
	if err != nil || string(f.GetBytes()) != "OggS" || f.GetFileType().GetPredicateName() != mtproto.Predicate_storage_fileUnknown {
		t.Fatalf("audio/ogg: %v, %v", f, err)
	}
}

// The default fetcher reaches no internal address: a signed address of the
// test site itself, on loopback, is not fetched.
func TestURLSourceDefaultFetcherIsPublicOnly(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the internal site was reached")
	}))
	defer site.Close()
	cache, err := NewDiskCache(t.TempDir(), 1<<20, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	s := NewServer(cache)
	s.Register(mtproto.Predicate_inputWebFileLocation, NewURLSource(testKey, NewFetcher()))
	if _, err := s.Get(context.Background(), Request{}, signed(site.URL+"/a.jpg"), 0, 4096); !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("%v", err)
	}
}

// Without a key no address is served, and with one the cache is set up.
func TestNew(t *testing.T) {
	s, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	u := "http://bot.example/a.jpg"
	if _, err := s.Get(context.Background(), Request{}, signed(u), 0, 4096); !errors.Is(err, mtproto.ErrLocationInvalid) {
		t.Errorf("no key: %v", err)
	}
	if _, err := New(Config{HMACKey: "short"}); err == nil {
		t.Error("a short key accepted")
	}
	s, err = New(Config{HMACKey: string(testKey), CacheDir: t.TempDir(), CacheBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.sources[mtproto.Predicate_inputWebFileLocation]; !ok || s.cache == nil {
		t.Error("the URL source is not registered")
	}
}

// The log of a failed fetch names what failed, never the address or host.
func TestURLErrKindHidesAddress(t *testing.T) {
	const host = "secret-host.example"
	link := "https://" + host + "/for-alice.jpg"
	for _, err := range []error{
		&url.Error{Op: "Get", URL: link, Err: &net.DNSError{Err: "no such host", Name: host}},
		&url.Error{Op: "Get", URL: link, Err: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused to " + host)}},
		&url.Error{Op: "Get", URL: link, Err: errors.New("x509: certificate is valid for other.example, not " + host)},
		&url.Error{Op: "Get", URL: link, Err: context.DeadlineExceeded},
	} {
		if got := errKind(err); strings.Contains(got, host) || got == "" {
			t.Errorf("errKind(%v) = %q", err, got)
		}
	}
	if got := errKind(fmt.Errorf("%w: HTTP 404", webfetch.ErrStatus)); !strings.Contains(got, "HTTP 404") {
		t.Errorf("status: %q", got)
	}
}
