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

// Package metrics holds the Prometheus metrics of a CDN node (task 102).
// None of them carries a file token, a file id or a client address.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Results of upload.getCdnFile (Requests).
const (
	// RequestOK: bytes of a file were sent.
	RequestOK = "ok"
	// RequestEOF: the offset is at or past the end of the file: empty bytes.
	RequestEOF = "eof"
	// RequestReupload: the node does not have the file:
	// upload.cdnFileReuploadNeeded.
	RequestReupload = "reupload"
	// RequestNoKeys: the node does not have the file and no request_token
	// key yet: FILE_TOKEN_INVALID.
	RequestNoKeys = "no_keys"
	// RequestTokenInvalid: a file_token that is not the home's
	// (FILE_TOKEN_INVALID).
	RequestTokenInvalid = "token_invalid"
	// RequestRangeInvalid: LIMIT_INVALID or OFFSET_INVALID.
	RequestRangeInvalid = "range_invalid"
	// RequestReadError: the disk failed; the file is dropped and the client
	// told to reupload.
	RequestReadError = "read_error"
	// RequestMethodInvalid: any other method: CDN_METHOD_INVALID.
	RequestMethodInvalid = "method_invalid"
)

// Reasons of Evictions.
const (
	// EvictCapacity: dropped, least recently served, to make room for a new
	// file.
	EvictCapacity = "capacity"
	// EvictIdle: not served for CDN_IDLE_TTL.
	EvictIdle = "idle"
	// EvictReadError: the disk failed reading it.
	EvictReadError = "read_error"
	// EvictDeleted: deleted by the home.
	EvictDeleted = "deleted"
)

var (
	ServedBytes = promauto.NewCounter(prometheus.CounterOpts{
		Name: "pluma_cdn_served_bytes_total",
		Help: "Bytes of files sent to clients by upload.getCdnFile.",
	})
	Requests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "pluma_cdn_requests_total",
		Help: "Requests to the node by result (ok, eof, reupload, no_keys, token_invalid, range_invalid, read_error, method_invalid).",
	}, []string{"result"})
	Handshakes = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "pluma_cdn_handshakes_total",
		Help: "Key exchanges by key type (perm, temp, media_temp; none when the type is not known yet) and result (ok, retry, error).",
	}, []string{"type", "result"})
	Connections = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "pluma_cdn_connections",
		Help: "Client connections open.",
	})
	ConnectionsRefused = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "pluma_cdn_connections_refused_total",
		Help: "Connections and key exchanges refused by a per-address limit (conn, key_exchange).",
	}, []string{"limit"})
	AuthKeys = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "pluma_cdn_auth_keys",
		Help: "Client auth keys held in memory.",
	})
	CacheBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "pluma_cdn_cache_bytes",
		Help: "Bytes of the files held.",
	})
	CacheFiles = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "pluma_cdn_cache_files",
		Help: "Files held.",
	})
	CacheCap = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "pluma_cdn_cache_cap_bytes",
		Help: "The ceiling of the files held (CDN_CACHE_BYTES).",
	})
	DiskFree = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "pluma_cdn_disk_free_bytes",
		Help: "Free space of the data volume.",
	})
	Evictions = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "pluma_cdn_evictions_total",
		Help: "Files dropped by reason (capacity, idle, read_error, deleted).",
	}, []string{"reason"})
	// CapacityEvictionAge is how long a file dropped for room had been on
	// the node: evictions of files younger than a day (le="86400") mean the
	// cache is too small for what clients watch.
	CapacityEvictionAge = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "pluma_cdn_capacity_eviction_age_seconds",
		Help:    "Age of the files dropped to make room, since their upload.",
		Buckets: []float64{3600, 6 * 3600, 86400, 3 * 86400, 7 * 86400, 30 * 86400},
	})
	EgressMonthBytes = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "pluma_cdn_egress_month_bytes",
		Help: "Bytes sent to clients this month (UTC), transport included; kept across restarts.",
	})
	Puts = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "pluma_cdn_puts_total",
		Help: "Files pushed by the home by result (ok, invalid, exhausted, error).",
	}, []string{"result"})
	PutBytes = promauto.NewCounter(prometheus.CounterOpts{
		Name: "pluma_cdn_put_bytes_total",
		Help: "Bytes of files stored from the home.",
	})
)
