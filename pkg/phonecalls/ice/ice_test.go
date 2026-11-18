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
		if _, err := Parse(bad, "", Secrets{}); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
	if s, err := Parse("", "", Secrets{}); err != nil || !s.Empty() {
		t.Fatalf("empty: %v %v", s, err)
	}
}

func TestConnections(t *testing.T) {
	s, err := Parse("198.51.100.7:3478:stun+turn,[2001:db8::7]:3478:stun+turn,stun.example:3478:stun,gone.example:3478:stun,v6only.example:3478:stun", "secret", Secrets{})
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

// Throwaway secrets of the tests; the passwords below were made from them
// independently of this package: printf %s 4600:77 | openssl dgst -sha1
// -hmac <secret> -binary | base64, the TURN REST credential coturn's
// use-auth-secret checks.
var (
	secretHz = strings.Repeat("a", 64)
	secretRu = strings.Repeat("b", 64)
)

const (
	passwordShared = "1rWAOB1gJCSVCGx8gvcSjhq0zdo=" // "secret"
	passwordHz     = "RCd7YjtcqbfTPZVQgA6wppxVaFA=" // secretHz
	passwordRu     = "B0NnvbJXDpXruwDyTP9BwriCQrw=" // secretRu
)

func TestParseSecrets(t *testing.T) {
	for _, tc := range []struct{ name, cfg, want string }{
		{"no =", "ru" + secretRu, "want label=secret"},
		{"empty label", "=" + secretRu, "a label is"},
		{"upper case label", "RU=" + secretRu, "a label is"},
		{"label too long", strings.Repeat("r", 33) + "=" + secretRu, "a label is"},
		{"twice", "ru=" + secretRu + ",ru=" + secretHz, `"ru" is set twice`},
		{"short", "ru=" + strings.Repeat("b", 31), `the secret of "ru"`},
		{"too long", "ru=" + strings.Repeat("b", 129), `the secret of "ru"`},
		{"not alphanumeric", "ru=" + strings.Repeat("b", 40) + "-x", `the secret of "ru"`},
		{"line break", "ru=" + strings.Repeat("b", 40) + "\nexternal-ip=1.2.3.4", `the secret of "ru"`},
	} {
		_, err := ParseSecrets(tc.cfg)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", tc.name, err, tc.want)
			continue
		}
		if strings.Contains(err.Error(), strings.Repeat("b", 31)) || strings.Contains(err.Error(), secretHz) {
			t.Errorf("%s: the error quotes a secret: %v", tc.name, err)
		}
	}
	for _, ok := range []string{"", " ", "ru=" + secretRu, " ru=" + secretRu + " , hz=" + secretHz + ",", "r1=" + strings.Repeat("c", 32), "r1=" + strings.Repeat("C", 128)} {
		if _, err := ParseSecrets(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
}

func TestParseLabels(t *testing.T) {
	secrets, err := ParseSecrets("ru=" + secretRu + ",unused=" + secretHz)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, cfg, shared, want string }{
		// The node's label without its secret stops bff at start.
		{"no secret of the label", "198.51.100.7:3478:stun+turn@hz", "secret", `no secret labelled "hz"`},
		{"empty label", "198.51.100.7:3478:turn@", "secret", "a label is"},
		{"bad label", "198.51.100.7:3478:turn@RU", "secret", "a label is"},
		{"label of a STUN server", "198.51.100.7:3478:stun@ru", "secret", "a label names the secret of a TURN server"},
		{"unlabelled TURN without the shared secret", "198.51.100.7:3478:turn,194.87.226.185:3478:turn@ru", "", "needs a TurnSecret"},
		{"IPv6 of another label", "198.51.100.7:3478:turn@ru,[2001:db8::7]:3478:turn", "secret", "same port, kinds and label"},
	} {
		_, err := Parse(tc.cfg, tc.shared, secrets)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v, want %q", tc.name, err, tc.want)
		}
	}
	// Every TURN server labelled: no shared secret is needed.
	if _, err := Parse("194.87.226.185:3478:stun+turn@ru,198.51.100.9:3478:stun", "", secrets); err != nil {
		t.Errorf("labelled only: %v", err)
	}
}

// TestConnectionsPerServerSecret: each TURN server's credentials are made
// from its own secret, so a node holding one secret cannot make the
// credentials of another server; the username is the same for all.
func TestConnectionsPerServerSecret(t *testing.T) {
	secrets, err := ParseSecrets("ru=" + secretRu + ",hz=" + secretHz)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Parse("198.51.100.7:3478:stun+turn,194.87.226.185:3478:stun+turn@ru,[2001:db8::185]:3478:stun+turn@ru,203.0.113.9:3478:turn@hz,198.51.100.10:3478:stun", "secret", secrets)
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Unix(1000, 0) }
	list := s.Connections(context.Background(), 77, time.Hour)
	if len(list) != 4 {
		t.Fatalf("%d connections, want 4", len(list))
	}
	for i, want := range []struct {
		ip, ipv6, password string
		turn               bool
	}{
		{"198.51.100.7", "", passwordShared, true},
		{"194.87.226.185", "2001:db8::185", passwordRu, true},
		{"203.0.113.9", "", passwordHz, true},
		{"198.51.100.10", "", "", false},
	} {
		c := list[i]
		if c.GetIp() != want.ip || c.GetIpv6() != want.ipv6 || c.GetTurn() != want.turn || c.GetPassword() != want.password {
			t.Errorf("connection %d: %+v, want %+v", i, c, want)
		}
		if want.turn && c.GetUsername() != "4600:77" {
			t.Errorf("connection %d: username %q", i, c.GetUsername())
		}
		if c.GetId() != int64(i+1) {
			t.Errorf("connection %d: id %d", i, c.GetId())
		}
	}
}
