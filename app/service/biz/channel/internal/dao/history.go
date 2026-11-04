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

package dao

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/teamgram/proto/mtproto"
	"github.com/teamgram/teamgram-server/app/service/biz/channel/channel"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// HistoryMax is the most messages of one page of history, as the clients
// ask (100 at most, core.telegram.org/method/messages.getHistory).
const HistoryMax = 100

// MemberIds are the users in the channel now: the recipients of its
// updates.
func (d *Dao) MemberIds(ctx context.Context, channelId int64) ([]int64, error) {
	var ids []int64
	err := d.Mysql.QueryRowsPartial(ctx, &ids, "SELECT user_id FROM channel_participants WHERE channel_id = ? AND state = 0 ORDER BY user_id", channelId)
	return ids, err
}

// viewer is what bounds what a user sees of a channel: messages from
// available_min_id on, nothing of a channel it may not read.
type viewer struct {
	canRead        bool
	availableMinId int32
}

// viewerOf reads what userId may see of channelId: a member, or anyone not
// kicked from a public channel (ADR 0034, p. 4).
func (d *Dao) viewerOf(ctx context.Context, userId, channelId int64) (viewer, error) {
	mc, err := d.GetMutableChannel(ctx, channelId, []int64{userId})
	if err != nil {
		return viewer{}, err
	}
	if mc.GetChannel().GetDeleted() {
		return viewer{}, nil
	}
	p, _ := mc.GetParticipant(userId)
	if p.Member() {
		return viewer{canRead: true, availableMinId: p.GetAvailableMinId()}, nil
	}
	return viewer{canRead: mc.GetChannel().Public() && mc.ToChat(userId, 0).GetPredicateName() == mtproto.Predicate_channel}, nil
}

type messageRow struct {
	Id          int32  `db:"id"`
	MessageData string `db:"message_data"`
}

func decode(rows []messageRow, channelId, selfId int64) ([]*mtproto.Message, error) {
	out := make([]*mtproto.Message, 0, len(rows))
	for _, r := range rows {
		var m mtproto.Message
		if err := jsonx.UnmarshalFromString(r.MessageData, &m); err != nil {
			return nil, fmt.Errorf("channel: message %d of channel %d: %w", r.Id, channelId, err)
		}
		out = append(out, channel.ViewMessage(&m, selfId))
	}
	return out, nil
}

// History is a page of channelId's history as selfId sees it, newest first,
// with the count of the messages it may see: add_offset messages below
// offset_id (and before offset_date) are skipped, a negative one takes that
// many newer ones; min_id and max_id bound the ids. ErrChannelPrivate for a
// channel selfId may not read.
func (d *Dao) History(ctx context.Context, selfId, channelId int64, offsetId, offsetDate, addOffset, limit, maxId, minId int32) ([]*mtproto.Message, int32, error) {
	v, err := d.viewerOf(ctx, selfId, channelId)
	if err != nil {
		return nil, 0, err
	}
	if !v.canRead {
		return nil, 0, mtproto.ErrChannelPrivate
	}
	limit = min(max(limit, 1), HistoryMax)
	addOffset = max(addOffset, -limit)
	if offsetId <= 0 {
		offsetId = math.MaxInt32
	}
	if offsetDate <= 0 {
		offsetDate = math.MaxInt32
	}
	upper := int32(math.MaxInt32)
	if maxId > 0 {
		upper = maxId
	}
	lower := max(minId, v.availableMinId-1)
	where := "channel_id = ? AND deleted = 0 AND id > ? AND id < ?"
	args := []any{channelId, lower, upper}

	var rows []messageRow
	if addOffset < 0 {
		var newer []messageRow
		if err = d.Mysql.QueryRowsPartial(ctx, &newer,
			"SELECT id, message_data FROM channel_messages WHERE "+where+" AND id >= ? ORDER BY id LIMIT ?",
			append(slices.Clone(args), offsetId, -addOffset)...); err != nil {
			return nil, 0, err
		}
		slices.Reverse(newer)
		rows = newer
	}
	if rest := limit - int32(len(rows)); rest > 0 {
		var older []messageRow
		if err = d.Mysql.QueryRowsPartial(ctx, &older,
			"SELECT id, message_data FROM channel_messages WHERE "+where+" AND id < ? AND date2 < ? ORDER BY id DESC LIMIT ? OFFSET ?",
			append(slices.Clone(args), offsetId, offsetDate, rest, max(addOffset, 0))...); err != nil {
			return nil, 0, err
		}
		rows = append(rows, older...)
	}
	var count []int32
	if err = d.Mysql.QueryRowsPartial(ctx, &count,
		"SELECT COUNT(*) FROM channel_messages WHERE channel_id = ? AND deleted = 0 AND id >= ?", channelId, v.availableMinId); err != nil {
		return nil, 0, err
	}
	messages, err := decode(rows, channelId, selfId)
	return messages, first(count), err
}

// Messages are the messages ids of channelId that selfId may see, as it sees
// them, in the order asked; a missing one is left out.
func (d *Dao) Messages(ctx context.Context, selfId, channelId int64, ids []int32) ([]*mtproto.Message, error) {
	if len(ids) == 0 {
		return []*mtproto.Message{}, nil
	}
	v, err := d.viewerOf(ctx, selfId, channelId)
	if err != nil {
		return nil, err
	}
	if !v.canRead {
		return nil, mtproto.ErrChannelPrivate
	}
	args := []any{channelId, v.availableMinId}
	for _, id := range ids {
		args = append(args, id)
	}
	var rows []messageRow
	if err = d.Mysql.QueryRowsPartial(ctx, &rows,
		"SELECT id, message_data FROM channel_messages WHERE channel_id = ? AND deleted = 0 AND id >= ? AND id IN (?"+strings.Repeat(", ?", len(ids)-1)+")",
		args...); err != nil {
		return nil, err
	}
	slices.SortFunc(rows, func(a, b messageRow) int { return slices.Index(ids, a.Id) - slices.Index(ids, b.Id) })
	return decode(rows, channelId, selfId)
}
