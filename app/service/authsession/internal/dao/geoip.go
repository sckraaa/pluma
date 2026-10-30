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
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/oschwald/geoip2-golang"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/metric"
)

// geoIPCheckEvery is how often authsession looks whether the GeoIP database
// file was replaced. geoipupdate puts a new MaxMind GeoLite City release in
// place twice a week (docs/ops/deployment.md, "GeoIP"), and the GeoLite EULA
// wants an old release deleted within 30 days of a new one: authsession
// switches to the new file without a restart.
const geoIPCheckEvery = 10 * time.Minute

// errNoGeoIP is the lookup error without a usable database.
var errNoGeoIP = errors.New("geoip: no database")

// metricGeoIPBuild is the build time of the database in use: the GeoIPStale
// alert fires when it falls behind MaxMind's twice-weekly releases
// (teamgramd/deploy/prometheus/server/rules/general.yml).
var metricGeoIPBuild = metric.NewGaugeVec(&metric.GaugeVecOpts{
	Namespace: "geoip", Subsystem: "database", Name: "build_timestamp_seconds",
	Help:   "Build time of the GeoIP database in use, as a Unix time.",
	Labels: []string{},
})

// geoIP is the GeoIP database (config GeoIPDatabase), which gives the place
// of a session in account.getAuthorizations and the New login message.
// Without one authsession works on and every place is "UNKNOWN": an empty
// path says so once, and a file that cannot be used is an error, logged once
// for each version of the file with the path and the reason.
//
// The reader maps the file into memory: an update must put a new file in
// place by a rename, never write over the open one. reload opens the new
// file and closes the old reader once no lookup uses it.
type geoIP struct {
	path string

	mu sync.RWMutex
	r  *geoip2.Reader

	// Only reload uses these: file is the file r was opened from, and
	// logged the last error logged, with the version of the file it was
	// about, so that an error that repeats at every check is logged once.
	file   os.FileInfo
	logged string
}

// newGeoIP opens the database at path; nil for an empty path, which turns
// GeoIP off.
func newGeoIP(path string) *geoIP {
	if path == "" {
		logx.Info("geoip: no database (GeoIPDatabase is empty), every place is UNKNOWN")
		return nil
	}
	g := &geoIP{path: path}
	g.reload()
	return g
}

// watch calls reload every geoIPCheckEvery for the life of the process,
// and reports the database in use.
func (g *geoIP) watch() {
	for range time.Tick(geoIPCheckEvery) {
		g.reload()
		g.report()
	}
}

// report sets metricGeoIPBuild from the database in use, on every check:
// go-zero drops a value set before its Prometheus agent starts
// (core/metric/metric.go, update), and authsession opens the database
// before that. Without a database the gauge is not set.
func (g *geoIP) report() {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.r != nil {
		metricGeoIPBuild.Set(float64(g.r.Metadata().BuildEpoch))
	}
}

// reload opens the file at the path when it is not the one open, and uses it
// from then on. A file that cannot be used leaves the open database in use;
// it is tried again at every check, since the cause may lie outside the
// file, such as its permissions. Calls of reload must not overlap: newGeoIP
// makes the first, then watch.
func (g *geoIP) reload() {
	fi, err := os.Stat(g.path)
	if err == nil {
		if sameFile(g.file, fi) {
			g.logged = ""
			return
		}
		var r *geoip2.Reader
		if r, err = openGeoIP(g.path); err == nil {
			g.use(r, fi)
			return
		}
	}
	logged := err.Error()
	if fi != nil {
		logged = fmt.Sprintf("%s (%d bytes, %s)", logged, fi.Size(), fi.ModTime())
	}
	if logged == g.logged {
		return
	}
	g.logged = logged
	if g.r != nil {
		logx.Errorf("geoip: cannot use the database %s, the one read before stays in use: %v", g.path, err)
	} else {
		logx.Errorf("geoip: cannot use the database %s, every place is UNKNOWN: %v", g.path, err)
	}
}

// use makes r, opened from the file fi, the database of the lookups.
func (g *geoIP) use(r *geoip2.Reader, fi os.FileInfo) {
	m := r.Metadata()
	logx.Infof("geoip: database %s, %s built %s", g.path, m.DatabaseType,
		time.Unix(int64(m.BuildEpoch), 0).UTC().Format(time.DateOnly))

	g.mu.Lock()
	old := g.r
	g.r = r
	g.mu.Unlock()
	g.file, g.logged = fi, ""
	// Lookups hold the read lock while they use a reader: none uses the old
	// one any more.
	if old != nil {
		_ = old.Close()
	}
}

// city looks ip up. The result holds copies of the database's strings and
// stays valid after a reload.
func (g *geoIP) city(ip net.IP) (*geoip2.City, error) {
	if g == nil {
		return nil, errNoGeoIP
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	if g.r == nil {
		return nil, errNoGeoIP
	}
	return g.r.City(ip)
}

// sameFile tells whether fi is the version of the file that seen describes:
// the same file, not changed since.
func sameFile(seen, fi os.FileInfo) bool {
	return seen != nil && os.SameFile(seen, fi) && seen.ModTime().Equal(fi.ModTime()) && seen.Size() == fi.Size()
}

// openGeoIP opens the City database at path. geoip2-golang v1.8.0 also
// takes a Country database for City lookups (reader.go, getDBType): its
// places are the country alone.
func openGeoIP(path string) (*geoip2.Reader, error) {
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
		return nil, err
	}
	return r, nil
}
