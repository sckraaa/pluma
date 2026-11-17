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
	"strconv"

	"github.com/zeromicro/go-zero/core/metric"
)

// The metrics of the CDN at home (docs/ops/monitoring.md). None of them
// carries a file token, a file id or a client address.
var (
	metricRedirects = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "pluma",
		Subsystem: "cdn",
		Name:      "redirects_total",
		Help:      "Decisions of upload.getFile with cdn_supported on a file that may go through the CDN, by delivery: redirect, no_caps (the client sent no pluma_cdn), small (below CDN_MIN_FILE_SIZE), avoided (the key fell back from the delivery), unhealthy (the delivery is out), budget (the delivery spent its month), not_ready (not prepared yet), error (the file served from home after a failure).",
		Labels:    []string{"dc", "result"},
	})
	metricFallbacks = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "pluma",
		Subsystem: "cdn",
		Name:      "fallback_total",
		Help:      "Clients that asked for a file without cdn_supported soon after it was redirected to the delivery: they gave up on it, and their key avoids it for CDN_AVOID_TTL.",
		Labels:    []string{"dc"},
	})
	metricDevHTTP = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "pluma",
		Subsystem: "cdn",
		Name:      "dev_http",
		Help:      "1 for a delivery with an http:// BaseURL (CDN_DEV_ALLOW_HTTP=1), which only the e2e stand may have.",
		Labels:    []string{"dc"},
	})
	metricReuploads = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "pluma",
		Subsystem: "cdn",
		Name:      "reupload_total",
		Help:      "upload.reuploadCdnFile by result: token_invalid (every call within the limit), flood.",
		Labels:    []string{"result"},
	})

	metricPrepares = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "pluma",
		Subsystem: "cdn",
		Name:      "prepare_total",
		Help:      "Preparations of files for a target (the hashes for the home origin), by result: ok, or why one failed.",
		Labels:    []string{"result"},
	})
	metricPrepareBytes = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "pluma",
		Subsystem: "cdn",
		Name:      "prepare_bytes_total",
		Help:      "Bytes of files read from dfs for their preparation.",
	})
	metricQueue = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "pluma",
		Subsystem: "cdn",
		Name:      "queue",
		Help:      "Preparations of files, by state: queued, preparing, ready, absent, failed.",
		Labels:    []string{"state"},
	})
	metricOriginRequests = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "pluma",
		Subsystem: "cdn",
		Name:      "origin_requests_total",
		Help:      "Requests to the home origin by the status of the answer; aborted: the read of the file failed mid-answer and the connection was cut.",
		Labels:    []string{"code"},
	})
	metricOriginBytes = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "pluma",
		Subsystem: "cdn",
		Name:      "origin_bytes_total",
		Help:      "Bytes of the bodies the home origin sent to the CDNs.",
	})
	metricOriginInflight = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "pluma",
		Subsystem: "cdn",
		Name:      "origin_inflight",
		Help:      "Requests the home origin is serving now.",
	})
	metricUsage = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "pluma",
		Subsystem: "cdn",
		Name:      "usage_bytes",
		Help:      "Traffic of a delivery this calendar month (UTC): origin (served by the home origin, exact) and edge_est (the home's estimate of what the CDN sent to clients).",
		Labels:    []string{"dc", "kind"},
	})
	metricUsageLimit = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "pluma",
		Subsystem: "cdn",
		Name:      "usage_limit_bytes",
		Help:      "The monthly budget of a delivery (CDN_GCORE_MONTHLY_GB) in bytes: it leaves the routes once its usage reaches it.",
		Labels:    []string{"dc"},
	})
	metricDeliveryHealthy = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "pluma",
		Subsystem: "cdn",
		Name:      "delivery_healthy",
		Help:      "1 while the last check of a delivery passed, 0 while it is out.",
		Labels:    []string{"dc"},
	})
)

// Kinds of metricUsage.
const (
	usageKindOrigin  = "origin"
	usageKindEdgeEst = "edge_est"
)

// stateNames are the names of the states of metricQueue.
var stateNames = map[ReplicaState]string{
	StateQueued:  "queued",
	StatePushing: "preparing",
	StateReady:   "ready",
	StateAbsent:  "absent",
	StateFailed:  "failed",
}

// dcLabel is the label of a delivery in the metrics.
func dcLabel(dc int32) string { return strconv.Itoa(int(dc)) }

// Results of upload.getFile (metricRedirects).
const (
	ResultRedirect  = "redirect"
	ResultNotReady  = "not_ready"
	ResultUnhealthy = "unhealthy"
	ResultSmall     = "small"
	ResultError     = "error"
	ResultNoCaps    = "no_caps"
	ResultAvoided   = "avoided"
	ResultBudget    = "budget"
)

// Results of upload.reuploadCdnFile (metricReuploads).
const (
	reuploadTokenInvalid = "token_invalid"
	reuploadFlood        = "flood"
)
