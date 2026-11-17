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

// The parsers of the graphs' JSON in the four official clients, step by
// step as their sources do it (~/pluma-work/reference-src-new, the releases
// pinned in the clients' upstream.env), so that a graph that would be
// hidden, or take a client's screen down, fails here.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

type rawGraph struct {
	root    map[string]any
	columns [][]any
}

func parseRaw(data []byte) (*rawGraph, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var root map[string]any
	if err := d.Decode(&root); err != nil {
		return nil, err
	}
	g := &rawGraph{root: root}
	cols, ok := root["columns"].([]any)
	if !ok {
		return nil, fmt.Errorf("no columns")
	}
	for _, c := range cols {
		col, ok := c.([]any)
		if !ok {
			return nil, fmt.Errorf("column %v is not an array", c)
		}
		g.columns = append(g.columns, col)
	}
	return g, nil
}

func (g *rawGraph) object(key string) (map[string]any, bool) {
	m, ok := g.root[key].(map[string]any)
	return m, ok
}

func (g *rawGraph) str(key string) (string, bool) {
	s, ok := g.root[key].(string)
	return s, ok
}

func longs(col []any) ([]int64, error) {
	v := make([]int64, 0, len(col))
	for _, e := range col {
		n, ok := e.(json.Number)
		if !ok {
			return nil, fmt.Errorf("value %v is not a number", e)
		}
		i, err := n.Int64()
		if err != nil {
			return nil, err
		}
		v = append(v, i)
	}
	return v, nil
}

// javaColorRe is Android's and tdesktop's pattern of a color: a key, then
// the hex from the last '#' on (ChartData.java:90,
// statistics_data_deserialize.cpp:166; Matcher.matches is anchored, the
// first group greedy).
var javaColorRe = regexp.MustCompile(`^(.*)(#.*)$`)

// androidParseColor is Color.parseColor: "#RRGGBB" or "#AARRGGBB".
func androidParseColor(s string) bool {
	if len(s) != 7 && len(s) != 9 {
		return false
	}
	_, err := strconv.ParseUint(s[1:], 16, 32)
	return err == nil
}

// androidColorKeys are the statisticChartLine_ keys of ThemeColors.java
// (:1708-1717).
var androidColorKeys = map[string]bool{
	"blue": true, "green": true, "red": true, "golden": true, "lightblue": true,
	"lightgreen": true, "orange": true, "indigo": true, "purple": true, "cyan": true,
}

type androidKind int

const (
	androidLinear androidKind = iota
	androidDouble             // y_scaled fields, DoubleLinearChartData
	androidStack              // percentage fields, StackLinearChartData
)

// android is ChartData's constructor (ChartData.java:47-108) and that of
// the subclass the field's kind takes (StatisticActivity.java:293-304,
// createChartData), then the empty check of createViewData (:1015-1016).
func android(data []byte, kind androidKind, languages bool) error {
	g, err := parseRaw(data)
	if err != nil {
		return err
	}
	type line struct {
		id  string
		y   []int64
		max int64
	}
	var x []int64
	var lines []line
	for _, col := range g.columns {
		id, ok := col[0].(string)
		if !ok {
			return fmt.Errorf("JSONException: column id %v", col[0])
		}
		values, err := longs(col[1:])
		if err != nil {
			return fmt.Errorf("JSONException: %v", err)
		}
		if id == "x" {
			x = values
		} else {
			l := line{id: id, y: values}
			for _, v := range values {
				if v > l.max {
					l.max = v
				}
			}
			lines = append(lines, l)
		}
		// `if (x.length > 1)` runs after every column (:72).
		if x == nil {
			return fmt.Errorf("NullPointerException: x.length before the x column")
		}
	}
	colors, hasColors := g.object("colors")
	names, hasNames := g.object("names")
	for _, l := range lines {
		if hasColors {
			c, ok := colors[l.id].(string)
			if !ok {
				return fmt.Errorf("JSONException: no color of %s", l.id)
			}
			if m := javaColorRe.FindStringSubmatch(c); m != nil {
				if m[1] != "" && !androidColorKeys[strings.ToLower(m[1])] {
					return fmt.Errorf("color key %q of %s unknown", m[1], l.id)
				}
				if !androidParseColor(m[2]) {
					return fmt.Errorf("IllegalArgumentException: color %q of %s", m[2], l.id)
				}
			}
		}
		if hasNames {
			if _, ok := names[l.id].(string); !ok {
				return fmt.Errorf("JSONException: no name of %s", l.id)
			}
		}
	}

	switch kind {
	case androidDouble:
		// DoubleLinearChartData.measure (:25-36): max / m in long.
		var max int64
		for _, l := range lines {
			if l.max > max {
				max = l.max
			}
		}
		for _, l := range lines {
			if max != l.max && l.max == 0 {
				return fmt.Errorf("ArithmeticException: / by zero, column %s", l.id)
			}
		}
	case androidStack:
		if languages {
			// The languages of under 1% and mostly empty go (:21-45).
			total := int64(0)
			totals := make([]int64, len(lines))
			empty := make([]int, len(lines))
			for k, l := range lines {
				for _, v := range l.y {
					totals[k] += v
					if v == 0 {
						empty[k]++
					}
				}
				total += totals[k]
			}
			kept := lines[:0:0]
			for k, l := range lines {
				share := float64(totals[k]) / float64(total)
				if !(share < 0.01 && float64(empty[k]) > float64(len(x))/2) {
					kept = append(kept, l)
				}
			}
			lines = kept
		}
		// `lines.get(0)` (:49).
		if len(lines) == 0 {
			return fmt.Errorf("IndexOutOfBoundsException: no lines")
		}
	}
	if len(x) < MinPoints {
		return fmt.Errorf("empty: %d points", len(x))
	}
	return nil
}

// iosColorNames are the names GColor(hexString:) knows
// (UIColor+Utils.swift:32-64).
var iosColorNames = map[string]bool{
	"red": true, "green": true, "blue": true, "golden": true, "yellow": true, "lightgreen": true,
	"lightblue": true, "seablue": true, "orange": true, "violet": true, "emerald": true, "pink": true, "indigo": true,
}

func iosColor(s string) bool {
	parts := strings.Split(s, "#")
	if parts[0] != "" && iosColorNames[strings.ToLower(parts[0])] {
		return true
	}
	hex := parts[len(parts)-1]
	if len(hex) != 6 && len(hex) != 8 {
		return false
	}
	_, err := strconv.ParseUint(hex, 16, 64)
	return err == nil
}

// ios is ChartsCollection(from:) (ChartsCollection.swift:38-99).
func ios(data []byte) error {
	g, err := parseRaw(data)
	if err != nil {
		return err
	}
	var types, names, colors map[string]string
	for key, dst := range map[string]*map[string]string{"types": &types, "names": &names, "colors": &colors} {
		m, ok := g.object(key)
		if !ok {
			return fmt.Errorf("unable to get %s", key)
		}
		*dst = map[string]string{}
		for k, v := range m {
			s, ok := v.(string)
			if !ok {
				return fmt.Errorf("unable to get %s: %v is not a string", key, v)
			}
			(*dst)[k] = s
		}
	}
	var axis int
	var charts []int
	for _, col := range g.columns {
		id, ok := col[0].(string)
		if !ok {
			return fmt.Errorf("unable to get column name")
		}
		switch types[id] {
		case "x":
			axis = len(col) - 1
		case "line", "area", "bar", "step":
			if !iosColor(colors[id]) {
				return fmt.Errorf("unable to get color of %s: %q", id, colors[id])
			}
			if _, ok := names[id]; !ok {
				return fmt.Errorf("unable to get name of %s", id)
			}
			charts = append(charts, len(col)-1)
		default:
			return fmt.Errorf("unable to get column type of %s: %q", id, types[id])
		}
	}
	if axis == 0 || len(charts) == 0 {
		return fmt.Errorf("invalid number of items: %d, %v", axis, charts)
	}
	for _, n := range charts {
		if n != axis {
			return fmt.Errorf("invalid number of items: %d, %v", axis, charts)
		}
	}
	return nil
}

// iosStep is the step of a post's graph iOS finds in the raw JSON
// (MessageStatistics.swift:104-116), seconds; 86400 when it finds none.
func iosStep(data string) int64 {
	start := strings.Index(data, `["x",`)
	if start < 0 {
		return 86400
	}
	rest := data[start+len(`["x",`):]
	end := strings.Index(rest, "],")
	if end < 0 {
		return 86400
	}
	var values []int64
	for _, s := range strings.Split(rest[:end], ",") {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			values = append(values, v)
		}
	}
	if len(values) < 2 {
		return 86400
	}
	d := values[1] - values[0]
	if d < 0 {
		d = -d
	}
	return d / 1000
}

// tdesktopColorKeys are the keys FillLineColorsByKey paints with the theme
// (chart_widget.cpp:41-65).
var tdesktopColorKeys = map[string]bool{
	"BLUE": true, "GREEN": true, "RED": true, "GOLDEN": true, "LIGHTBLUE": true,
	"LIGHTGREEN": true, "ORANGE": true, "INDIGO": true, "PURPLE": true, "CYAN": true,
}

// tdesktop is StatisticalChartFromJSON (statistics_data_deserialize.cpp:
// 22-176): weekFormat is what it reads from xTooltipFormatter.
func tdesktop(data []byte) (weekFormat bool, err error) {
	g, err := parseRaw(data)
	if err != nil {
		return false, err
	}
	if len(g.columns) == 0 {
		return false, fmt.Errorf("empty columns")
	}
	var x []float64
	var lines [][]any
	ids := []string{}
	for _, col := range g.columns {
		if len(col) == 0 {
			return false, fmt.Errorf("empty column")
		}
		id, _ := col[0].(string)
		if id == "x" {
			for _, v := range col[1:] {
				f, err := v.(json.Number).Float64()
				if err != nil {
					return false, err
				}
				x = append(x, f)
			}
		} else {
			lines = append(lines, col[1:])
			ids = append(ids, id)
		}
	}
	if len(x) == 0 {
		return false, fmt.Errorf("mismatched columns: no x")
	}
	for _, l := range lines {
		if len(l) != len(x) {
			return false, fmt.Errorf("mismatched columns")
		}
	}
	sub, ok := g.object("subchart")
	if !ok {
		return false, fmt.Errorf("no subchart: the footer is shown")
	}
	show, ok := sub["show"].(bool)
	if !ok {
		return false, fmt.Errorf("subchart.show is not a bool")
	}
	if zoom, ok := sub["defaultZoom"].([]any); ok {
		if !show {
			return false, fmt.Errorf("defaultZoom of a hidden subchart")
		}
		if len(zoom) != 2 {
			return false, fmt.Errorf("defaultZoom %v", zoom)
		}
		for _, z := range zoom {
			f, err := z.(json.Number).Float64()
			if err != nil {
				return false, err
			}
			found := false
			for _, v := range x {
				found = found || v == f
			}
			if !found {
				return false, fmt.Errorf("defaultZoom %v is not an x", f)
			}
		}
	} else if show {
		return false, fmt.Errorf("a subchart without defaultZoom")
	}
	tip, _ := g.str("xTooltipFormatter")
	colors, _ := g.object("colors")
	for _, id := range ids {
		c, _ := colors[id].(string)
		m := javaColorRe.FindStringSubmatch(c)
		if m == nil {
			return false, fmt.Errorf("color of %s %q", id, c)
		}
		if m[1] != "" && !tdesktopColorKeys[m[1]] {
			return false, fmt.Errorf("color key %q of %s is not one tdesktop paints", m[1], id)
		}
	}
	return strings.Contains(tip, "'week'"), nil
}

// webkFormatters are the strings of Web K's table (lib/tchart/utils.ts:
// 186-199).
var webkFormatters = map[string]bool{
	"statsFormatDayHourFull": true, "statsFormatDayHour": true,
	"statsFormat('week')": true, "statsFormat('day')": true, "statsFormat('hour')": true, "statsFormat('5min')": true,
	"statsFormatTooltipValue":    true,
	"statsTooltipFormat('week')": true, "statsTooltipFormat('day')": true, "statsTooltipFormat('hour')": true, "statsTooltipFormat('5min')": true,
	"null": true,
}

// webk is JSON.parse and the chart's setup (statistics.tsx:363-390,
// lib/tchart/chart.ts:140-225, utils.ts:184-213).
func webk(data []byte) error {
	g, err := parseRaw(data)
	if err != nil {
		return err
	}
	for _, f := range []string{"xTickFormatter", "xTooltipFormatter", "xRangeFormatter", "yTickFormatter", "yTooltipFormatter"} {
		s, _ := g.str(f)
		if !webkFormatters[s] {
			return fmt.Errorf("%s %q prints raw numbers", f, s)
		}
	}
	colors, _ := g.object("colors")
	types, _ := g.object("types")
	for _, col := range g.columns {
		id, _ := col[0].(string)
		switch types[id] {
		case "x", "line", "step", "bar", "area":
		default:
			return fmt.Errorf("type %v of %s", types[id], id)
		}
		if id == "x" {
			continue
		}
		c, _ := colors[id].(string)
		if !strings.Contains(c, "#") {
			return fmt.Errorf("color %q of %s has no hex", c, id)
		}
	}
	sub, ok := g.object("subchart")
	if !ok {
		return fmt.Errorf("no subchart: it is shown")
	}
	if _, ok := sub["show"].(bool); !ok {
		return fmt.Errorf("subchart.show missing: it is shown")
	}
	return nil
}

func androidKindOf(g Graph) androidKind {
	switch {
	case g.YScaled:
		return androidDouble
	case g.Percentage:
		return androidStack
	}
	return androidLinear
}

// allClients runs the four parsers over data of graph g.
func allClients(t *testing.T, name string, g Graph, data []byte) {
	t.Helper()
	if err := android(data, androidKindOf(g), g.Percentage); err != nil {
		t.Errorf("%s: Android: %v\n%s", name, err, data)
	}
	if err := ios(data); err != nil {
		t.Errorf("%s: iOS: %v\n%s", name, err, data)
	}
	week, err := tdesktop(data)
	if err != nil {
		t.Errorf("%s: tdesktop: %v\n%s", name, err, data)
	}
	if week != (g.Scale == Week) {
		t.Errorf("%s: tdesktop week format %v for %v", name, week, g.Scale)
	}
	if err := webk(data); err != nil {
		t.Errorf("%s: Web K: %v\n%s", name, err, data)
	}
}

func TestClientsReadGolden(t *testing.T) {
	for _, c := range goldenCases() {
		data := build(t, c)
		allClients(t, c.name, c.g, data)
		if c.step != 0 {
			if got := iosStep(string(data)); got != c.step {
				t.Errorf("%s: iOS step %d, want %d", c.name, got, c.step)
			}
		}
	}
}

// Graphs of every type and scale with random values, many of them zeros:
// whatever the data, what Build gives every client reads.
func TestClientsReadRandom(t *testing.T) {
	r := rand.New(rand.NewSource(103))
	kinds := []Graph{
		{Type: Line, Scale: Day, Subchart: true},
		{Type: Step, Scale: Day, YScaled: true, Subchart: true},
		{Type: Line, Scale: Day, YScaled: true, Subchart: true},
		{Type: Step, Scale: Hour, YScaled: true},
		{Type: Bar, Scale: Day, Stacked: true, Subchart: true},
		{Type: Bar, Scale: FiveMin, Stacked: true},
		{Type: Bar, Scale: Hour, Stacked: true},
		{Type: Area, Scale: Day, Stacked: true, Percentage: true, Subchart: true},
		{Type: Area, Scale: Week, Stacked: true, Percentage: true, Subchart: true},
		{Type: Step, Scale: HourOfDay},
		{Type: Line, Scale: HourOfDay},
	}
	for n := 0; n < 3000; n++ {
		g := kinds[r.Intn(len(kinds))]
		points := 1 + r.Intn(60)
		if g.Scale == HourOfDay {
			points = 24
		}
		g.X = Span(g.Scale, day0, day0+int64(points-1)*g.Scale.Seconds()).X()
		series := 1 + r.Intn(3)
		if g.YScaled {
			series = 2
		} else if g.Percentage || g.Stacked {
			series = 1 + r.Intn(MaxSeries)
		}
		for i := 0; i < series; i++ {
			s := Series{NameKey: fmt.Sprintf("k%d", i), Values: make([]int64, len(g.X))}
			density := r.Intn(4) // 0: a column of zeros
			for j := range s.Values {
				if density > 0 && r.Intn(4) < density {
					s.Values[j] = r.Int63n(1_000_000)
				}
			}
			g.Series = append(g.Series, s)
		}
		out, err := g.Build(testText, "en", r.Intn(2) == 0)
		if err != nil {
			t.Fatalf("%d: %v", n, err)
		}
		if out.GetJson() == nil {
			continue // a statsGraphError, hidden by every client
		}
		allClients(t, fmt.Sprintf("random %d", n), g, []byte(out.GetJson().GetData()))
		if t.Failed() {
			return
		}
	}
}

// The parsers above catch what they stand for: each broken JSON fails the
// client that would fail on it.
func TestClientParsersCatch(t *testing.T) {
	const good = `{"columns":[["x",1700000000000,1700086400000],["y0",1,2],["y1",3,4]],"types":{"x":"x","y0":"step","y1":"step"},"names":{"y0":"a","y1":"b"},"colors":{"y0":"BLUE#327FE5","y1":"#9F79E8"},"subchart":{"show":true,"defaultZoom":[1700000000000,1700086400000]},"y_scaled":true,"xTickFormatter":"statsFormat('day')","xTooltipFormatter":"statsTooltipFormat('day')","xRangeFormatter":"null","yTickFormatter":"null","yTooltipFormatter":"statsFormatTooltipValue"}`
	if err := android([]byte(good), androidDouble, false); err != nil {
		t.Fatalf("Android: %v", err)
	}
	if err := ios([]byte(good)); err != nil {
		t.Fatalf("iOS: %v", err)
	}
	if _, err := tdesktop([]byte(good)); err != nil {
		t.Fatalf("tdesktop: %v", err)
	}
	if err := webk([]byte(good)); err != nil {
		t.Fatalf("Web K: %v", err)
	}

	edit := func(old, new string) []byte {
		if !strings.Contains(good, old) {
			t.Fatalf("%q not in the sample", old)
		}
		return []byte(strings.Replace(good, old, new, 1))
	}
	xLast := edit(`["x",1700000000000,1700086400000],["y0",1,2],["y1",3,4]`, `["y0",1,2],["y1",3,4],["x",1700000000000,1700086400000]`)
	zeroColumn := edit(`["y1",3,4]`, `["y1",0,0]`)
	noColor := edit(`,"y1":"#9F79E8"`, ``)
	noName := edit(`,"y1":"b"`, ``)
	short := edit(`["y1",3,4]`, `["y1",3]`)
	onePoint := edit(`["x",1700000000000,1700086400000],["y0",1,2],["y1",3,4]`, `["x",1700000000000],["y0",1],["y1",3]`)
	badFormatter := edit(`"statsFormat('day')"`, `"statsFormat('days')"`)
	noSubchart := edit(`"subchart":{"show":true,"defaultZoom":[1700000000000,1700086400000]},`, ``)
	zoomOff := edit(`"defaultZoom":[1700000000000,1700086400000]`, `"defaultZoom":[1700000000001,1700086400000]`)
	lowerKey := edit(`BLUE#327FE5`, `blue#327FE5`)
	badHex := edit(`BLUE#327FE5`, `BLUE#327FE`)
	noTypes := edit(`"types":{"x":"x","y0":"step","y1":"step"},`, ``)

	if err := android(xLast, androidLinear, false); err == nil {
		t.Error("Android read x last")
	}
	if err := android(zeroColumn, androidDouble, false); err == nil {
		t.Error("Android divided by a zero column")
	}
	if err := android(zeroColumn, androidLinear, false); err != nil {
		t.Errorf("Android linear with a zero column: %v", err)
	}
	if err := android(noColor, androidLinear, false); err == nil {
		t.Error("Android read a column without a color")
	}
	if err := android(noName, androidLinear, false); err == nil {
		t.Error("Android read a column without a name")
	}
	if err := android(onePoint, androidLinear, false); err == nil {
		t.Error("Android drew one point")
	}
	if err := android(badHex, androidLinear, false); err == nil {
		t.Error("Android parsed a short hex")
	}
	if err := android([]byte(`{"columns":[["x",1,2]],"types":{"x":"x"},"names":{},"colors":{}}`), androidStack, true); err == nil {
		t.Error("Android stacked no lines")
	}
	if err := ios(short); err == nil {
		t.Error("iOS read columns of two lengths")
	}
	if err := ios(noColor); err == nil {
		t.Error("iOS read a column without a color")
	}
	if err := ios(noTypes); err == nil {
		t.Error("iOS read a graph without types")
	}
	if err := ios(badHex); err != nil {
		t.Errorf("iOS takes the key of a short hex: %v", err)
	}
	if _, err := tdesktop(short); err == nil {
		t.Error("tdesktop read columns of two lengths")
	}
	if _, err := tdesktop(zoomOff); err == nil {
		t.Error("tdesktop found a defaultZoom that is not an x")
	}
	if _, err := tdesktop(noSubchart); err == nil {
		t.Error("tdesktop hid a missing subchart")
	}
	if _, err := tdesktop(lowerKey); err == nil {
		t.Error("tdesktop painted a lower case key")
	}
	if week, _ := tdesktop(edit(`statsTooltipFormat('day')`, `statsTooltipFormat('week')`)); !week {
		t.Error("tdesktop missed the weeks")
	}
	if err := webk(badFormatter); err == nil {
		t.Error("Web K formatted an unknown formatter")
	}
	if err := webk(noSubchart); err == nil {
		t.Error("Web K hid a missing subchart")
	}
	if got := iosStep(good); got != 86400 {
		t.Errorf("iOS step %d", got)
	}
	if got := iosStep(string(xLast)); got != 86400 {
		t.Errorf("iOS step of x last %d", got)
	}
}
