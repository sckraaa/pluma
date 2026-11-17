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

package cdn

import (
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestConfigDefaults: empty values take their defaults.
func TestConfigDefaults(t *testing.T) {
	c := testConfig(t)
	s, err := c.Parse()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Deliveries) != 1 || len(s.Routes) != 1 || s.Routes[0].Countries != nil || !slices.Equal(s.Routes[0].Deliveries, []int32{testDc}) {
		t.Fatalf("deliveries %+v, routes %+v", s.Deliveries, s.Routes)
	}
	d, ok := s.Delivery(testDc)
	if !ok || d.Name != "gcore" || d.BaseURL != "https://cdn.example.com" || d.Source != SourceHomeOrigin ||
		d.Target() != TargetHomeOrigin || d.MonthlyBytes != 800_000_000_000 || d.InsecureHTTP || d.Signer.Name() != SignerGcoreMD5 {
		t.Fatalf("delivery %+v", d)
	}
	if s.URLTTL != 6*time.Hour || s.PrepareBytesPerSecond != 100_000_000 || s.MinFileSize != 1<<20 ||
		!slices.Equal(s.Kinds, []FileKind{KindDocument, KindPhoto}) || s.DevAllowHTTP || s.HealthEvery != time.Minute || s.AvoidTTL != time.Hour {
		t.Fatalf("settings %+v", s)
	}
	if s.Origin.Listen != "0.0.0.0:20480" || s.Origin.MaxConcurrent != 16 || string(s.Origin.AuthValue) != c.Origin.AuthValue {
		t.Fatalf("origin %+v", s.Origin)
	}
	if !s.AllowsKind(KindPhoto) || s.AllowsKind(KindEncrypted) {
		t.Fatal("kinds")
	}
}

// TestConfigOff: without a delivery with a BaseURL the CDN is off, and
// nothing else is looked at.
func TestConfigOff(t *testing.T) {
	for name, c := range map[string]Config{
		"empty":       {},
		"no BaseURL":  {Deliveries: []DeliveryConfig{{Id: testDc, Name: "gcore", Key: "short"}}, Routes: "RU:202", Kinds: "encrypted"},
		"blank space": {Deliveries: []DeliveryConfig{{Id: testDc, Name: "gcore", BaseURL: "  "}}},
	} {
		if c.Enabled() {
			t.Errorf("%s: enabled", name)
		}
		if s, err := c.Parse(); s != nil || err != nil {
			t.Errorf("%s: %+v, %v", name, s, err)
		}
	}
}

// TestConfigValues: the values that are taken.
func TestConfigValues(t *testing.T) {
	c := testConfig(t)
	c.Deliveries = append(c.Deliveries,
		DeliveryConfig{Id: 202, Name: "spare", Signer: SignerPathHMAC, BaseURL: "https://spare.example.com/", Key: base64.StdEncoding.EncodeToString(randomBytes(t, 32)), MonthlyGB: "100"},
		DeliveryConfig{Id: 203, Name: "off"})
	c.Routes = " * : 202 , 201 "
	c.UrlTTL, c.HealthEvery, c.AvoidTTL = "20s", "5s", "30s"
	c.PrepareRateMbit, c.MinFileSize, c.Kinds = "8", "2097152", " photo "
	c.Origin.Listen, c.Origin.MaxConcurrent = "bff:20480", "4"
	s, err := c.Parse()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Deliveries) != 2 || !slices.Equal(s.Routes[0].Deliveries, []int32{202, 201}) {
		t.Fatalf("deliveries %+v, routes %+v", s.Deliveries, s.Routes)
	}
	d, _ := s.Delivery(202)
	if d.BaseURL != "https://spare.example.com" || d.Signer.Name() != SignerPathHMAC || d.MonthlyBytes != 100_000_000_000 {
		t.Fatalf("delivery %+v", d)
	}
	if _, ok := s.Delivery(203); ok {
		t.Fatal("a delivery without BaseURL is on")
	}
	if s.URLTTL != 20*time.Second || s.HealthEvery != 5*time.Second || s.AvoidTTL != 30*time.Second ||
		s.PrepareBytesPerSecond != 1_000_000 || s.MinFileSize != 2<<20 || !slices.Equal(s.Kinds, []FileKind{KindPhoto}) ||
		s.Origin.Listen != "bff:20480" || s.Origin.MaxConcurrent != 4 {
		t.Fatalf("settings %+v", s)
	}

	// The stand of the e2e tests.
	c = testConfig(t)
	c.Deliveries[0].BaseURL, c.DevAllowHTTP = "http://fakecdn:8080", "1"
	if s, err = c.Parse(); err != nil || !s.DevAllowHTTP || !s.Deliveries[0].InsecureHTTP || s.Deliveries[0].BaseURL != "http://fakecdn:8080" {
		t.Fatalf("http with CDN_DEV_ALLOW_HTTP: %+v, %v", s, err)
	}
}

// TestConfigRejects: a wrong value stops the start.
func TestConfigRejects(t *testing.T) {
	for name, change := range map[string]func(*Config){
		"country route":    func(c *Config) { c.Routes = "RU,BY:201;*:201" },
		"route to unknown": func(c *Config) { c.Routes = "*:202" },
		"route to a delivery off": func(c *Config) {
			c.Deliveries = append(c.Deliveries, DeliveryConfig{Id: 202, Name: "off"})
			c.Routes = "*:201,202"
		},
		"route syntax":           func(c *Config) { c.Routes = "*201" },
		"two routes for all":     func(c *Config) { c.Routes = "*:201;*:201" },
		"http without dev":       func(c *Config) { c.Deliveries[0].BaseURL = "http://cdn.example.com" },
		"dev flag not 0 or 1":    func(c *Config) { c.DevAllowHTTP = "yes" },
		"BaseURL with a path":    func(c *Config) { c.Deliveries[0].BaseURL = "https://cdn.example.com/o" },
		"BaseURL with a query":   func(c *Config) { c.Deliveries[0].BaseURL = "https://cdn.example.com?a=1" },
		"BaseURL without host":   func(c *Config) { c.Deliveries[0].BaseURL = "https://" },
		"BaseURL scheme":         func(c *Config) { c.Deliveries[0].BaseURL = "ftp://cdn.example.com" },
		"gcore key short":        func(c *Config) { c.Deliveries[0].Key = "abcde" },
		"gcore key long":         func(c *Config) { c.Deliveries[0].Key = strings.Repeat("a", 33) },
		"gcore key with a space": func(c *Config) { c.Deliveries[0].Key = "abc defgh" },
		"path-hmac key 16 bytes": func(c *Config) {
			c.Deliveries[0].Signer, c.Deliveries[0].Key = SignerPathHMAC, base64.StdEncoding.EncodeToString(make([]byte, 16))
		},
		"path-hmac key not base64": func(c *Config) { c.Deliveries[0].Signer, c.Deliveries[0].Key = SignerPathHMAC, "abcdefghij" },
		"unknown signer":           func(c *Config) { c.Deliveries[0].Signer = "s3-presign" },
		"unknown source":           func(c *Config) { c.Deliveries[0].Source = "push-store" },
		"delivery id 200":          func(c *Config) { c.Deliveries[0].Id = 200 },
		"delivery id 1000":         func(c *Config) { c.Deliveries[0].Id = 1000; c.Routes = "*:1000" },
		"no name":                  func(c *Config) { c.Deliveries[0].Name = "" },
		"two with one id":          func(c *Config) { c.Deliveries = append(c.Deliveries, DeliveryConfig{Id: testDc, Name: "other"}) },
		"two with one name":        func(c *Config) { c.Deliveries = append(c.Deliveries, DeliveryConfig{Id: 202, Name: "gcore"}) },
		"monthly GB 0":             func(c *Config) { c.Deliveries[0].MonthlyGB = "0" },
		"no origin auth":           func(c *Config) { c.Origin.AuthValue = "" },
		"origin auth not hex":      func(c *Config) { c.Origin.AuthValue = strings.Repeat("g", 64) },
		"origin auth short":        func(c *Config) { c.Origin.AuthValue = strings.Repeat("a", 62) },
		"origin listen":            func(c *Config) { c.Origin.Listen = "20480" },
		"origin max 0":             func(c *Config) { c.Origin.MaxConcurrent = "0" },
		"URL TTL over a day":       func(c *Config) { c.UrlTTL = "25h" },
		"URL TTL too short":        func(c *Config) { c.UrlTTL = "5s" },
		"URL TTL not seconds":      func(c *Config) { c.UrlTTL = "20500ms" },
		"min size not MiB":         func(c *Config) { c.MinFileSize = "1000000" },
		"encrypted kind":           func(c *Config) { c.Kinds = "document,encrypted" },
		"unknown kind":             func(c *Config) { c.Kinds = "video" },
		"zero prepare rate":        func(c *Config) { c.PrepareRateMbit = "0" },
		"health every 0":           func(c *Config) { c.HealthEvery = "0s" },
		"avoid TTL 2 days":         func(c *Config) { c.AvoidTTL = "48h" },
	} {
		c := testConfig(t)
		change(&c)
		if s, err := c.Parse(); err == nil {
			t.Errorf("%s: accepted, %+v", name, s)
		}
	}
	c := testConfig(t)
	c.Routes = "RU:201;*:201"
	if _, err := c.Parse(); !errors.Is(err, ErrCountryRoutes) || err.Error() != "cdn: country routes need a country source" {
		t.Errorf("country route: %v", err)
	}
}

// TestConfigSecrets: the keys and the value of the origin never print.
func TestConfigSecrets(t *testing.T) {
	c := testConfig(t)
	s, err := c.Parse()
	if err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{fmt.Sprintf("%v", s), fmt.Sprintf("%+v", s), fmt.Sprintf("%#v", s.Origin), fmt.Sprintf("%+v", s.Deliveries)} {
		if strings.Contains(out, c.Origin.AuthValue) || strings.Contains(out, c.Deliveries[0].Key) {
			t.Fatalf("a secret of the config is printed: %s", out)
		}
	}
}

func TestParseRoutes(t *testing.T) {
	r, err := ParseRoutes("*:201,202")
	if err != nil || len(r) != 1 || r[0].Countries != nil || !slices.Equal(r[0].Deliveries, []int32{201, 202}) {
		t.Fatalf("%+v, %v", r, err)
	}
	for _, bad := range []string{"", "*", "*:", "*:abc", "*:201,201", "*:200", "*:1000", "*:201;", "ru:201", "RUS:201", "RU,:201", "RU:201", "RU,BY:202;*:201"} {
		if r, err := ParseRoutes(bad); err == nil {
			t.Errorf("%q: %+v", bad, r)
		}
	}
}
