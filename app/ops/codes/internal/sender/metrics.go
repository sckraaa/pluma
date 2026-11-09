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

package sender

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// The alerts on them are in teamgramd/deploy/prometheus/server/rules/codes.yml.
var (
	metricSent = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "codes_sent_total",
		Help: "Login codes handled by the sender, by result.",
	}, []string{"result"})
	metricQueue = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "codes_queue_length",
		Help: "Login codes waiting to be sent.",
	})
	metricSessionReady = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "codes_session_ready",
		Help: "1 when the TDLib session of the account is authorized.",
	})
	metricAccountLimited = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "codes_account_limited",
		Help: "1 while the account waits out a Telegram flood wait or restriction.",
	})
	metricContactsToRemove = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "codes_contacts_to_remove",
		Help: "Imported contacts still to be removed from the account.",
	})
	metricRemovalErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "codes_contact_removal_errors_total",
		Help: "Failed removals of an imported contact.",
	})
	metricStateSaveErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "codes_state_save_errors_total",
		Help: "Failed writes of the state file of the sender.",
	})
)
