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

package statsgraph

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teamgram/proto/mtproto"
)

var update = flag.Bool("update", false, "rewrite the golden graphs")

// testText stands for langpack.StatsText: the key and the language, so a
// golden graph shows which text went where.
func testText(key, lang string) string { return key + " " + lang }

// day0 is 2026-10-01 00:00 UTC.
const day0 = int64(1790812800)

func seq(n int, f func(i int) int64) []int64 {
	v := make([]int64, n)
	for i := range v {
		v[i] = f(i)
	}
	return v
}

func days(n int) []int64 { return Span(Day, day0, day0+int64(n-1)*86400).X() }

type goldenCase struct {
	name string
	lang string
	dark bool
	g    Graph
	// step is the step iOS finds in the JSON of a post's graph
	// (MessageStatistics.swift:104-116), seconds.
	step int64
}

// goldenCases are graphs of every kind the statistics give, each written to
// testdata/<name>.json and read by the parsers of the clients (compat_test).
func goldenCases() []goldenCase {
	langs := make([]Series, 12)
	for i := range langs {
		langs[i] = Series{NameKey: "PlumaStatsLang_l" + string(rune('a'+i)), Values: seq(5, func(d int) int64 { return int64(12-i) * int64(d+1) })}
	}
	return []goldenCase{
		{name: "growth", lang: "en", step: 86400, g: Graph{
			Type: Line, Scale: Day, X: days(40), Subchart: true, ZoomToken: "v1.zoom.token",
			Series: []Series{{NameKey: "PlumaStatsSubscribers", Values: seq(40, func(i int) int64 { return 100 + int64(i) })}},
		}},
		{name: "followers", lang: "ru", step: 86400, g: Graph{
			Type: Line, Scale: Day, X: days(3), Subchart: true,
			Series: []Series{
				{NameKey: "PlumaStatsJoined", Color: Green, Values: []int64{3, 0, 7}},
				{NameKey: "PlumaStatsLeft", Color: Red, Values: []int64{1, 2, 0}},
			},
		}},
		{name: "followers_dark", lang: "ru", dark: true, step: 86400, g: Graph{
			Type: Line, Scale: Day, X: days(3), Subchart: true,
			Series: []Series{
				{NameKey: "PlumaStatsJoined", Color: Green, Values: []int64{3, 0, 7}},
				{NameKey: "PlumaStatsLeft", Color: Red, Values: []int64{1, 2, 0}},
			},
		}},
		// Posts nobody shared: the column of shares is all zeros and goes.
		{name: "interactions", lang: "en", step: 86400, g: Graph{
			Type: Step, Scale: Day, X: days(4), YScaled: true, Subchart: true, ZoomToken: "z",
			Series: []Series{
				{NameKey: "PlumaStatsViews", Values: []int64{10, 20, 5, 0}},
				{NameKey: "PlumaStatsShares", Color: LightBlue, Values: []int64{0, 0, 0, 0}},
			},
		}},
		{name: "views_by_source", lang: "en", step: 86400, g: Graph{
			Type: Bar, Scale: Day, X: days(3), Stacked: true, Subchart: true,
			Series: []Series{
				{NameKey: "PlumaStatsSourceSubscribers", Values: []int64{5, 6, 7}},
				{NameKey: "PlumaStatsSourceChannels", Values: []int64{0, 1, 0}},
				{NameKey: "PlumaStatsSourceGroups", Values: []int64{0, 0, 0}},
				{NameKey: "PlumaStatsSourcePrivate", Values: []int64{1, 0, 2}},
				{NameKey: "PlumaStatsSourceOther", Values: []int64{2, 2, 2}},
			},
		}},
		{name: "languages", lang: "de", step: 86400, g: Graph{
			Type: Area, Scale: Day, X: days(5), Stacked: true, Percentage: true, Subchart: true,
			Series: Collapse(langs, 9, Series{NameKey: "PlumaStatsOther"}),
		}},
		{name: "top_hours", lang: "en", g: Graph{
			Type: Step, Scale: HourOfDay, X: Span(HourOfDay, 0, 0).X(),
			Series: []Series{
				{NameKey: "PlumaStatsThisPeriod", Values: seq(24, func(h int) int64 { return int64(h * h % 17) })},
				{NameKey: "PlumaStatsPreviousPeriod", Color: LightBlue, Values: seq(24, func(h int) int64 { return int64(h % 5) })},
			},
		}},
		{name: "weekdays", lang: "en", step: 7 * 86400, g: Graph{
			Type: Area, Scale: Week, X: Span(Week, day0, day0+20*86400).X(), Stacked: true, Percentage: true, Subchart: true,
			Series: func() []Series {
				s := make([]Series, 7)
				for d := range s {
					s[d] = Series{NameKey: "PlumaStatsWeekday" + string(rune('1'+d)), Values: seq(4, func(w int) int64 { return int64(d + w + 1) })}
				}
				return s
			}(),
		}},
		{name: "message_hourly", lang: "en", step: 3600, g: Graph{
			Type: Step, Scale: Hour, X: Span(Hour, day0+30*60, day0+5*3600).X(), YScaled: true, ZoomToken: "",
			Series: []Series{
				{NameKey: "PlumaStatsViews", Values: []int64{4, 9, 3, 1, 0, 2}},
				{NameKey: "PlumaStatsShares", Color: LightBlue, Values: []int64{0, 1, 0, 0, 0, 1}},
			},
		}},
		// The names of a poll's options are content and may hold anything,
		// the substring iOS looks for among them.
		{name: "poll", lang: "en", step: 300, g: Graph{
			Type: Bar, Scale: FiveMin, X: Span(FiveMin, day0, day0+15*60).X(), Stacked: true,
			Series: []Series{
				{Name: `Yes ["x",1],`, Values: []int64{1, 0, 2, 1}},
				{Name: "No — <never>", Values: []int64{0, 3, 0, 0}},
				{Name: "Maybe \"later\"", Values: []int64{0, 0, 0, 5}},
			},
		}},
	}
}

func build(t *testing.T, c goldenCase) []byte {
	t.Helper()
	g := c.g
	out, err := g.Build(testText, c.lang, c.dark)
	if err != nil {
		t.Fatal(err)
	}
	if out.GetPredicateName() != mtproto.Predicate_statsGraph {
		t.Fatalf("%s: %s %q", c.name, out.GetPredicateName(), out.GetError())
	}
	if g.ZoomToken != "" && out.GetZoomToken().GetValue() != g.ZoomToken {
		t.Fatalf("%s: zoom token %v", c.name, out.GetZoomToken())
	}
	if g.ZoomToken == "" && out.GetZoomToken() != nil {
		t.Fatalf("%s: zoom token %v", c.name, out.GetZoomToken())
	}
	return []byte(out.GetJson().GetData())
}

func TestGolden(t *testing.T) {
	for _, c := range goldenCases() {
		t.Run(c.name, func(t *testing.T) {
			got := build(t, c)
			file := filepath.Join("testdata", c.name+".json")
			if *update {
				if err := os.WriteFile(file, append(got, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, bytes.TrimSpace(want)) {
				t.Fatalf("got\n%s\nwant\n%s", got, want)
			}
		})
	}
}

// The shape every graph has, whatever its kind: compact, columns first, x
// the first column and not the last, columns of one length.
func TestShape(t *testing.T) {
	for _, c := range goldenCases() {
		data := build(t, c)
		var compact bytes.Buffer
		if err := json.Compact(&compact, data); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(compact.Bytes(), data) {
			t.Errorf("%s: not compact", c.name)
		}
		if !bytes.HasPrefix(data, []byte(`{"columns":[["x",`)) {
			t.Errorf("%s: does not start with the x column: %.40s", c.name, data)
		}
		var g struct {
			Columns [][]any `json:"columns"`
		}
		if err := json.Unmarshal(data, &g); err != nil {
			t.Fatal(err)
		}
		if len(g.Columns) < 2 {
			t.Errorf("%s: %d columns", c.name, len(g.Columns))
		}
		for _, col := range g.Columns {
			if len(col) != len(g.Columns[0]) {
				t.Errorf("%s: column %v of %d, x of %d", c.name, col[0], len(col), len(g.Columns[0]))
			}
		}
	}
}

func TestTooLittleToDraw(t *testing.T) {
	one := Graph{Type: Line, Scale: Day, X: days(1), Series: []Series{{NameKey: "k", Values: []int64{5}}}}
	none := Graph{Type: Line, Scale: Day, X: days(3)}
	silent := Graph{Type: Area, Scale: Day, X: days(3), Stacked: true, Percentage: true,
		Series: []Series{{NameKey: "a", Values: []int64{0, 0, 0}}, {NameKey: "b", Values: []int64{0, 0, 0}}}}
	empty := Graph{Type: Line, Scale: Day, Series: []Series{{NameKey: "k"}}}
	for name, g := range map[string]Graph{"one point": one, "no series": none, "percentage of nothing": silent, "no points": empty} {
		out, err := g.Build(testText, "ru", false)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if out.GetPredicateName() != mtproto.Predicate_statsGraphError || out.GetError() != NotEnoughDataKey+" ru" {
			t.Errorf("%s: %s %q", name, out.GetPredicateName(), out.GetError())
		}
		g.Empty = "Data since 1 Dec"
		out, err = g.Build(testText, "ru", false)
		if err != nil || out.GetError() != "Data since 1 Dec" {
			t.Errorf("%s: %v %q", name, err, out.GetError())
		}
	}

	e := Error("No votes yet")
	if e.GetPredicateName() != mtproto.Predicate_statsGraphError || e.GetError() != "No votes yet" {
		t.Errorf("Error: %s %q", e.GetPredicateName(), e.GetError())
	}
}

func TestBadGraphs(t *testing.T) {
	ok := func() Graph {
		return Graph{Type: Line, Scale: Day, X: days(3), Series: []Series{{NameKey: "k", Values: []int64{1, 2, 3}}}}
	}
	if _, err := (&Graph{Type: Line, Scale: Day, X: days(3), Series: []Series{{NameKey: "k", Values: []int64{1, 2, 3}}}}).Build(testText, "en", false); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(g *Graph){
		"no type":             func(g *Graph) { g.Type = "" },
		"pie type":            func(g *Graph) { g.Type = "pie" },
		"no scale":            func(g *Graph) { g.Scale = 0 },
		"short series":        func(g *Graph) { g.Series[0].Values = []int64{1, 2} },
		"long series":         func(g *Graph) { g.Series[0].Values = []int64{1, 2, 3, 4} },
		"x not increasing":    func(g *Graph) { g.X[2] = g.X[1] },
		"no name":             func(g *Graph) { g.Series[0].NameKey = "" },
		"two names":           func(g *Graph) { g.Series[0].Name = "n" },
		"empty text":          func(g *Graph) { g.Series[0].NameKey = "empty" },
		"bad color":           func(g *Graph) { g.Series[0].Color = Indigo + 1 },
		"negative color":      func(g *Graph) { g.Series[0].Color = -1 },
		"y_scaled bars":       func(g *Graph) { g.Type, g.YScaled = Bar, true },
		"y_scaled stacked":    func(g *Graph) { g.YScaled, g.Stacked = true, true },
		"y_scaled one series": func(g *Graph) { g.Type, g.YScaled = Step, true },
		"percentage of lines": func(g *Graph) { g.Percentage, g.Stacked = true, true },
		"percentage alone":    func(g *Graph) { g.Type, g.Percentage = Area, true },
		"stacked lines":       func(g *Graph) { g.Stacked = true },
		"hours with subchart": func(g *Graph) {
			g.Scale, g.X, g.Subchart = HourOfDay, []int64{0, 1, 2}, true
		},
		"hour 24": func(g *Graph) { g.Scale, g.X = HourOfDay, []int64{0, 1, 24} },
		"too many series": func(g *Graph) {
			for len(g.Series) <= MaxSeries {
				g.Series = append(g.Series, Series{NameKey: "k", Values: []int64{1, 2, 3}})
			}
		},
	}
	text := func(key, lang string) string {
		if key == "empty" {
			return ""
		}
		return testText(key, lang)
	}
	for name, f := range cases {
		g := ok()
		f(&g)
		if _, err := g.Build(text, "en", false); !errors.Is(err, ErrBadGraph) {
			t.Errorf("%s: %v", name, err)
		}
	}

	g := ok()
	for len(g.Series) < MaxSeries {
		g.Series = append(g.Series, Series{NameKey: "k", Values: []int64{1, 2, 3}})
	}
	if _, err := g.Build(testText, "en", false); err != nil {
		t.Errorf("%d series: %v", MaxSeries, err)
	}
}

// The column of zeros goes only from the graphs of two axes, and only when
// the other column has values.
func TestZeroSeries(t *testing.T) {
	cols := func(g Graph) int {
		t.Helper()
		out, err := g.Build(testText, "en", false)
		if err != nil {
			t.Fatal(err)
		}
		var j struct {
			Columns [][]any `json:"columns"`
		}
		if err := json.Unmarshal([]byte(out.GetJson().GetData()), &j); err != nil {
			t.Fatal(err)
		}
		return len(j.Columns) - 1
	}
	series := func(a, b []int64) []Series {
		return []Series{{NameKey: "a", Values: a}, {NameKey: "b", Values: b}}
	}
	if n := cols(Graph{Type: Step, Scale: Day, X: days(3), YScaled: true, Series: series([]int64{1, 0, 2}, []int64{0, 0, 0})}); n != 1 {
		t.Errorf("y_scaled with a zero column: %d columns", n)
	}
	if n := cols(Graph{Type: Step, Scale: Day, X: days(3), YScaled: true, Series: series([]int64{0, 0, 0}, []int64{0, 0, 0})}); n != 2 {
		t.Errorf("y_scaled of zeros: %d columns", n)
	}
	if n := cols(Graph{Type: Step, Scale: Day, X: days(3), YScaled: true, Series: series([]int64{1, 0, 0}, []int64{0, 0, 3})}); n != 2 {
		t.Errorf("y_scaled of values: %d columns", n)
	}
	if n := cols(Graph{Type: Bar, Scale: Day, X: days(3), Stacked: true, Series: series([]int64{1, 0, 2}, []int64{0, 0, 0})}); n != 2 {
		t.Errorf("bars with a zero column: %d columns", n)
	}
}

func TestColors(t *testing.T) {
	if got := colorValue(Auto, 0, false); got != "BLUE#327FE5" {
		t.Errorf("first: %s", got)
	}
	if got := colorValue(Auto, 0, true); got != "BLUE#529FFF" {
		t.Errorf("first dark: %s", got)
	}
	if got := colorValue(Red, 0, false); got != "RED#E05356" {
		t.Errorf("red: %s", got)
	}
	if got := colorValue(Indigo, 3, true); got != "INDIGO#875CE5" {
		t.Errorf("indigo dark: %s", got)
	}
	if got := colorValue(Auto, 8, false); got != "#9F79E8" {
		t.Errorf("ninth: %s", got)
	}
	if got := colorValue(Auto, 8, true); got != "#9F79E8" {
		t.Errorf("ninth dark: %s", got)
	}
	if got := colorValue(Auto, paletteSize, false); got != colorValue(Auto, 0, false) {
		t.Errorf("past the palette: %s", got)
	}
	seen := map[string]bool{}
	for i := 0; i < paletteSize; i++ {
		v := colorValue(Auto, i, false)
		if seen[v] {
			t.Errorf("%s twice", v)
		}
		seen[v] = true
		key, hex, ok := strings.Cut(v, "#")
		if !ok || len(hex) != 6 || strings.Trim(hex, "0123456789ABCDEF") != "" {
			t.Errorf("%d: %s", i, v)
		}
		if (i < namedColors) != (key != "") {
			t.Errorf("%d: key %q", i, key)
		}
	}
}

func TestFormatters(t *testing.T) {
	want := map[Scale][2]string{
		Day:       {"statsFormat('day')", "statsTooltipFormat('day')"},
		Week:      {"statsFormat('week')", "statsTooltipFormat('week')"},
		Hour:      {"statsFormat('hour')", "statsTooltipFormat('hour')"},
		FiveMin:   {"statsFormat('5min')", "statsTooltipFormat('5min')"},
		HourOfDay: {"statsFormatDayHour", "statsFormatDayHourFull"},
	}
	for s, w := range want {
		tick, tip := xFormatters(s)
		if tick != w[0] || tip != w[1] {
			t.Errorf("%v: %s %s", s, tick, tip)
		}
	}
}

func TestDefaultZoom(t *testing.T) {
	x := days(40)
	if got := defaultZoomStart(x); got != x[10] {
		t.Errorf("40 days: %d, want %d", got, x[10])
	}
	x = days(30)
	if got := defaultZoomStart(x); got != x[0] {
		t.Errorf("30 days: %d, want %d", got, x[0])
	}
	x = days(31)
	if got := defaultZoomStart(x); got != x[1] {
		t.Errorf("31 days: %d, want %d", got, x[1])
	}
	x = Span(Hour, day0, day0+7*86400).X()
	if got := defaultZoomStart(x); got != x[0] {
		t.Errorf("hours: %d", got)
	}
}
