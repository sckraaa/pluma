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

// Package statsgraph makes the graphs of the statistics of channels,
// groups, posts, stories and polls (task 103, ADR 0066): the JSON of a
// statsGraph (core.telegram.org/api/stats, "Graph"), its time buckets,
// palettes and formatters, and the signed tokens of zooming into a day.
//
// The rules of the JSON are those of the parsers of the official clients,
// all in this one place (ADR 0066, p. 4); a graph that breaks one is either
// hidden by a client or takes its statistics screen down:
//
//   - columns, types, names and colors are all there and every y column has
//     a type, a name and a color (iOS ChartsCollection.swift:40-86, Android
//     ChartData.java:90-108);
//   - the x column comes first (Android reads x.length while it reads every
//     column, ChartData.java:47-77) and is not the last one, and the JSON is
//     compact with "columns" its first key (iOS finds the step of a post's
//     graph by the substring `["x",` up to the next `],`,
//     MessageStatistics.swift:104-116);
//   - all columns have the length of x (iOS ChartsCollection.swift:91-95,
//     tdesktop statistics_data_deserialize.cpp:100-106), there is a y column
//     (Android StackLinearChartData.java:49) and two points or more (Android
//     StatisticActivity.java:1015-1016), at most 50 columns (the
//     documentation);
//   - a graph of two y axes has no column of zeros next to one that is not
//     (Android DoubleLinearChartData.java:34 divides by the column's max);
//   - subchart is always written: Web K shows it when it is missing
//     (lib/tchart/chart.ts:156), tdesktop too
//     (statistics_data_deserialize.cpp:110-116); defaultZoom holds exact x
//     values (tdesktop finds them by equality, :117-138);
//   - the formatters are strings of Web K's table (lib/tchart/utils.ts:
//     184-213; any other string prints raw milliseconds), weeks are told by
//     'week' in xTooltipFormatter (tdesktop :150-157).
package statsgraph

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/teamgram/proto/mtproto"
)

// Type is how the clients draw the columns of a graph; the documentation
// gives one type to all the y columns of a graph.
type Type string

const (
	Line Type = "line"
	Step Type = "step"
	Bar  Type = "bar"
	Area Type = "area"
)

// MaxSeries is the number of y columns a graph may have: the documentation
// allows 50 columns, x is one of them. Graphs with an open number of series
// (languages, reactions) keep the largest with Collapse.
const MaxSeries = 49

// MinPoints is the number of points a graph needs to be drawn: Android
// shows a graph of fewer as empty (StatisticActivity.java:1015-1016).
const MinPoints = 2

// DefaultZoomMs is how much of a graph with a subchart the clients show at
// first: its last 30 days (ADR 0066, p. 6).
const DefaultZoomMs = 30 * secondsPerDay * msPerSecond

// Series is a y column of a graph.
type Series struct {
	// NameKey is the key of the name in the server's texts of the
	// statistics, written in the language of the viewer; Name is a name
	// shown as it is (the text of a poll option, a reaction). One of the
	// two.
	NameKey string
	Name    string
	// Color of the series; Auto takes the one of its position.
	Color Color
	// Values, one for each x.
	Values []int64
}

// Graph is a graph before it is written as JSON.
type Graph struct {
	Type Type
	// Scale of the x axis; X are its values (Buckets.X), strictly
	// increasing.
	Scale Scale
	X     []int64
	// Series are the y columns, at most MaxSeries.
	Series []Series
	// YScaled: two y axes, one for each of two series. The clients choose
	// the kind of graph by the field it comes in, not by this flag
	// (Android StatisticActivity.java:293-304), so it is set for exactly
	// the fields they draw with two axes.
	YScaled bool
	// Stacked: bars or areas on top of each other.
	Stacked bool
	// Percentage: stacked areas of shares of the total; the clients turn
	// it into a pie when zoomed.
	Percentage bool
	// Subchart: the overview under the graph, zoomed at first to its last
	// DefaultZoomMs. For the graphs of days; never for HourOfDay.
	Subchart bool
	// ZoomToken, when not empty, lets the clients zoom into a point
	// (stats.loadAsyncGraph): a Signer's token.
	ZoomToken string
	// Empty is the text of the statsGraphError given instead of the graph
	// when it has fewer than MinPoints points or no series, written in the
	// language of the viewer; when empty, that of NotEnoughDataKey.
	Empty string
}

// TextFunc returns the text of a key of the server's texts of the
// statistics in a language (langpack.StatsText).
type TextFunc func(key, lang string) string

// NotEnoughDataKey is the key of the text of a graph with too little data
// to draw, when the graph does not give its own.
const NotEnoughDataKey = "PlumaStatsNotEnoughData"

// Error is the statsGraphError of text: every client hides the graph
// (Android StatisticActivity.java:1002, iOS PeerStatistics.swift:42-51,
// tdesktop info_statistics_inner_widget.cpp:150-160, Web K
// statistics.tsx:907-910); Android shows the text of a poll's
// (ChatActivity.java:34959-34963) and Web K that of any (:888-910).
func Error(text string) *mtproto.StatsGraph {
	return mtproto.MakeTLStatsGraphError(&mtproto.StatsGraph{Error: text}).To_StatsGraph()
}

// Build writes g for a viewer of language lang (names from text) and
// theme dark. A graph with too little to draw is a statsGraphError
// (g.Empty); a graph that breaks the rules of the package is an error of
// the caller, never sent.
func (g *Graph) Build(text TextFunc, lang string, dark bool) (*mtproto.StatsGraph, error) {
	if err := g.check(); err != nil {
		return nil, err
	}
	series := g.Series
	if g.YScaled {
		series = dropZeroSeries(series)
	}
	if len(g.X) < MinPoints || len(series) == 0 || (g.Percentage && allZero(series)) {
		return Error(g.emptyText(text, lang)), nil
	}

	data, err := g.marshal(series, text, lang, dark)
	if err != nil {
		return nil, err
	}
	out := &mtproto.StatsGraph{Json: mtproto.MakeTLDataJSON(&mtproto.DataJSON{Data: string(data)}).To_DataJSON()}
	if g.ZoomToken != "" {
		out.ZoomToken = mtproto.MakeFlagsString(g.ZoomToken)
	}
	return mtproto.MakeTLStatsGraph(out).To_StatsGraph(), nil
}

func (g *Graph) emptyText(text TextFunc, lang string) string {
	if g.Empty != "" {
		return g.Empty
	}
	return text(NotEnoughDataKey, lang)
}

// ErrBadGraph is the error of a graph against the rules of the package.
var ErrBadGraph = errors.New("statsgraph: bad graph")

func badGraph(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrBadGraph, fmt.Sprintf(format, a...))
}

func (g *Graph) check() error {
	switch g.Type {
	case Line, Step, Bar, Area:
	default:
		return badGraph("type %q", g.Type)
	}
	if !g.Scale.valid() {
		return badGraph("scale %d", int(g.Scale))
	}
	if len(g.Series) > MaxSeries {
		return badGraph("%d series, at most %d", len(g.Series), MaxSeries)
	}
	for i := 1; i < len(g.X); i++ {
		if g.X[i] <= g.X[i-1] {
			// Android and tdesktop divide by the span and the step of x
			// (ChartData.java:131-136, statistics_data_deserialize.cpp:88).
			return badGraph("x not increasing at %d", i)
		}
	}
	if g.Scale == HourOfDay {
		if g.Subchart {
			return badGraph("subchart of hours of the day")
		}
		for _, x := range g.X {
			if x < 0 || x >= hoursPerDay {
				return badGraph("hour of the day %d", x)
			}
		}
	}
	// The documentation: y_scaled is for lines and steps, percentage for
	// areas, stacked for bars (and the percentage areas).
	if g.YScaled && g.Type != Line && g.Type != Step {
		return badGraph("y_scaled %s", g.Type)
	}
	if g.YScaled && (g.Stacked || g.Percentage) {
		return badGraph("y_scaled and stacked")
	}
	// Two axes, one for each of two series: every field the clients draw
	// with two axes carries two (docs/tasks/103, the clients' contract); a
	// dropped column of zeros leaves one.
	if g.YScaled && len(g.Series) != 2 {
		return badGraph("y_scaled with %d series", len(g.Series))
	}
	if g.Percentage && (g.Type != Area || !g.Stacked) {
		return badGraph("percentage of %s, stacked %v", g.Type, g.Stacked)
	}
	if g.Stacked && g.Type != Bar && g.Type != Area {
		return badGraph("stacked %s", g.Type)
	}
	for i, s := range g.Series {
		if len(s.Values) != len(g.X) {
			return badGraph("series %d: %d values for %d points", i, len(s.Values), len(g.X))
		}
		if (s.NameKey == "") == (s.Name == "") {
			return badGraph("series %d: one of NameKey and Name", i)
		}
		if s.Color < Auto || s.Color > Color(namedColors) {
			return badGraph("series %d: color %d", i, int(s.Color))
		}
	}
	return nil
}

// dropZeroSeries leaves out of a graph of two axes the series of zeros when
// another has a value: Android divides the largest value of the graph by
// that of each series as long integers (DoubleLinearChartData.java:34) and
// a zero there throws out of the network callback. A graph of zeros only
// stays as it is: there the two are equal and Android takes 1 (:31-33).
func dropZeroSeries(series []Series) []Series {
	if allZero(series) {
		return series
	}
	out := make([]Series, 0, len(series))
	for _, s := range series {
		if !zero(s.Values) {
			out = append(out, s)
		}
	}
	return out
}

func zero(values []int64) bool {
	for _, v := range values {
		if v != 0 {
			return false
		}
	}
	return true
}

func allZero(series []Series) bool {
	for _, s := range series {
		if !zero(s.Values) {
			return false
		}
	}
	return true
}

// graphJSON is the JSON of a graph, its fields in the order they are
// written: columns first.
type graphJSON struct {
	Columns           [][]any           `json:"columns"`
	Types             map[string]string `json:"types"`
	Names             map[string]string `json:"names"`
	Colors            map[string]string `json:"colors"`
	Subchart          subchartJSON      `json:"subchart"`
	YScaled           bool              `json:"y_scaled,omitempty"`
	Stacked           bool              `json:"stacked,omitempty"`
	Percentage        bool              `json:"percentage,omitempty"`
	XTickFormatter    string            `json:"xTickFormatter"`
	XTooltipFormatter string            `json:"xTooltipFormatter"`
	XRangeFormatter   string            `json:"xRangeFormatter"`
	YTickFormatter    string            `json:"yTickFormatter"`
	YTooltipFormatter string            `json:"yTooltipFormatter"`
}

type subchartJSON struct {
	Show        bool      `json:"show"`
	DefaultZoom *[2]int64 `json:"defaultZoom,omitempty"`
}

// The formatters of the y axis, the same for every graph: "null" is Web K's
// default number of the ticks, statsFormatTooltipValue its number of the
// tooltip (lib/tchart/utils.ts:194, :199-208).
const (
	yTickFormatter    = "null"
	yTooltipFormatter = "statsFormatTooltipValue"
)

// xFormatters are the formatters of the x axis of a scale: the ticks and
// the tooltip, from Web K's table (lib/tchart/utils.ts:186-198). The range
// over the graph is always "null", Web K's full date (:203-206): it reads
// it whenever the subchart is shown and for every zoomed graph
// (lib/tchart/chart.ts:210, :1288), and a missing one is a raw number.
func xFormatters(s Scale) (tick, tooltip string) {
	switch s {
	case HourOfDay:
		return "statsFormatDayHour", "statsFormatDayHourFull"
	default:
		return "statsFormat('" + s.String() + "')", "statsTooltipFormat('" + s.String() + "')"
	}
}

const xRangeFormatter = "null"

func columnID(i int) string { return fmt.Sprintf("y%d", i) }

func (g *Graph) marshal(series []Series, text TextFunc, lang string, dark bool) ([]byte, error) {
	j := graphJSON{
		Columns:           make([][]any, 0, len(series)+1),
		Types:             make(map[string]string, len(series)+1),
		Names:             make(map[string]string, len(series)),
		Colors:            make(map[string]string, len(series)),
		YScaled:           g.YScaled,
		Stacked:           g.Stacked,
		Percentage:        g.Percentage,
		XRangeFormatter:   xRangeFormatter,
		YTickFormatter:    yTickFormatter,
		YTooltipFormatter: yTooltipFormatter,
	}
	j.XTickFormatter, j.XTooltipFormatter = xFormatters(g.Scale)

	j.Columns = append(j.Columns, column("x", g.X))
	j.Types["x"] = "x"
	for i, s := range series {
		id := columnID(i)
		name := s.Name
		if s.NameKey != "" {
			name = text(s.NameKey, lang)
		}
		if name == "" {
			return nil, badGraph("series %d: empty name", i)
		}
		j.Columns = append(j.Columns, column(id, s.Values))
		j.Types[id] = string(g.Type)
		j.Names[id] = name
		j.Colors[id] = colorValue(s.Color, i, dark)
	}

	if g.Subchart {
		j.Subchart.Show = true
		j.Subchart.DefaultZoom = &[2]int64{defaultZoomStart(g.X), g.X[len(g.X)-1]}
	}
	return json.Marshal(j)
}

func column(id string, values []int64) []any {
	c := make([]any, 0, len(values)+1)
	c = append(c, id)
	for _, v := range values {
		c = append(c, v)
	}
	return c
}

// defaultZoomStart is the first x of the last DefaultZoomMs of x: the 30
// last points of a graph of days, all of a shorter one.
func defaultZoomStart(x []int64) int64 {
	last := x[len(x)-1]
	for _, v := range x {
		if v > last-DefaultZoomMs {
			return v
		}
	}
	return last
}
