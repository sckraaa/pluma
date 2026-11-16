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

package translate

import (
	"context"
	"errors"

	"github.com/zeromicro/go-zero/core/metric"
)

// Metrics of the translation (ADR 0064, section 13). They carry lengths,
// counts and times, never a text.
var (
	metricRequests = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "translate",
		Name:      "requests_total",
		Help:      "Answers of the translating methods, by method and result.",
		Labels:    []string{"method", "result"},
	})
	metricItems = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "translate",
		Name:      "items_total",
		Help:      "Items answered, by where the translation came from: cache, engine, or empty (nothing to translate).",
		Labels:    []string{"source"},
	})
	metricEngineSeconds = metric.NewHistogramVec(&metric.HistogramVecOpts{
		Namespace: "translate",
		Name:      "engine_seconds",
		Help:      "Time one item took in the engine, its fallback pieces included.",
		Labels:    []string{},
		Buckets:   []float64{0.25, 0.5, 1, 2, 3, 5, 8, 13, 20, 30, 60, 120},
	})
	metricEngineErrors = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "translate",
		Name:      "engine_errors_total",
		Help:      "Failed attempts of an item in the engine, by kind.",
		Labels:    []string{"kind"},
	})
	metricEngineUp = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "translate",
		Name:      "engine_up",
		Help:      "1 while the engine takes new items, 0 while it is waited for after an error.",
		Labels:    []string{},
	})
	metricQueueDepth = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "translate",
		Name:      "queue_depth",
		Help:      "Items waiting for the engine, in it, or waiting to be tried again.",
		Labels:    []string{},
	})
	metricMarkupFallback = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "translate",
		Name:      "markup_fallback_total",
		Help:      "Items whose answer did not keep the tags and that went by pieces.",
		Labels:    []string{},
	})
	metricCacheRows = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "translate",
		Name:      "cache_rows",
		Help:      "Rows of translation_cache.",
		Labels:    []string{},
	})
	metricLimitErrors = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "translate",
		Name:      "limit_errors_total",
		Help:      "Checks of the per-user limits that failed in Redis and let the items through.",
		Labels:    []string{},
	})
	metricCacheErrors = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "translate",
		Name:      "cache_errors_total",
		Help:      "Reads and writes of translation_cache that failed, by operation.",
		Labels:    []string{"op"},
	})
)

// Results of a request, the label result of translate_requests_total. The
// scheduler counts the requests that reach it (Scheduler.Translate); the
// handlers count those they answer themselves with CountRequest.
const (
	ResultOK       = "ok"       // every item answered
	ResultWait     = "wait"     // not ready within the wait: FLOOD_WAIT_X, the work goes on
	ResultDown     = "down"     // the engine is waited for: FLOOD_WAIT_10
	ResultFailed   = "failed"   // an item failed lately: FLOOD_WAIT_X
	ResultLimited  = "limited"  // over the user's limit: FLOOD_WAIT_X
	ResultStopped  = "stopped"  // bff is stopping
	ResultCanceled = "canceled" // the caller went away
	ResultInvalid  = "invalid"  // refused by the checks of the request
	ResultOff      = "off"      // no engine is configured
)

// Sources of an item, the label source of translate_items_total.
const (
	SourceCache  = "cache"
	SourceEngine = "engine"
	SourceEmpty  = "empty"
)

// Kinds of a failed attempt, the label kind of translate_engine_errors_total.
const (
	errKindBusy        = "busy"        // 503 or 429: loading the model, or full
	errKindDown        = "down"        // nothing listens, or the token or the path is refused
	errKindTimeout     = "timeout"     // over the deadline of the request
	errKindUnavailable = "unavailable" // another failure of the engine or of the connection
	errKindRejected    = "rejected"    // the engine refused the request
	errKindBadAnswer   = "bad_answer"  // the answer is not a translation
	errKindOther       = "other"
)

// CountRequest counts an answer of method that the scheduler did not
// count: the refusals of the handler (ResultInvalid, ResultOff).
func CountRequest(method, result string) { metricRequests.Inc(method, result) }

// SetCacheRows publishes the number of rows of translation_cache.
func SetCacheRows(n int64) { metricCacheRows.Set(float64(n)) }

func countItems(source string, n int) {
	if n > 0 {
		metricItems.Add(float64(n), source)
	}
}

func errKind(err error) string {
	switch {
	case errors.Is(err, ErrBusy):
		return errKindBusy
	case errors.Is(err, ErrDown):
		return errKindDown
	case errors.Is(err, ErrUnavailable) && errors.Is(err, context.DeadlineExceeded):
		return errKindTimeout
	case errors.Is(err, ErrUnavailable):
		return errKindUnavailable
	case errors.Is(err, ErrRejected), errors.Is(err, ErrToLangInvalid):
		return errKindRejected
	case errors.Is(err, ErrBadAnswer):
		return errKindBadAnswer
	default:
		return errKindOther
	}
}

func setEngineUp(up bool) {
	if up {
		metricEngineUp.Set(1)
	} else {
		metricEngineUp.Set(0)
	}
}
