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
	"net"
	"time"

	"github.com/oschwald/geoip2-golang"
	"github.com/zeromicro/go-zero/core/logx"
)

// openGeoIP opens the GeoIP database at path (config GeoIPDatabase), which
// gives the place of a session in account.getAuthorizations and the New
// login message. Without one authsession works on and every place is
// "UNKNOWN": an empty path says so once, and a database that cannot be used
// is an error, logged once with the path and the reason.
//
// The reader maps the file into memory: an update must put a new file in
// place by a rename and restart authsession, never write over the open one
// (docs/ops/deployment.md, "GeoIP (DB-IP Lite)").
func openGeoIP(path string) *geoip2.Reader {
	if path == "" {
		logx.Info("geoip: no database (GeoIPDatabase is empty), every place is UNKNOWN")
		return nil
	}
	r, err := geoip2.Open(path)
	if err == nil {
		// A database of another kind, such as ASN, opens but has no
		// places: every lookup would fail.
		_, err = r.City(net.IPv4zero)
	}
	if err != nil {
		// geoip2.Open returns the reader of a database of an unknown type
		// together with the error.
		if r != nil {
			_ = r.Close()
		}
		logx.Errorf("geoip: cannot use the database %s, every place is UNKNOWN: %v", path, err)
		return nil
	}
	m := r.Metadata()
	logx.Infof("geoip: database %s, %s built %s", path, m.DatabaseType,
		time.Unix(int64(m.BuildEpoch), 0).UTC().Format(time.DateOnly))
	return r
}
