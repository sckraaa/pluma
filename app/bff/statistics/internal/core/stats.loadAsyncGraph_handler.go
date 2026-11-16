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
	"context"
	"strconv"
	"time"

	"github.com/teamgram/proto/mtproto"
)

// StatsLoadAsyncGraph
// stats.loadAsyncGraph#621d5fa0 flags:# token:string x:flags.0?long = StatsGraph;
//
// The zoom of a graph of days into the hours of the day of x (milliseconds,
// the x of its point): the zoomDays on each side, within the graph's own
// window (ADR 0066, p. 2). The token is the graph's zoom_token, signed by
// this server for the viewer: another viewer's or a forged one is
// GRAPH_INVALID_RELOAD, one of another version of the format
// GRAPH_OUTDATED_RELOAD, an old one GRAPH_EXPIRED_RELOAD (the errors of the
// method's page), on which the clients load the statistics again. The
// rights are checked again. No statsGraphAsync is given out (ADR 0066,
// p. 2), so every token is a zoom; without x the zoom is of today. The
// answer is always a graph: a zoom never has fewer than two points.
func (c *StatisticsCore) StatsLoadAsyncGraph(in *mtproto.TLStatsLoadAsyncGraph) (_ *mtproto.StatsGraph, err error) {
	defer c.observe("stats.loadAsyncGraph", time.Now(), &err)
	if err = c.enabled(); err != nil {
		return nil, err
	}
	tok, err := c.svcCtx.Signer.Verify(in.GetToken(), c.now)
	if err != nil {
		return nil, err
	}
	spec, ok := graphs[tok.Kind]
	if !ok || tok.Viewer != c.MD.GetUserId() || tok.PeerType != mtproto.PEER_CHANNEL {
		return nil, mtproto.ErrGraphInvalidReload
	}
	mc, err := c.statsChannelById(tok.PeerID, spec.kind)
	if err != nil {
		return nil, err
	}
	ch := mc.GetChannel()
	t := target{
		channelId: ch.GetId(),
		broadcast: ch.GetBroadcast(),
		start:     ch.GetDate(),
		members:   int64(ch.GetParticipantsCount()),
		now:       c.now,
	}
	if spec.object {
		if tok.Object <= 0 || tok.Object > int64(^uint32(0)>>1) {
			return nil, mtproto.ErrGraphInvalidReload
		}
		if t, err = c.postTarget(c.ctx, t.channelId, t.members, int32(tok.Object)); err != nil {
			return nil, err
		}
	}
	x := c.now * 1000
	if in.GetX() != nil {
		x = in.GetX().GetValue()
	}
	v := view{lang: tok.Lang, dark: tok.Dark}
	key := c.flightKey("zoom:"+tok.Kind+":"+strconv.FormatInt(x, 10), t.channelId, int64(t.object), v)
	return build(c, key, func(ctx context.Context) (*mtproto.StatsGraph, error) {
		from, err := c.graphStart(ctx, spec, t)
		if err != nil {
			return nil, err
		}
		return c.hourly(ctx, tok.Kind, t, zoomHours(from, t.now, x), v)
	})
}
