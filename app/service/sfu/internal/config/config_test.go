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
	"strconv"
	"testing"
	"time"

	"github.com/teamgram/teamgram-server/pkg/conf"
)

func TestShippedConfigLoads(t *testing.T) {
	var c Config
	if err := conf.Load("../../etc/sfu.yaml", &c); err != nil {
		t.Fatal(err)
	}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Media.TCPListen == "" || c.Media.TCPListen == ":443" {
		t.Errorf("ICE-TCP port %q: it must be on and not 443, which Caddy has", c.Media.TCPListen)
	}
	if c.ICE.FailedTimeout != 25*time.Second || c.Limits.MaxVideos != 30 {
		t.Errorf("%+v", c)
	}
	addrs, err := c.Addresses(20600, 20601)
	if err != nil || len(addrs) != 2 || addrs[0].TCP || !addrs[1].TCP || addrs[1].Port != 20601 {
		t.Fatalf("%+v %v", addrs, err)
	}
}

func TestAddressesAndValidate(t *testing.T) {
	var c Config
	c.Control.ListenOn, c.Control.CertFile, c.Control.KeyFile, c.Control.ClientCAFile = ":1", "c", "k", "ca"
	c.Media.UDPListen = ":20600"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Addresses(20600, 0); err == nil {
		t.Error("no public IPs accepted")
	}
	c.Media.PublicIPs = []string{"203.0.113.9", "2001:db8::9"}
	c.Media.PublicUDPPort = 3478
	addrs, err := c.Addresses(20600, 0)
	if err != nil || len(addrs) != 2 || addrs[0].Port != 3478 || !addrs[1].IP.Is6() {
		t.Fatalf("%+v %v", addrs, err)
	}
	c.Media.PublicIPs = []string{"not-an-ip"}
	if _, err := c.Addresses(20600, 0); err == nil {
		t.Error("bad IP accepted")
	}

	bad := c
	bad.Control.ClientCAFile = ""
	if bad.Validate() == nil {
		t.Error("control without mutual TLS accepted")
	}
	bad = c
	bad.Media.TCPListen = "20601"
	if bad.Validate() == nil {
		t.Error("TCP address without a port accepted")
	}
}

// SFU_RELAYS (task 116): names, IPv4 and IPv6 in brackets, both ports;
// "direct", a name twice and a bad address stop the start.
func TestParseRelays(t *testing.T) {
	got, err := ParseRelays(" ru=194.87.226.185:20600/20601 , v6=[2001:db8::5]:3478/3479")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "ru" || got[0].IP.String() != "194.87.226.185" || got[0].UDPPort != 20600 || got[0].TCPPort != 20601 ||
		got[1].Name != "v6" || !got[1].IP.Is6() || got[1].UDPPort != 3478 || got[1].TCPPort != 3479 {
		t.Fatalf("%+v", got)
	}
	if got, err := ParseRelays(""); err != nil || got != nil {
		t.Fatalf("empty: %v %v", got, err)
	}
	if got, err := ParseRelays("e2e=127.0.0.1:21000/21000"); err != nil || len(got) != 1 {
		t.Fatalf("loopback of the stand: %v %v", got, err)
	}
	for _, bad := range []string{
		"ru",
		"ru=194.87.226.185",
		"ru=194.87.226.185:20600",
		"ru=194.87.226.185:20600/",
		"ru=194.87.226.185:0/20601",
		"ru=194.87.226.185:20600/0",
		"ru=194.87.226.185:20600/70000",
		"ru=2001:db8::5:20600/20601",
		"ru=fe80::1%eth0:20600/20601",
		"ru=0.0.0.0:20600/20601",
		"ru=224.0.0.1:20600/20601",
		"ru=relay.example:20600/20601",
		"direct=194.87.226.185:20600/20601",
		"RU=194.87.226.185:20600/20601",
		"1ru=194.87.226.185:20600/20601",
		"averyveryverylongname=194.87.226.185:20600/20601",
		"ru=194.87.226.185:20600/20601,ru=194.87.226.186:20600/20601",
		"ru=194.87.226.185:20600/20601,",
		"a=1.1.1.1:1/1,b=1.1.1.2:1/1,c=1.1.1.3:1/1,d=1.1.1.4:1/1,e=1.1.1.5:1/1,f=1.1.1.6:1/1,g=1.1.1.7:1/1,h=1.1.1.8:1/1",
	} {
		if got, err := ParseRelays(bad); err == nil {
			t.Errorf("%q accepted: %+v", bad, got)
		}
	}
}

// Each relay is a UDP candidate, and a TCP one when ICE-TCP is on;
// Validate refuses a bad SFU_RELAYS and more candidates than Web takes.
func TestRelayAddresses(t *testing.T) {
	var c Config
	c.Control.ListenOn, c.Control.CertFile, c.Control.KeyFile, c.Control.ClientCAFile = ":1", "c", "k", "ca"
	c.Media.UDPListen = ":20600"
	c.Media.TCPListen = ":20601"
	c.Media.PublicIPs = []string{"195.201.252.126"}
	c.Media.Relays = "ru=194.87.226.185:20610/20611"
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	relays, err := c.RelayAddresses()
	if err != nil {
		t.Fatal(err)
	}
	ru := relays["ru"]
	if len(relays) != 1 || len(ru) != 2 || ru[0].TCP || ru[0].Port != 20610 || !ru[1].TCP || ru[1].Port != 20611 || ru[0].IP.String() != "194.87.226.185" {
		t.Fatalf("%+v", relays)
	}
	c.Media.TCPListen = ""
	if relays, err = c.RelayAddresses(); err != nil || len(relays["ru"]) != 1 || relays["ru"][0].TCP {
		t.Fatalf("without ICE-TCP: %+v %v", relays, err)
	}

	bad := c
	bad.Media.Relays = "direct=194.87.226.185:20600/20601"
	if bad.Validate() == nil {
		t.Error("a relay named direct accepted")
	}
	bad = c
	bad.Media.TCPListen = ":20601"
	bad.Media.PublicIPs = make([]string, 31)
	for i := range bad.Media.PublicIPs {
		bad.Media.PublicIPs[i] = "203.0.113." + strconv.Itoa(i+1)
	}
	if err := bad.Validate(); err != nil {
		t.Fatalf("64 candidates: %v", err)
	}
	bad.Media.Relays = "ru=194.87.226.185:20600/20601,kz=198.51.100.7:20600/20601"
	if bad.Validate() == nil {
		t.Error("66 candidates accepted")
	}
}
