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

// Package metrics holds the Prometheus metrics of the SFU (ADR 0059 §11).
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Media kinds of the labels.
const (
	KindAudio  = "audio"
	KindCamera = "camera"
	KindScreen = "screen"
)

var (
	Rooms = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "sfu_rooms",
		Help: "Rooms open on the SFU.",
	})
	Connections = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "sfu_connections",
		Help: "Client connections of the SFU, by kind (main, presentation) and state (connecting, connected).",
	}, []string{"kind", "state"})
	PacketsIn = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sfu_packets_received_total",
		Help: "RTP packets received from senders, by media kind.",
	}, []string{"media"})
	BytesIn = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sfu_bytes_received_total",
		Help: "RTP bytes received from senders, by media kind.",
	}, []string{"media"})
	PacketsOut = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sfu_packets_sent_total",
		Help: "RTP packets forwarded to receivers, by media kind.",
	}, []string{"media"})
	BytesOut = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sfu_bytes_sent_total",
		Help: "RTP bytes forwarded to receivers, by media kind.",
	}, []string{"media"})
	SendErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "sfu_send_errors_total",
		Help: "Packets the SFU failed to send to a client.",
	})
	UnknownSSRC = promauto.NewCounter(prometheus.CounterOpts{
		Name: "sfu_unknown_ssrc_packets_total",
		Help: "RTP packets from a client with an SSRC it did not declare when joining.",
	})
	MutedDropped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "sfu_muted_audio_dropped_total",
		Help: "Audio packets of participants muted by an admin, not forwarded.",
	})
	NACKs = promauto.NewCounter(prometheus.CounterOpts{
		Name: "sfu_nack_requests_total",
		Help: "Packets receivers asked to retransmit.",
	})
	Retransmissions = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sfu_retransmissions_total",
		Help: "Answers to receivers' NACKs: rtx sent from the cache, upstream asked of the sender (not in the cache, or lost on the sender's link), missing a number the receiver was never given a place for.",
	}, []string{"result"})
	KeyframeRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sfu_keyframe_requests_total",
		Help: "Key frame requests sent to senders, by cause (receiver, layer_switch).",
	}, []string{"cause"})
	LayerSwitches = promauto.NewCounter(prometheus.CounterOpts{
		Name: "sfu_layer_switches_total",
		Help: "Changes of the simulcast layer a receiver gets of a sender.",
	})
	ForwardedLayers = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "sfu_forwarded_videos",
		Help: "Videos forwarded to receivers now, by layer (0 lowest; paused).",
	}, []string{"layer"})
	ReceiverLoss = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "sfu_receiver_loss_ratio",
		Help:    "Fraction of packets lost that receivers report.",
		Buckets: []float64{0.005, 0.01, 0.02, 0.05, 0.1, 0.2, 0.4},
	})
	ReceiverEstimate = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "sfu_receiver_estimate_bps",
		Help:    "Channel estimates of receivers, sampled each allocation.",
		Buckets: prometheus.ExponentialBuckets(100e3, 2, 9),
	})
	ICEPairs = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sfu_ice_pairs_total",
		Help: "Connections up, by transport of the selected pair (udp4, udp6, tcp4, tcp6) and ICE role of the SFU.",
	}, []string{"network", "role"})
	ConnectFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sfu_connect_failures_total",
		Help: "Connections lost or never made, by reason (no_check, ice, dtls).",
	}, []string{"reason"})
	ConnectSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "sfu_connect_seconds",
		Help:    "Time from a join to the connection being up.",
		Buckets: []float64{0.1, 0.25, 0.5, 1, 2, 4, 8, 16},
	})
	Uplink = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "sfu_uplink_bps",
		Help: "Bits per second the SFU forwards to receivers.",
	})
	UplinkLimit = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "sfu_uplink_limit_bps",
		Help: "Configured limit of the SFU's upload (0: none).",
	})
	EventsDropped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "sfu_events_dropped_total",
		Help: "Events not delivered to a slow subscriber of the control stream.",
	})
	E2EReports = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sfu_e2e_reports_total",
		Help: "PlumaE2EStatus reports of the data channel: accepted, or dropped by reason (size, version, schema, rate, kind).",
	}, []string{"result"})
	E2EUnparsedAudio = promauto.NewCounter(prometheus.CounterOpts{
		Name: "sfu_e2e_audio_unparsed_total",
		Help: "Sound packets whose payload is not a frame of tde2e: sent unencrypted or broken. Forwarded all the same; the SFU reads no frame.",
	})
)
