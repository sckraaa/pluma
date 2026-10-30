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

package dao

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeromicro/go-zero/core/logx/logtest"
)

type logEntry struct {
	Level   string `json:"level"`
	Content string `json:"content"`
}

// logEntries returns what was logged into b, one entry a line.
func logEntries(t *testing.T, b *logtest.Buffer) []logEntry {
	t.Helper()
	var entries []logEntry
	for _, line := range bytes.Split(bytes.TrimSpace(b.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var e logEntry
		if err := json.Unmarshal(line, &e); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		entries = append(entries, e)
	}
	return entries
}

// A GeoIP database that cannot be used does not stop authsession: one error
// at start names the path and the reason, and every place is UNKNOWN.
func TestOpenGeoIPCannotUse(t *testing.T) {
	dir := t.TempDir()
	notMMDB := filepath.Join(dir, "not-a-database.mmdb")
	if err := os.WriteFile(notMMDB, []byte("a file of another kind"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, path, reason string
	}{
		{"missing", filepath.Join(dir, "dbip-city-lite.mmdb"), "no such file or directory"},
		{"not an MMDB", notMMDB, "invalid MaxMind DB file"},
		{"a directory", dir, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			logs := logtest.NewCollector(t)
			if r := openGeoIP(c.path); r != nil {
				t.Fatalf("a reader of %s", c.path)
			}
			entries := logEntries(t, logs)
			if len(entries) != 1 || entries[0].Level != "error" ||
				!strings.Contains(entries[0].Content, c.path) || !strings.Contains(entries[0].Content, c.reason) {
				t.Fatalf("logged %+v, want one error with %q and %q", entries, c.path, c.reason)
			}
			d := &Dao{MMDB: openGeoIP(c.path)}
			if place, region := d.getCountryAndRegionByIp("8.8.8.8"); place != "UNKNOWN" || region != "" {
				t.Fatalf("place %q, %q, want UNKNOWN", place, region)
			}
		})
	}
}

// An empty GeoIPDatabase turns GeoIP off: that is said once, not as an
// error, and every place is UNKNOWN.
func TestOpenGeoIPOff(t *testing.T) {
	logs := logtest.NewCollector(t)
	d := &Dao{MMDB: openGeoIP("")}
	entries := logEntries(t, logs)
	if d.MMDB != nil || len(entries) != 1 || entries[0].Level != "info" || !strings.Contains(entries[0].Content, "GeoIPDatabase") {
		t.Fatalf("reader %v, logged %+v", d.MMDB, entries)
	}
	for _, ip := range []string{"8.8.8.8", "", "not an address"} {
		if place, region := d.getCountryAndRegionByIp(ip); place != "UNKNOWN" || region != "" {
			t.Fatalf("place of %q: %q, %q, want UNKNOWN", ip, place, region)
		}
	}
}
