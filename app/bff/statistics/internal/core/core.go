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

// Package core answers stats.* (task 103, ADR 0066): the statistics of
// channels, supergroups, posts, stories and polls, counted when they are
// asked for from the tables of the server (ADR 0066, p. 2), with no graph
// left for later (statsGraphAsync) and the zoom of a day into its hours
// signed into the graph's zoom_token.
package core

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/proto/mtproto/rpc/metadata"
	"github.com/teamgram/teamgram-server/app/bff/statistics/internal/svc"
	"github.com/teamgram/teamgram-server/pkg/channelstats"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/metric"
	"google.golang.org/grpc/status"
)

type StatisticsCore struct {
	ctx    context.Context
	svcCtx *svc.ServiceContext
	logx.Logger
	MD *metadata.RpcMetadata
	// now is the unix time of the request: one for all its graphs.
	now int64
	// sinces are the dates the kinds of events are recorded since, as
	// read by this request.
	sinces map[channelstats.Kind]int64
}

func New(ctx context.Context, svcCtx *svc.ServiceContext) *StatisticsCore {
	return &StatisticsCore{
		ctx:    ctx,
		svcCtx: svcCtx,
		Logger: logx.WithContext(ctx),
		MD:     metadata.RpcMetadataFromIncoming(ctx),
		now:    time.Now().Unix(),
	}
}

// requestTimeout bounds the building of one answer (ADR 0066, p. 2): the
// queries of a year of a channel take tens of milliseconds.
const requestTimeout = 10 * time.Second

var (
	metricRequests = metric.NewCounterVec(&metric.CounterVecOpts{
		Namespace: "stats",
		Name:      "request_total",
		Help:      "Requests of stats.* by method and result: ok, refused (an error the client is told: no rights, a bad argument, statistics off), error.",
		Labels:    []string{"method", "result"},
	})
	metricDuration = metric.NewHistogramVec(&metric.HistogramVecOpts{
		Namespace: "stats",
		Name:      "request_duration_seconds",
		Help:      "Time to answer a request of stats.*, in seconds.",
		Labels:    []string{"method"},
		Buckets:   []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	})
)

// observe counts a request of method that began at start and ended with
// *err; deferred by every handler.
func (c *StatisticsCore) observe(method string, start time.Time, err *error) {
	metricDuration.ObserveFloat(time.Since(start).Seconds(), method)
	result := "ok"
	if *err != nil {
		result = "error"
		switch status.Code(*err) {
		case mtproto.ErrBadRequest, mtproto.ErrForbidden:
			result = "refused"
		default:
			c.Logger.Errorf("%s - error: %v", method, *err)
		}
	}
	metricRequests.Inc(method, result)
}

// enabled answers ERR_ENTERPRISE_IS_BLOCKED while the statistics are off
// (no STATS_GRAPH_TOKEN_KEY): a 400, which the clients do not retry, and
// none of them asks, as can_view_stats is not set then.
func (c *StatisticsCore) enabled() error {
	if c.svcCtx.Signer == nil {
		return mtproto.ErrEnterpriseIsBlocked
	}
	return nil
}

// flightKey is the key that joins equal requests: the same answer for the
// same viewer, language and theme. The viewer is in it because the zoom
// tokens of the graphs are the viewer's own.
func (c *StatisticsCore) flightKey(method string, channelId int64, object int64, v view) string {
	return fmt.Sprintf("%s:%d:%d:%d:%s:%t", method, channelId, object, c.MD.GetUserId(), v.lang, v.dark)
}

// build runs f under key, joined with an equal request already running,
// within requestTimeout; the context of f outlives a caller that gives up,
// so the others still get the answer.
func build[T any](c *StatisticsCore, key string, f func(ctx context.Context) (T, error)) (T, error) {
	v, err, _ := c.svcCtx.Flight.Do(key, func() (any, error) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(c.ctx), requestTimeout)
		defer cancel()
		return f(ctx)
	})
	if err != nil {
		var zero T
		return zero, err
	}
	out, ok := v.(T)
	if !ok {
		var zero T
		return zero, errors.New("stats: joined request of another type")
	}
	return out, nil
}
