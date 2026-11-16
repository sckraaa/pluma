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
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

// The statistics run without a key (off) or with a key of 32 bytes or more
// in standard base64; a bad key stops bff at its start, and the error does
// not show it.
func TestStatsConfig(t *testing.T) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	good := base64.StdEncoding.EncodeToString(raw)
	short := base64.StdEncoding.EncodeToString(raw[:16])
	for _, c := range []struct {
		name    string
		c       StatsConfig
		enabled bool
		ok      bool
	}{
		{"off", StatsConfig{ThisDc: 1}, false, true},
		{"on", StatsConfig{ThisDc: 1, TokenKey: good}, true, true},
		{"short key", StatsConfig{ThisDc: 1, TokenKey: short}, true, false},
		{"not base64", StatsConfig{ThisDc: 1, TokenKey: "%%%" + good}, true, false},
		{"no dc", StatsConfig{TokenKey: good}, true, false},
	} {
		err := c.c.Validate()
		if (err == nil) != c.ok || c.c.Enabled() != c.enabled {
			t.Errorf("%s: enabled %v, %v", c.name, c.c.Enabled(), err)
		}
		if err != nil && (strings.Contains(err.Error(), good) || strings.Contains(err.Error(), short)) {
			t.Errorf("%s: the error shows the key", c.name)
		}
	}
}
