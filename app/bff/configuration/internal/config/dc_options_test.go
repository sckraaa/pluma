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

package config

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/cdn"
)

// testSecret is a throwaway CDN DC secret, "dd" and 32 hex digits, and its
// 16 bytes.
func testSecret(t *testing.T) (string, []byte) {
	t.Helper()
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	return "dd" + hex.EncodeToString(raw), raw
}

// cdnOn is an enabled Cdn block with the CDN DC at addr.
func cdnOn(addr, secret string) cdn.Config {
	return cdn.Config{
		NodeControl:          "10.77.0.1:20510",
		CertFile:             "client.pem",
		KeyFile:              "client-key.pem",
		CAFile:               "server-ca.pem",
		DcAddress:            addr,
		DcSecret:             secret,
		MonthlyEgressLimitGB: "1000",
	}
}

type wantOption struct {
	id     int32
	ip     string
	port   int32
	ipv6   bool
	static bool
	cdn    bool
	secret []byte
}

func checkOptions(t *testing.T, got []*mtproto.DcOption, want []wantOption) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%d options, want %d: %v", len(got), len(want), got)
	}
	for i, w := range want {
		o := got[i]
		if o.GetPredicateName() != mtproto.Predicate_dcOption {
			t.Errorf("option %d: predicate %q", i, o.GetPredicateName())
		}
		if o.GetId() != w.id || o.GetIpAddress() != w.ip || o.GetPort() != w.port {
			t.Errorf("option %d: DC %d at %s port %d, want DC %d at %s port %d",
				i, o.GetId(), o.GetIpAddress(), o.GetPort(), w.id, w.ip, w.port)
		}
		if o.GetIpv6() != w.ipv6 || o.GetStatic() != w.static || o.GetCdn() != w.cdn ||
			o.GetTcpoOnly() != w.cdn || o.GetMediaOnly() || o.GetThisPortOnly() {
			t.Errorf("option %d: ipv6 %v static %v cdn %v tcpo_only %v media_only %v this_port_only %v, want ipv6 %v static %v cdn and tcpo_only %v",
				i, o.GetIpv6(), o.GetStatic(), o.GetCdn(), o.GetTcpoOnly(), o.GetMediaOnly(), o.GetThisPortOnly(), w.ipv6, w.static, w.cdn)
		}
		if !bytes.Equal(o.GetSecret(), w.secret) {
			t.Errorf("option %d: secret %x, want %x", i, o.GetSecret(), w.secret)
		}
	}
}

func TestDcOptions(t *testing.T) {
	secret, raw := testSecret(t)
	wire := append([]byte{0xdd}, raw...)
	for _, c := range []struct {
		name string
		cfg  Config
		want []wantOption
	}{
		{name: "nothing configured", cfg: Config{}},
		{name: "blank", cfg: Config{Dc1Addresses: "  "}},
		{
			name: "DC 1 alone, in the order given",
			cfg:  Config{Dc1Addresses: "203.0.113.10:10443, 203.0.113.10:5222,[2001:db8::1]:10443"},
			want: []wantOption{
				{id: 1, ip: "203.0.113.10", port: 10443, static: true},
				{id: 1, ip: "203.0.113.10", port: 5222, static: true},
				{id: 1, ip: "2001:db8::1", port: 10443, ipv6: true, static: true},
			},
		},
		{
			name: "an IPv4-mapped address is IPv4",
			cfg:  Config{Dc1Addresses: "[::ffff:203.0.113.10]:10443"},
			want: []wantOption{{id: 1, ip: "203.0.113.10", port: 10443, static: true}},
		},
		{
			name: "DC 1 and the CDN DC",
			cfg: Config{
				Dc1Addresses: "203.0.113.10:10443,203.0.113.10:5222",
				Cdn:          cdnOn("203.0.113.10:8443", secret),
			},
			want: []wantOption{
				{id: 1, ip: "203.0.113.10", port: 10443, static: true},
				{id: 1, ip: "203.0.113.10", port: 5222, static: true},
				{id: cdn.DefaultDcId, ip: "203.0.113.10", port: 8443, cdn: true, secret: wire},
			},
		},
		{
			name: "the CDN DC of its own number, on IPv6",
			cfg: func() Config {
				c := Config{Dc1Addresses: "203.0.113.10:10443", Cdn: cdnOn("[2001:db8::2]:8443", secret)}
				c.Cdn.DcId = "202"
				return c
			}(),
			want: []wantOption{
				{id: 1, ip: "203.0.113.10", port: 10443, static: true},
				{id: 202, ip: "2001:db8::2", port: 8443, ipv6: true, cdn: true, secret: wire},
			},
		},
		{
			// The node's address without the node: the CDN is off, and
			// no client is sent to a DC that redirects never name.
			name: "CDN DC address without the node",
			cfg: Config{
				Dc1Addresses: "203.0.113.10:10443",
				Cdn:          cdn.Config{DcAddress: "203.0.113.10:8443", DcSecret: secret},
			},
			want: []wantOption{{id: 1, ip: "203.0.113.10", port: 10443, static: true}},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.cfg.DcOptions()
			if err != nil {
				t.Fatalf("DcOptions: %v", err)
			}
			checkOptions(t, got, c.want)
		})
	}
}

// TestDcOptionsRefused: a list the clients cannot follow stops the start.
func TestDcOptionsRefused(t *testing.T) {
	secret, _ := testSecret(t)
	for _, c := range []struct {
		name, want string
		cfg        Config
	}{
		{name: "CDN without DC 1", want: "PLUMA_DC1_ADDRESSES", cfg: Config{Cdn: cdnOn("203.0.113.10:8443", secret)}},
		{name: "a name in DC 1", want: "PLUMA_DC1_ADDRESSES", cfg: Config{Dc1Addresses: "pluma.example:10443"}},
		{name: "no port", want: "PLUMA_DC1_ADDRESSES", cfg: Config{Dc1Addresses: "203.0.113.10"}},
		{name: "port 0", want: "PLUMA_DC1_ADDRESSES", cfg: Config{Dc1Addresses: "203.0.113.10:0"}},
		{name: "IPv6 without brackets", want: "PLUMA_DC1_ADDRESSES", cfg: Config{Dc1Addresses: "2001:db8::1:10443"}},
		{name: "IPv6 with a zone", want: "PLUMA_DC1_ADDRESSES", cfg: Config{Dc1Addresses: "[fe80::1%eth0]:10443"}},
		{name: "unspecified address", want: "PLUMA_DC1_ADDRESSES", cfg: Config{Dc1Addresses: "0.0.0.0:10443"}},
		{name: "an empty item", want: "PLUMA_DC1_ADDRESSES", cfg: Config{Dc1Addresses: "203.0.113.10:10443,,203.0.113.10:5222"}},
		{name: "twice", want: "twice", cfg: Config{Dc1Addresses: "203.0.113.10:10443,203.0.113.10:10443"}},
		{
			name: "a name for the CDN DC", want: "CDN_DC_ADDRESS",
			cfg: Config{Dc1Addresses: "203.0.113.10:10443", Cdn: cdnOn("cdn.pluma.example:8443", secret)},
		},
		{
			name: "an unspecified CDN DC address", want: "CDN_DC_ADDRESS",
			cfg: Config{Dc1Addresses: "203.0.113.10:10443", Cdn: cdnOn("0.0.0.0:8443", secret)},
		},
		{
			name: "a CDN DC address with a zone", want: "CDN_DC_ADDRESS",
			cfg: Config{Dc1Addresses: "203.0.113.10:10443", Cdn: cdnOn("[fe80::1%eth0]:8443", secret)},
		},
		{
			name: "secret one byte short", want: "CDN_DC_SECRET",
			cfg: Config{Dc1Addresses: "203.0.113.10:10443", Cdn: cdnOn("203.0.113.10:8443", secret[:len(secret)-2])},
		},
		{
			name: "secret without dd", want: "CDN_DC_SECRET",
			cfg: Config{Dc1Addresses: "203.0.113.10:10443", Cdn: cdnOn("203.0.113.10:8443", secret[2:])},
		},
		{
			name: "secret of a fake-TLS proxy", want: "CDN_DC_SECRET",
			cfg: Config{Dc1Addresses: "203.0.113.10:10443", Cdn: cdnOn("203.0.113.10:8443", "ee"+secret[2:])},
		},
		{
			name: "the CDN DC numbered as the home", want: "CDN_DC_ID",
			cfg: func() Config {
				c := Config{Dc1Addresses: "203.0.113.10:10443", Cdn: cdnOn("203.0.113.10:8443", secret)}
				c.Cdn.DcId = "1"
				return c
			}(),
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := c.cfg.DcOptions()
			if err == nil {
				t.Fatalf("accepted: %v", got)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("error %q does not name %s", err, c.want)
			}
			if strings.Contains(err.Error(), secret[2:]) {
				t.Errorf("error %q prints the secret", err)
			}
		})
	}
}
