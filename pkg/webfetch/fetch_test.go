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

package webfetch

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// testClient serves every site from h: whatever host a link names, the
// client connects to the test server.
func testClient(t *testing.T, h http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(Options{
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, srv.Listener.Addr().String())
		},
		UserAgent:    "test",
		Timeout:      5 * time.Second,
		MaxRedirects: 2,
	})
}

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestAccepts(t *testing.T) {
	const accept = "image/*, video/mp4;q=0.9, audio/mpeg"
	for mt, want := range map[string]bool{
		"image/jpeg": true, "image/webp": true, "video/mp4": true, "audio/mpeg": true,
		"video/webm": false, "text/html": false, "audio/ogg": false, "imagex/png": false,
	} {
		if got := Accepts(accept, mt); got != want {
			t.Errorf("Accepts(%s) = %v, want %v", mt, got, want)
		}
	}
	if !Accepts("*/*", "application/pdf") {
		t.Error("*/* takes any type")
	}
}

func TestFetch(t *testing.T) {
	modified := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	c := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a.jpg":
			if r.Header.Get("User-Agent") != "test" || r.Header.Get("Accept") == "" {
				t.Errorf("headers %v", r.Header)
			}
			w.Header().Set("Content-Type", "IMAGE/JPEG; charset=binary")
			w.Header().Set("Last-Modified", modified.Format(http.TimeFormat))
			_, _ = w.Write([]byte("jpeg"))
		case "/page":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>"))
		case "/untyped":
			w.Header()["Content-Type"] = nil
			_, _ = w.Write([]byte{0, 1, 2})
		case "/big.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(make([]byte, 100))
		case "/to-internal":
			http.Redirect(w, r, "http://10.0.0.1/a.jpg", http.StatusFound)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	ctx := context.Background()
	b, err := c.Fetch(ctx, mustURL(t, "http://site.example/a.jpg"), "image/*", 10)
	if err != nil || string(b.Bytes) != "jpeg" || b.MimeType != "image/jpeg" || !b.Modified.Equal(modified) {
		t.Fatalf("%+v, %v", b, err)
	}
	for path, want := range map[string]error{
		"/page":        ErrType,
		"/untyped":     ErrType,
		"/big.jpg":     ErrTooLarge,
		"/missing.jpg": ErrStatus,
		"/to-internal": ErrNotPublic,
	} {
		if _, err := c.Fetch(ctx, mustURL(t, "http://site.example"+path), "image/*", 10); !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", path, err, want)
		}
	}
	if _, err := c.Fetch(ctx, mustURL(t, "http://site.example/loop"), "image/*", 10); err == nil {
		t.Error("a redirect loop followed")
	}
}

// Without its own dial a client reaches no internal address, whatever the
// link says.
func TestFetchRefusesInternalAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the internal server was reached")
	}))
	defer srv.Close()
	c := New(Options{Timeout: 2 * time.Second, MaxRedirects: 1})
	for _, link := range []string{srv.URL + "/a.jpg", "http://10.1.2.3/a.jpg", "http://[::1]/a.jpg", "file:///etc/passwd"} {
		if _, err := c.Fetch(context.Background(), mustURL(t, link), "image/*", 1<<20); err == nil {
			t.Errorf("%s fetched", link)
		}
	}
}

// A host of AllowHosts is fetched from although it is internal, as the
// test stand's sites are; every other internal host stays refused, a
// redirect from the allowed one to another included.
func TestFetchAllowHosts(t *testing.T) {
	var internal *httptest.Server
	internal = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/a.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("jpeg"))
		case "/away":
			// The same server under a name that is not allowed.
			_, port, _ := net.SplitHostPort(internal.Listener.Addr().String())
			http.Redirect(w, r, "http://localhost:"+port+"/a.jpg", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer internal.Close()
	host, port, err := net.SplitHostPort(internal.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}

	c := New(Options{Timeout: 2 * time.Second, MaxRedirects: 2, AllowHosts: []string{" " + strings.ToUpper(host) + ". "}})
	body, err := c.Fetch(context.Background(), mustURL(t, internal.URL+"/a.jpg"), "image/*", 1<<20)
	if err != nil || string(body.Bytes) != "jpeg" {
		t.Fatalf("an allowed host: %q, %v", body.Bytes, err)
	}
	if _, err = c.Fetch(context.Background(), mustURL(t, internal.URL+"/away"), "image/*", 1<<20); !errors.Is(err, ErrNotPublic) {
		t.Errorf("a redirect to a host not allowed: %v", err)
	}
	for _, link := range []string{"http://localhost:" + port + "/a.jpg", "http://10.1.2.3/a.jpg", "ftp://" + host + "/a.jpg", "http://u:p@" + host + ":" + port + "/a.jpg"} {
		if _, err := c.Fetch(context.Background(), mustURL(t, link), "image/*", 1<<20); err == nil {
			t.Errorf("%s fetched", link)
		}
	}

	if _, err = New(Options{Timeout: 2 * time.Second}).Fetch(context.Background(), mustURL(t, internal.URL+"/a.jpg"), "image/*", 1<<20); !errors.Is(err, ErrNotPublic) {
		t.Errorf("without AllowHosts: %v", err)
	}
}
