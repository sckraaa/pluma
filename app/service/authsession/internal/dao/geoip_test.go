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
			if place := d.placeByIp("8.8.8.8"); place != "UNKNOWN" {
				t.Fatalf("place %q, want UNKNOWN", place)
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
		if place := d.placeByIp(ip); place != "UNKNOWN" {
			t.Fatalf("place of %q: %q, want UNKNOWN", ip, place)
		}
	}
}

// geoIPDatabase writes a DB-IP City Lite database that puts every IPv4
// address in city, country (English names) with the country's isoCode, and
// returns its path. It is the MaxMind DB format as maxminddb-golang v1.10.0
// reads it (reader.go FromBytes, decoder.go decodeCtrlData): a search tree
// of one node whose two 24-bit records point at the one data record, the
// 16-byte data section separator, the record, and the metadata after its
// marker. Every string must be shorter than 29 bytes, the longest size a
// control byte holds itself.
func geoIPDatabase(t *testing.T, city, country, isoCode string) string {
	t.Helper()
	const nodeCount = 1
	var b bytes.Buffer
	str := func(s string) {
		if len(s) >= 29 {
			t.Fatalf("%q is too long for this writer", s)
		}
		b.WriteByte(2<<5 | byte(len(s)))
		b.WriteString(s)
	}
	mapOf := func(pairs int) { b.WriteByte(7<<5 | byte(pairs)) }
	uint16Of := func(v byte) { b.Write([]byte{5<<5 | 1, v}) }
	// An extended type has 0 as the type of the control byte, and the
	// type less 7 in the next byte.
	extended := func(typ, size byte) { b.Write([]byte{size, typ - 7}) }

	// The tree: both branches of node 0 lead to the record at offset 0 of
	// the data section, numbered node_count + 16 + offset.
	record := []byte{0, 0, nodeCount + 16}
	b.Write(record)
	b.Write(record)
	b.Write(make([]byte, 16))

	mapOf(2)
	str("city")
	mapOf(1)
	str("names")
	mapOf(1)
	str("en")
	str(city)
	str("country")
	mapOf(2)
	str("iso_code")
	str(isoCode)
	str("names")
	mapOf(1)
	str("en")
	str(country)

	b.WriteString("\xAB\xCD\xEFMaxMind.com")
	mapOf(9)
	str("binary_format_major_version")
	uint16Of(2)
	str("binary_format_minor_version")
	uint16Of(0)
	str("build_epoch")
	extended(9, 0) // a uint64 of no bytes: 0
	str("database_type")
	str("DBIP-City-Lite")
	str("description")
	mapOf(0)
	str("ip_version")
	uint16Of(4)
	str("languages")
	extended(11, 0) // an empty array
	str("node_count")
	b.Write([]byte{6<<5 | 1, nodeCount})
	str("record_size")
	uint16Of(24)

	path := filepath.Join(t.TempDir(), "dbip-city-lite.mmdb")
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The place of a session is "City, Country", or the country alone, from the
// English names of the database.
func TestPlaceByIp(t *testing.T) {
	for _, c := range []struct {
		city, country, want string
	}{
		{"Prague", "Czechia", "Prague, Czechia"},
		{"", "Czechia", "Czechia"},
		{"Prague", "", "UNKNOWN"},
		{"", "", "UNKNOWN"},
	} {
		d := &Dao{MMDB: openGeoIP(geoIPDatabase(t, c.city, c.country, "CZ"))}
		if d.MMDB == nil {
			t.Fatal("the test database does not open")
		}
		if got := d.placeByIp("193.17.47.1"); got != c.want {
			t.Errorf("city %q, country %q: place %q, want %q", c.city, c.country, got, c.want)
		}
		// A database of IPv4 addresses has no place for an IPv6 one.
		if got := d.placeByIp("2001:148f:ffff::1"); got != "UNKNOWN" {
			t.Errorf("an IPv6 address: place %q, want UNKNOWN", got)
		}
		_ = d.MMDB.Close()
	}
}
