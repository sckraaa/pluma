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
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/teamgram/marmota/pkg/stores/sqlc"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
	"github.com/zeromicro/go-zero/core/logx/logtest"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// The authorization of a Web K session in Safari 26 on a Mac, as
// account.getAuthorizations, updateNewAuthorization and the New login
// message show it (task 019), built from what its initConnection said.
func TestGetAuthorization(t *testing.T) {
	// miniredis does not implement CLIENT SETINFO; DisableIdentity keeps the
	// failing connect-time command from tripping the go-zero redis breaker.
	mr := miniredis.RunT(t)
	d := &Dao{
		CachedConn: sqlc.NewNodeConn(nil, redis.MustNewRedis(redis.RedisConf{Host: mr.Addr(), Type: redis.NodeType, DisableIdentity: true})),
		geoIP:      newGeoIP(geoIPDatabase(t, "Prague", "Czechia", "CZ")),
	}
	if d.geoIP.r == nil {
		t.Fatal("the test database does not open")
	}
	t.Cleanup(func() { _ = d.geoIP.r.Close() })
	ctx := context.Background()
	const key = 1001
	if err := d.CachedConn.SetCache(ctx, genAuthDataCacheKey(key), &CacheAuthData{
		Client: authsession.MakeTLClientSession(&authsession.ClientSession{
			AuthKeyId:     key,
			Ip:            "193.17.47.1",
			ApiId:         10004,
			DeviceModel:   "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.0 Safari/605.1.15",
			SystemVersion: "MacIntel",
			AppVersion:    "2.2 K",
			LangPack:      "webk",
		}).To_ClientSession(),
	}); err != nil {
		t.Fatal(err)
	}

	a, err := d.GetAuthorization(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	// The place is found by the address, which is not shown.
	if a.GetIp() != "" {
		t.Errorf("ip %q, want none", a.GetIp())
	}
	// The place names the city; no region repeats it, Web K shows the two
	// joined.
	if a.GetCountry() != "Prague, Czechia" || a.GetRegion() != "" {
		t.Errorf("place %q, region %q, want Prague, Czechia and no region", a.GetCountry(), a.GetRegion())
	}
	// The browser and the system, as Telegram named a session of Web A.
	if a.GetDeviceModel() != "Safari 26" || a.GetSystemVersion() != "macOS" || a.GetPlatform() != "" {
		t.Errorf("device %q, system %q, platform %q, want Safari 26, macOS and no platform",
			a.GetDeviceModel(), a.GetSystemVersion(), a.GetPlatform())
	}
}

// The region of a site's login is the place of the address as a session's
// is written, and nothing, not "UNKNOWN", when the place is not known: the
// clients show the region of a site whenever it is not empty.
func TestIpRegion(t *testing.T) {
	d := &Dao{geoIP: newGeoIP(geoIPDatabase(t, "Prague", "Czechia", "CZ"))}
	if d.geoIP.r == nil {
		t.Fatal("the test database does not open")
	}
	t.Cleanup(func() { _ = d.geoIP.r.Close() })
	if got := d.IpRegion("193.17.47.1"); got != "Prague, Czechia" {
		t.Errorf("known address: %q, want Prague, Czechia", got)
	}
	for _, ip := range []string{"", "not an address", "193.17.47.1:443"} {
		if got := d.IpRegion(ip); got != "" {
			t.Errorf("%q: %q, want none", ip, got)
		}
	}
	none := &Dao{geoIP: newGeoIP("")}
	if got := none.IpRegion("193.17.47.1"); got != "" {
		t.Errorf("no database: %q, want none", got)
	}
}

// countryDao is a Dao whose GeoIP database puts every address of its
// ipVersion (and for 6 also every IPv4 address) in the country of isoCode.
func countryDao(t *testing.T, isoCode, country string, ipVersion uint16) *Dao {
	t.Helper()
	path := filepath.Join(t.TempDir(), "GeoLite2-City.mmdb")
	writeGeoIPDatabase(t, path, geoIPRecord{city: "Somewhere", country: country, isoCode: isoCode, dbType: "GeoLite2-City", ipVersion: ipVersion})
	d := &Dao{geoIP: newGeoIP(path)}
	if d.geoIP.r == nil {
		t.Fatal("the test database does not open")
	}
	t.Cleanup(func() { _ = d.geoIP.r.Close() })
	return d
}

// The country of an address is the ISO code of its country in upper case,
// and nothing when the address is not a public one, is no address, or the
// database cannot tell: a client is routed by it (task 109), and nothing
// routes it as a client of no country.
func TestIpCountry(t *testing.T) {
	ru := countryDao(t, "RU", "Russia", 6)
	for _, ip := range []string{"95.108.198.1", "2a02:6b8::1", "::ffff:95.108.198.1"} {
		if got := ru.IpCountry(ip); got != "RU" {
			t.Errorf("Russian database, %q: %q, want RU", ip, got)
		}
	}
	if got := countryDao(t, "DE", "Germany", 4).IpCountry("193.99.144.80"); got != "DE" {
		t.Errorf("German address: %q, want DE", got)
	}
	for _, ip := range []string{
		"10.1.2.3", "172.16.0.1", "192.168.0.139", "fd7a:115c:a1e0::1", // private
		"127.0.0.1", "::1", "169.254.1.1", "fe80::1", "224.0.0.1", "0.0.0.0", "::", "255.255.255.255",
		"", "not an address", "95.108.198.1:443", "[2a02:6b8::1]", "95.108.198",
	} {
		if got := ru.IpCountry(ip); got != "" {
			t.Errorf("%q: %q, want none", ip, got)
		}
	}

	// The code of the database is taken in upper case; a record without a
	// code, or with something else, has no country.
	for code, want := range map[string]string{"de": "DE", "Cz": "CZ", "": "", "RUS": "", "R1": "", "Ру": ""} {
		if got := countryDao(t, code, "Somewhere", 4).IpCountry("193.99.144.80"); got != want {
			t.Errorf("code %q: %q, want %q", code, got, want)
		}
	}

	// An IPv6 address in an IPv4-only database is not known, and the error
	// logged without the address: maxminddb names it in the error.
	logs := logtest.NewCollector(t)
	if got := countryDao(t, "DE", "Germany", 4).IpCountry("2a02:6b8::1"); got != "" {
		t.Errorf("IPv6 address in an IPv4 database: %q, want none", got)
	}
	if out := logs.String(); !strings.Contains(out, "IPv6") || strings.Contains(out, "2a02") {
		t.Errorf("logged %q, want the family and no address", out)
	}

	none := &Dao{geoIP: newGeoIP("")}
	if got := none.IpCountry("95.108.198.1"); got != "" {
		t.Errorf("no database: %q, want none", got)
	}
}
