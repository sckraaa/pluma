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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/pkg/statsgraph"
)

// StatsGetMessageStats
// stats.getMessageStats#b6e0a3f5 flags:# dark:flags.0?true channel:InputChannel msg_id:int = stats.MessageStats;
//
// The statistics of a post of a broadcast channel, for an admin who sees
// the channel's: its views and forwards, and its reactions by emotion,
// from its posting on. In hours while the post is 48 hours old or younger
// (iOS draws an hourly graph by the step of 3600 between the first two x,
// MessageStatistics.swift:104-116), in days after, zooming into the hours
// of a day. A post that is not there, deleted, or a service message is
// MESSAGE_ID_INVALID. The posts of supergroups have no views
// (channel.incrementMessageViews counts those of broadcast channels only):
// a supergroup is BROADCAST_REQUIRED.
func (c *StatisticsCore) StatsGetMessageStats(in *mtproto.TLStatsGetMessageStats) (_ *mtproto.Stats_MessageStats, err error) {
	defer c.observe("stats.getMessageStats", time.Now(), &err)
	if err = c.enabled(); err != nil {
		return nil, err
	}
	mc, err := c.statsChannel(in.GetChannel(), broadcastOnly)
	if err != nil {
		return nil, err
	}
	ch := mc.GetChannel()
	t, err := c.postTarget(c.ctx, ch.GetId(), int64(ch.GetParticipantsCount()), in.GetMsgId())
	if err != nil {
		return nil, err
	}
	v := view{lang: c.MD.GetLangCode(), dark: in.GetDark()}
	return build(c, c.flightKey("message", t.channelId, int64(t.object), v), func(ctx context.Context) (*mtproto.Stats_MessageStats, error) {
		return c.messageStats(ctx, t, v)
	})
}

// postTarget is the target of the post msgId of the channel, or
// MESSAGE_ID_INVALID.
func (c *StatisticsCore) postTarget(ctx context.Context, channelId, members int64, msgId int32) (target, error) {
	if msgId <= 0 {
		return target{}, mtproto.ErrMessageIdInvalid
	}
	date, ok, err := c.svcCtx.Dao.PostDate(ctx, channelId, msgId)
	if err != nil {
		return target{}, err
	}
	if !ok {
		return target{}, mtproto.ErrMessageIdInvalid
	}
	return target{channelId: channelId, object: msgId, broadcast: true, start: date, members: members, now: c.now}, nil
}

func (c *StatisticsCore) messageStats(ctx context.Context, t target, v view) (*mtproto.Stats_MessageStats, error) {
	out := &mtproto.Stats_MessageStats{}
	var err error
	if t.now-t.start <= hourlyAge {
		hours := statsgraph.Span(statsgraph.Hour, t.start, t.now)
		if out.ViewsGraph, err = c.hourly(ctx, "post_views", t, hours, v); err != nil {
			return nil, err
		}
		if out.ReactionsByEmotionGraph, err = c.hourly(ctx, "post_reactions", t, hours, v); err != nil {
			return nil, err
		}
	} else {
		if out.ViewsGraph, err = c.daily(ctx, "post_views", t, v); err != nil {
			return nil, err
		}
		if out.ReactionsByEmotionGraph, err = c.daily(ctx, "post_reactions", t, v); err != nil {
			return nil, err
		}
	}
	return mtproto.MakeTLStatsMessageStats(out).To_Stats_MessageStats(), nil
}
