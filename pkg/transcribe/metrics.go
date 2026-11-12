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

package transcribe

import (
	"strconv"

	"github.com/zeromicro/go-zero/core/metric"
)

// Metrics of the server side (ADR 0056, p. 10); the alerts on them are in
// teamgramd/deploy/prometheus/server/rules/stt.yml.
var (
	metricRequests = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "transcribe",
		Name:      "requests_total",
		Help:      "Answers of messages.transcribeAudio, by result.",
		Labels:    []string{"result"},
	})
	metricJobs = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "transcribe",
		Name:      "jobs_total",
		Help:      "Attempts to transcribe a record, by result.",
		Labels:    []string{"result"},
	})
	metricQueueDepth = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "transcribe",
		Name:      "queue_depth",
		Help:      "Records waiting or being transcribed.",
		Labels:    []string{},
	})
	metricOldestJob = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: "transcribe",
		Name:      "oldest_job_seconds",
		Help:      "Age of the oldest record waiting or being transcribed, in seconds; 0 without one.",
		Labels:    []string{},
	})
	metricEngineSeconds = metric.NewHistogramVec(&metric.HistogramVecOpts{
		Namespace: "transcribe",
		Name:      "engine_seconds",
		Help:      "Time the engine took for one record.",
		Labels:    []string{},
		Buckets:   []float64{0.25, 0.5, 1, 2, 5, 10, 20, 40, 80, 160, 320},
	})
	metricAudioSeconds = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "transcribe",
		Name:      "audio_seconds_total",
		Help:      "Seconds of sound the engine transcribed.",
		Labels:    []string{},
	})
	metricRatings = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "transcribe",
		Name:      "ratings_total",
		Help:      "Ratings users gave to transcriptions, by model.",
		Labels:    []string{"model", "good"},
	})
)

// Results of messages.transcribeAudio, the label of transcribe_requests_total.
const (
	RequestCached   = "cached"   // the text was there
	RequestDone     = "done"     // transcribed within the wait
	RequestPending  = "pending"  // answered pending
	RequestFailed   = "failed"   // the record failed
	RequestOff      = "off"      // no engine is configured
	RequestRefused  = "refused"  // not a voice message the caller can read
	RequestInternal = "internal" // a failure of the server
)

// Results of an attempt, the label of transcribe_jobs_total.
const (
	JobDone     = "done"
	JobRetry    = "retry"    // will be tried again
	JobBusy     = "busy"     // the engine was busy; not an attempt
	JobDown     = "down"     // the engine could not be used (down, or the configuration); not an attempt
	JobFailed   = "failed"   // out of attempts
	JobRejected = "rejected" // the record cannot be transcribed
	JobNoRoom   = "no_room"  // too little free disk for the record; not an attempt
	JobLost     = "lost"     // the lease ran out, another attempt took over
	JobRetired  = "retired"  // of a model that is no longer configured
)

// CountRequest counts an answer of messages.transcribeAudio.
func CountRequest(result string) { metricRequests.Inc(result) }

// CountJob counts an attempt.
func CountJob(result string) { metricJobs.Inc(result) }

// ObserveEngine records one answer of the engine: how long it took for how
// much sound.
func ObserveEngine(tookSeconds, audioSeconds float64) {
	metricEngineSeconds.ObserveFloat(tookSeconds)
	metricAudioSeconds.Add(audioSeconds)
}

// CountRating counts a rating of a text of model.
func CountRating(model string, good bool) { metricRatings.Inc(model, strconv.FormatBool(good)) }

// SetQueue publishes the size of the queue and the age of its oldest record.
func SetQueue(depth int, oldestSeconds float64) {
	metricQueueDepth.Set(float64(depth))
	metricOldestJob.Set(oldestSeconds)
}
