//  Copyright (c) 2026 The Teamgram Authors. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package gnet

import (
	"errors"
	"io"

	"github.com/zeromicro/go-zero/core/metric"
)

const namespace = "gnetway"

var (
	metricConnOpen = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: namespace,
		Subsystem: "conn",
		Name:      "open_total",
		Help:      "Total number of connections opened.",
		Labels:    []string{"proto"}, // tcp, websocket
	})

	metricConnClose = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: namespace,
		Subsystem: "conn",
		Name:      "close_total",
		Help:      "Total number of connections closed.",
		Labels:    []string{"proto"},
	})

	metricConnTimeout = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: namespace,
		Subsystem: "conn",
		Name:      "timeout_total",
		Help:      "Total number of connections closed by timeout.",
		Labels:    []string{},
	})

	metricHandshake = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: namespace,
		Subsystem: "handshake",
		Name:      "total",
		Help:      "Total number of handshake attempts.",
		Labels:    []string{"result"}, // ok, error
	})

	metricMsgProcess = metric.NewHistogramVec(&metric.HistogramVecOpts{
		Namespace: namespace,
		Subsystem: "msg",
		Name:      "process_duration_ms",
		Help:      "Message processing duration in milliseconds.",
		Labels:    []string{"type"}, // encrypted, unencrypted
		Buckets:   []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000},
	})

	metricQuickAck = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: namespace,
		Subsystem: "msg",
		Name:      "quick_ack_total",
		Help:      "Total number of quick ACK responses sent.",
		Labels:    []string{},
	})

	metricPanicRecovered = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: namespace,
		Subsystem: "conn",
		Name:      "panic_recovered_total",
		Help:      "Panics while processing a connection, recovered by closing it.",
		Labels:    []string{"where"}, // traffic, trigger, worker, close
	})

	metricLimitRefused = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: namespace,
		Subsystem: "limit",
		Name:      "refused_total",
		Help:      "Connections, key exchanges and path probes refused by the limits of one address (tasks 008, 113).",
		Labels:    []string{"what"}, // conn, key_exchange, probe
	})

	metricCodecDecodeError = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: namespace,
		Subsystem: "codec",
		Name:      "decode_errors_total",
		Help:      "Total number of MTProto codec decode errors, labeled by transport and reason.",
		Labels:    []string{"proto", "reason"}, // proto: tcp, websocket; reason: bad_magic, bad_len, bad_crc, bad_seq, decrypt, transport_unsupported, unexpected_eof, other
	})

	// The metrics of the entries (task 109): entry is the listener kind
	// (tcp, ws, ws_ppv1, edge, http), via the edge an Edge connection came
	// through (EdgeVias or other; empty for the other entries). Never an
	// address.
	metricEntryConns = metric.NewGaugeVec(&metric.GaugeVecOpts{
		Namespace: namespace,
		Subsystem: "entry",
		Name:      "conns",
		Help:      "Open connections by entry; an Edge connection counts from its upgrade.",
		Labels:    []string{"entry", "via"},
	})

	metricEntryClosed = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: namespace,
		Subsystem: "entry",
		Name:      "closed_total",
		Help:      "Connections closed, by entry and reason.",
		Labels:    []string{"entry", "via", "reason"}, // reason: closeReason*
	})

	metricEntryBytes = metric.NewHistogramVec(&metric.HistogramVecOpts{
		Namespace: namespace,
		Subsystem: "entry",
		Name:      "conn_bytes",
		Help:      "Bytes a connection carried before it closed: in, read from the socket; out, MTProto transport bytes written (without WebSocket or HTTP framing).",
		Labels:    []string{"entry", "via", "direction"},
		// 16 KB and 64 KB: where filtered networks freeze connections
		// (task 092).
		Buckets: []float64{4 << 10, 16 << 10, 32 << 10, 64 << 10, 256 << 10, 1 << 20, 4 << 20, 16 << 20, 64 << 20, 256 << 20},
	})

	metricEdgeNoClientAddr = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: namespace,
		Subsystem: "edge",
		Name:      "no_client_addr_total",
		Help:      "Edge upgrade requests refused without a client address in X-Pluma-Client-Addr.",
		Labels:    []string{"via"},
	})

	// The path probes (pluma.probe, task 113): what became of each
	// request (probeResult*) and the bytes of the requests (in) and their
	// answers (out, MTProto transport bytes), by entry and edge. That the
	// answer was written does not mean it arrived: what a path carries only
	// the client knows.
	metricProbe = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: namespace,
		Subsystem: "probe",
		Name:      "total",
		Help:      "Path probes by entry and result.",
		Labels:    []string{"entry", "via", "result"},
	})

	metricProbeBytes = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: namespace,
		Subsystem: "probe",
		Name:      "bytes_total",
		Help:      "Bytes of path probes: in, the requests; out, the answers (MTProto transport bytes).",
		Labels:    []string{"entry", "via", "direction"},
	})

	metricEdgePing = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: namespace,
		Subsystem: "edge",
		Name:      "pings_total",
		Help:      "WebSocket pings the Edge entry sent.",
		Labels:    []string{"via"},
	})
)

// Reasons of gnetway_entry_closed_total.
const (
	// closeReasonPeer: the client (or the proxy in front) closed it.
	closeReasonPeer = "peer"
	// closeReasonIOError: reading or writing the socket failed.
	closeReasonIOError = "io_error"
	// closeReasonServer: the gateway closed it for another reason (a
	// transport or protocol error, an unknown or expired key, shutdown).
	closeReasonServer  = "server"
	closeReasonTimeout = "timeout"
	// closeReasonLimit: the address holds too many connections.
	closeReasonLimit = "limit"
	// closeReasonNoAddr: an Edge upgrade request without a client address.
	closeReasonNoAddr = "no_addr"
)

// Labels of gnetway_entry_* by listener.
const (
	entryTCP    = "tcp"
	entryWS     = "ws"
	entryWSPPV1 = "ws_ppv1"
	entryEdge   = "edge"
	entryHTTP   = "http"
)

// connEntry returns the entry label of a connection opened on its
// listener (OnOpen, before a PROXY header is read).
func connEntry(ctx *connContext) string {
	switch {
	case ctx.edge:
		return entryEdge
	case ctx.http:
		return entryHTTP
	case ctx.websocket && ctx.ppv1:
		return entryWSPPV1
	case ctx.websocket:
		return entryWS
	default:
		return entryTCP
	}
}

// entryOpened counts the connection as open in its entry.
func entryOpened(ctx *connContext) {
	if ctx.entryCounted {
		return
	}
	ctx.entryCounted = true
	metricEntryConns.Inc(ctx.entry, ctx.via)
}

// entryClosed records the connection's close: err is the one gnet gives
// OnClose.
func entryClosed(ctx *connContext, err error) {
	if ctx.entryCounted {
		ctx.entryCounted = false
		metricEntryConns.Dec(ctx.entry, ctx.via)
	}
	reason := ctx.closeReason
	if reason == "" {
		switch {
		case err == nil:
			reason = closeReasonServer
		case errors.Is(err, io.EOF):
			reason = closeReasonPeer
		default:
			reason = closeReasonIOError
		}
	}
	metricEntryClosed.Inc(ctx.entry, ctx.via, reason)
	metricEntryBytes.Observe(ctx.bytesIn, ctx.entry, ctx.via, "in")
	metricEntryBytes.Observe(ctx.bytesOut, ctx.entry, ctx.via, "out")
}
