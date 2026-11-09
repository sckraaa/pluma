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
	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/internal/dao"
)

// applyTop scopes q to the topic of a forum, or the thread that top starts.
func (c *ChannelCore) applyTop(channelId int64, top int32, q *dao.SearchQuery) error {
	forum, err := c.svcCtx.Dao.IsForum(c.ctx, channelId)
	if err != nil {
		return err
	}
	if forum {
		q.Topic = top
		return nil
	}
	t, err := c.svcCtx.Dao.ThreadOf(c.ctx, channelId, top)
	if err != nil {
		return err
	}
	q.ThreadTop = t.TopId
	return nil
}

// ChannelSearchCount
// channel.searchCount self_id:long channel_id:long top_msg_id:int media_type:int = Int32;
func (c *ChannelCore) ChannelSearchCount(in *channel.TLChannelSearchCount) (*mtproto.Int32, error) {
	q := dao.SearchQuery{MediaType: in.MediaType}
	if in.TopMsgId != 0 {
		if err := c.applyTop(in.ChannelId, in.TopMsgId, &q); err != nil {
			c.Logger.Errorf("channel.searchCount - error: %v", err)
			return nil, err
		}
	}
	n, err := c.svcCtx.Dao.SearchCount(c.ctx, in.SelfId, in.ChannelId, q)
	if err != nil {
		return nil, c.membershipError("channel.searchCount", err)
	}
	return &mtproto.Int32{V: n}, nil
}

// ChannelSearchCalendar
// channel.searchCalendar self_id:long channel_id:long media_type:int offset_id:int offset_date:int = messages.SearchResultsCalendar;
func (c *ChannelCore) ChannelSearchCalendar(in *channel.TLChannelSearchCalendar) (*mtproto.Messages_SearchResultsCalendar, error) {
	days, total, err := c.svcCtx.Dao.SearchCalendar(c.ctx, in.SelfId, in.ChannelId, dao.SearchQuery{MediaType: in.MediaType}, in.OffsetId, in.OffsetDate)
	if err != nil {
		return nil, c.membershipError("channel.searchCalendar", err)
	}
	r := &mtproto.Messages_SearchResultsCalendar{
		Count:    total,
		Periods:  make([]*mtproto.SearchResultsCalendarPeriod, 0, len(days)),
		Messages: []*mtproto.Message{},
		Chats:    []*mtproto.Chat{},
		Users:    []*mtproto.User{},
	}
	ids := make([]int32, 0, len(days))
	for _, d := range days {
		ids = append(ids, d.MinId)
		r.Periods = append(r.Periods, mtproto.MakeTLSearchResultsCalendarPeriod(&mtproto.SearchResultsCalendarPeriod{
			Date: d.Date, MinMsgId: d.MinId, MaxMsgId: d.MaxId, Count: d.Count,
		}).To_SearchResultsCalendarPeriod())
	}
	if len(days) > 0 {
		last := days[len(days)-1]
		r.MinDate, r.MinMsgId = last.MinDate, last.MinId
		if r.Messages, err = c.svcCtx.Dao.Messages(c.ctx, in.SelfId, in.ChannelId, ids); err != nil {
			return nil, c.membershipError("channel.searchCalendar", err)
		}
	}
	return mtproto.MakeTLMessagesSearchResultsCalendar(r).To_Messages_SearchResultsCalendar(), nil
}

// ChannelSearchPositions
// channel.searchPositions self_id:long channel_id:long media_type:int offset_id:int limit:int = messages.SearchResultsPositions;
func (c *ChannelCore) ChannelSearchPositions(in *channel.TLChannelSearchPositions) (*mtproto.Messages_SearchResultsPositions, error) {
	positions, total, err := c.svcCtx.Dao.SearchPositions(c.ctx, in.SelfId, in.ChannelId, dao.SearchQuery{MediaType: in.MediaType}, in.OffsetId, in.Limit)
	if err != nil {
		return nil, c.membershipError("channel.searchPositions", err)
	}
	out := make([]*mtproto.SearchResultsPosition, 0, len(positions))
	for _, p := range positions {
		out = append(out, mtproto.MakeTLSearchResultPosition(&mtproto.SearchResultsPosition{
			MsgId: p.Id, Date: p.Date, Offset: p.Offset,
		}).To_SearchResultsPosition())
	}
	return mtproto.MakeTLMessagesSearchResultsPositions(&mtproto.Messages_SearchResultsPositions{
		Count: total, Positions: out,
	}).To_Messages_SearchResultsPositions(), nil
}

// ChannelSearchAcross
// channel.searchAcross self_id:long public_only:Bool broadcasts_only:Bool groups_only:Bool community_id:long folder_mode:int folder_channels:Vector<long> q:string media_type:int min_date:int max_date:int offset_rate:int offset_channel_id:long offset_id:int limit:int = messages.Messages;
func (c *ChannelCore) ChannelSearchAcross(in *channel.TLChannelSearchAcross) (*mtproto.Messages_Messages, error) {
	messages, count, more, err := c.svcCtx.Dao.SearchAcross(c.ctx, dao.AcrossQuery{
		SelfId:          in.SelfId,
		PublicOnly:      mtproto.FromBool(in.PublicOnly),
		BroadcastsOnly:  mtproto.FromBool(in.BroadcastsOnly),
		GroupsOnly:      mtproto.FromBool(in.GroupsOnly),
		CommunityId:     in.CommunityId,
		FolderMode:      in.FolderMode,
		FolderChannels:  in.FolderChannels,
		Q:               in.Q,
		MediaType:       in.MediaType,
		MinDate:         in.MinDate,
		MaxDate:         in.MaxDate,
		OffsetRate:      in.OffsetRate,
		OffsetChannelId: in.OffsetChannelId,
		OffsetId:        in.OffsetId,
		Limit:           in.Limit,
	})
	if err != nil {
		c.Logger.Errorf("channel.searchAcross - error: %v", err)
		return nil, err
	}
	r := &mtproto.Messages_Messages{
		Messages: messages,
		Topics:   []*mtproto.ForumTopic{},
		Chats:    []*mtproto.Chat{},
		Users:    []*mtproto.User{},
		Count:    count,
	}
	if more && len(messages) > 0 {
		r.NextRate = mtproto.MakeFlagsInt32(messages[len(messages)-1].GetDate())
	}
	return mtproto.MakeTLMessagesMessagesSlice(r).To_Messages_Messages(), nil
}
