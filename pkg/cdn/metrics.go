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

// The metrics of the CDN at home (task 102, docs/ops/monitoring.md); the
// node has its own. None of them carries a file token, a file id or a
// client address.
var (
	metricRedirects = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "pluma",
		Subsystem: "cdn",
		Name:      "redirects_total",
		Help:      "Decisions of upload.getFile on a file that may go through the CDN: redirect, not_ready (no copy on the node yet), unhealthy (the node is out), small (below CDN_MIN_FILE_SIZE), error (the file served from home after a failure).",
		Labels:    []string{"result"},
	})
	metricPushes = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "pluma",
		Subsystem: "cdn",
		Name:      "push_total",
		Help:      "Pushes of files to a node, by result: ok, or why the push failed.",
		Labels:    []string{"result"},
	})
	metricPushBytes = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "pluma",
		Subsystem: "cdn",
		Name:      "push_bytes_total",
		Help:      "Bytes of the cipher of files sent to the nodes.",
	})
	metricQueue = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "pluma",
		Subsystem: "cdn",
		Name:      "queue",
		Help:      "Copies of files on the node, by state: queued, pushing, ready, absent, failed.",
		Labels:    []string{"state"},
	})
	metricNodeHealthy = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "pluma",
		Subsystem: "cdn",
		Name:      "node_healthy",
		Help:      "1 while the node takes redirects, 0 while it is out.",
		Labels:    []string{"dc"},
	})
	metricNodeEgress = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "pluma",
		Subsystem: "cdn",
		Name:      "node_egress_bytes",
		Help:      "Bytes the node sent to clients this calendar month (UTC), as it reports them.",
		Labels:    []string{"dc"},
	})
	metricNodeEgressLimit = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "pluma",
		Subsystem: "cdn",
		Name:      "node_egress_limit_bytes",
		Help:      "CDN_MONTHLY_EGRESS_LIMIT_GB in bytes: the node is taken out at it.",
		Labels:    []string{"dc"},
	})
	metricReuploads = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "pluma",
		Subsystem: "cdn",
		Name:      "reupload_total",
		Help:      "upload.reuploadCdnFile by result: ok, token_invalid, request_token_invalid, timeout, unhealthy, flood, error.",
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
)

// Results of upload.reuploadCdnFile (metricReuploads).
const (
	reuploadOK                = "ok"
	reuploadTokenInvalid      = "token_invalid"
	reuploadRequestTokenWrong = "request_token_invalid"
	reuploadTimeout           = "timeout"
	reuploadUnhealthy         = "unhealthy"
	reuploadFlood             = "flood"
	reuploadError             = "error"
)

var stateNames = map[ReplicaState]string{
	StateQueued:  "queued",
	StatePushing: "pushing",
	StateReady:   "ready",
	StateAbsent:  "absent",
	StateFailed:  "failed",
}

func dcLabel(dc int32) string { return strconv.Itoa(int(dc)) }
