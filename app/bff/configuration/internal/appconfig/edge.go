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

package appconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/teamgram/proto/mtproto"
)

// plumaEdgeKey is Pluma's own key of the app config that tells the patched
// clients where the edges are, the WebSocket entries behind a CDN or the
// Russian node, and in which order to try them against the direct address
// (task 109, ADR 0072 §4, the clients' contract of the task's brief, p. 6):
//
//	{"v":1,"ws":["wss://…/apiws",…],"order":["direct","edge"],"sticky_s":1800}
//
// A client takes the last value it accepted and keeps the list built into
// it otherwise.
const plumaEdgeKey = "pluma_edge"

// The bounds the clients accept in pluma_edge (the contract, p. 6); a value
// outside them would be dropped by every client, so it stops the start.
const (
	edgeVersion        = 1
	maxEdgesPerRoute   = 4
	DefaultEdgeStickyS = 1800
	minEdgeStickyS     = 60
	maxEdgeStickyS     = 86400
	maxEdgeNameLen     = 16
	edgeOrderDirect    = "direct"
	edgeOrderEdge      = "edge"
	edgeWebSocketPath  = "/apiws"
	edgeAllCountries   = "*"
	edgeRecordSep      = ";"
	edgeListSep        = ","
	edgeNameURLSep     = "="
	edgeRecordFieldSep = ":"
)

// Edges is pluma_edge for each record of EDGE_ROUTES, picked by the
// country of the client's address. A nil *Edges is no edge at all: the
// config has no pluma_edge and the clients use the list built into them.
// It is safe for concurrent use.
type Edges struct {
	routes    []edgeRoute
	byCountry map[string]int // the index in routes of a country's record
	all       int            // the index of the record "*"

	mu   sync.Mutex
	base *AppConfig   // the config the configs below were made of
	with []*AppConfig // base with the pluma_edge of each record
}

// edgeRoute is a record of EDGE_ROUTES with its value of pluma_edge.
type edgeRoute struct {
	value *mtproto.JSONValue
	text  string // the value as compact JSON, which the hash covers
}

// edgeValue is pluma_edge as JSON, its fields in the order of the contract.
type edgeValue struct {
	V       int      `json:"v"`
	WS      []string `json:"ws"`
	Order   []string `json:"order"`
	StickyS int      `json:"sticky_s"`
}

// ParseEdges parses the edges of bff's config:
//
//   - urls is EDGE_WS_URLS, the edges by name, "<name>=<wss URL>" separated
//     by ",", e.g. "gcore=wss://x.gcorecdn.com/apiws,ru=wss://ru.pluma.chat/apiws";
//     a name is 1-16 of a-z and 0-9. Empty: no edges, and nil.
//   - routes is EDGE_ROUTES, records "<countries or *>:<order>:<names>"
//     separated by ";", e.g. "RU:edge,direct:ru,gcore;*:direct,edge:gcore,ru",
//     with the countries as in CDN_ROUTES (ISO 3166-1 alpha-2 codes in upper
//     case, ADR 0071 §1). Empty: "*:direct,edge:" and every edge in the
//     order of urls.
//   - stickyS is EDGE_STICKY_S, how many seconds a client keeps to an edge
//     it switched to, 60-86400. Empty: 1800.
//
// An edge is wss://<host name>/apiws on port 443. Every record names 1-4
// edges, each once; the order is direct,edge or edge,direct; exactly one
// record is "*", and no country is in two records.
func ParseEdges(urls, routes, stickyS string) (*Edges, error) {
	sticky := DefaultEdgeStickyS
	if s := strings.TrimSpace(stickyS); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < minEdgeStickyS || n > maxEdgeStickyS {
			return nil, fmt.Errorf("edge: EDGE_STICKY_S %q is not a number of seconds from %d to %d", stickyS, minEdgeStickyS, maxEdgeStickyS)
		}
		sticky = n
	}
	urls, routes = strings.TrimSpace(urls), strings.TrimSpace(routes)
	if urls == "" {
		if routes != "" {
			return nil, errors.New("edge: EDGE_ROUTES without EDGE_WS_URLS")
		}
		return nil, nil
	}

	var names []string
	byName := map[string]string{}
	for item := range strings.SplitSeq(urls, edgeListSep) {
		name, u, ok := strings.Cut(strings.TrimSpace(item), edgeNameURLSep)
		if !ok {
			return nil, fmt.Errorf("edge: EDGE_WS_URLS item %q: want <name>=<wss URL>", item)
		}
		name, u = strings.TrimSpace(name), strings.TrimSpace(u)
		if !validEdgeName(name) {
			return nil, fmt.Errorf("edge: EDGE_WS_URLS item %q: the name is not 1-%d of a-z and 0-9", item, maxEdgeNameLen)
		}
		if _, ok := byName[name]; ok {
			return nil, fmt.Errorf("edge: EDGE_WS_URLS names %q twice", name)
		}
		u, err := checkEdgeURL(u)
		if err != nil {
			return nil, fmt.Errorf("edge: EDGE_WS_URLS %s: %w", name, err)
		}
		for n, other := range byName {
			if other == u {
				return nil, fmt.Errorf("edge: EDGE_WS_URLS gives %s the address of %s", name, n)
			}
		}
		names = append(names, name)
		byName[name] = u
	}

	if routes == "" {
		routes = edgeAllCountries + edgeRecordFieldSep + edgeOrderDirect + edgeListSep + edgeOrderEdge +
			edgeRecordFieldSep + strings.Join(names, edgeListSep)
	}
	e := &Edges{byCountry: map[string]int{}, all: -1}
	for rec := range strings.SplitSeq(routes, edgeRecordSep) {
		rec = strings.TrimSpace(rec)
		fields := strings.Split(rec, edgeRecordFieldSep)
		if len(fields) != 3 {
			return nil, fmt.Errorf("edge: EDGE_ROUTES record %q: want <countries or *>:<order>:<names>", rec)
		}
		i := len(e.routes)
		who := strings.TrimSpace(fields[0])
		if who == edgeAllCountries {
			if e.all >= 0 {
				return nil, errors.New("edge: EDGE_ROUTES has two records for *")
			}
			e.all = i
		} else {
			for c := range strings.SplitSeq(who, edgeListSep) {
				c = strings.TrimSpace(c)
				if !validCountry(c) {
					return nil, fmt.Errorf("edge: EDGE_ROUTES record %q: %q is not a country code", rec, c)
				}
				if _, ok := e.byCountry[c]; ok {
					return nil, fmt.Errorf("edge: EDGE_ROUTES names country %s twice", c)
				}
				e.byCountry[c] = i
			}
		}

		order := splitList(fields[1])
		if !slices.Equal(order, []string{edgeOrderDirect, edgeOrderEdge}) && !slices.Equal(order, []string{edgeOrderEdge, edgeOrderDirect}) {
			return nil, fmt.Errorf("edge: EDGE_ROUTES record %q: the order is %q, want direct,edge or edge,direct", rec, fields[1])
		}
		v := edgeValue{V: edgeVersion, WS: []string{}, Order: order, StickyS: sticky}
		var seen []string
		for _, name := range splitList(fields[2]) {
			u, ok := byName[name]
			if !ok {
				return nil, fmt.Errorf("edge: EDGE_ROUTES record %q: no edge %q in EDGE_WS_URLS", rec, name)
			}
			if slices.Contains(seen, name) {
				return nil, fmt.Errorf("edge: EDGE_ROUTES record %q names edge %s twice", rec, name)
			}
			seen = append(seen, name)
			v.WS = append(v.WS, u)
		}
		if len(v.WS) == 0 || len(v.WS) > maxEdgesPerRoute {
			return nil, fmt.Errorf("edge: EDGE_ROUTES record %q has %d edges, want 1 to %d", rec, len(v.WS), maxEdgesPerRoute)
		}
		r, err := newEdgeRoute(v)
		if err != nil {
			return nil, err
		}
		e.routes = append(e.routes, r)
	}
	if e.all < 0 {
		return nil, errors.New("edge: EDGE_ROUTES has no record for *, the clients of every other country")
	}
	return e, nil
}

// newEdgeRoute makes the value of pluma_edge of v.
func newEdgeRoute(v edgeValue) (edgeRoute, error) {
	text, err := json.Marshal(v)
	if err != nil {
		return edgeRoute{}, fmt.Errorf("edge: %w", err)
	}
	d := json.NewDecoder(bytes.NewReader(text))
	d.UseNumber()
	jv, err := value(d)
	if err != nil {
		return edgeRoute{}, fmt.Errorf("edge: %w", err)
	}
	return edgeRoute{value: jv, text: string(text)}, nil
}

// ByCountry is whether the config depends on the country of the client:
// with only the record "*" there is no need to look the country up.
func (e *Edges) ByCountry() bool {
	return e != nil && len(e.byCountry) > 0
}

// Config returns base with the pluma_edge of the record of country, or of
// the record "*" when no record names it ("" is an unknown country), and
// a hash of its own: a client that comes from another country takes the
// other value with its next help.getAppConfig. base itself for nil Edges.
// The configs of the records are made once for each new base.
func (e *Edges) Config(base *AppConfig, country string) *AppConfig {
	if e == nil {
		return base
	}
	i, ok := e.byCountry[country]
	if !ok {
		i = e.all
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.base != base {
		e.base, e.with = base, make([]*AppConfig, len(e.routes))
	}
	if e.with[i] == nil {
		r := e.routes[i]
		e.with[i] = base.withValues([]keyValue{{key: plumaEdgeKey, value: r.value, text: r.text}})
	}
	return e.with[i]
}

// checkEdgeURL returns the address of an edge the clients accept,
// wss://<host name>/apiws on port 443 without a user, a query or a
// fragment, in one form: the host in lower case and no port, so that one
// edge written two ways is still one address. The host is a name, never
// an IP address: the edge is a CDN's host or the node's, reached by its
// TLS certificate's name.
func checkEdgeURL(s string) (string, error) {
	u, err := url.Parse(s)
	if err != nil {
		return "", fmt.Errorf("%q is not a URL", s)
	}
	if u.Scheme != "wss" {
		return "", fmt.Errorf("%q is not a wss:// URL", s)
	}
	host := u.Hostname()
	if host == "" || net.ParseIP(host) != nil || !validHostName(host) {
		return "", fmt.Errorf("%q: the host is not a DNS name", s)
	}
	if p := u.Port(); p != "" && p != "443" {
		return "", fmt.Errorf("%q: the port is not 443", s)
	}
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return "", fmt.Errorf("%q: a user, a query or a fragment", s)
	}
	if u.Path != edgeWebSocketPath || u.RawPath != "" {
		return "", fmt.Errorf("%q: the path is not %s", s, edgeWebSocketPath)
	}
	return "wss://" + strings.ToLower(host) + edgeWebSocketPath, nil
}

// validHostName is whether h is a DNS name of letters, digits and hyphens
// in labels of at most 63, with a dot: a name the clients' TLS checks a
// certificate against.
func validHostName(h string) bool {
	if len(h) > 253 || !strings.Contains(h, ".") {
		return false
	}
	for label := range strings.SplitSeq(h, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return true
}

// validEdgeName is whether s is 1-16 of a-z and 0-9.
func validEdgeName(s string) bool {
	if s == "" || len(s) > maxEdgeNameLen {
		return false
	}
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	return true
}

// validCountry is whether c is two upper-case Latin letters, as an ISO
// 3166-1 alpha-2 code of authsession.getIpCountry.
func validCountry(c string) bool {
	return len(c) == 2 && c[0] >= 'A' && c[0] <= 'Z' && c[1] >= 'A' && c[1] <= 'Z'
}

// splitList splits s at "," and trims the items.
func splitList(s string) []string {
	var out []string
	for item := range strings.SplitSeq(s, edgeListSep) {
		out = append(out, strings.TrimSpace(item))
	}
	return out
}
