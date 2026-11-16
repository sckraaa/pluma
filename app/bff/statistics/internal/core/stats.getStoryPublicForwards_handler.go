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
	"time"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/bff/statistics/internal/dao"
	"github.com/teamgram/teamgram-server/pkg/channelstats"
)

// StatsGetStoryPublicForwards
// stats.getStoryPublicForwards#a6437ef6 peer:InputPeer id:int offset:string limit:int = stats.PublicForwards;
//
// The public forwards of a story of a channel or a supergroup, for whoever
// sees the statistics of its peer: the messages that share it in public
// channels and supergroups (publicForwardMessage) and the stories of public
// channels that repost it (publicForwardStory), in one list by date, the
// latest first, paged as stats.getMessagePublicForwards. Android reads both
// kinds in the answer of either method (MessageStatisticActivity.java:
// 614-650). The errors are those of stats.getStoryStats.
func (c *StatisticsCore) StatsGetStoryPublicForwards(in *mtproto.TLStatsGetStoryPublicForwards) (_ *mtproto.Stats_PublicForwards, err error) {
	defer c.observe("stats.getStoryPublicForwards", time.Now(), &err)
	if err = c.enabled(); err != nil {
		return nil, err
	}
	mc, err := c.storyChannel(in.GetPeer())
	if err != nil {
		return nil, err
	}
	ch := mc.GetChannel()
	t, err := c.storyTarget(c.ctx, ch.GetId(), ch.GetBroadcast(), int64(ch.GetParticipantsCount()), in.GetId())
	if err != nil {
		return nil, err
	}
	src := dao.ForwardSource{Kind: channelstats.ForwardStory, ChannelId: t.channelId, Id: t.object, Reposts: true}
	return c.publicForwards(src, in.GetOffset(), in.GetLimit())
}
