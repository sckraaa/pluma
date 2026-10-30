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
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/zeromicro/go-zero/core/logx/logtest"
	"github.com/zeromicro/go-zero/core/prometheus"
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
// names the path and the reason, every place is UNKNOWN, and checking the
// same file again logs nothing more.
func TestGeoIPCannotUse(t *testing.T) {
	dir := t.TempDir()
	notMMDB := filepath.Join(dir, "not-a-database.mmdb")
	if err := os.WriteFile(notMMDB, []byte("a file of another kind"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, path, reason string
	}{
		{"missing", filepath.Join(dir, "GeoLite2-City.mmdb"), "no such file or directory"},
		{"not an MMDB", notMMDB, "invalid MaxMind DB file"},
		{"a directory", dir, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			logs := logtest.NewCollector(t)
			d := &Dao{geoIP: newGeoIP(c.path)}
			d.geoIP.reload()
			entries := logEntries(t, logs)
			if len(entries) != 1 || entries[0].Level != "error" ||
				!strings.Contains(entries[0].Content, c.path) || !strings.Contains(entries[0].Content, c.reason) ||
				!strings.Contains(entries[0].Content, "every place is UNKNOWN") {
				t.Fatalf("logged %+v, want one error with %q and %q", entries, c.path, c.reason)
			}
			if place := d.placeByIp("8.8.8.8"); place != "UNKNOWN" {
				t.Fatalf("place %q, want UNKNOWN", place)
			}
		})
	}
}

// An empty GeoIPDatabase turns GeoIP off: that is said once, not as an
// error, and every place is UNKNOWN.
func TestGeoIPOff(t *testing.T) {
	logs := logtest.NewCollector(t)
	d := &Dao{geoIP: newGeoIP("")}
	entries := logEntries(t, logs)
	if d.geoIP != nil || len(entries) != 1 || entries[0].Level != "info" || !strings.Contains(entries[0].Content, "GeoIPDatabase") {
		t.Fatalf("database %v, logged %+v", d.geoIP, entries)
	}
	for _, ip := range []string{"8.8.8.8", "", "not an address"} {
		if place := d.placeByIp(ip); place != "UNKNOWN" {
			t.Fatalf("place of %q: %q, want UNKNOWN", ip, place)
		}
	}
}

// geoIPRecord is what a test database says of every IPv4 address: the
// English names, the country's ISO code and, unless 0, the accuracy radius
// in kilometres; dbType and buildEpoch are the database_type and build_epoch
// of its metadata.
type geoIPRecord struct {
	city, country, isoCode string
	radius                 uint16
	dbType                 string
	buildEpoch             uint32
}

// geoIPDatabase writes a DB-IP City Lite database that puts every IPv4
// address in city, country, and returns its path.
func geoIPDatabase(t *testing.T, city, country, isoCode string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dbip-city-lite.mmdb")
	writeGeoIPDatabase(t, path, geoIPRecord{city: city, country: country, isoCode: isoCode, dbType: "DBIP-City-Lite"})
	return path
}

// writeGeoIPDatabase puts a database of rec at path as geoipupdate does: it
// writes a new file next to it and renames it over path. It is the MaxMind
// DB format as maxminddb-golang v1.10.0 reads it (reader.go FromBytes,
// decoder.go decodeCtrlData): a search tree of one node whose two 24-bit
// records point at the one data record, the 16-byte data section separator,
// the record, and the metadata after its marker. Every string must be
// shorter than 29 bytes, the longest size a control byte holds itself.
func writeGeoIPDatabase(t *testing.T, path string, rec geoIPRecord) {
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
	// A uint16 of as many bytes as it needs, big-endian.
	uint16Of := func(v uint16) {
		switch {
		case v == 0:
			b.WriteByte(5 << 5)
		case v < 256:
			b.Write([]byte{5<<5 | 1, byte(v)})
		default:
			b.Write([]byte{5<<5 | 2, byte(v >> 8), byte(v)})
		}
	}
	// An extended type has 0 as the type of the control byte, and the
	// type less 7 in the next byte.
	extended := func(typ, size byte) { b.Write([]byte{size, typ - 7}) }

	// The tree: both branches of node 0 lead to the record at offset 0 of
	// the data section, numbered node_count + 16 + offset.
	record := []byte{0, 0, nodeCount + 16}
	b.Write(record)
	b.Write(record)
	b.Write(make([]byte, 16))

	if rec.radius != 0 {
		mapOf(3)
	} else {
		mapOf(2)
	}
	str("city")
	mapOf(1)
	str("names")
	mapOf(1)
	str("en")
	str(rec.city)
	str("country")
	mapOf(2)
	str("iso_code")
	str(rec.isoCode)
	str("names")
	mapOf(1)
	str("en")
	str(rec.country)
	if rec.radius != 0 {
		str("location")
		mapOf(1)
		str("accuracy_radius")
		uint16Of(rec.radius)
	}

	b.WriteString("\xAB\xCD\xEFMaxMind.com")
	mapOf(9)
	str("binary_format_major_version")
	uint16Of(2)
	str("binary_format_minor_version")
	uint16Of(0)
	str("build_epoch")
	extended(9, 4) // a uint64 of 4 bytes
	b.Write([]byte{byte(rec.buildEpoch >> 24), byte(rec.buildEpoch >> 16), byte(rec.buildEpoch >> 8), byte(rec.buildEpoch)})
	str("database_type")
	str(rec.dbType)
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

	writeRenamed(t, path, b.Bytes())
}

// writeRenamed puts data at path by a rename, the way an update must.
func writeRenamed(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path+".temporary", data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".temporary", path); err != nil {
		t.Fatal(err)
	}
}

// The place of a session is "City, Country", or the country alone, from the
// English names of the database; the country alone also when the database
// knows the city only within a radius wider than maxCityRadius.
func TestPlaceByIp(t *testing.T) {
	for _, c := range []struct {
		rec  geoIPRecord
		want string
	}{
		{geoIPRecord{city: "Prague", country: "Czechia", dbType: "DBIP-City-Lite"}, "Prague, Czechia"},
		{geoIPRecord{country: "Czechia", dbType: "DBIP-City-Lite"}, "Czechia"},
		{geoIPRecord{city: "Prague", dbType: "DBIP-City-Lite"}, "UNKNOWN"},
		{geoIPRecord{dbType: "DBIP-City-Lite"}, "UNKNOWN"},
		{geoIPRecord{city: "Prague", country: "Czechia", radius: 20, dbType: "GeoLite2-City"}, "Prague, Czechia"},
		{geoIPRecord{city: "Prague", country: "Czechia", radius: maxCityRadius, dbType: "GeoLite2-City"}, "Prague, Czechia"},
		{geoIPRecord{city: "Prague", country: "Czechia", radius: maxCityRadius + 1, dbType: "GeoLite2-City"}, "Czechia"},
		{geoIPRecord{city: "Prague", country: "Czechia", radius: 1000, dbType: "GeoLite2-City"}, "Czechia"},
		{geoIPRecord{city: "Prague", radius: 1000, dbType: "GeoLite2-City"}, "UNKNOWN"},
	} {
		path := filepath.Join(t.TempDir(), "GeoLite2-City.mmdb")
		c.rec.isoCode = "CZ"
		writeGeoIPDatabase(t, path, c.rec)
		d := &Dao{geoIP: newGeoIP(path)}
		if d.geoIP.r == nil {
			t.Fatal("the test database does not open")
		}
		if got := d.placeByIp("193.17.47.1"); got != c.want {
			t.Errorf("%+v: place %q, want %q", c.rec, got, c.want)
		}
		// A database of IPv4 addresses has no place for an IPv6 one.
		if got := d.placeByIp("2001:148f:ffff::1"); got != "UNKNOWN" {
			t.Errorf("an IPv6 address: place %q, want UNKNOWN", got)
		}
		_ = d.geoIP.r.Close()
	}
}

// authsession takes up a new database file, put in place by a rename, at
// the next check: whether it comes after start, replaces the one in use or
// cannot be used. A file that cannot be used, or that is gone, leaves the
// one in use, and each error is logged once.
func TestGeoIPReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "GeoLite2-City.mmdb")
	city := func(name string) geoIPRecord {
		return geoIPRecord{city: name, country: "Czechia", isoCode: "CZ", radius: 20, dbType: "GeoLite2-City"}
	}
	logs := logtest.NewCollector(t)
	d := &Dao{geoIP: newGeoIP(path)}
	t.Cleanup(func() {
		if d.geoIP.r != nil {
			_ = d.geoIP.r.Close()
		}
	})
	step := func(name, want, level, logged string) {
		t.Helper()
		logs.Reset()
		d.geoIP.reload()
		entries := logEntries(t, logs)
		switch {
		case level == "" && len(entries) != 0:
			t.Fatalf("%s: logged %+v, want nothing", name, entries)
		case level != "" && (len(entries) != 1 || entries[0].Level != level || !strings.Contains(entries[0].Content, logged)):
			t.Fatalf("%s: logged %+v, want one %s with %q", name, entries, level, logged)
		}
		if got := d.placeByIp("193.17.47.1"); got != want {
			t.Fatalf("%s: place %q, want %q", name, got, want)
		}
	}

	step("missing, checked again", "UNKNOWN", "", "")
	writeGeoIPDatabase(t, path, city("Prague"))
	step("comes", "Prague, Czechia", "info", "GeoLite2-City")
	step("unchanged", "Prague, Czechia", "", "")
	prague := d.geoIP.r
	writeGeoIPDatabase(t, path, city("Brno"))
	step("replaced", "Brno, Czechia", "info", "GeoLite2-City")
	// The reader of the replaced file is closed, its memory unmapped.
	if _, err := prague.City(net.IPv4(193, 17, 47, 1)); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("the replaced reader answers: %v", err)
	}
	writeRenamed(t, path, []byte("a truncated download"))
	step("broken", "Brno, Czechia", "error", "the one read before stays in use")
	step("broken, checked again", "Brno, Czechia", "", "")
	asn := city("Ostrava")
	asn.dbType = "GeoLite2-ASN"
	writeGeoIPDatabase(t, path, asn)
	step("another kind of database", "Brno, Czechia", "error", "the one read before stays in use")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	step("gone", "Brno, Czechia", "error", "the one read before stays in use")
	step("gone, checked again", "Brno, Czechia", "", "")
	writeGeoIPDatabase(t, path, city("Ostrava"))
	step("back", "Ostrava, Czechia", "info", "GeoLite2-City")
	// Moved away and back unchanged: the second move is logged again.
	if err := os.Rename(path, path+".aside"); err != nil {
		t.Fatal(err)
	}
	step("moved away", "Ostrava, Czechia", "error", "the one read before stays in use")
	if err := os.Rename(path+".aside", path); err != nil {
		t.Fatal(err)
	}
	step("moved back", "Ostrava, Czechia", "", "")
	if err := os.Rename(path, path+".aside"); err != nil {
		t.Fatal(err)
	}
	step("moved away again", "Ostrava, Czechia", "error", "the one read before stays in use")
}

// A file that could not be read is tried again at every check: once its
// permissions are fixed, it is taken up although the file itself did not
// change.
func TestGeoIPReloadPermissions(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file of mode 0")
	}
	path := filepath.Join(t.TempDir(), "GeoLite2-City.mmdb")
	writeGeoIPDatabase(t, path, geoIPRecord{city: "Prague", country: "Czechia", isoCode: "CZ", radius: 20, dbType: "GeoLite2-City"})
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	logs := logtest.NewCollector(t)
	d := &Dao{geoIP: newGeoIP(path)}
	d.geoIP.reload()
	if entries := logEntries(t, logs); len(entries) != 1 || !strings.Contains(entries[0].Content, "permission denied") {
		t.Fatalf("logged %+v, want one error about the permission", entries)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	d.geoIP.reload()
	t.Cleanup(func() {
		if d.geoIP.r != nil {
			_ = d.geoIP.r.Close()
		}
	})
	if got := d.placeByIp("193.17.47.1"); got != "Prague, Czechia" {
		t.Fatalf("place %q after the permissions were fixed, want Prague, Czechia", got)
	}
}

// Lookups run while the database is replaced: each sees one database or
// the other, never a closed one (run with -race).
func TestGeoIPReloadConcurrent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "GeoLite2-City.mmdb")
	cities := []string{"Prague", "Brno"}
	rec := func(i int) geoIPRecord {
		return geoIPRecord{city: cities[i%2], country: "Czechia", isoCode: "CZ", radius: 20, dbType: "GeoLite2-City"}
	}
	writeGeoIPDatabase(t, path, rec(0))
	d := &Dao{geoIP: newGeoIP(path)}
	t.Cleanup(func() { _ = d.geoIP.r.Close() })

	stop := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan string, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if got := d.placeByIp("193.17.47.1"); got != "Prague, Czechia" && got != "Brno, Czechia" {
					errs <- got
					return
				}
			}
		}()
	}
	for i := 1; i <= 50; i++ {
		writeGeoIPDatabase(t, path, rec(i))
		d.geoIP.reload()
	}
	close(stop)
	wg.Wait()
	close(errs)
	for got := range errs {
		t.Errorf("place %q during a reload", got)
	}
}

// The build time of the database in use is exported for the GeoIPStale alert
// (teamgramd/deploy/prometheus/server/rules/general.yml), on every check and
// for the database the check switched to.
func TestGeoIPReport(t *testing.T) {
	prometheus.Enable()
	path := filepath.Join(t.TempDir(), "GeoLite2-City.mmdb")
	const built, rebuilt = 1793318400, 1793664000 // 2026-10-30 and 2026-11-03, 00:00 UTC
	rec := geoIPRecord{city: "Prague", country: "Czechia", isoCode: "CZ", radius: 20, dbType: "GeoLite2-City", buildEpoch: built}
	writeGeoIPDatabase(t, path, rec)
	g := newGeoIP(path)
	t.Cleanup(func() { _ = g.r.Close() })
	g.report()
	if got := geoIPBuildGauge(t); got != built {
		t.Fatalf("build time %v, want %v", got, built)
	}
	rec.buildEpoch = rebuilt
	writeGeoIPDatabase(t, path, rec)
	g.reload()
	g.report()
	if got := geoIPBuildGauge(t); got != rebuilt {
		t.Fatalf("build time after the reload %v, want %v", got, rebuilt)
	}
}

// geoIPBuildGauge is the value of geoip_database_build_timestamp_seconds.
func geoIPBuildGauge(t *testing.T) float64 {
	t.Helper()
	families, err := prom.DefaultGatherer.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		if f.GetName() == "geoip_database_build_timestamp_seconds" && len(f.GetMetric()) == 1 {
			return f.GetMetric()[0].GetGauge().GetValue()
		}
	}
	t.Fatal("geoip_database_build_timestamp_seconds is not exported")
	return 0
}
