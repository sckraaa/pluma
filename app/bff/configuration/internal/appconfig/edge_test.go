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
	"encoding/json"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

const (
	testGcoreEdge = "wss://e2x.gcorecdn.com/apiws"
	testRuEdge    = "wss://ru.pluma.chat/apiws"
	testEdgeURLs  = "gcore=" + testGcoreEdge + ",ru=" + testRuEdge
)

// plainJSON is v as the plain Go value encoding/json gives of the JSON a
// client reads: the test compares what the clients parse, not the TL form.
func plainJSON(t *testing.T, v *mtproto.JSONValue) any {
	t.Helper()
	var out any
	switch v.GetPredicateName() {
	case mtproto.Predicate_jsonObject:
		m := map[string]any{}
		for _, e := range v.GetValue_VECTORJSONOBJECTVALUE() {
			m[e.GetKey()] = plainJSON(t, e.GetValue())
		}
		out = m
	case mtproto.Predicate_jsonArray:
		l := []any{}
		for _, e := range v.GetValue_VECTORJSONVALUE() {
			l = append(l, plainJSON(t, e))
		}
		out = l
	case mtproto.Predicate_jsonString:
		out = v.GetValue_STRING()
	case mtproto.Predicate_jsonNumber:
		out = v.GetValue_FLOAT64()
	default:
		t.Fatalf("pluma_edge has a %s", v.GetPredicateName())
	}
	return out
}

// edgeOf is pluma_edge of c as the clients read it.
func edgeOf(t *testing.T, c *AppConfig) map[string]any {
	t.Helper()
	m, ok := plainJSON(t, field(t, c, plumaEdgeKey)).(map[string]any)
	if !ok {
		t.Fatal("pluma_edge is not an object")
	}
	return m
}

// wantEdge is pluma_edge with ws, order and sticky_s, as the contract of
// the clients writes it.
func wantEdge(ws []string, order []string, sticky float64) map[string]any {
	w := []any{}
	for _, s := range ws {
		w = append(w, s)
	}
	return map[string]any{"v": float64(1), "ws": w, "order": []any{order[0], order[1]}, "sticky_s": sticky}
}

func mustParseEdges(t *testing.T, urls, routes, sticky string) *Edges {
	t.Helper()
	e, err := ParseEdges(urls, routes, sticky)
	if err != nil {
		t.Fatalf("ParseEdges(%q, %q, %q): %v", urls, routes, sticky, err)
	}
	return e
}

// No EDGE_WS_URLS: no edges, and the config is the one given, without
// pluma_edge, whatever the country.
func TestEdgesOff(t *testing.T) {
	e := mustParseEdges(t, "", "", "")
	if e != nil {
		t.Fatalf("edges %v without EDGE_WS_URLS", e)
	}
	base := mustParse(t, `{"a": 1}`)
	if got := e.Config(base, "RU"); got != base {
		t.Error("no edges changed the config")
	}
	if e.ByCountry() {
		t.Error("no edges look the country up")
	}
}

// Without EDGE_ROUTES every client gets every edge in the order of
// EDGE_WS_URLS, the direct address first, and the default sticky_s; the
// country is not looked up.
func TestEdgesDefaultRoute(t *testing.T) {
	e := mustParseEdges(t, " "+testEdgeURLs+" ", "", "")
	if e.ByCountry() {
		t.Error("a single record * looks the country up")
	}
	base := mustParse(t, `{"a": 1}`)
	got := e.Config(base, "")
	if k := keyList(got); k != "a,pluma_edge" {
		t.Errorf("keys %s", k)
	}
	want := wantEdge([]string{testGcoreEdge, testRuEdge}, []string{"direct", "edge"}, 1800)
	if m := edgeOf(t, got); !reflect.DeepEqual(m, want) {
		t.Errorf("pluma_edge %v, want %v", m, want)
	}
	if got.Hash == base.Hash || got.Hash == 0 {
		t.Errorf("hash %d, the file's %d: the clients would keep the config without the edges", got.Hash, base.Hash)
	}
	if again := e.Config(base, "RU"); again != got {
		t.Error("another country got another config with a single record *")
	}
	if field(t, base, "a") == nil || len(base.Config.GetValue_VECTORJSONOBJECTVALUE()) != 1 {
		t.Error("the file's config changed")
	}
}

// The value is the JSON of the contract (the brief of task 109, "Контракт
// клиентов", p. 6), byte for byte in what the hash covers.
func TestEdgesValueJSON(t *testing.T) {
	e := mustParseEdges(t, testEdgeURLs, "*:edge,direct:ru", "600")
	if got, want := e.routes[0].text, `{"v":1,"ws":["wss://ru.pluma.chat/apiws"],"order":["edge","direct"],"sticky_s":600}`; got != want {
		t.Errorf("pluma_edge %s, want %s", got, want)
	}
	var v struct {
		V       int      `json:"v"`
		WS      []string `json:"ws"`
		Order   []string `json:"order"`
		StickyS int      `json:"sticky_s"`
	}
	if err := json.Unmarshal([]byte(e.routes[0].text), &v); err != nil || v.V != 1 || v.StickyS != 600 {
		t.Errorf("pluma_edge %s: %v", e.routes[0].text, err)
	}
}

// Clients of the countries of a record get its edges and order, the others
// the record *, and an unknown country ("") too; each record has its own
// hash, so a client that moved to another country takes the other value.
func TestEdgesByCountry(t *testing.T) {
	e := mustParseEdges(t, testEdgeURLs, "RU,BY:edge,direct:ru,gcore; *:direct,edge:gcore,ru", "1200")
	if !e.ByCountry() {
		t.Error("records of countries do not look the country up")
	}
	base := mustParse(t, `{"a": 1, "pluma_edge": null, "b": 2}`)
	ru, by, de, unknown := e.Config(base, "RU"), e.Config(base, "BY"), e.Config(base, "DE"), e.Config(base, "")

	wantRu := wantEdge([]string{testRuEdge, testGcoreEdge}, []string{"edge", "direct"}, 1200)
	wantAll := wantEdge([]string{testGcoreEdge, testRuEdge}, []string{"direct", "edge"}, 1200)
	for name, c := range map[string]struct {
		got  *AppConfig
		want map[string]any
	}{"RU": {ru, wantRu}, "BY": {by, wantRu}, "DE": {de, wantAll}, "unknown": {unknown, wantAll}} {
		if m := edgeOf(t, c.got); !reflect.DeepEqual(m, c.want) {
			t.Errorf("%s: pluma_edge %v, want %v", name, m, c.want)
		}
		if k := keyList(c.got); k != "a,pluma_edge,b" {
			t.Errorf("%s: keys %s, want pluma_edge in the file's place", name, k)
		}
	}
	if ru.Hash == de.Hash || ru.Hash == base.Hash || de.Hash == base.Hash {
		t.Errorf("hashes: RU %d, DE %d, the file %d", ru.Hash, de.Hash, base.Hash)
	}
	if by != ru || unknown != de {
		t.Error("the configs of one record are made more than once")
	}
	// A client from Russia with the config of another country gets the one
	// of Russia; with its own, help.appConfigNotModified.
	if r := ru.Reply(de.Hash); r.GetPredicateName() != mtproto.Predicate_help_appConfig || r.GetHash() != ru.Hash {
		t.Errorf("a client that came to Russia gets %s with hash %d", r.GetPredicateName(), r.GetHash())
	}
	if r := ru.Reply(ru.Hash); r.GetPredicateName() != mtproto.Predicate_help_appConfigNotModified {
		t.Errorf("a client with the config of Russia gets %s", r.GetPredicateName())
	}
}

// A new base (Live read new login domains) makes new configs of the
// records, with new hashes; the domains stay in what the hash covers.
func TestEdgesNewBase(t *testing.T) {
	e := mustParseEdges(t, testEdgeURLs, "RU:edge,direct:ru;*:direct,edge:gcore", "")
	base := mustParse(t, `{"a": 1, "url_auth_domains": []}`)
	one, two := base.WithURLAuthDomains([]string{"one.example"}), base.WithURLAuthDomains([]string{"two.example"})
	ruOne, ruTwo := e.Config(one, "RU"), e.Config(two, "RU")
	if ruOne.Hash == ruTwo.Hash {
		t.Errorf("hash %d for both sets of login domains: the clients would keep the old domains", ruOne.Hash)
	}
	if d := stringsOf(field(t, ruTwo, "url_auth_domains")); len(d) != 1 || d[0] != "two.example" {
		t.Errorf("url_auth_domains %v", d)
	}
	if again := e.Config(one, "RU"); again.Hash != ruOne.Hash {
		t.Errorf("the same base gives hash %d, then %d", ruOne.Hash, again.Hash)
	}
}

// Configs are asked for at once by many requests.
func TestEdgesConcurrent(t *testing.T) {
	e := mustParseEdges(t, testEdgeURLs, "RU:edge,direct:ru;*:direct,edge:gcore", "")
	base := mustParse(t, `{"a": 1}`)
	var wg sync.WaitGroup
	hashes := make([]int32, 64)
	for i := range hashes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hashes[i] = e.Config(base, []string{"RU", "DE"}[i%2]).Hash
		}()
	}
	wg.Wait()
	for i, h := range hashes {
		if h != hashes[i%2] {
			t.Fatalf("request %d got hash %d, want %d", i, h, hashes[i%2])
		}
	}
}

// Wrong values stop the start: the clients would drop the whole value, or
// a typo would route a country nowhere.
func TestParseEdgesRefuses(t *testing.T) {
	for _, c := range []struct {
		name, urls, routes, sticky, want string
	}{
		{"routes without edges", "", "*:direct,edge:gcore", "", "without EDGE_WS_URLS"},
		{"no name", testGcoreEdge, "", "", "want <name>=<wss URL>"},
		{"upper-case name", "Gcore=" + testGcoreEdge, "", "", "the name"},
		{"long name", "abcdefghijklmnopq=" + testGcoreEdge, "", "", "the name"},
		{"name twice", "a=" + testGcoreEdge + ",a=" + testRuEdge, "", "", "twice"},
		{"address twice", "a=" + testGcoreEdge + ",b=" + testGcoreEdge, "", "", "the address of"},
		{"address twice, written another way", "a=" + testGcoreEdge + ",b=wss://E2X.gcorecdn.com:443/apiws", "", "", "the address of"},
		{"empty query", "a=wss://e2x.gcorecdn.com/apiws?", "", "", "a query"},
		{"escaped path", "a=wss://e2x.gcorecdn.com/api%77s", "", "", "the path"},
		{"https", "a=https://e2x.gcorecdn.com/apiws", "", "", "not a wss://"},
		{"ws", "a=ws://e2x.gcorecdn.com/apiws", "", "", "not a wss://"},
		{"IPv4", "a=wss://195.201.252.126/apiws", "", "", "not a DNS name"},
		{"IPv6", "a=wss://[2001:db8::1]/apiws", "", "", "not a DNS name"},
		{"bare name", "a=wss://localhost/apiws", "", "", "not a DNS name"},
		{"port", "a=wss://e2x.gcorecdn.com:8443/apiws", "", "", "port"},
		{"path", "a=wss://e2x.gcorecdn.com/", "", "", "the path"},
		{"query", "a=wss://e2x.gcorecdn.com/apiws?x=1", "", "", "a query"},
		{"user", "a=wss://u@e2x.gcorecdn.com/apiws", "", "", "a user"},
		{"five by default", "a=wss://a.example/apiws,b=wss://b.example/apiws,c=wss://c.example/apiws,d=wss://d.example/apiws,e=wss://e.example/apiws", "", "", "want 1 to 4"},
		{"no record *", testEdgeURLs, "RU:edge,direct:ru", "", "no record for *"},
		{"two records *", testEdgeURLs, "*:direct,edge:ru;*:direct,edge:gcore", "", "two records for *"},
		{"country twice", testEdgeURLs, "RU:edge,direct:ru;RU,BY:edge,direct:gcore;*:direct,edge:gcore", "", "country RU twice"},
		{"lower-case country", testEdgeURLs, "ru:edge,direct:ru;*:direct,edge:gcore", "", "not a country code"},
		{"three letters", testEdgeURLs, "RUS:edge,direct:ru;*:direct,edge:gcore", "", "not a country code"},
		{"two fields", testEdgeURLs, "*:gcore", "", "want <countries or *>:<order>:<names>"},
		{"one way", testEdgeURLs, "*:direct:gcore", "", "want direct,edge or edge,direct"},
		{"edge twice in order", testEdgeURLs, "*:edge,edge:gcore", "", "want direct,edge or edge,direct"},
		{"unknown edge", testEdgeURLs, "*:direct,edge:gcore,amf", "", `no edge "amf"`},
		{"no edge", testEdgeURLs, "*:direct,edge:", "", `no edge ""`},
		{"edge twice", testEdgeURLs, "*:direct,edge:gcore,gcore", "", "twice"},
		{"sticky short", testEdgeURLs, "", "59", "EDGE_STICKY_S"},
		{"sticky long", testEdgeURLs, "", "86401", "EDGE_STICKY_S"},
		{"sticky word", testEdgeURLs, "", "30m", "EDGE_STICKY_S"},
	} {
		t.Run(c.name, func(t *testing.T) {
			e, err := ParseEdges(c.urls, c.routes, c.sticky)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("ParseEdges(%q, %q, %q) = %v, %v; want an error with %q", c.urls, c.routes, c.sticky, e, err, c.want)
			}
		})
	}
}

// The bounds themselves are accepted: 4 edges in a record, sticky_s 60 and
// 86400, a port 443 written out.
func TestParseEdgesBounds(t *testing.T) {
	four := "a=wss://a.example/apiws,b=wss://b.example:443/apiws,c=wss://c.example/apiws,d=wss://d.example/apiws"
	for _, sticky := range []string{"60", "86400"} {
		e := mustParseEdges(t, four, "", sticky)
		ws := edgeOf(t, e.Config(mustParse(t, `{}`), ""))["ws"].([]any)
		if len(ws) != 4 {
			t.Errorf("%d edges, want 4", len(ws))
		}
		// The clients get each address in one form, without the port.
		if len(ws) > 1 && ws[1] != "wss://b.example/apiws" {
			t.Errorf("the edge written with :443 is handed out as %v", ws[1])
		}
	}
}
