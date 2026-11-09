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
	"github.com/teamgram/teamgram-server/pkg/channelbox"
	"github.com/teamgram/teamgram-server/pkg/polls"
	"github.com/teamgram/teamgram-server/pkg/reactions"

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
	member         bool
	broadcast      bool
	availableMinId int32
}

// viewerOf reads what userId may see of channelId: a member, anyone not
// kicked from a public channel (ADR 0034, p. 4), or a member of the
// channel whose discussion group it is (task 048).
func (d *Dao) viewerOf(ctx context.Context, userId, channelId int64) (viewer, error) {
	mc, err := d.GetMutableChannel(ctx, channelId, []int64{userId})
	if err != nil {
		return viewer{}, err
	}
	if mc.GetChannel().GetDeleted() {
		return viewer{}, nil
	}
	p, _ := mc.GetParticipant(userId)
	broadcast := mc.GetChannel().GetBroadcast()
	if p.Member() {
		return viewer{canRead: true, member: true, broadcast: broadcast, availableMinId: p.GetAvailableMinId()}, nil
	}
	// A public channel, or the discussion group of a channel the user is
	// in (task 048).
	readable := mc.GetChannel().Public() || p.GetState() == channel.StateLinked
	return viewer{canRead: readable && mc.ToChat(userId, 0).GetPredicateName() == mtproto.Predicate_channel, broadcast: broadcast}, nil
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
	return d.page(ctx, selfId, channelId, pageQuery{}, offsetId, offsetDate, addOffset, limit, maxId, minId)
}

// SearchQuery is what a search of a channel asks for, every part optional:
// the text, the author, a kind of message (mtproto.MEDIA_*, MEDIA_EMPTY for
// any) and the dates.
type SearchQuery struct {
	Q                string
	FromId           int64
	MediaType        int32
	MinDate, MaxDate int32
	// UnreadMentionsOf keeps the messages that mention this user and that
	// it has not read.
	UnreadMentionsOf int64
	// ThreadTop keeps the replies of the thread that starts from this
	// message (task 048).
	ThreadTop int32
	// Topic keeps the messages of this topic of a forum (task 060).
	Topic int32
}

// SearchMax is the most messages of one page of a search
// (core.telegram.org/method/messages.search: up to 100).
const SearchMax = 100

// Search is a page of the messages of channelId that match q, as selfId
// sees them, newest first, with the count of all that match; the paging is
// History's. Pinned messages are MEDIA_PINNED; photos and videos together
// MEDIA_PHOTOVIDEO, as the media lists of the clients ask. Service messages
// are left out but for the photo's changes (MEDIA_CHAT_PHOTO).
func (d *Dao) Search(ctx context.Context, selfId, channelId int64, q SearchQuery, offsetId, addOffset, limit, maxId, minId int32) ([]*mtproto.Message, int32, error) {
	var pq pageQuery
	add := func(cond string, args ...any) {
		pq.where += " AND " + cond
		pq.args = append(pq.args, args...)
	}
	if q.Q != "" {
		add("message LIKE ? ESCAPE '!'", "%"+likeEscaper.Replace(q.Q)+"%")
	}
	if q.FromId != 0 {
		add("sender_user_id = ?", q.FromId)
	}
	if q.ThreadTop != 0 {
		add("reply_top_id = ?", q.ThreadTop)
	}
	if q.Topic != 0 {
		add("topic_id = ?", channelbox.TopicColumn(q.Topic))
	}
	if q.UnreadMentionsOf != 0 {
		add("id IN (SELECT message_id FROM channel_message_mentions WHERE channel_id = ? AND user_id = ? AND `read` = 0)", channelId, q.UnreadMentionsOf)
	}
	// Service messages are found only as changes of the photo.
	if q.MediaType != mtproto.MEDIA_CHAT_PHOTO {
		add("JSON_UNQUOTE(JSON_EXTRACT(message_data, '$.predicate_name')) = ?", mtproto.Predicate_message)
	}
	switch q.MediaType {
	case mtproto.MEDIA_EMPTY:
	case mtproto.MEDIA_PINNED:
		add("pinned = 1")
	case mtproto.MEDIA_PHOTOVIDEO:
		add("message_filter_type IN (?, ?)", mtproto.MEDIA_PHOTOS_ONLY, mtproto.MEDIA_VIDEOS_ONLY)
	default:
		add("message_filter_type = ?", q.MediaType)
	}
	if q.MinDate > 0 {
		add("date2 >= ?", q.MinDate)
	}
	if q.MaxDate > 0 {
		add("date2 <= ?", q.MaxDate)
	}
	return d.page(ctx, selfId, channelId, pq, offsetId, 0, addOffset, min(limit, SearchMax), maxId, minId)
}

// likeEscaper escapes the wildcards of LIKE with '!'.
var likeEscaper = strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")

// pageQuery narrows a page of messages: conditions on channel_messages
// with their arguments.
type pageQuery struct {
	where string
	args  []any
}

func (d *Dao) page(ctx context.Context, selfId, channelId int64, pq pageQuery, offsetId, offsetDate, addOffset, limit, maxId, minId int32) ([]*mtproto.Message, int32, error) {
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
	where := "channel_id = ? AND deleted = 0 AND id > ? AND id < ?" + pq.where
	args := append([]any{channelId, lower, upper}, pq.args...)

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
		"SELECT COUNT(*) FROM channel_messages WHERE channel_id = ? AND deleted = 0 AND id >= ?"+pq.where,
		append([]any{channelId, v.availableMinId}, pq.args...)...); err != nil {
		return nil, 0, err
	}
	messages, err := decode(rows, channelId, selfId)
	if err != nil {
		return nil, 0, err
	}
	if err = d.withReactions(ctx, channelId, selfId, v.broadcast, messages); err != nil {
		return nil, 0, err
	}
	if err = polls.AttachMessages(ctx, d.Mysql, selfId, messages...); err != nil {
		return nil, 0, err
	}
	if err = d.WithPostCounters(ctx, channelId, messages); err != nil {
		return nil, 0, err
	}
	return messages, first(count), d.withReplies(ctx, selfId, channelId, v.broadcast, messages)
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
	messages, err := decode(rows, channelId, selfId)
	if err != nil {
		return nil, err
	}
	if err = d.withReactions(ctx, channelId, selfId, v.broadcast, messages); err != nil {
		return nil, err
	}
	if err = polls.AttachMessages(ctx, d.Mysql, selfId, messages...); err != nil {
		return nil, err
	}
	if err = d.WithPostCounters(ctx, channelId, messages); err != nil {
		return nil, err
	}
	return messages, d.withReplies(ctx, selfId, channelId, v.broadcast, messages)
}

// withReactions sets on messages the reactions to them as selfId sees them
// (task 044).
func (d *Dao) withReactions(ctx context.Context, channelId, selfId int64, broadcast bool, messages []*mtproto.Message) error {
	ids := make([]int32, 0, len(messages))
	for _, m := range messages {
		if m.GetPredicateName() == mtproto.Predicate_message {
			ids = append(ids, m.GetId())
		}
	}
	rows, err := reactions.LoadChannel(ctx, d.Mysql, channelId, ids)
	if err != nil {
		return err
	}
	kind := reactions.ChannelKind(broadcast)
	for _, m := range messages {
		if r, ok := rows[m.GetId()]; ok && m.GetPredicateName() == mtproto.Predicate_message {
			m.Reactions = reactions.View(r, selfId, m.GetFromId().GetUserId(), kind)
		}
	}
	return nil
}
