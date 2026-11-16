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
	"testing"
	"time"
)

func validConfig() Config {
	return Config{
		NodeControl:          "10.77.0.1:20510",
		CertFile:             "/run/secrets/cdn/client.pem",
		KeyFile:              "/run/secrets/cdn/client-key.pem",
		CAFile:               "/run/secrets/cdn/server-ca.pem",
		DcAddress:            "195.201.252.126:8443",
		DcSecret:             "dd000102030405060708090a0b0c0d0e0f",
		MonthlyEgressLimitGB: "15000",
	}
}

// TestConfigDefaults: empty limits take their defaults.
func TestConfigDefaults(t *testing.T) {
	s, err := validConfig().Parse()
	if err != nil {
		t.Fatal(err)
	}
	if s.DcId != 201 || s.MinFileSize != 1<<20 || s.PushBytesPerSecond != 2_500_000 || s.PushConcurrency != 1 ||
		s.ReuploadWait != 10*time.Second || s.MonthlyEgressLimit != 15000*1_000_000_000 || s.ServerName != "10.77.0.1" ||
		s.DcAddress != "195.201.252.126:8443" || s.DcSecret[15] != 0x0f {
		t.Fatalf("settings %+v", s)
	}
	if s, err = (Config{}).Parse(); s != nil || err != nil {
		t.Fatalf("disabled: %v, %v", s, err)
	}
}

// TestConfigRejects: a wrong value stops the start.
func TestConfigRejects(t *testing.T) {
	for name, change := range map[string]func(*Config){
		"no certificates":        func(c *Config) { c.KeyFile = "" },
		"control without port":   func(c *Config) { c.NodeControl = "10.77.0.1" },
		"dc 1":                   func(c *Config) { c.DcId = "1" },
		"dc 1000":                func(c *Config) { c.DcId = "1000" },
		"name for address":       func(c *Config) { c.DcAddress = "cdn.example.com:8443" },
		"address without port":   func(c *Config) { c.DcAddress = "195.201.252.126" },
		"secret without dd":      func(c *Config) { c.DcSecret = "000102030405060708090a0b0c0d0e0f" },
		"short secret":           func(c *Config) { c.DcSecret = "dd0001" },
		"no egress limit":        func(c *Config) { c.MonthlyEgressLimitGB = "" },
		"min size not MiB":       func(c *Config) { c.MinFileSize = "1000000" },
		"zero rate":              func(c *Config) { c.PushRateMbit = "0" },
		"too many pushes":        func(c *Config) { c.PushConcurrency = "9" },
		"reupload wait too long": func(c *Config) { c.ReuploadWait = "5m" },
	} {
		c := validConfig()
		change(&c)
		if s, err := c.Parse(); err == nil {
			t.Errorf("%s: accepted, %+v", name, s)
		}
	}
}
