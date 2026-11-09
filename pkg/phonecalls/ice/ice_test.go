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

package ice

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestParseErrors(t *testing.T) {
	for _, bad := range []string{"host:3478", "host:0:stun", "host:99999:stun", "host:3478:relay", "host:3478:turn", "[2001:db8::7]:3478:stun"} {
		if _, err := Parse(bad, ""); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
	if s, err := Parse("", ""); err != nil || !s.Empty() {
		t.Fatalf("empty: %v %v", s, err)
	}
}

func TestConnections(t *testing.T) {
	s, err := Parse("198.51.100.7:3478:stun+turn,[2001:db8::7]:3478:stun+turn,stun.example:3478:stun,gone.example:3478:stun,v6only.example:3478:stun", "secret")
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Unix(1000, 0) }
	s.resolver = func(_ context.Context, host string) ([]net.IPAddr, error) {
		if host == "gone.example" {
			return nil, errors.New("no such host")
		}
		if host == "v6only.example" {
			return []net.IPAddr{{IP: net.ParseIP("2001:db8::9")}}, nil
		}
		return []net.IPAddr{{IP: net.ParseIP("203.0.113.5")}, {IP: net.ParseIP("2001:db8::5")}}, nil
	}
	list := s.Connections(context.Background(), 77, time.Hour)
	if len(list) != 2 {
		t.Fatalf("%d connections, want 2 (a server whose name does not resolve or has no IPv4 is left out)", len(list))
	}
	first := list[0]
	if first.GetIp() != "198.51.100.7" || first.GetIpv6() != "2001:db8::7" || !first.GetStun() || !first.GetTurn() {
		t.Fatalf("first: %+v", first)
	}
	h := hmac.New(sha1.New, []byte("secret"))
	h.Write([]byte("4600:77"))
	want := base64.StdEncoding.EncodeToString(h.Sum(nil))
	if first.GetUsername() != "4600:77" || first.GetPassword() != want {
		t.Fatalf("credentials %q %q", first.GetUsername(), first.GetPassword())
	}
	for _, c := range list {
		if c.GetIp() == "" {
			t.Fatalf("a connection without an IPv4 address: %+v", c)
		}
	}
	third := list[1]
	if third.GetIp() != "203.0.113.5" || third.GetIpv6() != "2001:db8::5" || third.GetUsername() != "" {
		t.Fatalf("stun host: %+v", third)
	}
	if !strings.HasPrefix(first.GetPredicateName(), "phoneConnectionWebrtc") {
		t.Fatalf("predicate %s", first.GetPredicateName())
	}
}
