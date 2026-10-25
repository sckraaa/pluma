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
)

// metricSent counts the sends by provider (apns, fcm) and result
// (pushprovider.Outcome). auth_failed means the provider refused our key: the
// alert PushProviderAuthFailed.
var metricSent = metric.NewCounterVec(&metric.CounterVecOpts{
	Namespace: "push",
	Name:      "sent_total",
	Help:      "Pushes handed to APNs and FCM, by provider and result.",
	Labels:    []string{"provider", "result"},
})

// metricDropped counts the pushes not sent to a device, by reason:
// initiator, online, unbound, muted, error.
var metricDropped = metric.NewCounterVec(&metric.CounterVecOpts{
	Namespace: "push",
	Name:      "dropped_total",
	Help:      "Pushes not sent to a device, by reason.",
	Labels:    []string{"reason"},
})
