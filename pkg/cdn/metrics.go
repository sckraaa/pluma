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
		Help:      "Decisions of upload.getFile on a file that may go through the CDN: redirect, not_ready (not prepared yet), unhealthy (the delivery is out), small (below CDN_MIN_FILE_SIZE), error (the file served from home after a failure).",
		Labels:    []string{"result"},
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
)

// Results of upload.reuploadCdnFile (metricReuploads).
const (
	reuploadTokenInvalid = "token_invalid"
	reuploadFlood        = "flood"
)
