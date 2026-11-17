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

// Color is the color of a series. Auto, the zero value, takes the color of
// the series' position in its graph.
type Color int

// The named colors. Their keys are the ones the documentation
// (core.telegram.org/api/stats), Android (ThemeColors.java:1708-1715),
// iOS (GraphCore UIColor+Utils.swift:30-75) and tdesktop
// (statistics/chart_widget.cpp:41-65) all know; each client paints such a
// series with the color of its own theme, and the hex after the key is the
// fallback (Web K uses only the hex, statistics.tsx:130-132).
const (
	Auto Color = iota
	Blue
	Green
	Red
	Golden
	LightBlue
	LightGreen
	Orange
	Indigo
	namedColors = iota - 1
)

// The keys are written in upper case: tdesktop compares the key with
// "BLUE", "GREEN", ... exactly (chart_widget.cpp:43-61), while Android
// (ChartData.java:97, toLowerCase) and iOS (UIColor+Utils.swift:33,
// lowercased) take any case.
var colorKeys = [namedColors]string{
	"BLUE", "GREEN", "RED", "GOLDEN", "LIGHTBLUE", "LIGHTGREEN", "ORANGE", "INDIGO",
}

// The hex fallbacks of the named colors. The light ones are Android's
// default theme (ThemeColors.java:750-757), the dark ones its night theme
// (assets/night.attheme, statisticChartLine_*).
var (
	lightHex = [namedColors]string{
		"#327FE5", "#61C752", "#E05356", "#EBA52D", "#58A8ED", "#8FCF39", "#F28C39", "#7F79F3",
	}
	darkHex = [namedColors]string{
		"#529FFF", "#3DC23F", "#F34C44", "#DEAC1F", "#3C78EC", "#8FCF39", "#F28C39", "#875CE5",
	}
)

// extraHex are the colors of the series past the named ones, written as a
// bare "#RRGGBB" (no key every client knows): Android's purple and cyan
// (ThemeColors.java:758-759) and iOS's seablue, violet, emerald and pink
// (UIColor+Utils.swift:44-62). The clients have no dark variants of them;
// the same hex serves both themes. A graph with more series than colors
// (polls of 12 options are the largest, pkg/polls AnswersMax) repeats them.
var extraHex = [...]string{"#9F79E8", "#40D0CA", "#35AFDC", "#AF52DE", "#50E3C2", "#FF2D55"}

// paletteSize is the number of distinct colors of the series of a graph.
const paletteSize = namedColors + len(extraHex)

// colorValue is the value of colors[id] of the series at position i whose
// color is c: "KEY#RRGGBB" for a named color, "#RRGGBB" for the others.
func colorValue(c Color, i int, dark bool) string {
	n := int(c) - 1
	if c == Auto {
		n = i % paletteSize
	}
	if n >= namedColors {
		return extraHex[n-namedColors]
	}
	if dark {
		return colorKeys[n] + darkHex[n]
	}
	return colorKeys[n] + lightHex[n]
}
