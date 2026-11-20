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
	"errors"
	"fmt"
	"slices"
	"strings"

	pb "github.com/teamgram/teamgram-server/app/service/sfu/sfu"
)

// routesAll is the countries of the record of every other client.
const routesAll = "*"

// Route is a record of GROUPCALLS_ROUTES: the routes of the SFU, in order
// of preference, of the clients of some countries, or of every other
// client.
type Route struct {
	// Countries are ISO 3166-1 alpha-2 codes in upper case; nil for the
	// record "*".
	Countries []string
	// Routes are the route names of JoinRequest.routes.
	Routes []string
}

// Routes are the records of GROUPCALLS_ROUTES; nil is "*:direct" for
// everyone.
type Routes []Route

// ParseRoutes parses GROUPCALLS_ROUTES (task 116, ADR 0074), records
// "<countries or *>:<route,route,...>" separated by ";", e.g.
// "RU,BY:ru;*:direct": a client joins with the routes of the record that
// names the country of its address (authsession.getIpCountry), and of the
// record "*" when none does or the country is not known. There is exactly
// one record "*", and no country is in two records; the grammar of the
// countries is that of CDN_ROUTES (pkg/cdn.ParseRoutes) and EDGE_ROUTES.
// Whether the SFU has a route is the SFU's (SFU_RELAYS): it leaves out a
// route it does not have. Empty: nil.
func ParseRoutes(s string) (Routes, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	var routes Routes
	all := false
	seen := map[string]bool{}
	for rec := range strings.SplitSeq(s, ";") {
		rec = strings.TrimSpace(rec)
		who, names, ok := strings.Cut(rec, ":")
		if !ok {
			return nil, fmt.Errorf("groupcalls: GROUPCALLS_ROUTES record %q: want <countries or *>:<routes>", rec)
		}
		var r Route
		who = strings.TrimSpace(who)
		if who == routesAll {
			if all {
				return nil, errors.New("groupcalls: GROUPCALLS_ROUTES has two records for *")
			}
			all = true
		} else {
			for c := range strings.SplitSeq(who, ",") {
				c = strings.TrimSpace(c)
				if len(c) != 2 || c[0] < 'A' || c[0] > 'Z' || c[1] < 'A' || c[1] > 'Z' {
					return nil, fmt.Errorf("groupcalls: GROUPCALLS_ROUTES record %q: %q is not a country code", rec, c)
				}
				if seen[c] {
					return nil, fmt.Errorf("groupcalls: GROUPCALLS_ROUTES names country %s twice", c)
				}
				seen[c] = true
				r.Countries = append(r.Countries, c)
			}
		}
		for name := range strings.SplitSeq(names, ",") {
			name = strings.TrimSpace(name)
			if !pb.ValidRoute(name) {
				return nil, fmt.Errorf("groupcalls: GROUPCALLS_ROUTES record %q: %q is not a route name (a-z, 0-9 and '-', starting with a letter, at most 16)", rec, name)
			}
			if slices.Contains(r.Routes, name) {
				return nil, fmt.Errorf("groupcalls: GROUPCALLS_ROUTES record %q names route %s twice", rec, name)
			}
			r.Routes = append(r.Routes, name)
		}
		if len(r.Routes) > pb.MaxRoutes {
			return nil, fmt.Errorf("groupcalls: GROUPCALLS_ROUTES record %q has more than %d routes", rec, pb.MaxRoutes)
		}
		routes = append(routes, r)
	}
	if !all {
		// A client whose country is not known (no GeoIP answer, a private
		// address) must have a record too.
		return nil, errors.New("groupcalls: GROUPCALLS_ROUTES has no record for *, the clients of every other country")
	}
	return routes, nil
}

// For returns the routes of a client of country ("" when not known): those
// of the record that names it, or of "*". Nil routes are the SFU's
// "direct".
func (rs Routes) For(country string) []string {
	var all []string
	for _, r := range rs {
		if r.Countries == nil {
			all = r.Routes
			continue
		}
		if country != "" && slices.Contains(r.Countries, country) {
			return r.Routes
		}
	}
	return all
}

// Countries reports whether a record names a country: only then does a
// join need the client's country.
func (rs Routes) Countries() bool {
	for _, r := range rs {
		if r.Countries != nil {
			return true
		}
	}
	return false
}
