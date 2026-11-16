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

import "sort"

// Collapse keeps the keep series of series with the largest totals, in
// that order (the earlier of equal ones first), and sums the others into
// other, appended last; other's own Values are ignored. Nothing is summed
// when all fit: other is left out. For the graphs whose series are open
// (the languages of the members, the reactions): MaxSeries bounds the
// columns. The series must have values of one length.
func Collapse(series []Series, keep int, other Series) []Series {
	if keep < 0 {
		keep = 0
	}
	if len(series) <= keep {
		return series
	}
	order := make([]int, len(series))
	totals := make([]int64, len(series))
	for i, s := range series {
		order[i] = i
		for _, v := range s.Values {
			totals[i] += v
		}
	}
	sort.SliceStable(order, func(a, b int) bool { return totals[order[a]] > totals[order[b]] })

	out := make([]Series, 0, keep+1)
	for _, i := range order[:keep] {
		out = append(out, series[i])
	}
	other.Values = make([]int64, len(series[0].Values))
	for _, i := range order[keep:] {
		for j, v := range series[i].Values {
			other.Values[j] += v
		}
	}
	return append(out, other)
}
