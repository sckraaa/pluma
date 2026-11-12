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

package core

import (
	"github.com/zeromicro/go-zero/core/metric"
	"google.golang.org/grpc/status"
)

// The metrics of group calls on the signalling side (ADR 0059 §11,
// docs/ops/monitoring.md); the SFU has its own.
var (
	metricCalls = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "groupcalls",
		Name:      "calls_total",
		Help:      "Group calls created, started from a schedule and ended.",
		Labels:    []string{"event"},
	})
	metricJoins = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "groupcalls",
		Name:      "joins_total",
		Help:      "Joins of group calls, by connection (main, presentation).",
		Labels:    []string{"kind"},
	})
	metricLeaves = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "groupcalls",
		Name:      "leaves_total",
		Help:      "Leaves of group calls: by the client (self), on the SFU's word (disconnect), by the collector (absent; access: removed from the chat), with the end of the call (discard).",
		Labels:    []string{"reason"},
	})
	metricSFUErrors = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "groupcalls",
		Name:      "sfu_errors_total",
		Help:      "Calls of the SFU control API that failed, by operation.",
		Labels:    []string{"op"},
	})
	metricMuteRepairs = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "groupcalls",
		Name:      "mute_repairs_total",
		Help:      "Mutes the collector sent the SFU again because its mute of a participant differed from his word.",
	})
	metricSFUConfigured = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "groupcalls",
		Name:      "sfu_configured",
		Help:      "1 when bff has SFU nodes for group calls (the ops.sfu target is then expected).",
	})
	metricReloads = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "groupcalls",
		Name:      "reloads_total",
		Help:      "phone.getGroupCall: the clients read a call again, on joining it or on a gap in its versions.",
	})
	metricErrors = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "groupcalls",
		Name:      "errors_total",
		Help:      "Errors the phone.* methods of group calls answered, by method and error.",
		Labels:    []string{"method", "error"},
	})
)

// CountError counts an error a method answered: an error of the API by its
// text (a bounded set), any other as "internal", whose text is not.
func CountError(method string, err error) {
	if err == nil {
		return
	}
	label := "internal"
	if st, ok := status.FromError(err); ok && st.Code() >= 300 && st.Code() < 500 {
		label = st.Message()
	}
	metricErrors.Inc(method, label)
}
