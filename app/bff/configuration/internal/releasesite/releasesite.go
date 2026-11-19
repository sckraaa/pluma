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

// Package releasesite tells which site the releases of the client apps are
// taken from by a client of a country (task 113, docs/ops/releases.md): the
// site of PLUMA_RELEASES_URL, or a mirror of it for the countries where
// that name is blocked, the mirror on the Russian node for Russia. Both
// serve the same /dl/ and /iphone/install, so the update link of the
// iPhone app (help.getAppUpdate) differs only in the site.
//
// The files of /dl/ alone may come from elsewhere (task 115, ParseFiles):
// a CDN that caches the site's /dl/, by country as well, so that the
// update prefix of the desktop app (help.getConfig) points at the cache
// nearest to the client.
package releasesite

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Sites is the site of the releases and its mirrors by country. A nil
// *Sites is no site at all: no update is offered.
type Sites struct {
	main      string
	byCountry map[string]string
}

// Parse parses the releases' sites of bff's config:
//
//   - main is PLUMA_RELEASES_URL, https://pluma.chat; empty: no site, and
//     nil (routes must then be empty too).
//   - routes is PLUMA_RELEASES_ROUTES, records "<countries>=<site>"
//     separated by ";", e.g. "RU,BY=https://pluma.sckra.dev", with the
//     countries as in EDGE_ROUTES (ISO 3166-1 alpha-2 codes in upper case);
//     a client of any other country gets main. Empty: main for everyone.
//
// A site is https://<host name> with no path, query or fragment; a "/" at
// the end of one is dropped. No country is in two records.
func Parse(main, routes string) (*Sites, error) {
	main, routes = strings.TrimSpace(main), strings.TrimSpace(routes)
	if main == "" {
		if routes != "" {
			return nil, errors.New("releases: PLUMA_RELEASES_ROUTES without PLUMA_RELEASES_URL")
		}
		return nil, nil
	}
	// The site of PLUMA_RELEASES_URL is taken as it is, as before the
	// mirrors: only its "/" at the end goes.
	s := &Sites{main: strings.TrimRight(main, "/"), byCountry: map[string]string{}}
	if err := s.addRoutes("PLUMA_RELEASES_ROUTES", routes); err != nil {
		return nil, err
	}
	return s, nil
}

// ParseFiles parses the sites the clients download the files of /dl/ from
// (task 115): the caches of the site's /dl/, which serve the same paths,
// over the sites of site (Parse), whose /dl/ serve them where no cache is
// named.
//
//   - site is the result of Parse; nil: no releases, nil (files and
//     routes must then be empty too).
//   - files is PLUMA_RELEASES_FILES_URL, the CDN in front of the site's
//     /dl/ for a client of any country no record names, https://<host>;
//     empty: the main site.
//   - routes is PLUMA_RELEASES_FILES_ROUTES, records as in
//     PLUMA_RELEASES_ROUTES: "RU,BY=https://r1.sckra.dev", the cache of
//     the Russian node. A country of PLUMA_RELEASES_ROUTES that it does not
//     name keeps its mirror of the site: the CDN of files may be no more
//     reachable there than the main site is.
func ParseFiles(site *Sites, files, routes string) (*Sites, error) {
	files, routes = strings.TrimSpace(files), strings.TrimSpace(routes)
	if site == nil {
		if files != "" || routes != "" {
			return nil, errors.New("releases: PLUMA_RELEASES_FILES_URL or PLUMA_RELEASES_FILES_ROUTES without PLUMA_RELEASES_URL")
		}
		return nil, nil
	}
	s := &Sites{main: site.main, byCountry: map[string]string{}}
	if files != "" {
		main, err := checkSite(files)
		if err != nil {
			return nil, fmt.Errorf("releases: PLUMA_RELEASES_FILES_URL: %w", err)
		}
		s.main = main
	}
	if err := s.addRoutes("PLUMA_RELEASES_FILES_ROUTES", routes); err != nil {
		return nil, err
	}
	for c, mirror := range site.byCountry {
		if _, ok := s.byCountry[c]; !ok {
			s.byCountry[c] = mirror
		}
	}
	return s, nil
}

// addRoutes adds the records of routes, the value of the variable name:
// "<countries>=<site>" separated by ";".
func (s *Sites) addRoutes(name, routes string) error {
	if routes == "" {
		return nil
	}
	for rec := range strings.SplitSeq(routes, ";") {
		rec = strings.TrimSpace(rec)
		who, site, ok := strings.Cut(rec, "=")
		if !ok {
			return fmt.Errorf("releases: %s record %q: want <countries>=<site>", name, rec)
		}
		site, err := checkSite(strings.TrimSpace(site))
		if err != nil {
			return fmt.Errorf("releases: %s record %q: %w", name, rec, err)
		}
		for c := range strings.SplitSeq(who, ",") {
			c = strings.TrimSpace(c)
			if !validCountry(c) {
				return fmt.Errorf("releases: %s record %q: %q is not a country code", name, rec, c)
			}
			if _, ok := s.byCountry[c]; ok {
				return fmt.Errorf("releases: %s names country %s twice", name, c)
			}
			s.byCountry[c] = site
		}
	}
	return nil
}

// ByCountry is whether the site depends on the country of the client: with
// no mirror there is no need to look the country up.
func (s *Sites) ByCountry() bool {
	return s != nil && len(s.byCountry) > 0
}

// URL is the site of a client of country ("" is an unknown one, which gets
// the main site), without a "/" at the end; "" for nil Sites.
func (s *Sites) URL(country string) string {
	if s == nil {
		return ""
	}
	if site, ok := s.byCountry[country]; ok {
		return site
	}
	return s.main
}

// Main is the site of the clients of the countries no record names:
// PLUMA_RELEASES_URL for Parse, the files' own site for ParseFiles; "" for
// nil Sites.
func (s *Sites) Main() string {
	if s == nil {
		return ""
	}
	return s.main
}

// checkSite returns the site of a record in one form: https://<host> in
// lower case, with a port only when it is not 443.
func checkSite(s string) (string, error) {
	u, err := url.Parse(strings.TrimRight(s, "/"))
	if err != nil {
		return "", fmt.Errorf("%q is not a URL", s)
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("%q is not an https:// URL", s)
	}
	if u.Hostname() == "" || !strings.Contains(u.Hostname(), ".") {
		return "", fmt.Errorf("%q: the host is not a name", s)
	}
	if u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return "", fmt.Errorf("%q: a user, a path, a query or a fragment", s)
	}
	host := strings.ToLower(u.Hostname())
	if p := u.Port(); p != "" && p != "443" {
		host += ":" + p
	}
	return "https://" + host, nil
}

// validCountry is whether c is two upper-case Latin letters, as an ISO
// 3166-1 alpha-2 code of authsession.getIpCountry.
func validCountry(c string) bool {
	return len(c) == 2 && c[0] >= 'A' && c[0] <= 'Z' && c[1] >= 'A' && c[1] <= 'Z'
}
