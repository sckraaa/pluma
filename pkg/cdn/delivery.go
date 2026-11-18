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

package cdn

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Delivery is one way to hand a client an HTTPS URL for a prepared file
// (ADR 0071 §1-2): a CDN, how its URLs are signed and where it takes the
// cipher from. Its ID is the dc_id of upload.fileCdnRedirect; the clients
// do not connect to it (task 107, client contract p. 3), but tdesktop takes
// dc_id 0 for no CDN (storage/download_manager_mtproto.cpp:517-519) and
// 1000 and above for a temporary DC (mtproto/mtp_instance.h:45).
type Delivery struct {
	// ID is the dc_id in upload.fileCdnRedirect, MinDeliveryID to
	// MaxDeliveryID.
	ID   int32
	Name string
	// BaseURL is the scheme and host the URLs start with, no trailing
	// slash; its host is what the logs may name.
	BaseURL string
	// Signer makes the URLs: gcore-md5 or path-hmac (later: worker-hmac,
	// yandex-md5, s3-presign).
	Signer Signer
	// Source is where the CDN takes the cipher from: SourceHomeOrigin
	// today; a store the home uploads to later (target_id 2 and above).
	Source string
	// MonthlyBytes: the delivery leaves the routes for the rest of a
	// calendar month (UTC) once this many bytes went out through it.
	MonthlyBytes int64
	// InsecureHTTP marks an http:// BaseURL, which only the stand of the
	// e2e tests may have (CDN_DEV_ALLOW_HTTP).
	InsecureHTTP bool
	// OriginAuth is the value of OriginAuthHeader the CDN of a
	// SourceHomeOrigin delivery sends the origin: the delivery's own
	// (CDN_RU_ORIGIN_AUTH of delivery 202) or CDN_ORIGIN_AUTH. The origin
	// counts the bytes it serves for the delivery whose value came (task
	// 109, "Выдача 202"), so two deliveries whose paths are the same
	// (gcore-md5) never share a value.
	OriginAuth Secret
	// HealthAddr is host:port the checks of the delivery dial instead of
	// the host of BaseURL, whose name stays the SNI, the Host and the name
	// the certificate is checked for; empty: the host of BaseURL. Delivery
	// 202 is checked through the tunnel to its node (10.78.0.1:443),
	// because the way out of the home goes through the VPS, from which the
	// node may be as closed as the VPS is from Russia (task 109,
	// "Здоровье 202").
	HealthAddr string
}

// The ids of the deliveries.
const (
	MinDeliveryID = 201
	MaxDeliveryID = 999
)

// SourceHomeOrigin: the CDN pulls the cipher from the origin of the home,
// which encrypts on the fly; preparing a file only computes its hashes.
const SourceHomeOrigin = "home-origin"

// TargetHomeOrigin is cdn_replicas.target_id of the preparation for the
// home origin. A delivery whose CDN keeps its own copy (a store the home
// uploads to) will have a target of its own, 2 and above.
const TargetHomeOrigin int32 = 1

// Target is the cdn_replicas.target_id of the copy the delivery serves:
// the file is redirected to it only when that copy is ready.
func (d *Delivery) Target() int32 {
	// SourceHomeOrigin is the only source Parse accepts.
	return TargetHomeOrigin
}

// Route is a record of CDN_ROUTES: the deliveries, in order of preference,
// of the clients of some countries, or of every other client.
type Route struct {
	// Countries are ISO 3166-1 alpha-2 codes in upper case; nil for the
	// record "*", the clients of every country no other record names and
	// those whose country is not known.
	Countries []string
	// Deliveries are the ids of the deliveries, the preferred first.
	Deliveries []int32
}

// RoutesAll is the countries of the record of every other client.
const RoutesAll = "*"

// ParseRoutes parses CDN_ROUTES, records "<countries or *>:<id,id,...>"
// separated by ";", e.g. "*:201" or "RU,BY:202,201;*:201" (ADR 0071 §1,
// ADR 0072 §8 p. 5): a client is routed by the record that names the
// country of its address (authsession.getIpCountry), and by the record "*"
// when none does or the country is not known. There is exactly one record
// "*", and no country is in two records; the grammar of the countries is
// that of EDGE_ROUTES (app/bff/configuration/internal/appconfig/edge.go).
// Whether the ids name enabled deliveries is the config's check.
func ParseRoutes(s string) ([]Route, error) {
	var routes []Route
	all := false
	seen := map[string]bool{}
	for rec := range strings.SplitSeq(s, ";") {
		rec = strings.TrimSpace(rec)
		who, ids, ok := strings.Cut(rec, ":")
		if !ok {
			return nil, fmt.Errorf("cdn: CDN_ROUTES record %q: want <countries or *>:<ids>", rec)
		}
		var r Route
		who = strings.TrimSpace(who)
		if who == RoutesAll {
			if all {
				return nil, errors.New("cdn: CDN_ROUTES has two records for *")
			}
			all = true
		} else {
			for c := range strings.SplitSeq(who, ",") {
				c = strings.TrimSpace(c)
				if len(c) != 2 || c[0] < 'A' || c[0] > 'Z' || c[1] < 'A' || c[1] > 'Z' {
					return nil, fmt.Errorf("cdn: CDN_ROUTES record %q: %q is not a country code", rec, c)
				}
				if seen[c] {
					return nil, fmt.Errorf("cdn: CDN_ROUTES names country %s twice", c)
				}
				seen[c] = true
				r.Countries = append(r.Countries, c)
			}
		}
		for id := range strings.SplitSeq(ids, ",") {
			n, err := strconv.ParseInt(strings.TrimSpace(id), 10, 32)
			if err != nil || n < MinDeliveryID || n > MaxDeliveryID {
				return nil, fmt.Errorf("cdn: CDN_ROUTES record %q: delivery %q is not a number from %d to %d", rec, id, MinDeliveryID, MaxDeliveryID)
			}
			if slices.Contains(r.Deliveries, int32(n)) {
				return nil, fmt.Errorf("cdn: CDN_ROUTES record %q names delivery %d twice", rec, n)
			}
			r.Deliveries = append(r.Deliveries, int32(n))
		}
		routes = append(routes, r)
	}
	if !all {
		// A client whose country is not known (no GeoIP answer, a private
		// address) must have a record too.
		return nil, errors.New("cdn: CDN_ROUTES has no record for *, the clients of every other country")
	}
	return routes, nil
}
