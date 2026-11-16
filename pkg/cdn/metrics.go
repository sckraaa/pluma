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
)

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
