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
