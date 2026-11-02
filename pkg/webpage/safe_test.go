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
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"testing"
)

func TestPublicAddr(t *testing.T) {
	for _, tc := range []struct {
		addr   string
		public bool
	}{
		{"8.8.8.8", true},
		{"149.154.167.50", true},
		{"2a00:1450:4001:80b::200e", true},
		{"127.0.0.1", false},
		{"10.1.2.3", false},
		{"172.16.0.1", false},
		{"192.168.0.1", false},
		{"169.254.169.254", false}, // cloud metadata
		{"100.64.0.1", false},
		{"0.0.0.0", false},
		{"255.255.255.255", false},
		{"198.18.0.1", false},
		{"224.0.0.1", false},
		{"::1", false},
		{"::", false},
		{"fe80::1", false},
		{"fc00::1", false},
		{"::ffff:127.0.0.1", false},
		{"64:ff9b::7f00:1", false},
		{"2002:7f00:1::", false},
		{"2001:db8::1", false},
	} {
		if got := publicAddr(netip.MustParseAddr(tc.addr)); got != tc.public {
			t.Errorf("publicAddr(%s) = %v, want %v", tc.addr, got, tc.public)
		}
	}
}

func TestFetchable(t *testing.T) {
	for _, tc := range []struct {
		link string
		ok   bool
	}{
		{"https://telegram.org/", true},
		{"http://example.com/a?b=c", true},
		{"https://8.8.8.8/", true},
		{"ftp://example.com/", false},
		{"javascript:alert(1)", false},
		{"https://user:pass@example.com/", false},
		{"http://127.0.0.1:8080/", false},
		{"http://[::1]/", false},
		{"http://169.254.169.254/latest/meta-data/", false},
		{"http://localhost/", false},
		{"http://LOCALHOST./", false},
		{"http://api.localhost/", false},
		{"http://printer.local/", false},
		{"http://mysql/", false},
		{"http://db.internal/", false},
		{"http://router.lan/", false},
		{"http://nas.home.arpa/", false},
		{"http:///path", false},
	} {
		u, err := url.Parse(tc.link)
		if err != nil {
			t.Fatalf("%s: %v", tc.link, err)
		}
		if err := fetchable(u); (err == nil) != tc.ok {
			t.Errorf("fetchable(%s) = %v, want ok %v", tc.link, err, tc.ok)
		}
	}
}

// A name that resolves to an internal address is refused when dialled.
func TestPublicDialRefusesInternalAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the internal server was reached")
	}))
	defer srv.Close()
	conn, err := publicDial(context.Background(), "tcp", srv.Listener.Addr().String())
	if err == nil {
		conn.Close()
		t.Fatal("dialled a loopback address")
	}
	if !errors.Is(err, ErrNotPublic) {
		t.Fatalf("dial: %v, want ErrNotPublic", err)
	}
	if err := dialControl("tcp", "8.8.8.8:443", nil); err != nil {
		t.Fatalf("a public address: %v", err)
	}
	if err := dialControl("tcp6", net.JoinHostPort("::1", "80"), nil); !errors.Is(err, ErrNotPublic) {
		t.Fatalf("::1: %v, want ErrNotPublic", err)
	}
}
