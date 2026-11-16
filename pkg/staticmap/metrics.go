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

package staticmap

import (
	"github.com/zeromicro/go-zero/core/metric"
)

// Metrics of the static maps (ADR 0063, p. 8); the alerts on them are in the
// maps group of teamgramd/deploy/prometheus/server/rules/.
var (
	metricRender = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "webmap",
		Name:      "render_total",
		Help:      "Maps rendered for upload.getWebFile, by result.",
		Labels:    []string{"result"},
	})
	metricRenderSeconds = metric.NewHistogramVec(&metric.HistogramVecOpts{
		Namespace: "webmap",
		Name:      "render_seconds",
		Help:      "Time the maps service took for one map.",
		Labels:    []string{},
		Buckets:   []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 4, 8, 16},
	})
	metricCache = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "webmap",
		Name:      "cache_total",
		Help:      "Lookups of rendered maps in Redis, by result.",
		Labels:    []string{"result"},
	})
	metricFlood = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "webmap",
		Name:      "flood_total",
		Help:      "Map requests refused by the per-user limit.",
		Labels:    []string{},
	})
	metricLimitErrors = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "webmap",
		Name:      "limit_errors_total",
		Help:      "Per-user limit checks that failed in Redis; the map was rendered unlimited.",
		Labels:    []string{},
	})
)

// Results of a render, the label of webmap_render_total.
const (
	renderOK      = "ok"
	renderTimeout = "timeout" // the service, or the wait for a render slot, took too long
	renderError   = "error"   // the service could not be reached or refused
	renderInvalid = "invalid" // the answer was not a PNG of the requested size
)

// Results of a cache lookup or store, the label of webmap_cache_total.
const (
	cacheHit   = "hit"
	cacheMiss  = "miss"
	cacheError = "error" // Redis failed; the map is rendered and not stored
)
