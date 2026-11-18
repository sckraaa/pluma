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
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// updateE2EGeoIP rewrites the GeoIP database of the e2e stand from
// e2eGeoIPDatabase: go test -run TestE2EGeoIPDatabase -update-e2e-geoip.
var updateE2EGeoIP = flag.Bool("update-e2e-geoip", false, "write test/e2e/testdata/geoip/GeoLite2-City.mmdb")

// e2eGeoIPPath is the GeoIP database the e2e stand mounts into authsession
// (test/e2e/docker-compose.e2e.yaml).
var e2eGeoIPPath = filepath.Join("..", "..", "..", "..", "..", "test", "e2e", "testdata", "geoip", "GeoLite2-City.mmdb")

// e2eGeoIPPlace is what the database of the stand says of a half of the
// IPv4 addresses.
type e2eGeoIPPlace struct{ city, country, isoCode string }

// e2eGeoIPDatabase is the GeoIP database of the e2e stand: every IPv4
// address below 128.0.0.0 is in Moscow, Russia, every other one in
// Frankfurt, Germany, so that the tests route a client of either country
// (task 109, the edges by country) by its address. The private addresses
// of the stand's own network get no country all the same (IpCountry).
//
// It is the MaxMind DB format as maxminddb-golang v1.10.0 reads it (as
// writeGeoIPDatabase writes it, with two records): a search tree of one
// node whose two 24-bit records, for the first bit of an address 0 and 1,
// point at the two data records, the 16-byte data section separator, the
// records, and the metadata after its marker.
func e2eGeoIPDatabase(t *testing.T) []byte {
	t.Helper()
	const nodeCount = 1
	enc := func(b *bytes.Buffer) (str func(string), mapOf func(int), uint16Of func(uint16)) {
		str = func(s string) {
			if len(s) >= 29 {
				t.Fatalf("%q is too long for this writer", s)
			}
			b.WriteByte(2<<5 | byte(len(s)))
			b.WriteString(s)
		}
		mapOf = func(pairs int) { b.WriteByte(7<<5 | byte(pairs)) }
		uint16Of = func(v uint16) {
			if v < 256 {
				b.Write([]byte{5<<5 | 1, byte(v)})
				return
			}
			b.Write([]byte{5<<5 | 2, byte(v >> 8), byte(v)})
		}
		return
	}
	record := func(p e2eGeoIPPlace) []byte {
		var b bytes.Buffer
		str, mapOf, _ := enc(&b)
		mapOf(2)
		str("city")
		mapOf(1)
		str("names")
		mapOf(1)
		str("en")
		str(p.city)
		str("country")
		mapOf(2)
		str("iso_code")
		str(p.isoCode)
		str("names")
		mapOf(1)
		str("en")
		str(p.country)
		return b.Bytes()
	}
	low := record(e2eGeoIPPlace{"Moscow", "Russia", "RU"})
	high := record(e2eGeoIPPlace{"Frankfurt", "Germany", "DE"})

	var b bytes.Buffer
	// A record of the tree points at offset o of the data section as
	// node_count + 16 + o.
	pointer := func(o int) []byte { v := nodeCount + 16 + o; return []byte{byte(v >> 16), byte(v >> 8), byte(v)} }
	b.Write(pointer(0))
	b.Write(pointer(len(low)))
	b.Write(make([]byte, 16))
	b.Write(low)
	b.Write(high)

	str, mapOf, uint16Of := enc(&b)
	b.WriteString("\xAB\xCD\xEFMaxMind.com")
	mapOf(9)
	str("binary_format_major_version")
	uint16Of(2)
	str("binary_format_minor_version")
	b.WriteByte(5 << 5) // a uint16 0
	str("build_epoch")
	var epoch uint32 = 1794614400 // 2026-11-13, any fixed time
	b.Write([]byte{4, 9 - 7, byte(epoch >> 24), byte(epoch >> 16), byte(epoch >> 8), byte(epoch)})
	str("database_type")
	str("GeoLite2-City")
	str("description")
	mapOf(0)
	str("ip_version")
	uint16Of(4)
	str("languages")
	b.Write([]byte{0, 11 - 7}) // an empty array
	str("node_count")
	b.Write([]byte{6<<5 | 1, nodeCount})
	str("record_size")
	uint16Of(24)
	return b.Bytes()
}

// TestE2EGeoIPDatabase: the database the e2e stand mounts is the one
// e2eGeoIPDatabase writes, and authsession reads from it what the e2e tests
// of the edges by country expect: Russia for the lower half of the IPv4
// addresses, Germany for the upper, nothing for a private address.
func TestE2EGeoIPDatabase(t *testing.T) {
	want := e2eGeoIPDatabase(t)
	if *updateE2EGeoIP {
		if err := os.WriteFile(e2eGeoIPPath, want, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(e2eGeoIPPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s is not the database of e2eGeoIPDatabase: go test -run TestE2EGeoIPDatabase -update-e2e-geoip", e2eGeoIPPath)
	}

	d := &Dao{geoIP: newGeoIP(e2eGeoIPPath)}
	if d.geoIP.r == nil {
		t.Fatal("the database of the stand does not open")
	}
	t.Cleanup(func() { _ = d.geoIP.r.Close() })
	for ip, country := range map[string]string{
		"5.8.16.10":   "RU",
		"95.108.1.1":  "RU",
		"127.0.0.1":   "",
		"203.0.113.5": "DE",
		"198.51.100.": "",
		"172.18.0.1":  "",
		"10.78.0.1":   "",
	} {
		if got := d.IpCountry(ip); got != country {
			t.Errorf("IpCountry(%q) = %q, want %q", ip, got, country)
		}
	}
	if got := d.IpRegion("5.8.16.10"); got != "Moscow, Russia" {
		t.Errorf("IpRegion of a Russian address: %q", got)
	}
	if got := d.IpRegion("203.0.113.5"); got != "Frankfurt, Germany" {
		t.Errorf("IpRegion of a German address: %q", got)
	}
}
