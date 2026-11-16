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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teamgram/teamgram-server/pkg/conf"
)

const valid = `
Log:
  ServiceName: cdn
  Mode: console
DcId: 201
MTProto:
  ListenOn: 0.0.0.0:8443
  Secret: dd00112233445566778899aabbccddeeff
Store:
  DataDir: /var/lib/cdn
  CacheBytes: 1073741824
Control:
  ListenOn: 0.0.0.0:20510
  CertFile: /c/server.pem
  KeyFile: /c/server-key.pem
  ClientCAFile: /c/client-ca.pem
`

func load(t *testing.T, yaml string) (Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cdn.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	// go-zero's loader runs Validate itself; it is called again for the
	// error of a config that loads.
	var c Config
	if err := conf.Load(path, &c); err != nil {
		return c, err
	}
	return c, c.Validate()
}

func TestValid(t *testing.T) {
	c, err := load(t, valid)
	if err != nil {
		t.Fatal(err)
	}
	if c.MTProto.MaxAuthKeys != 20000 || c.MTProto.MaxConnsPerIP != 32 || c.MTProto.MaxInFlight != 16 ||
		c.MTProto.KeyExchangesPerIPMinute != 300 || c.Store.IdleTTL.Hours() != 720 || c.Store.DiskReserveBytes != 2<<30 {
		t.Fatalf("defaults: %+v %+v", c.MTProto, c.Store)
	}
	s, err := c.ParseSecret()
	if err != nil || s.Bytes()[0] != 0xdd {
		t.Fatalf("secret: %v", err)
	}
}

func TestInvalid(t *testing.T) {
	for name, edit := range map[string][2]string{
		"home dc":      {"DcId: 201", "DcId: 1"},
		"temporary dc": {"DcId: 201", "DcId: 1000"},
		"bare secret":  {"dd00112233445566778899aabbccddeeff", "00112233445566778899aabbccddeeff"},
		"short secret": {"dd00112233445566778899aabbccddeeff", "dd0011"},
		"ee secret":    {"dd00112233445566778899aabbccddeeff", "ee00112233445566778899aabbccddeeff"},
		"no cache":     {"CacheBytes: 1073741824", "CacheBytes: 0"},
		"no client ca": {"ClientCAFile: /c/client-ca.pem", "ClientCAFile: \"\""},
		"bad listen":   {"ListenOn: 0.0.0.0:8443", "ListenOn: nowhere"},
	} {
		if !strings.Contains(valid, edit[0]) {
			t.Fatalf("%s: %q is not in the config", name, edit[0])
		}
		if _, err := load(t, strings.Replace(valid, edit[0], edit[1], 1)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
