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

// Package readmetrics keeps what the clients report of how messages are read
// (messages.reportReadMetrics) and how music is listened to
// (messages.reportMusicListen), for the statistics of the chats (task 098,
// part D; read by task 103 through Store.Aggregate). The tables are
// message_read_metrics and music_listens
// (teamgramd/deploy/sql/migrate-20261119-read-metrics.sql).
package readmetrics

import (
	"github.com/teamgram/proto/mtproto"
)

const (
	// MaxMetrics is the most metrics of one messages.reportReadMetrics.
	// The clients send what was on the screen since the last batch: Desktop
	// every 5 s (api/api_read_metrics.cpp:16), Android when a chat closes
	// or its timer fires (ui/Components/chat/ChatActivityMessageMetricsView.java:102-116).
	MaxMetrics = 1000

	// MaxSeenRangePermille is the whole message seen. A ratio of the height
	// to the viewport has no such bound: a message taller than the screen
	// is over 1000, iOS sends heightToViewportRatio * 1000 as it is
	// (TelegramEngine/Messages/TelegramEngineMessages.swift:2010) and
	// Android the same (ChatActivityMessageMetricsView.java:311-316).
	MaxSeenRangePermille = 1000

	// ListenWindow is the seconds of one row of music_listens per user
	// and document: the listens of a calendar minute make one row.
	ListenWindow = 60

	// MaxAggregateMessages is the most messages of one Aggregate.
	MaxAggregateMessages = 100
)

// Metric is one inputMessageReadMetric: one showing (view_id) of a message
// on a client's screen.
type Metric struct {
	MsgId                         int32
	ViewId                        int64
	TimeInViewMs                  int32
	ActiveTimeInViewMs            int32
	HeightToViewportRatioPermille int32
	SeenRangeRatioPermille        int32
}

// FromInput reads the metrics of a request.
func FromInput(in []*mtproto.InputMessageReadMetric) []Metric {
	out := make([]Metric, 0, len(in))
	for _, m := range in {
		out = append(out, Metric{
			MsgId:                         m.GetMsgId(),
			ViewId:                        m.GetViewId(),
			TimeInViewMs:                  m.GetTimeInViewMs(),
			ActiveTimeInViewMs:            m.GetActiveTimeInViewMs(),
			HeightToViewportRatioPermille: m.GetHeightToViewportRatioPermille(),
			SeenRangeRatioPermille:        m.GetSeenRangeRatioPermille(),
		})
	}
	return out
}

// Validate checks a request's metrics as a whole: more than MaxMetrics is
// LIMIT_INVALID, a message id that is not positive MSG_ID_INVALID, a
// negative time or ratio or a seen range over the whole message
// INPUT_REQUEST_INVALID. The documentation of the method names no errors
// (core.telegram.org/method/messages.reportReadMetrics); the clients do not
// read the answer (Desktop api_read_metrics.cpp:62-66, Android
// ChatActivityMessageMetricsView.java:112), so a refused batch costs
// nothing but itself.
func Validate(ms []Metric) error {
	if len(ms) > MaxMetrics {
		return mtproto.ErrLimitInvalid
	}
	for _, m := range ms {
		if m.MsgId <= 0 {
			return mtproto.ErrMsgIdInvalid
		}
		if m.TimeInViewMs < 0 || m.ActiveTimeInViewMs < 0 || m.HeightToViewportRatioPermille < 0 ||
			m.SeenRangeRatioPermille < 0 || m.SeenRangeRatioPermille > MaxSeenRangePermille {
			return mtproto.ErrInputRequestInvalid
		}
	}
	return nil
}

// UpTo keeps the metrics of the messages up to top, the newest message of
// the chat: a metric of a message the chat does not have yet is not kept.
func UpTo(ms []Metric, top int32) []Metric {
	out := ms[:0:0]
	for _, m := range ms {
		if m.MsgId <= top {
			out = append(out, m)
		}
	}
	return out
}

// MaxMsgId is the newest message of the metrics, 0 for none.
func MaxMsgId(ms []Metric) int32 {
	var top int32
	for _, m := range ms {
		top = max(top, m.MsgId)
	}
	return top
}

// MessageStats is what the metrics of one message of a channel add up to.
type MessageStats struct {
	MsgId int32
	// Views is the showings (rows), Readers the users they are of.
	Views   int
	Readers int
	// Sums and medians over the showings.
	TimeInViewMsSum          int64
	ActiveTimeInViewMsSum    int64
	TimeInViewMsMedian       int32
	ActiveTimeInViewMsMedian int32
	SeenRangePermilleMedian  int32
}
