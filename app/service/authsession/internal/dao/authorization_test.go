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
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/teamgram/marmota/pkg/stores/sqlc"
	"github.com/teamgram/teamgram-server/app/service/authsession/authsession"
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
		MMDB:       openGeoIP(geoIPDatabase(t, "Prague", "Czechia", "CZ")),
	}
	if d.MMDB == nil {
		t.Fatal("the test database does not open")
	}
	t.Cleanup(func() { _ = d.MMDB.Close() })
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
