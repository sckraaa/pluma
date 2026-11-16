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

package translate

import (
	"strings"
	"testing"
	"time"
)

func TestConfigDefaults(t *testing.T) {
	c := Config{URL: "http://translate:8080", Model: "hy-mt"}.WithDefaults()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Profile != "hy-mt1.5" || c.Slots != 4 || c.SlotContext() != 4096 ||
		c.SyncWait != 25*time.Second || c.SyncWaitSingle != 45*time.Second ||
		c.FailedTTL != time.Minute || c.CacheTTL != 30*24*time.Hour ||
		c.MaxItemLength != 4096 || c.MaxItems != 100 || c.MaxRequestLength != 32768 ||
		c.ItemsPerMinute != 300 || c.ItemsPerHour != 3000 {
		t.Fatalf("defaults: %+v", c)
	}
	h := c.HTTPConfig()
	if h.URL != c.URL || h.Model != "hy-mt" || h.Timeout != 10*time.Second || h.TimeoutPerChar != 25*time.Millisecond {
		t.Fatalf("engine config: %+v", h)
	}
	if err := (Config{Slots: -1}).Validate(); err != nil {
		t.Fatalf("an off configuration is valid whatever the rest: %v", err)
	}
	if (Config{}).Enabled() {
		t.Fatal("enabled without a URL")
	}
}

func TestConfigValidate(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Config)
	}{
		{"not http", func(c *Config) { c.URL = "translate:8080" }},
		{"no model", func(c *Config) { c.Model = "" }},
		{"long model", func(c *Config) { c.Model = strings.Repeat("m", 65) }},
		{"short token", func(c *Config) { c.Token = "short" }},
		{"unknown profile", func(c *Config) { c.Profile = "gemma" }},
		{"too many slots", func(c *Config) { c.Slots = 17 }},
		{"small context", func(c *Config) { c.Context = 1024 }},
		{"long wait", func(c *Config) { c.SyncWaitSingle = 3 * time.Minute }},
		{"short failure", func(c *Config) { c.FailedTTL = time.Millisecond }},
		{"long item", func(c *Config) { c.MaxItemLength = 5000 }},
		{"many items", func(c *Config) { c.MaxItems = 101 }},
		{"request under an item", func(c *Config) { c.MaxRequestLength = 100 }},
		{"limit under a request", func(c *Config) { c.ItemsPerMinute = 50 }},
		{"hour under a minute", func(c *Config) { c.ItemsPerHour = 200 }},
	} {
		c := Config{URL: "http://translate:8080", Model: "hy-mt", Token: strings.Repeat("t", 32)}
		tc.edit(&c)
		if err := c.WithDefaults().Validate(); err == nil {
			t.Errorf("%s: accepted", tc.name)
		}
	}
}
