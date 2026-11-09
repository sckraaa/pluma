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
	channelpb "github.com/teamgram/teamgram-server/app/service/biz/channel/channel"
	"github.com/teamgram/teamgram-server/pkg/msgsearch"
)

// calendarKind is the kind messages.getSearchResultsCalendar and
// getSearchResultsPositions are asked for: the empty filter and the
// mentions filter are not supported by them
// (core.telegram.org/method/messages.getSearchResultsCalendar).
func calendarKind(f *mtproto.MessagesFilter) (int32, error) {
	kind, ok := msgsearch.KindOf(f)
	if !ok || kind == msgsearch.KindEmpty || kind == msgsearch.KindMyMentions {
		return 0, mtproto.ErrFilterNotSupported
	}
	return kind, nil
}

// searchCalendar answers messages.getSearchResultsCalendar. messages[i] is
// the message min_msg_id of periods[i]: Android reads them by the same
// index (CalendarActivity.java res.messages.get(i)), iOS looks the message
// up by the period's min_msg_id (SparseMessageList.swift); a period whose
// message went meanwhile is left out.
func (c *MessagesCore) searchCalendar(in *mtproto.TLMessagesGetSearchResultsCalendar) (*mtproto.Messages_SearchResultsCalendar, error) {
	kind, err := calendarKind(in.Filter)
	if err != nil {
		return nil, err
	}
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.Peer)
	var r *mtproto.Messages_SearchResultsCalendar
	if peer.IsChannel() {
		if r, err = c.svcCtx.Dao.Channel.ChannelSearchCalendar(c.ctx, &channelpb.TLChannelSearchCalendar{
			SelfId: c.MD.UserId, ChannelId: peer.PeerId, MediaType: kind, OffsetId: in.OffsetId, OffsetDate: in.OffsetDate,
		}); err != nil {
			return nil, err
		}
	} else {
		if r, err = c.boxCalendar(in, peer, kind); err != nil {
			return nil, err
		}
	}
	byId := make(map[int32]*mtproto.Message, len(r.GetMessages()))
	for _, m := range r.GetMessages() {
		byId[m.GetId()] = m
	}
	periods := make([]*mtproto.SearchResultsCalendarPeriod, 0, len(r.GetPeriods()))
	messages := make([]*mtproto.Message, 0, len(r.GetPeriods()))
	for _, p := range r.GetPeriods() {
		if m := byId[p.GetMinMsgId()]; m != nil {
			periods = append(periods, p)
			messages = append(messages, m)
		}
	}
	r.Periods, r.Messages = periods, messages
	holder := &mtproto.Messages_Messages{Messages: messages}
	if peer.IsChannel() {
		err = c.fillChannelPeers(holder, peer.PeerId)
	} else {
		err = c.fillMessagesPeers(holder)
	}
	if err != nil {
		return nil, err
	}
	r.Users, r.Chats = holder.GetUsers(), holder.GetChats()
	return r, nil
}

func (c *MessagesCore) boxCalendar(in *mtproto.TLMessagesGetSearchResultsCalendar, peer *mtproto.PeerUtil, kind int32) (*mtproto.Messages_SearchResultsCalendar, error) {
	q := msgsearch.BoxQuery{UserId: c.MD.UserId, PeerType: peer.PeerType, PeerId: peer.PeerId, Kind: kind}
	c.applySaved(&q, in.SavedPeerId)
	days, total, err := msgsearch.Calendar(c.ctx, c.svcCtx.Search, q.Source(), in.OffsetId, in.OffsetDate)
	if err != nil {
		return nil, err
	}
	r := &mtproto.Messages_SearchResultsCalendar{
		Count:    total,
		Periods:  make([]*mtproto.SearchResultsCalendarPeriod, 0, len(days)),
		Messages: []*mtproto.Message{},
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
		page, err := c.boxPage(ids, int32(len(ids)))
		if err != nil {
			return nil, err
		}
		r.Messages = page.GetMessages()
	}
	return mtproto.MakeTLMessagesSearchResultsCalendar(r).To_Messages_SearchResultsCalendar(), nil
}

// searchPositions answers messages.getSearchResultsPositions.
func (c *MessagesCore) searchPositions(in *mtproto.TLMessagesGetSearchResultsPositions) (*mtproto.Messages_SearchResultsPositions, error) {
	kind, err := calendarKind(in.Filter)
	if err != nil {
		return nil, err
	}
	peer := mtproto.FromInputPeer2(c.MD.UserId, in.Peer)
	if peer.IsChannel() {
		return c.svcCtx.Dao.Channel.ChannelSearchPositions(c.ctx, &channelpb.TLChannelSearchPositions{
			SelfId: c.MD.UserId, ChannelId: peer.PeerId, MediaType: kind, OffsetId: in.OffsetId, Limit: in.Limit,
		})
	}
	q := msgsearch.BoxQuery{UserId: c.MD.UserId, PeerType: peer.PeerType, PeerId: peer.PeerId, Kind: kind}
	c.applySaved(&q, in.SavedPeerId)
	positions, total, err := msgsearch.Positions(c.ctx, c.svcCtx.Search, q.Source(), in.OffsetId, in.Limit)
	if err != nil {
		return nil, err
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
